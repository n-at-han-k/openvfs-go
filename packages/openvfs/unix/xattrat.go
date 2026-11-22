//go:build linux || darwin

package unix

import "golang.org/x/sys/unix"

// There is no portable l*xattrat(), so the file is opened relative to the root
// descriptor without following a final symlink and the f*xattr() calls are used
// on it. Neither Linux nor macOS lets a user namespace attribute live on a
// symlink, so nothing that can carry user.* is lost.

func openAt(dirfd int, rel string) (int, error) {
	fd, err := unix.Openat(dirfd, rel, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err == nil {
		return fd, nil
	}
	// A write-only file cannot be opened for reading, but its attributes are
	// still reachable through a write-only descriptor.
	if err == unix.EACCES {
		if fd, werr := unix.Openat(dirfd, rel, unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0); werr == nil {
			return fd, nil
		}
	}
	return -1, err
}

func xattrGetAt(dirfd int, rel, name string, value []byte) (int, error) {
	fd, err := openAt(dirfd, rel)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.Fgetxattr(fd, name, value)
}

func xattrSetAt(dirfd int, rel, name string, value []byte, flags int) error {
	fd, err := openAt(dirfd, rel)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fsetxattr(fd, name, value, flags)
}

func xattrListAt(dirfd int, rel string, list []byte) (int, error) {
	fd, err := openAt(dirfd, rel)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.Flistxattr(fd, list)
}

func xattrRemoveAt(dirfd int, rel, name string) error {
	fd, err := openAt(dirfd, rel)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fremovexattr(fd, name)
}

// xattrGetAtString is xattrGetString against a root-relative path.
func xattrGetAtString(dirfd int, rel, name string) (string, bool) {
	size := 0
	for {
		size += 255
		value := make([]byte, size)
		res, err := xattrGetAt(dirfd, rel, name, value)
		if err == nil {
			return string(value[:res]), true
		}
		if !isErrRange(err) {
			return "", false
		}
	}
}
