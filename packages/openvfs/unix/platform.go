//go:build linux || darwin

package unix

import (
	"errors"

	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
)

// Platform is the set of operations that differ between Linux and macOS.
//
// Upstream expresses these with #ifdef __APPLE__ inside one translation unit.
// Here the shared layer is one package and each OS is its own, so the OS
// package fills this table at init and the shared code calls through it. The
// call sites are unchanged from the straight translation -- the names below are
// the same functions the C++ calls, they are just variables now.
type Platform struct {
	// Xattr*, the l*xattr family: no following of a final symlink.
	XattrGet    func(path, name string, value []byte) (int, error)
	XattrSet    func(path, name string, value []byte, flags int) error
	XattrList   func(path string, list []byte) (int, error)
	XattrRemove func(path, name string) error

	// IsErrNoAttr reports a missing attribute: ENODATA on Linux, ENOATTR on
	// macOS.
	IsErrNoAttr func(err error) bool

	// CallerName is getcallername(): /proc/<pid>/exe on Linux, KERN_PROCARGS2
	// on macOS.
	CallerName func(pid uint32) string
	// CallerIsThreadOfProcess answers the /proc/<pid>/task/<tid> question,
	// which upstream short circuits to false under __APPLE__.
	CallerIsThreadOfProcess func(callerThreadID uint32, processID int64) bool

	// StatMode widens st_mode, which is 32 bit on Linux and 16 bit on macOS.
	StatMode func(st *unix.Stat_t) uint32
	// FillStatfs maps struct statfs onto the fuse reply; macOS carries neither
	// f_namemax nor f_frsize.
	FillStatfs func(out *gofuse.StatfsOut, st *unix.Statfs_t)
	// MknodAt and MkfifoAt stand in for the syscalls macOS does not have.
	MknodAt  func(dirfd int, rel string, mode uint32, dev uint32) error
	MkfifoAt func(dirfd int, rel string, mode uint32) error

	// LogToSyslog mirrors a log line into syslog, which upstream does on
	// everything but Apple.
	LogToSyslog func(message string)
}

// The shared layer calls these under the names the C++ uses.
var (
	xattrGet    func(path, name string, value []byte) (int, error)
	xattrSet    func(path, name string, value []byte, flags int) error
	xattrList   func(path string, list []byte) (int, error)
	xattrRemove func(path, name string) error

	isErrNoAttr func(err error) bool

	getcallernameByPid      func(pid uint32) string
	callerIsThreadOfProcess func(callerThreadID uint32, processID int64) bool

	statMode   func(st *unix.Stat_t) uint32
	fillStatfs func(out *gofuse.StatfsOut, st *unix.Statfs_t)
	mknodAt    func(dirfd int, rel string, mode uint32, dev uint32) error
	mkfifoAt   func(dirfd int, rel string, mode uint32) error

	logToSyslog func(message string)
)

// platformInstalled reports whether SetPlatform has run. Every entry point
// checks it, so importing this package without its OS half fails with a clear
// error rather than a nil call deep inside a fuse callback.
func platformInstalled() bool { return xattrGet != nil }

// errNoPlatform is returned by the entry points when the OS half is missing.
var errNoPlatform = errors.New(
	"openvfs: no platform installed; import example.com/project/packages/unix/linux or .../darwin")

// SetPlatform installs the OS specific half of the backend. The linux and
// darwin packages call it from init(), so importing one of them is what makes
// this package usable.
func SetPlatform(p Platform) {
	xattrGet = p.XattrGet
	xattrSet = p.XattrSet
	xattrList = p.XattrList
	xattrRemove = p.XattrRemove
	isErrNoAttr = p.IsErrNoAttr
	getcallernameByPid = p.CallerName
	callerIsThreadOfProcess = p.CallerIsThreadOfProcess
	statMode = p.StatMode
	fillStatfs = p.FillStatfs
	mknodAt = p.MknodAt
	mkfifoAt = p.MkfifoAt
	logToSyslog = p.LogToSyslog
}
