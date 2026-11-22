//go:build linux || darwin

package unix

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// unixUtimeOmit is UTIME_OMIT from <sys/stat.h>: leave this timestamp alone.
const unixUtimeOmit = (1 << 30) - 2

// errnoValue is the errno behind an error, which upstream reads from the global.
func errnoValue(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := err.(unix.Errno); ok {
		return int(e)
	}
	if e, ok := err.(syscall.Errno); ok {
		return int(e)
	}
	return int(syscall.EIO)
}

// asErrno converts a syscall error into the negative errno upstream returns
// from a fuse callback.
func asErrno(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	if e, ok := err.(unix.Errno); ok {
		return syscall.Errno(e)
	}
	if e, ok := err.(syscall.Errno); ok {
		return e
	}
	return syscall.EIO
}

// isErrRange reports ERANGE, the signal Xattr::CPP::getxattr retries on.
func isErrRange(err error) bool { return err == unix.ERANGE }

// The errno values the hydration path returns, named once so that the shared
// code in openvfsfuse.go does not have to reach for a platform package.
const (
	errPerm     = int(syscall.EPERM)
	errIO       = int(syscall.EIO)
	errTimedOut = int(syscall.ETIMEDOUT)
)
