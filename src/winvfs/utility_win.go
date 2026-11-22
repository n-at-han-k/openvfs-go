//go:build windows

package winvfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Transpiled from src/libsync/common/utility_win.{h,cpp}, limited to what the
// cfapi plugin uses.

// CreateHandleParameter carries the defaults upstream declares at namespace
// scope so that Handle's default argument can use them.
type CreateHandleParameter struct {
	AccessMode     uint32
	ShareMode      uint32
	CreationFlags  uint32
	FollowSymlinks bool
	Async          bool
}

// DefaultCreateHandleParameter is the `= {}` upstream passes by default.
func DefaultCreateHandleParameter() CreateHandleParameter {
	return CreateHandleParameter{
		AccessMode:     0,
		ShareMode:      windows.FILE_SHARE_WRITE | windows.FILE_SHARE_READ | windows.FILE_SHARE_DELETE,
		CreationFlags:  windows.OPEN_EXISTING,
		FollowSymlinks: true,
		Async:          false,
	}
}

// Handle is the RAII for Windows Handles. Go has no destructor, so the owner
// calls Close; every use in this package does so with defer.
type Handle struct {
	handle windows.Handle
	err    windows.Errno
	path   string
}

// CreateHandle is Utility::Handle::createHandle().
func CreateHandle(path string, p CreateHandleParameter) *Handle {
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_BACKUP_SEMANTICS)
	if !p.FollowSymlinks {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	if p.Async {
		flags |= windows.FILE_FLAG_OVERLAPPED
	}

	normalized := lexicallyNormal(path)
	name, err := windows.UTF16PtrFromString(normalized)
	if err != nil {
		return &Handle{handle: windows.InvalidHandle, err: windows.ERROR_INVALID_NAME, path: path}
	}
	h, err := windows.CreateFile(name, p.AccessMode, p.ShareMode, nil, p.CreationFlags, flags, 0)
	out := &Handle{handle: h, path: path}
	if h == windows.InvalidHandle {
		// upstream falls back to GetLastError() when no error was passed in
		if errno, ok := err.(windows.Errno); ok {
			out.err = errno
		} else {
			out.err = windows.ERROR_INVALID_HANDLE
		}
	}
	return out
}

// CreateHandleDefault is createHandle(path) with the default parameter.
func CreateHandleDefault(path string) *Handle {
	return CreateHandle(path, DefaultCreateHandleParameter())
}

// IsValid is Handle::operator bool().
func (h *Handle) IsValid() bool { return h != nil && h.handle != windows.InvalidHandle }

func (h *Handle) Handle() windows.Handle { return h.handle }

func (h *Handle) Path() string { return h.path }

func (h *Handle) Error() windows.Errno { return h.err }

// ErrorMessage is Handle::errorMessage().
func (h *Handle) ErrorMessage() string { return FormatWinError(int32(h.err)) }

func (h *Handle) Close() {
	if h != nil && h.handle != windows.InvalidHandle {
		_ = windows.CloseHandle(h.handle)
		h.handle = windows.InvalidHandle
	}
}

// FormatWinError is Utility::formatWinError().
func FormatWinError(errorCode int32) string {
	return fmt.Sprintf("WindowsError: 0x%x: %s", uint32(errorCode), errorMessageText(uint32(errorCode)))
}

// errorMessageText stands in for _com_error::ErrorMessage().
func errorMessageText(code uint32) string {
	const flags = windows.FORMAT_MESSAGE_FROM_SYSTEM | windows.FORMAT_MESSAGE_IGNORE_INSERTS
	var buf [512]uint16
	n, err := windows.FormatMessage(flags, 0, code, 0, buf[:], nil)
	if err != nil || n == 0 {
		return fmt.Sprintf("Unknown error 0x%x", code)
	}
	return strings.TrimRight(windows.UTF16ToString(buf[:n]), "\r\n ")
}

// hundredNanoFiletimeEpochOffset is the number of 100ns intervals between the
// FILETIME epoch (1601-01-01) and the Unix epoch.
const hundredNanoFiletimeEpochOffset = 116444736000000000

// UnixTimeToLargeIntegerFiletime is
// Utility::UnixTimeToLargeIntegerFiletime(): a time_t as the 100ns count a
// FILETIME carries.
func UnixTimeToLargeIntegerFiletime(t int64) LARGE_INTEGER {
	return LARGE_INTEGER(t*10000000 + hundredNanoFiletimeEpochOffset)
}

// lexicallyNormal stands in for std::filesystem::path::lexically_normal().
func lexicallyNormal(path string) string {
	return filepath.Clean(path)
}

// convertSidToStringSid is the free function of the same name.
func convertSidToStringSid(sid *windows.SID) string {
	s, err := sid.String()
	if err != nil {
		return ""
	}
	return s
}

// retrieveWindowsSid is the free function of the same name, by way of
// getCurrentTokenInformation().
func retrieveWindowsSid() string {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return ""
	}
	return convertSidToStringSid(user.User.Sid)
}

// createSyncRootID is the free function of the same name.
//
// We must set specific Registry keys to make the progress bar refresh correctly
// and also add status icons into Windows Explorer. More about this here:
// https://docs.microsoft.com/en-us/windows/win32/shell/integrate-cloud-storage
func createSyncRootID(providerName, accountUUID, syncRootPath string) string {
	windowsSid := retrieveWindowsSid()
	if windowsSid == "" {
		logWarning("Failed to set Registry keys for shell integration, as windowsSid is empty. Progress bar will not work.")
		return ""
	}

	// syncRootId should be: [storage provider ID]![Windows SID]![Account ID]![PathHash]
	// multiple sync folders for the same account) folder registry keys go like:
	// OpenCloud!S-1-5-21-2096452760-2617351404-2281157308-1001!AccountUUID!hash
	canonical := syncRootPath
	if resolved, err := filepath.Abs(syncRootPath); err == nil {
		canonical = resolved
	}
	sum := sha256.Sum256([]byte(canonical))
	stableFolderHash := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s!%s!%s!%s", providerName, windowsSid, accountUUID, stableFolderHash)
}

// GetFileInformationByHandleEx wraps the call the plugin makes with
// FileAttributeTagInfo.
func getFileAttributeTagInfo(h windows.Handle) (FILE_ATTRIBUTE_TAG_INFO, error) {
	var info FILE_ATTRIBUTE_TAG_INFO
	err := windows.GetFileInformationByHandleEx(h, FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	return info, err
}
