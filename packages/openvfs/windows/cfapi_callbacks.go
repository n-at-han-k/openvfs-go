//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	cf "github.com/deploymenttheory/go-bindings-win32/bindings/win32/storage/cloudfilters"
)

// The CF_CALLBACK functions of cfapiwrapper.cpp.
//
// One thing differs from C++ by necessity: Windows calls these on its own
// threads and holds the callback context for the life of the connection, so it
// cannot be a Go pointer -- Go forbids storing one in memory owned by C -- and
// is an index into vfsRegistry instead.
//
// The CF_CALLBACK_INFO and CF_CALLBACK_PARAMETERS arguments point into memory
// Windows owns for the duration of the call, so nothing read out of them may
// outlive the callback; every byte slice taken from one is copied.

var (
	vfsRegistryMu   sync.RWMutex
	vfsRegistry            = map[uint64]*VfsCfApi{}
	vfsRegistryNext uint64 = 1
	vfsPinner       runtime.Pinner
)

// callbackToken is what CfConnectSyncRoot is handed as its context. It holds no
// Go pointers of its own, so pinning it is enough to make its address safe to
// give to the cloud filter for the life of the connection.
type callbackToken struct{ id uint64 }

// registerVfs hands out the opaque context passed to CfConnectSyncRoot.
//
// The context cannot be the *VfsCfApi itself: it is full of Go pointers, and
// the cloud filter keeps the context for the life of the connection. A pinned
// token carrying only an index is passed instead, which the callbacks resolve
// back through this map.
func registerVfs(v *VfsCfApi) unsafe.Pointer {
	vfsRegistryMu.Lock()
	defer vfsRegistryMu.Unlock()
	id := vfsRegistryNext
	vfsRegistryNext++
	vfsRegistry[id] = v
	token := &callbackToken{id: id}
	vfsPinner.Pin(token)
	v.registryID = id
	v.token = token
	return unsafe.Pointer(token)
}

func unregisterVfs(id uint64) {
	vfsRegistryMu.Lock()
	defer vfsRegistryMu.Unlock()
	delete(vfsRegistry, id)
}

// lookupVfs resolves CF_CALLBACK_INFO::CallbackContext back to the plugin, and
// stands in for the reinterpret_cast plus the className assert upstream makes.
func lookupVfs(context unsafe.Pointer) *VfsCfApi {
	if context == nil {
		return nil
	}
	token := (*callbackToken)(context)
	vfsRegistryMu.RLock()
	defer vfsRegistryMu.RUnlock()
	return vfsRegistry[token.id]
}

// cfApiFetchDataCallback is the callback of the same name.
func cfApiFetchDataCallback(callbackInfo *cf.CF_CALLBACK_INFO, parameters *cf.CF_CALLBACK_PARAMETERS) uintptr {
	context := logCallback("cfApiFetchDataCallback", callbackInfo,
		map[string]string{"reason": enumToDisplayNameDehydrationReason(fetchDataArm(parameters).LastDehydrationReason)},
		[]string{"reason"})

	if callbackInfo.ProcessInfo != nil && uint32(os.Getpid()) == callbackInfo.ProcessInfo.ProcessId {
		lcCfApiWrapper.Critical("implicit hydration triggered by the client itself. Will lead to a deadlock. Cancel %s %d",
			context.Path, context.TransferKey)
		return 0
	}

	if context.Vfs == nil {
		return 0
	}
	fileSize := callbackInfo.FileSize
	// switch to main thread
	context.Vfs.post(func() {
		if hydration := RequestHydration(context, fileSize); hydration != nil {
			hydration.Start()
		}
	})
	return 0
}

// cfApiCancelFetchData is the callback of the same name.
func cfApiCancelFetchData(callbackInfo *cf.CF_CALLBACK_INFO, _ *cf.CF_CALLBACK_PARAMETERS) uintptr {
	context := logCallback("cfApiCancelFetchData", callbackInfo, nil, nil)

	lcCfApiWrapper.Info("Cancel fetch data of %s", context)
	if context.Vfs != nil {
		context.Vfs.post(func() { context.Vfs.CancelHydration(context) })
	}
	return 0
}

// cfApiNotifyFileOpenCompletion is the callback of the same name.
func cfApiNotifyFileOpenCompletion(callbackInfo *cf.CF_CALLBACK_INFO, _ *cf.CF_CALLBACK_PARAMETERS) uintptr {
	logCallback("cfApiNotifyFileOpenCompletion", callbackInfo, nil, nil)
	return 0
}

// cfApiValidateData is the callback of the same name.
func cfApiValidateData(callbackInfo *cf.CF_CALLBACK_INFO, _ *cf.CF_CALLBACK_PARAMETERS) uintptr {
	logCallback("cfApiValidateData", callbackInfo, nil, nil)
	return 0
}

// cfApiCancelFetchPlaceHolders is the callback of the same name.
func cfApiCancelFetchPlaceHolders(callbackInfo *cf.CF_CALLBACK_INFO, _ *cf.CF_CALLBACK_PARAMETERS) uintptr {
	logCallback("cfApiCancelFetchPlaceHolders", callbackInfo, nil, nil)
	return 0
}

// cfApiRename is the callback of the same name.
func cfApiRename(callbackInfo *cf.CF_CALLBACK_INFO, callbackParameters *cf.CF_CALLBACK_PARAMETERS) uintptr {
	rename := renameArm(callbackParameters)

	target := utf16PtrToString(callbackInfo.VolumeDosName) + utf16PtrToString(rename.TargetPath)
	// isExcluded can't handle windows paths, as the target does not exist yet,
	// we can't use QFileInfo::canonicalFilePath
	qtPath := strings.ReplaceAll(target, "\\", "/")
	context := logCallback("cfApiRename", callbackInfo,
		map[string]string{"flags": flagsToString(rename.Flags), "target": target},
		[]string{"flags", "target"})

	reject := false
	// if both are managed by us, we prevent the move of a placeholder to a
	// folder where its ignored, as it would implicitly cause a deletion
	if rename.Flags&(cf.CF_CALLBACK_RENAME_FLAG_TARGET_IN_SCOPE|cf.CF_CALLBACK_RENAME_FLAG_SOURCE_IN_SCOPE) != 0 &&
		// CF_CALLBACK_RENAME_FLAG_TARGET_IN_SCOPE is also set for any other
		// sync root we manage but in that case windows will hydrate the file
		// before its moved
		context.Vfs != nil &&
		isChildPathOf(target, context.Vfs.params.FilesystemPath()) &&
		context.Vfs.IsDehydratedPlaceholder(context.Path) &&
		context.Vfs.params.SyncEngine != nil &&
		context.Vfs.params.SyncEngine.IsExcluded(qtPath) {
		reject = true
	}
	if reject {
		lcCfApiWrapper.Info("Rejecting rename of %s", context)
	} else {
		lcCfApiWrapper.Info("Accepting rename of %s", context)
	}

	opInfo := cf.CF_OPERATION_INFO{}
	opInfo.StructSize = uint32(unsafe.Sizeof(opInfo))
	opInfo.ConnectionKey = callbackInfo.ConnectionKey
	opInfo.TransferKey = callbackInfo.TransferKey
	opInfo.Type = cf.CF_OPERATION_TYPE_ACK_RENAME

	params := cf.CF_OPERATION_PARAMETERS{}
	params.ParamSize = sizeOfOpParamAckRename
	ack := ackRenameArm(&params)
	if reject {
		ack.CompletionStatus = STATUS_CLOUD_FILE_NOT_SUPPORTED
	} else {
		ack.CompletionStatus = STATUS_SUCCESS
	}

	if err := cf.CfExecute(&opInfo, &params); err != nil {
		lcCfApiWrapper.Warning("CfExecute failed: %v", err)
	}
	return 0
}

// cfApiRenameCompletion is the callback of the same name.
func cfApiRenameCompletion(callbackInfo *cf.CF_CALLBACK_INFO, _ *cf.CF_CALLBACK_PARAMETERS) uintptr {
	logCallback("cfApiRenameCompletion", callbackInfo, nil, nil)
	return 0
}

// cfApiNotifyFileCloseCompletion is the callback of the same name.
func cfApiNotifyFileCloseCompletion(callbackInfo *cf.CF_CALLBACK_INFO, _ *cf.CF_CALLBACK_PARAMETERS) uintptr {
	logCallback("cfApiNotifyFileCloseCompletion", callbackInfo, nil, nil)
	return 0
}

// The trampolines handed to CfConnectSyncRoot. NewCallback must be called once
// per function and not per connection, since each call consumes one of a
// process-wide limited number of slots.
var (
	cbFetchData                 = cf.CF_CALLBACK(syscall.NewCallback(cfApiFetchDataCallback))
	cbCancelFetchData           = cf.CF_CALLBACK(syscall.NewCallback(cfApiCancelFetchData))
	cbNotifyFileOpenCompletion  = cf.CF_CALLBACK(syscall.NewCallback(cfApiNotifyFileOpenCompletion))
	cbNotifyFileCloseCompletion = cf.CF_CALLBACK(syscall.NewCallback(cfApiNotifyFileCloseCompletion))
	cbValidateData              = cf.CF_CALLBACK(syscall.NewCallback(cfApiValidateData))
	cbCancelFetchPlaceHolders   = cf.CF_CALLBACK(syscall.NewCallback(cfApiCancelFetchPlaceHolders))
	cbRename                    = cf.CF_CALLBACK(syscall.NewCallback(cfApiRename))
	cbRenameCompletion          = cf.CF_CALLBACK(syscall.NewCallback(cfApiRenameCompletion))
)

// flagsToString stands in for QVariant::fromValue(QFlags(...)) in the log line.
func flagsToString(flags cf.CF_CALLBACK_RENAME_FLAGS) string {
	var parts []string
	if flags&cf.CF_CALLBACK_RENAME_FLAG_IS_DIRECTORY != 0 {
		parts = append(parts, "IS_DIRECTORY")
	}
	if flags&cf.CF_CALLBACK_RENAME_FLAG_SOURCE_IN_SCOPE != 0 {
		parts = append(parts, "SOURCE_IN_SCOPE")
	}
	if flags&cf.CF_CALLBACK_RENAME_FLAG_TARGET_IN_SCOPE != 0 {
		parts = append(parts, "TARGET_IN_SCOPE")
	}
	if len(parts) == 0 {
		return "NONE"
	}
	return strings.Join(parts, "|")
}

// isChildPathOf is FileSystem::isChildPathOf().
func isChildPathOf(path, parent string) bool {
	p := strings.ToLower(filepath.Clean(path))
	base := strings.ToLower(filepath.Clean(parent))
	if p == base {
		return true
	}
	return strings.HasPrefix(p, base+string(filepath.Separator))
}

// baseName and parentPath are std::filesystem::path::filename() and
// ::parent_path().
func baseName(path string) string   { return filepath.Base(path) }
func parentPath(path string) string { return filepath.Dir(path) }
