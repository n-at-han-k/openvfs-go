//go:build linux

package linux

import "golang.org/x/sys/unix"

// The Linux arm of Xattr::{get,set,list,remove}xattr: the l* variants, which do
// not follow a final symlink.

func xattrGet(path, name string, value []byte) (int, error) {
	return unix.Lgetxattr(path, name, value)
}

func xattrSet(path, name string, value []byte, flags int) error {
	return unix.Lsetxattr(path, name, value, flags)
}

func xattrList(path string, list []byte) (int, error) {
	return unix.Llistxattr(path, list)
}

func xattrRemove(path, name string) error {
	return unix.Lremovexattr(path, name)
}

// isErrNoAttr is the errno openVFSfuse_getxattr() compares against before it
// hands out the default placeholder data. Linux reports a missing attribute as ENODATA.
func isErrNoAttr(err error) bool { return err == unix.ENODATA }
