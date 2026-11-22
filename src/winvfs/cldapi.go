//go:build windows

package winvfs

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The cloud filter entry points, which upstream gets by linking cldapi.lib.
// They are resolved lazily so that the binary still starts on a Windows without
// the cloud filter feature; the error then surfaces at the first call, where
// upstream would have failed to load at all.

var (
	modcldapi = windows.NewLazySystemDLL("cldapi.dll")

	procCfConnectSyncRoot                     = modcldapi.NewProc("CfConnectSyncRoot")
	procCfDisconnectSyncRoot                  = modcldapi.NewProc("CfDisconnectSyncRoot")
	procCfGetPlaceholderInfo                  = modcldapi.NewProc("CfGetPlaceholderInfo")
	procCfGetPlaceholderStateFromAttributeTag = modcldapi.NewProc("CfGetPlaceholderStateFromAttributeTag")
	procCfCreatePlaceholders                  = modcldapi.NewProc("CfCreatePlaceholders")
	procCfUpdatePlaceholder                   = modcldapi.NewProc("CfUpdatePlaceholder")
	procCfConvertToPlaceholder                = modcldapi.NewProc("CfConvertToPlaceholder")
	procCfSetPinState                         = modcldapi.NewProc("CfSetPinState")
	procCfSetInSyncState                      = modcldapi.NewProc("CfSetInSyncState")
	procCfExecute                             = modcldapi.NewProc("CfExecute")
	procCfReportProviderProgress              = modcldapi.NewProc("CfReportProviderProgress")
)

// hresult is the HRESULT every Cf* call but one returns. S_OK is 0.
type hresult int32

const S_OK hresult = 0

// The Win32 codes the wrapper compares HRESULTs against.
const (
	errorMoreData      = windows.Errno(234) // ERROR_MORE_DATA
	errorNotACloudFile = windows.Errno(395) // ERROR_NOT_A_CLOUD_FILE
)

// The same codes mapped through HRESULT_FROM_WIN32. These are vars because the
// mapped values have the high bit set and are not representable as int32
// constants.
var (
	hrErrorMoreData      = hresultFromWin32(errorMoreData)
	hrErrorNotACloudFile = hresultFromWin32(errorNotACloudFile)
)

func (h hresult) failed() bool { return h < 0 }

// hresultFromWin32 is the HRESULT_FROM_WIN32 macro.
func hresultFromWin32(err windows.Errno) hresult {
	if err == 0 {
		return S_OK
	}
	return hresult(0x80070000 | (uint32(err) & 0xFFFF))
}

func CfConnectSyncRoot(syncRootPath *uint16, callbackTable *CF_CALLBACK_REGISTRATION, callbackContext unsafe.Pointer, connectFlags uint32, connectionKey *CF_CONNECTION_KEY) hresult {
	r, _, _ := procCfConnectSyncRoot.Call(
		uintptr(unsafe.Pointer(syncRootPath)),
		uintptr(unsafe.Pointer(callbackTable)),
		uintptr(callbackContext),
		uintptr(connectFlags),
		uintptr(unsafe.Pointer(connectionKey)),
	)
	return hresult(r)
}

func CfDisconnectSyncRoot(connectionKey CF_CONNECTION_KEY) hresult {
	r, _, _ := procCfDisconnectSyncRoot.Call(uintptr(connectionKey.Internal))
	return hresult(r)
}

func CfGetPlaceholderInfo(fileHandle windows.Handle, infoClass uint32, infoBuffer unsafe.Pointer, infoBufferLength uint32, returnedLength *uint32) hresult {
	r, _, _ := procCfGetPlaceholderInfo.Call(
		uintptr(fileHandle),
		uintptr(infoClass),
		uintptr(infoBuffer),
		uintptr(infoBufferLength),
		uintptr(unsafe.Pointer(returnedLength)),
	)
	return hresult(r)
}

// CfGetPlaceholderStateFromAttributeTag returns a CF_PLACEHOLDER_STATE rather
// than an HRESULT.
func CfGetPlaceholderStateFromAttributeTag(fileAttributes, reparseTag uint32) uint32 {
	r, _, _ := procCfGetPlaceholderStateFromAttributeTag.Call(uintptr(fileAttributes), uintptr(reparseTag))
	return uint32(r)
}

func CfCreatePlaceholders(baseDirectoryPath *uint16, placeholderArray *CF_PLACEHOLDER_CREATE_INFO, placeholderCount uint32, createFlags uint32, entriesProcessed *uint32) hresult {
	r, _, _ := procCfCreatePlaceholders.Call(
		uintptr(unsafe.Pointer(baseDirectoryPath)),
		uintptr(unsafe.Pointer(placeholderArray)),
		uintptr(placeholderCount),
		uintptr(createFlags),
		uintptr(unsafe.Pointer(entriesProcessed)),
	)
	return hresult(r)
}

func CfUpdatePlaceholder(fileHandle windows.Handle, fsMetadata *CF_FS_METADATA, fileIdentity unsafe.Pointer, fileIdentityLength uint32, dehydrateRangeArray unsafe.Pointer, dehydrateRangeCount uint32, updateFlags uint32, updateUsn *int64, overlapped unsafe.Pointer) hresult {
	r, _, _ := procCfUpdatePlaceholder.Call(
		uintptr(fileHandle),
		uintptr(unsafe.Pointer(fsMetadata)),
		uintptr(fileIdentity),
		uintptr(fileIdentityLength),
		uintptr(dehydrateRangeArray),
		uintptr(dehydrateRangeCount),
		uintptr(updateFlags),
		uintptr(unsafe.Pointer(updateUsn)),
		uintptr(overlapped),
	)
	return hresult(r)
}

func CfConvertToPlaceholder(fileHandle windows.Handle, fileIdentity unsafe.Pointer, fileIdentityLength uint32, convertFlags uint32, convertUsn *int64, overlapped unsafe.Pointer) hresult {
	r, _, _ := procCfConvertToPlaceholder.Call(
		uintptr(fileHandle),
		uintptr(fileIdentity),
		uintptr(fileIdentityLength),
		uintptr(convertFlags),
		uintptr(unsafe.Pointer(convertUsn)),
		uintptr(overlapped),
	)
	return hresult(r)
}

func CfSetPinState(fileHandle windows.Handle, pinState uint32, pinFlags uint32, overlapped unsafe.Pointer) hresult {
	r, _, _ := procCfSetPinState.Call(
		uintptr(fileHandle),
		uintptr(pinState),
		uintptr(pinFlags),
		uintptr(overlapped),
	)
	return hresult(r)
}

func CfSetInSyncState(fileHandle windows.Handle, inSyncState uint32, inSyncFlags uint32, inSyncUsn *int64) hresult {
	r, _, _ := procCfSetInSyncState.Call(
		uintptr(fileHandle),
		uintptr(inSyncState),
		uintptr(inSyncFlags),
		uintptr(unsafe.Pointer(inSyncUsn)),
	)
	return hresult(r)
}

func CfExecute(opInfo *CF_OPERATION_INFO, opParams *CF_OPERATION_PARAMETERS) hresult {
	r, _, _ := procCfExecute.Call(
		uintptr(unsafe.Pointer(opInfo)),
		uintptr(unsafe.Pointer(opParams)),
	)
	return hresult(r)
}

func CfReportProviderProgress(connectionKey CF_CONNECTION_KEY, transferKey CF_TRANSFER_KEY, providerProgressTotal, providerProgressCompleted LARGE_INTEGER) hresult {
	r, _, _ := procCfReportProviderProgress.Call(
		uintptr(connectionKey.Internal),
		uintptr(transferKey),
		uintptr(providerProgressTotal),
		uintptr(providerProgressCompleted),
	)
	return hresult(r)
}
