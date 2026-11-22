//go:build windows

package windows

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	cf "github.com/deploymenttheory/go-bindings-win32/bindings/win32/storage/cloudfilters"
	"golang.org/x/sys/windows"
)

// Transpiled from src/plugins/vfs/cfapi/vfs_cfapi.{h,cpp}.

// VfsCfApi is the class of the same name.
//
// Upstream is a QObject and relies on the Qt event loop to funnel the cloud
// filter callbacks onto the main thread, asserting that in cancelHydration()
// and the fetch-data handler. Here a single goroutine draining tasks plays that
// role, and post() is the QTimer::singleShot(0, ...) / queued invokeMethod.
type VfsCfApi struct {
	params    SetupParams
	callbacks Callbacks

	hydrationJobs map[int64]*CfApiHydrationJob
	connectionKey cf.CF_CONNECTION_KEY

	tasks      chan func()
	stopTasks  chan struct{}
	stopOnce   sync.Once
	registryID uint64
	token      *callbackToken
}

// compile-time check that the plugin satisfies the interface it is selected by.
var _ Vfs = (*VfsCfApi)(nil)

// NewVfsCfApi is the VfsCfApi constructor.
func NewVfsCfApi() *VfsCfApi {
	v := &VfsCfApi{
		hydrationJobs: map[int64]*CfApiHydrationJob{},
		tasks:         make(chan func(), 64),
		stopTasks:     make(chan struct{}),
	}
	go v.runTasks()
	return v
}

// runTasks is the main thread upstream asserts it is on.
func (v *VfsCfApi) runTasks() {
	for {
		select {
		case <-v.stopTasks:
			return
		case task := <-v.tasks:
			task()
		}
	}
}

// post queues work onto the task goroutine. Windows calls the cloud filter
// callbacks on its own threads, so everything touching hydrationJobs goes
// through here.
func (v *VfsCfApi) post(task func()) {
	select {
	case v.tasks <- task:
	case <-v.stopTasks:
	}
}

// callbackContext is the opaque context handed to CfConnectSyncRoot.
func (v *VfsCfApi) callbackContext() unsafe.Pointer { return registerVfs(v) }

// Params is Vfs::params().
func (v *VfsCfApi) Params() SetupParams { return v.params }

// Mode is VfsCfApi::mode().
func (v *VfsCfApi) Mode() Mode { return ModeWindowsCfApi }

// Start is Vfs::start(), which stores the parameters and calls startImpl().
func (v *VfsCfApi) Start(params SetupParams, callbacks Callbacks) {
	v.params = params
	v.callbacks = callbacks
	v.startImpl(params)
}

// startImpl is VfsCfApi::startImpl().
func (v *VfsCfApi) startImpl(params SetupParams) {
	RegisterSyncRoot(params, func(errorMessage string) {
		if errorMessage == "" {
			connectResult, err := ConnectSyncRoot(v.params.Root, v)
			if err != nil {
				lcCfApi.Critical("Initialization failed, couldn't connect sync root: %v", err)
				return
			}

			v.connectionKey = connectResult
			if v.callbacks.Started != nil {
				v.callbacks.Started()
			}
		} else {
			lcCfApi.Critical("%s", errorMessage)
			if v.callbacks.Error != nil {
				v.callbacks.Error(errorMessage)
			}
		}
	})
}

// Stop is VfsCfApi::stop().
func (v *VfsCfApi) Stop() {
	if v.connectionKey != 0 {
		if err := DisconnectSyncRoot(v.connectionKey); err != nil {
			lcCfApi.Critical("Disconnect failed for %s : %v", v.params.FilesystemPath(), err)
		}
		v.connectionKey = 0
	}
	v.stopOnce.Do(func() {
		close(v.stopTasks)
		unregisterVfs(v.registryID)
	})
}

// UnregisterFolder is VfsCfApi::unregisterFolder().
func (v *VfsCfApi) UnregisterFolder() {
	if err := UnregisterSyncRoot(v.params); err != nil {
		lcCfApi.Critical("Unregistration failed for %s : %v", v.params.FilesystemPath(), err)
	}
}

// SocketAPIPinStateActionsShown is
// VfsCfApi::socketApiPinStateActionsShown().
func (v *VfsCfApi) SocketAPIPinStateActionsShown() bool {
	return false
}

// updateMetadata is VfsCfApi::updateMetadata().
func (v *VfsCfApi) updateMetadata(syncItem SyncFileItem, filePath, replacesFile string) (ConvertToPlaceholderResult, error) {
	replacesPath := ""
	if replacesFile != "" {
		replacesPath = replacesFile
	}
	handle := CreateHandleDefault(filePath)
	defer handle.Close()
	if !handle.IsValid() {
		errorMessage := fmt.Errorf("Couldn't create handle for placeholder %s Error: %s", filePath, handle.ErrorMessage())
		lcCfApi.Warning("%v %s", errorMessage, replacesPath)
		return 0, errorMessage
	}

	if syncItem.Type == ItemTypeVirtualFileDehydration {
		result, err := DehydratePlaceholder(handle, syncItem.FileID)
		return result, err
	}
	if FindPlaceholderInfoBasic(handle, false).IsValid() {
		return UpdatePlaceholderInfo(handle, syncItem.Modtime, syncItem.Size, syncItem.FileID,
			replacesPath, syncItem.Type != ItemTypeVirtualFile)
	}
	return ConvertToPlaceholder(handle, syncItem.Modtime, syncItem.Size, syncItem.FileID, replacesPath)
}

// UpdateMetadata exposes the protected updateMetadata() hook.
func (v *VfsCfApi) UpdateMetadata(syncItem SyncFileItem, filePath, replacesFile string) (ConvertToPlaceholderResult, error) {
	return v.updateMetadata(syncItem, filePath, replacesFile)
}

// CreatePlaceholder is VfsCfApi::createPlaceholder().
func (v *VfsCfApi) CreatePlaceholder(item SyncFileItem) error {
	return CreatePlaceholderInfo(filepath.Join(v.params.Root, item.LocalName),
		item.Modtime, item.Size, item.FileID, item.Type == ItemTypeDirectory)
}

// NeedsMetadataUpdate is VfsCfApi::needsMetadataUpdate().
func (v *VfsCfApi) NeedsMetadataUpdate(item SyncFileItem) bool {
	handle := CreateHandleDefault(filepath.Join(v.params.Root, item.LocalName))
	defer handle.Close()
	if !handle.IsValid() {
		if handle.Error() == windows.ERROR_FILE_NOT_FOUND || handle.Error() == windows.ERROR_PATH_NOT_FOUND {
			return false
		}
		lcCfApi.Warning("Couldn't create handle for placeholder %s Error: %s", handle.Path(), handle.ErrorMessage())
		return false
	}
	return !FindPlaceholderInfoBasic(handle, false).IsValid()
}

// IsDehydratedPlaceholder is VfsCfApi::isDehydratedPlaceholder().
func (v *VfsCfApi) IsDehydratedPlaceholder(filePath string) bool {
	return IsDehydratedPlaceholder(filePath)
}

// StatTypeVirtualFile is VfsCfApi::statTypeVirtualFile().
func (v *VfsCfApi) StatTypeVirtualFile(fileInfo LocalInfo) LocalInfo {
	// only get placeholder info if it's a file
	if fileInfo.Type == ItemTypeFile {
		handle := CreateHandleDefault(fileInfo.Path)
		defer handle.Close()
		if !handle.IsValid() {
			lcCfApi.Warning("Couldn't create handle for placeholder %s", fileInfo.Path)
			return LocalInfo{}
		}
		if placeholderInfo := FindPlaceholderInfoBasic(handle, false); placeholderInfo.IsValid() {
			attributeInfo, err := getFileAttributeTagInfo(handle.Handle())
			if err != nil {
				lcCfApi.Critical("GetFileInformationByHandle failed on %s %v", fileInfo.Path, err)
				return LocalInfo{}
			}
			placeholderState := cf.CfGetPlaceholderStateFromAttributeTag(attributeInfo.FileAttributes, attributeInfo.ReparseTag)
			if placeholderState&cf.CF_PLACEHOLDER_STATE_PLACEHOLDER != 0 {
				if placeholderState&cf.CF_PLACEHOLDER_STATE_PARTIAL != 0 {
					if placeholderInfo.PinState() == PinAlwaysLocal {
						fileInfo.Type = ItemTypeVirtualFileDownload
					} else {
						fileInfo.Type = ItemTypeVirtualFile
					}
				} else {
					if placeholderInfo.PinState() == PinOnlineOnly {
						fileInfo.Type = ItemTypeVirtualFileDehydration
					}
					// else nothing to do
				}
			}
		}
	}
	return fileInfo
}

// SetPinState is VfsCfApi::setPinState().
func (v *VfsCfApi) SetPinState(folderPath string, state PinState) bool {
	lcCfApi.Debug("setPinState %s %d", folderPath, state)

	localPath := filepath.Join(v.params.Root, folderPath)
	handle := CreateHandleDefault(localPath)
	defer handle.Close()
	if !handle.IsValid() {
		lcCfApi.Warning("Couldn't create handle for placeholder %s", localPath)
		return false
	}
	_, err := SetPinState(handle, state, Recurse)
	return err == nil
}

// PinState is VfsCfApi::pinState().
func (v *VfsCfApi) PinState(folderPath string) (PinState, bool) {
	localPath := filepath.Join(v.params.Root, folderPath)
	handle := CreateHandleDefault(localPath)
	defer handle.Close()
	if !handle.IsValid() {
		lcCfApi.Warning("Couldn't create handle for placeholder %s", localPath)
		return 0, false
	}
	info := FindPlaceholderInfoBasic(handle, false)
	if !info.IsValid() {
		lcCfApi.Debug("Couldn't find pin state for regular non-placeholder file %s", localPath)
		return 0, false
	}

	return info.PinState(), true
}

// Availability is VfsCfApi::availability().
func (v *VfsCfApi) Availability(folderPath string) AvailabilityResult {
	basePinState, ok := v.PinState(folderPath)
	if ok {
		switch basePinState {
		case PinAlwaysLocal:
			return AvailabilityOf(AvailabilityAlwaysLocal)
		case PinInherited:
		case PinOnlineOnly:
			return AvailabilityOf(AvailabilityOnlineOnly)
		case PinUnspecified:
		case PinExcluded:
		}
		return AvailabilityOf(AvailabilityMixed)
	}
	return AvailabilityErrorOf(AvailabilityErrorNoSuchItem)
}

// CancelHydration is VfsCfApi::cancelHydration(). It must run on the task
// goroutine, which is where the cancel callback posts it.
func (v *VfsCfApi) CancelHydration(context CallBackContext) {
	// Find matching hydration job for request id
	if hydrationJob, ok := v.hydrationJobs[context.TransferKey]; ok {
		lcCfApi.Info("Cancel hydration %s", hydrationJob.Context())
		// the job itself will take care of hydrationJobs and its deletion
		hydrationJob.Abort()
	}
}

// FileStatusChanged is VfsCfApi::fileStatusChanged().
func (v *VfsCfApi) FileStatusChanged(systemFileName string, fileStatus SyncFileStatusTag) {
	handle := CreateHandleDefault(systemFileName)
	defer handle.Close()
	if !handle.IsValid() {
		if handle.Error() == windows.ERROR_FILE_NOT_FOUND || handle.Error() == windows.ERROR_PATH_NOT_FOUND {
			return
		}
		lcCfApi.Warning("Couldn't create handle for placeholder %s Error: %s", handle.Path(), handle.ErrorMessage())
		return
	}
	if fileStatus == StatusUpToDate {
		if info := FindPlaceholderInfoBasic(handle, false); info.IsValid() {
			_, _ = UpdatePlaceholderMarkInSync(info.Handle())
			if info.PinState() == PinExcluded {
				// clear possible exclude flag
				// a file usually does not change from excluded to not excluded, but ...
				_, _ = SetPinState(handle, PinInherited, Recurse)
			}
		}
	} else if fileStatus == StatusExcluded {
		_, _ = SetPinState(handle, PinExcluded, Recurse)
	}
}

// CfApiVfsPluginFactory is the class of the same name.
type CfApiVfsPluginFactory struct{}

// Create is DefaultPluginFactory<VfsCfApi>::create().
func (CfApiVfsPluginFactory) Create() Vfs { return NewVfsCfApi() }

// Prepare is CfApiVfsPluginFactory::prepare().
func (CfApiVfsPluginFactory) Prepare(path string, accountUUID string) error {
	if isRootPath(path) {
		return fmt.Errorf("The Virtual filesystem feature does not support a drive as sync root")
	}
	fileSystem, err := fileSystemForPath(path)
	if err != nil {
		return fmt.Errorf("The Virtual filesystem feature requires a NTFS file system, %s is using %s", path, fileSystem)
	}
	if !strings.HasPrefix(strings.ToUpper(fileSystem), "NTFS") {
		return fmt.Errorf("The Virtual filesystem feature requires a NTFS file system, %s is using %s", path, fileSystem)
	}
	driveType, err := getDriveType(path)
	if err == nil && driveType == windows.DRIVE_REMOTE {
		return fmt.Errorf("The Virtual filesystem feature is not supported on network drives")
	}
	return nil
}

// isRootPath is QDir(path).isRoot().
func isRootPath(path string) bool {
	clean := filepath.Clean(path)
	return clean == filepath.VolumeName(clean)+string(filepath.Separator)
}

// fileSystemForPath is FileSystem::fileSystemForPath().
func fileSystemForPath(path string) (string, error) {
	root := filepath.VolumeName(filepath.Clean(path)) + string(filepath.Separator)
	wroot, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", err
	}
	var name [windows.MAX_PATH + 1]uint16
	var serial, maxComp, flags uint32
	var fsName [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformation(wroot, &name[0], uint32(len(name)), &serial, &maxComp, &flags,
		&fsName[0], uint32(len(fsName))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(fsName[:]), nil
}

// getDriveType is GetDriveTypeW() on the first three characters of the path,
// which is what upstream passes.
func getDriveType(path string) (uint32, error) {
	root := filepath.VolumeName(filepath.Clean(path)) + string(filepath.Separator)
	wroot, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	return windows.GetDriveType(wroot), nil
}

// getInode is FileSystem::getInode(): the NTFS file index stands in for it.
func getInode(path string) (uint64, bool) {
	handle := CreateHandleDefault(path)
	defer handle.Close()
	if !handle.IsValid() {
		return 0, false
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle.Handle(), &info); err != nil {
		return 0, false
	}
	return uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), true
}
