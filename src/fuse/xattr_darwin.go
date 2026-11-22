package fuse

import "golang.org/x/sys/unix"

// The Apple arm of Xattr::{get,set,list,remove}xattr. Upstream passes
// XATTR_NOFOLLOW to the *xattr() calls; x/sys/unix hardcodes position and
// options to zero, so the same no-follow semantics come from opening the path
// with O_NOFOLLOW and using the f*xattr() calls, exactly as the *At variants
// below the mount do.

func xattrGet(path, name string, value []byte) (int, error) {
	return xattrGetAt(unix.AT_FDCWD, path, name, value)
}

func xattrSet(path, name string, value []byte, flags int) error {
	return xattrSetAt(unix.AT_FDCWD, path, name, value, flags)
}

func xattrList(path string, list []byte) (int, error) {
	return xattrListAt(unix.AT_FDCWD, path, list)
}

func xattrRemove(path, name string) error {
	return xattrRemoveAt(unix.AT_FDCWD, path, name)
}

// isErrNoAttr is the errno openVFSfuse_getxattr() compares against before it
// hands out the default placeholder data. macOS reports a missing attribute as ENOATTR.
func isErrNoAttr(err error) bool { return err == unix.ENOATTR }
