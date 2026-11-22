//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"unsafe"

	cf "github.com/deploymenttheory/go-bindings-win32/bindings/win32/storage/cloudfilters"
	"golang.org/x/sys/windows"
)

// CfApiWrapper::registerSyncRoot() and ::unregisterSyncRoot().
//
// DELIBERATE DEVIATION. Upstream registers through WinRT:
// StorageFolder::GetFolderFromPathAsync, then StorageProviderSyncRootInfo with
// a dozen properties, then StorageProviderSyncRootManager::Register. Reaching
// WinRT needs the activation factory and the IStorageProviderSyncRootInfo
// vtable, whose interface GUIDs come out of the Windows SDK metadata; they are
// not derivable here, and inventing them would produce code that compiles and
// then fails at runtime for no visible reason. This uses the Win32 equivalent
// in cldapi.dll instead.
//
// What CfRegisterSyncRoot keeps: the sync root itself, the provider name and
// version, the sync root identity, and the hydration, population, in-sync and
// hardlink policies -- everything the placeholder and hydration behaviour
// depends on.
//
// What it drops, because those live only on StorageProviderSyncRootInfo: the
// Explorer navigation pane entry, DisplayNameResource, IconResource,
// RecycleBinUri, AllowPinning and ShowSiblingsAsGroup. Restoring them needs the
// WinRT path.

// RegisterSyncRoot is CfApiWrapper::registerSyncRoot(). Upstream is
// asynchronous because GetFolderFromPathAsync is; the callback contract is kept
// so that the caller in startImpl() stays a literal translation, and it runs on
// a goroutine for the same reason.
func RegisterSyncRoot(params SetupParams, callback func(errorMessage string)) {
	go func() {
		callback(registerSyncRootSync(params))
	}()
}

func registerSyncRootSync(params SetupParams) string {
	nativePath := toNativeSeparators(params.FilesystemPath())

	accountUUID := ""
	if params.Account != nil {
		accountUUID = params.Account.UUID()
	}
	id := createSyncRootID(params.ProviderName, accountUUID, params.FilesystemPath())
	if id == "" {
		return fmt.Sprintf("Failed to register sync root %s: could not build the sync root id", params.FilesystemPath())
	}

	wProviderName, err := windows.UTF16PtrFromString(params.ProviderName)
	if err != nil {
		return fmt.Sprintf("Failed to register sync root %s: %v", params.FilesystemPath(), err)
	}
	wProviderVersion, err := windows.UTF16PtrFromString(params.ProviderVersion)
	if err != nil {
		return fmt.Sprintf("Failed to register sync root %s: %v", params.FilesystemPath(), err)
	}
	// The sync root identity is the same id WinRT would have been given.
	identity := windows.StringToUTF16(id)
	identityBytes := unsafe.Slice((*byte)(unsafe.Pointer(&identity[0])), len(identity)*2)

	registration := cf.CF_SYNC_REGISTRATION{}
	registration.StructSize = uint32(unsafe.Sizeof(registration))
	registration.ProviderName = wProviderName
	registration.ProviderVersion = wProviderVersion
	registration.SyncRootIdentity = unsafe.Pointer(&identityBytes[0])
	registration.SyncRootIdentityLength = uint32(len(identityBytes))

	policies := cf.CF_SYNC_POLICIES{}
	policies.StructSize = uint32(unsafe.Sizeof(policies))
	// winrt::StorageProviderHydrationPolicy::Full plus
	// HydrationPolicyModifier::AutoDehydrationAllowed
	policies.Hydration = cf.CF_HYDRATION_POLICY{
		Primary:  cf.CF_HYDRATION_POLICY_FULL,
		Modifier: cf.CF_HYDRATION_POLICY_MODIFIER_AUTO_DEHYDRATION_ALLOWED,
	}
	// winrt::StorageProviderPopulationPolicy::AlwaysFull
	policies.Population = cf.CF_POPULATION_POLICY{Primary: cf.CF_POPULATION_POLICY_ALWAYS_FULL}
	// winrt::StorageProviderInSyncPolicy::PreserveInsyncForSyncEngine
	policies.InSync = cf.CF_INSYNC_POLICY_PRESERVE_INSYNC_FOR_SYNC_ENGINE
	// winrt::StorageProviderHardlinkPolicy::None
	policies.HardLink = cf.CF_HARDLINK_POLICY_NONE

	// don't confuse Windows with parallel registrations
	sRegisterMutex.Lock()
	err = cf.CfRegisterSyncRoot(nativePath, &registration, &policies, cf.CF_REGISTER_FLAG_UPDATE)
	sRegisterMutex.Unlock()

	// keep the identity and the two provider strings alive until the call has
	// returned, since the struct holds raw pointers into them
	runtime.KeepAlive(identity)
	runtime.KeepAlive(identityBytes)
	runtime.KeepAlive(wProviderName)
	runtime.KeepAlive(wProviderVersion)

	if err != nil {
		return fmt.Sprintf("Failed to register sync root %s: %v", params.FilesystemPath(), err)
	}
	return ""
}

// UnregisterSyncRoot is CfApiWrapper::unregisterSyncRoot().
func UnregisterSyncRoot(params SetupParams) error {
	sRegisterMutex.Lock()
	err := cf.CfUnregisterSyncRoot(toNativeSeparators(params.FilesystemPath()))
	sRegisterMutex.Unlock()
	if err != nil {
		return fmt.Errorf("unregisterSyncRoot failed: %v", err)
	}
	return nil
}

// toNativeSeparators is QDir::toNativeSeparators().
func toNativeSeparators(path string) string {
	out := make([]byte, 0, len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			out = append(out, '\\')
			continue
		}
		out = append(out, path[i])
	}
	return string(out)
}
