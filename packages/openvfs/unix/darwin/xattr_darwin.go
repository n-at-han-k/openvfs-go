package darwin

import "golang.org/x/sys/unix"

// The Apple arm of Xattr::{get,set,list,remove}xattr. Upstream passes
// XATTR_NOFOLLOW to the *xattr() calls; x/sys/unix hardcodes position and
// options to zero, so the same no-follow semantics come from opening the path
// with O_NOFOLLOW and using the f*xattr() calls.

// openNoFollow opens a path without following a final symlink. macOS refuses
// user namespace attributes on anything but regular files and directories, so
// nothing that can carry user.openvfs.* is lost by needing a descriptor.
func openNoFollow(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err == nil {
		return fd, nil
	}
	// A write-only file cannot be opened for reading, but its attributes are
	// still reachable through a write-only descriptor.
	if err == unix.EACCES {
		if fd, werr := unix.Open(path, unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0); werr == nil {
			return fd, nil
		}
	}
	return -1, err
}

func xattrGet(path, name string, value []byte) (int, error) {
	fd, err := openNoFollow(path)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.Fgetxattr(fd, name, value)
}

func xattrSet(path, name string, value []byte, flags int) error {
	fd, err := openNoFollow(path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fsetxattr(fd, name, value, flags)
}

func xattrList(path string, list []byte) (int, error) {
	fd, err := openNoFollow(path)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.Flistxattr(fd, list)
}

func xattrRemove(path, name string) error {
	fd, err := openNoFollow(path)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fremovexattr(fd, name)
}

// isErrNoAttr is the errno openVFSfuse_getxattr() compares against before it
// hands out the default placeholder data. macOS reports a missing attribute as
// ENOATTR.
func isErrNoAttr(err error) bool { return err == unix.ENOATTR }
