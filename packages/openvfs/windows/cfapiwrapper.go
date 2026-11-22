//go:build windows

package windows

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-win32/bindings/win32/foundation"
	cf "github.com/deploymenttheory/go-bindings-win32/bindings/win32/storage/cloudfilters"
	"golang.org/x/sys/windows"
)

// Transpiled from src/plugins/vfs/cfapi/cfapiwrapper.{h,cpp}.

// sRegisterMutex is the std::mutex of the same name, held across every
// registration and connection so that Windows is not confused by parallel
// registrations.
var sRegisterMutex sync.Mutex

// CallBackContext is the struct of the same name.
type CallBackContext struct {
	Vfs           *VfsCfApi
	Path          string
	TransferKey   int64
	ConnectionKey cf.CF_CONNECTION_KEY
	FileID        []byte
	ExtraArgs     map[string]string
	// extraOrder keeps ExtraArgs printable in insertion order, which a QMap
	// gives for free by sorting on the key.
	extraOrder []string
}

// RequestHexID is CallBackContext::requestHexId().
func (c CallBackContext) RequestHexID() string {
	return strconv.FormatInt(c.TransferKey, 16)
}

// String is `QDebug operator<<(QDebug, const CallBackContext &)`.
func (c CallBackContext) String() string {
	var b strings.Builder
	b.WriteString("cfapiCallback(")
	b.WriteString(c.Path)
	b.WriteString(", ")
	b.WriteString(c.RequestHexID())
	for _, k := range c.extraOrder {
		b.WriteString(", ")
		b.WriteString(k)
		b.WriteString("=\"")
		b.WriteString(c.ExtraArgs[k])
		b.WriteString("\"")
	}
	b.WriteString(")")
	return b.String()
}

func (c *CallBackContext) setExtra(key, value string) {
	if c.ExtraArgs == nil {
		c.ExtraArgs = map[string]string{}
	}
	if _, seen := c.ExtraArgs[key]; !seen {
		c.extraOrder = append(c.extraOrder, key)
	}
	c.ExtraArgs[key] = value
}

// enumToDisplayNameDehydrationReason is
// Utility::enumToDisplayName(CF_CALLBACK_DEHYDRATION_REASON).
func enumToDisplayNameDehydrationReason(reason cf.CF_CALLBACK_DEHYDRATION_REASON) string {
	switch reason {
	case cf.CF_CALLBACK_DEHYDRATION_REASON_NONE:
		return "CF_CALLBACK_DEHYDRATION_REASON_NONE"
	case cf.CF_CALLBACK_DEHYDRATION_REASON_USER_MANUAL:
		return "CF_CALLBACK_DEHYDRATION_REASON_USER_MANUAL"
	case cf.CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_LOW_SPACE:
		return "CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_LOW_SPACE"
	case cf.CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_INACTIVITY:
		return "CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_INACTIVITY"
	case cf.CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_OS_UPGRADE:
		return "CF_CALLBACK_DEHYDRATION_REASON_SYSTEM_OS_UPGRADE"
	}
	return ""
}

// SetPinRecurseMode is the enum of the same name.
type SetPinRecurseMode int

const (
	NoRecurse SetPinRecurseMode = iota
	Recurse
	ChildrenOnly
)

func (m SetPinRecurseMode) String() string {
	switch m {
	case NoRecurse:
		return "NoRecurse"
	case Recurse:
		return "Recurse"
	case ChildrenOnly:
		return "ChildrenOnly"
	}
	return ""
}

// PlaceHolderInfo is the PlaceHolderInfo<T> template.
//
// The template parameter exists in C++ only so that get() can reinterpret_cast
// the buffer; Go cannot cast to an unconstrained type parameter, so the one
// type carries the info class it was filled for and projects the buffer through
// Basic()/Standard(). PinState() and FileID() dispatch on that class, because
// PinState sits at a different offset in each struct.
type PlaceHolderInfo struct {
	handle    *Handle
	data      []byte
	infoClass cf.CF_PLACEHOLDER_INFO_CLASS
}

// IsValid is PlaceHolderInfo::isValid() / operator bool().
func (p PlaceHolderInfo) IsValid() bool { return len(p.data) != 0 }

// Size is PlaceHolderInfo::size().
func (p PlaceHolderInfo) Size() int { return len(p.data) }

// Handle is PlaceHolderInfo::handle().
func (p PlaceHolderInfo) Handle() *Handle { return p.handle }

// Basic projects the buffer as CF_PLACEHOLDER_BASIC_INFO.
func (p PlaceHolderInfo) Basic() *cf.CF_PLACEHOLDER_BASIC_INFO {
	if !p.IsValid() {
		return nil
	}
	return (*cf.CF_PLACEHOLDER_BASIC_INFO)(unsafe.Pointer(&p.data[0]))
}

// Standard projects the buffer as CF_PLACEHOLDER_STANDARD_INFO.
func (p PlaceHolderInfo) Standard() *cf.CF_PLACEHOLDER_STANDARD_INFO {
	if !p.IsValid() {
		return nil
	}
	return (*cf.CF_PLACEHOLDER_STANDARD_INFO)(unsafe.Pointer(&p.data[0]))
}

func (p PlaceHolderInfo) rawPinState() cf.CF_PIN_STATE {
	if p.infoClass == cf.CF_PLACEHOLDER_INFO_STANDARD {
		return p.Standard().PinState
	}
	return p.Basic().PinState
}

func (p PlaceHolderInfo) rawFileIdentityLength() uint32 {
	if p.infoClass == cf.CF_PLACEHOLDER_INFO_STANDARD {
		return p.Standard().FileIdentityLength
	}
	return p.Basic().FileIdentityLength
}

// InSyncState reads get()->InSyncState.
func (p PlaceHolderInfo) InSyncState() cf.CF_IN_SYNC_STATE {
	if p.infoClass == cf.CF_PLACEHOLDER_INFO_STANDARD {
		return p.Standard().InSyncState
	}
	return p.Basic().InSyncState
}

// FileID is PlaceHolderInfo::fileId(). FileIdentity is a one element array at
// the end of the struct that the API overruns with the real identity, so it is
// read back out of the buffer the call was given.
func (p PlaceHolderInfo) FileID() []byte {
	length := p.rawFileIdentityLength()
	if length == 0 {
		return nil
	}
	var offset int
	if p.infoClass == cf.CF_PLACEHOLDER_INFO_STANDARD {
		offset = int(unsafe.Offsetof(cf.CF_PLACEHOLDER_STANDARD_INFO{}.FileIdentity))
	} else {
		offset = int(unsafe.Offsetof(cf.CF_PLACEHOLDER_BASIC_INFO{}.FileIdentity))
	}
	if offset+int(length) > len(p.data) {
		lcCfApiWrapper.Warning("placeholder buffer too small for the file identity")
		return nil
	}
	out := make([]byte, length)
	copy(out, p.data[offset:offset+int(length)])
	return out
}

// PinState is PlaceHolderInfo::pinState().
func (p PlaceHolderInfo) PinState() PinState {
	switch p.rawPinState() {
	case cf.CF_PIN_STATE_UNSPECIFIED:
		return PinUnspecified
	case cf.CF_PIN_STATE_PINNED:
		return PinAlwaysLocal
	case cf.CF_PIN_STATE_UNPINNED:
		return PinOnlineOnly
	case cf.CF_PIN_STATE_INHERIT:
		return PinInherited
	case cf.CF_PIN_STATE_EXCLUDED:
		return PinExcluded
	}
	// Q_UNREACHABLE()
	return PinUnspecified
}

// getPlaceholderInfo retrieves the placeholder info; by default we don't
// request the full FileIdentity.
func getPlaceholderInfo(handle *Handle, infoClass cf.CF_PLACEHOLDER_INFO_CLASS, withFileIdentity bool) ([]byte, bool) {
	size := 0
	switch {
	case withFileIdentity:
		size = 512
	case infoClass == cf.CF_PLACEHOLDER_INFO_BASIC:
		size = int(unsafe.Sizeof(cf.CF_PLACEHOLDER_BASIC_INFO{}))
	default:
		size = int(unsafe.Sizeof(cf.CF_PLACEHOLDER_STANDARD_INFO{}))
	}
	buffer := make([]byte, size)
	var actualSize uint32
	err := cf.CfGetPlaceholderInfo(foundationHandle(handle), infoClass, buffer, &actualSize)

	if err == nil || (!withFileIdentity && isErrorMoreData(err)) {
		if withFileIdentity {
			if int(actualSize) < len(buffer) {
				buffer = buffer[:actualSize]
			}
		}
		return buffer, true
	} else if isErrorNotACloudFile(err) {
		// native file, not yet converted
		return nil, true
	}
	lcCfApiWrapper.Warning("Failed to retrieve placeholder info: %v", err)
	return nil, false
}

// FindPlaceholderInfoBasic is findPlaceholderInfo<CF_PLACEHOLDER_BASIC_INFO>().
func FindPlaceholderInfoBasic(handle *Handle, withFileIdentity bool) PlaceHolderInfo {
	info, ok := getPlaceholderInfo(handle, cf.CF_PLACEHOLDER_INFO_BASIC, withFileIdentity)
	if !ok || len(info) == 0 {
		return PlaceHolderInfo{handle: handle, infoClass: cf.CF_PLACEHOLDER_INFO_BASIC}
	}
	return PlaceHolderInfo{handle: handle, data: info, infoClass: cf.CF_PLACEHOLDER_INFO_BASIC}
}

// FindPlaceholderInfoStandard is
// findPlaceholderInfo<CF_PLACEHOLDER_STANDARD_INFO>().
func FindPlaceholderInfoStandard(handle *Handle, withFileIdentity bool) PlaceHolderInfo {
	info, ok := getPlaceholderInfo(handle, cf.CF_PLACEHOLDER_INFO_STANDARD, withFileIdentity)
	if !ok || len(info) == 0 {
		return PlaceHolderInfo{handle: handle, infoClass: cf.CF_PLACEHOLDER_INFO_STANDARD}
	}
	return PlaceHolderInfo{handle: handle, data: info, infoClass: cf.CF_PLACEHOLDER_INFO_STANDARD}
}

// utf16PtrToString is QString::fromWCharArray().
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	return windows.UTF16PtrToString(p)
}

// logCallback is the function of the same name: it builds the context from the
// callback info and logs who asked for the operation.
func logCallback(function string, callbackInfo *cf.CF_CALLBACK_INFO, extra map[string]string, extraOrder []string) CallBackContext {
	context := CallBackContext{
		Vfs:           lookupVfs(callbackInfo.CallbackContext),
		Path:          utf16PtrToString(callbackInfo.VolumeDosName) + utf16PtrToString(callbackInfo.NormalizedPath),
		TransferKey:   callbackInfo.TransferKey,
		ConnectionKey: callbackInfo.ConnectionKey,
		FileID:        identityBytes(callbackInfo.FileIdentity, callbackInfo.FileIdentityLength),
	}
	for _, k := range extraOrder {
		context.setExtra(k, extra[k])
	}

	app := "NULL"
	appID := ""
	var pid uint32

	if callbackInfo.ProcessInfo != nil {
		app = utf16PtrToString(callbackInfo.ProcessInfo.ImagePath)
		appID = utf16PtrToString(callbackInfo.ProcessInfo.ApplicationId)
		pid = callbackInfo.ProcessInfo.ProcessId
	}
	lcCfApiWrapper.Info("%s %s requested by application: %s PID: %d AppID: %s",
		function, context, app, pid, appID)
	return context
}

// identityBytes copies a FileIdentity out of the callback's memory, which Go
// must not retain a pointer into.
func identityBytes(p unsafe.Pointer, length uint32) []byte {
	if p == nil || length == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), length)...)
}

// pinStateToCfPinState is the free function of the same name.
func pinStateToCfPinState(state PinState) cf.CF_PIN_STATE {
	switch state {
	case PinInherited:
		return cf.CF_PIN_STATE_INHERIT
	case PinAlwaysLocal:
		return cf.CF_PIN_STATE_PINNED
	case PinOnlineOnly:
		return cf.CF_PIN_STATE_UNPINNED
	case PinUnspecified:
		return cf.CF_PIN_STATE_UNSPECIFIED
	case PinExcluded:
		return cf.CF_PIN_STATE_EXCLUDED
	}
	// Q_UNREACHABLE()
	return cf.CF_PIN_STATE_UNSPECIFIED
}

// pinRecurseModeToCfSetPinFlags is the free function of the same name.
func pinRecurseModeToCfSetPinFlags(mode SetPinRecurseMode) cf.CF_SET_PIN_FLAGS {
	switch mode {
	case NoRecurse:
		return cf.CF_SET_PIN_FLAG_NONE
	case Recurse:
		return cf.CF_SET_PIN_FLAG_RECURSE
	case ChildrenOnly:
		return cf.CF_SET_PIN_FLAG_RECURSE_ONLY
	}
	// Q_UNREACHABLE()
	return cf.CF_SET_PIN_FLAG_NONE
}

// updatePlaceholderState is the function of the same name in the anonymous
// namespace.
func updatePlaceholderState(handle *Handle, modtime int64, size int64, fileID []byte, replacesPath string, isHydrated bool) (ConvertToPlaceholderResult, error) {
	var info PlaceHolderInfo
	if replacesPath != "" {
		replacesHandle := CreateHandleDefault(replacesPath)
		defer replacesHandle.Close()
		if !replacesHandle.IsValid() {
			errorMessage := fmt.Errorf("Couldn't create handle for placeholder %s Error: %s", replacesPath, replacesHandle.ErrorMessage())
			lcCfApiWrapper.Warning("%v", errorMessage)
			return 0, errorMessage
		}
		info = FindPlaceholderInfoBasic(replacesHandle, false)
	}
	if !info.IsValid() {
		info = FindPlaceholderInfoBasic(handle, false)
	}
	if !info.IsValid() {
		return 0, fmt.Errorf("Can't update non existing placeholder info")
	}

	previousPinState := info.PinState()

	var metadata *cf.CF_FS_METADATA
	if !isHydrated {
		metadata = &cf.CF_FS_METADATA{}
		metadata.FileSize = size
		metadata.BasicInfo.CreationTime = UnixTimeToLargeIntegerFiletime(modtime)
		metadata.BasicInfo.LastWriteTime = metadata.BasicInfo.CreationTime
		metadata.BasicInfo.LastAccessTime = metadata.BasicInfo.CreationTime
		metadata.BasicInfo.ChangeTime = metadata.BasicInfo.CreationTime
	}
	lcCfApiWrapper.Info("updatePlaceholderState %s %d %x", handle.Path(), modtime, fileID)

	err := cf.CfUpdatePlaceholder(foundationHandle(handle), metadata, fileID, nil,
		cf.CF_UPDATE_FLAG_MARK_IN_SYNC, nil, nil)

	if err != nil {
		errorMessage := fmt.Errorf("Couldn't update placeholder info %s Error: %v", handle.Path(), err)
		lcCfApiWrapper.Warning("%v %s", errorMessage, replacesPath)
		return 0, errorMessage
	}

	// Pin state tends to be lost on updates, so restore it every time
	if _, err := SetPinState(handle, previousPinState, NoRecurse); err != nil {
		return 0, fmt.Errorf("Couldn't restore pin state")
	}

	return ConvertToPlaceholderOk, nil
}

// ConnectSyncRoot is CfApiWrapper::connectSyncRoot().
func ConnectSyncRoot(path string, context *VfsCfApi) (cf.CF_CONNECTION_KEY, error) {
	sRegisterMutex.Lock()
	defer sRegisterMutex.Unlock()

	var key cf.CF_CONNECTION_KEY

	cfApiCallbacks := []cf.CF_CALLBACK_REGISTRATION{
		{Type: cf.CF_CALLBACK_TYPE_FETCH_DATA, Callback: cbFetchData},
		{Type: cf.CF_CALLBACK_TYPE_CANCEL_FETCH_DATA, Callback: cbCancelFetchData},
		{Type: cf.CF_CALLBACK_TYPE_NOTIFY_FILE_OPEN_COMPLETION, Callback: cbNotifyFileOpenCompletion},
		{Type: cf.CF_CALLBACK_TYPE_NOTIFY_FILE_CLOSE_COMPLETION, Callback: cbNotifyFileCloseCompletion},
		{Type: cf.CF_CALLBACK_TYPE_VALIDATE_DATA, Callback: cbValidateData},
		{Type: cf.CF_CALLBACK_TYPE_CANCEL_FETCH_PLACEHOLDERS, Callback: cbCancelFetchPlaceHolders},
		{Type: cf.CF_CALLBACK_TYPE_NOTIFY_RENAME, Callback: cbRename},
		{Type: cf.CF_CALLBACK_TYPE_NOTIFY_RENAME_COMPLETION, Callback: cbRenameCompletion},
		// CF_CALLBACK_REGISTRATION_END
		{Type: cf.CF_CALLBACK_TYPE_NONE, Callback: 0},
	}

	if err := cf.CfConnectSyncRoot(lexicallyNormal(path), &cfApiCallbacks[0], context.callbackContext(),
		cf.CF_CONNECT_FLAG_REQUIRE_PROCESS_INFO|cf.CF_CONNECT_FLAG_REQUIRE_FULL_FILE_PATH|cf.CF_CONNECT_FLAG_BLOCK_SELF_IMPLICIT_HYDRATION,
		&key); err != nil {
		return key, err
	}
	return key, nil
}

// DisconnectSyncRoot is CfApiWrapper::disconnectSyncRoot().
func DisconnectSyncRoot(key cf.CF_CONNECTION_KEY) error {
	sRegisterMutex.Lock()
	defer sRegisterMutex.Unlock()
	if err := cf.CfDisconnectSyncRoot(key); err != nil {
		lcCfApiWrapper.Warning("Disconnecting sync root failed %v", err)
		return err
	}
	return nil
}

// IsDehydratedPlaceholder is CfApiWrapper::isDehydratedPlaceholder().
func IsDehydratedPlaceholder(path string) bool {
	handle := CreateHandleDefault(path)
	defer handle.Close()
	if !handle.IsValid() {
		lcCfApiWrapper.Warning("Failed to get file handle %s %s", path, handle.ErrorMessage())
		return false
	}
	targInfo, err := getFileAttributeTagInfo(handle.Handle())
	if err != nil {
		lcCfApiWrapper.Warning("Failed to get file attribute tag info for %s %v", path, err)
		return false
	}
	state := cf.CfGetPlaceholderStateFromAttributeTag(targInfo.FileAttributes, targInfo.ReparseTag)
	if state == cf.CF_PLACEHOLDER_STATE_NO_STATES {
		return false
	}
	return state&cf.CF_PLACEHOLDER_STATE_PARTIAL != 0
}

// SetPinState is CfApiWrapper::setPinState().
func SetPinState(handle *Handle, state PinState, mode SetPinRecurseMode) (ConvertToPlaceholderResult, error) {
	cfState := pinStateToCfPinState(state)
	flags := pinRecurseModeToCfSetPinFlags(mode)

	if err := cf.CfSetPinState(foundationHandle(handle), cfState, flags, nil); err == nil {
		return ConvertToPlaceholderOk, nil
	} else {
		lcCfApiWrapper.Warning("Couldn't set pin state %d for %s with recurse mode %s: %v",
			state, handle.Path(), mode, err)
		return 0, fmt.Errorf("Couldn't set pin state")
	}
}

// CreatePlaceholderInfo is CfApiWrapper::createPlaceholderInfo().
func CreatePlaceholderInfo(path string, modtime int64, size int64, fileID []byte, isDirectory bool) error {
	fileName := baseName(path)
	baseDir := lexicallyNormal(parentPath(path))

	wFileName, err := windows.UTF16PtrFromString(fileName)
	if err != nil {
		return err
	}
	identity, identityLen := identityPointer(fileID)

	cloudEntry := cf.CF_PLACEHOLDER_CREATE_INFO{}
	cloudEntry.FileIdentity = identity
	cloudEntry.FileIdentityLength = identityLen
	cloudEntry.RelativeFileName = wFileName
	cloudEntry.Flags = cf.CF_PLACEHOLDER_CREATE_FLAG_MARK_IN_SYNC
	cloudEntry.FsMetadata.FileSize = size
	cloudEntry.FsMetadata.BasicInfo.FileAttributes = windows.FILE_ATTRIBUTE_NORMAL
	cloudEntry.FsMetadata.BasicInfo.CreationTime = UnixTimeToLargeIntegerFiletime(modtime)
	cloudEntry.FsMetadata.BasicInfo.LastWriteTime = cloudEntry.FsMetadata.BasicInfo.CreationTime
	cloudEntry.FsMetadata.BasicInfo.LastAccessTime = cloudEntry.FsMetadata.BasicInfo.CreationTime
	cloudEntry.FsMetadata.BasicInfo.ChangeTime = cloudEntry.FsMetadata.BasicInfo.CreationTime

	if isDirectory {
		cloudEntry.Flags |= cf.CF_PLACEHOLDER_CREATE_FLAG_DISABLE_ON_DEMAND_POPULATION
		cloudEntry.FsMetadata.BasicInfo.FileAttributes = windows.FILE_ATTRIBUTE_DIRECTORY
		cloudEntry.FsMetadata.FileSize = 0
	}

	lcCfApiWrapper.Debug("CfCreatePlaceholders %s %d", path, modtime)
	if err := cf.CfCreatePlaceholders(baseDir, []cf.CF_PLACEHOLDER_CREATE_INFO{cloudEntry}, cf.CF_CREATE_FLAG_NONE, nil); err != nil {
		lcCfApiWrapper.Warning("Couldn't create placeholder info for %s : %v", path, err)
		return fmt.Errorf("Couldn't create placeholder info")
	}
	// keep the identity alive until the call has returned
	runtime.KeepAlive(fileID)

	parentHandle := CreateHandleDefault(baseDir)
	defer parentHandle.Close()
	if !parentHandle.IsValid() {
		errorMessage := fmt.Errorf("Couldn't create handle for placeholder %s Error: %s", parentHandle.Path(), parentHandle.ErrorMessage())
		lcCfApiWrapper.Warning("%v", errorMessage)
		return errorMessage
	}
	parentInfo := FindPlaceholderInfoBasic(parentHandle, false)
	state := PinInherited
	if parentInfo.IsValid() && parentInfo.PinState() == PinOnlineOnly {
		state = PinOnlineOnly
	}

	handle := CreateHandleDefault(baseDir)
	defer handle.Close()
	if !handle.IsValid() {
		errorMessage := fmt.Errorf("Couldn't create handle for placeholder %s Error: %s", handle.Path(), handle.ErrorMessage())
		lcCfApiWrapper.Warning("%v", errorMessage)
		return errorMessage
	}
	if _, err := SetPinState(handle, state, NoRecurse); err != nil {
		return fmt.Errorf("Couldn't set the default inherit pin state")
	}

	return nil
}

// UpdatePlaceholderInfo is CfApiWrapper::updatePlaceholderInfo().
func UpdatePlaceholderInfo(handle *Handle, modtime int64, size int64, fileID []byte, replacesPath string, isHydrated bool) (ConvertToPlaceholderResult, error) {
	return updatePlaceholderState(handle, modtime, size, fileID, replacesPath, isHydrated)
}

// DehydratePlaceholder is CfApiWrapper::dehydratePlaceholder().
func DehydratePlaceholder(handle *Handle, fileID []byte) (ConvertToPlaceholderResult, error) {
	info := FindPlaceholderInfoBasic(handle, false)
	if info.IsValid() {
		if err := cf.CfUpdatePlaceholder(foundationHandle(handle), nil, fileID, nil,
			cf.CF_UPDATE_FLAG_MARK_IN_SYNC|cf.CF_UPDATE_FLAG_DEHYDRATE, nil, nil); err != nil {
			errorMessage := fmt.Errorf("Couldn't update placeholder info %s Error: %v", handle.Path(), err)
			lcCfApiWrapper.Warning("%v", errorMessage)
			return 0, errorMessage
		}
	} else {
		if err := cf.CfConvertToPlaceholder(foundationHandle(handle), fileID,
			cf.CF_CONVERT_FLAG_MARK_IN_SYNC|cf.CF_CONVERT_FLAG_DEHYDRATE, nil, nil); err != nil {
			errorMessage := fmt.Errorf("Couldn't convert to placeholder %s Error: %v", handle.Path(), err)
			lcCfApiWrapper.Warning("%v", errorMessage)
			return 0, errorMessage
		}
		_, _ = SetPinState(handle, PinOnlineOnly, NoRecurse)
	}
	return ConvertToPlaceholderOk, nil
}

// ConvertToPlaceholder is CfApiWrapper::convertToPlaceholder().
func ConvertToPlaceholder(handle *Handle, modtime int64, size int64, fileID []byte, replacesPath string) (ConvertToPlaceholderResult, error) {
	if err := cf.CfConvertToPlaceholder(foundationHandle(handle), fileID, cf.CF_CONVERT_FLAG_MARK_IN_SYNC, nil, nil); err != nil {
		errorMessage := fmt.Errorf("Couldn't convert to placeholder %s Error: %v", handle.Path(), err)
		lcCfApiWrapper.Warning("%v", errorMessage)
		return 0, errorMessage
	}
	// we are converting an existing file, so it must be hydrated
	return updatePlaceholderState(handle, modtime, size, fileID, replacesPath, true)
}

// UpdatePlaceholderMarkInSync is CfApiWrapper::updatePlaceholderMarkInSync().
func UpdatePlaceholderMarkInSync(handle *Handle) (ConvertToPlaceholderResult, error) {
	err := cf.CfSetInSyncState(foundationHandle(handle), cf.CF_IN_SYNC_STATE_IN_SYNC, cf.CF_SET_IN_SYNC_FLAG_NONE, nil)
	if err == nil {
		return ConvertToPlaceholderOk, nil
	}
	if isErrorNotACloudFile(err) {
		return 0, fmt.Errorf("»%s« is not a cloud file", handle.Path())
	}
	wrapped := fmt.Errorf("updatePlaceholderMarkInSync failed: %v", err)
	lcCfApiWrapper.Warning("%v", wrapped)
	return 0, wrapped
}

// IsPlaceHolderInSync is CfApiWrapper::isPlaceHolderInSync().
func IsPlaceHolderInSync(handle *Handle) bool {
	if originalInfo := FindPlaceholderInfoBasic(handle, false); originalInfo.IsValid() {
		return originalInfo.InSyncState() == cf.CF_IN_SYNC_STATE_IN_SYNC
	}
	return true
}

// foundationHandle adapts a Handle to the HANDLE typedef the bindings use.
func foundationHandle(h *Handle) foundation.HANDLE {
	return foundation.HANDLE(h.Handle())
}

// identityPointer pins a FileIdentity for the duration of a cldapi call, for
// the one struct field that still takes a raw pointer.
func identityPointer(fileID []byte) (unsafe.Pointer, uint32) {
	if len(fileID) == 0 {
		return nil, 0
	}
	return unsafe.Pointer(&fileID[0]), uint32(len(fileID))
}

// isErrorMoreData and isErrorNotACloudFile recognise the two Win32 codes the
// wrapper branches on. The bindings return a win32.HRESULT, whose Is method
// matches a FACILITY_WIN32 HRESULT against the syscall.Errno it wraps, so
// errors.Is does the HRESULT_FROM_WIN32 comparison upstream writes out.
const errorNotACloudFile = syscall.Errno(395) // ERROR_NOT_A_CLOUD_FILE

func isErrorMoreData(err error) bool { return errors.Is(err, windows.ERROR_MORE_DATA) }

func isErrorNotACloudFile(err error) bool { return errors.Is(err, errorNotACloudFile) }
