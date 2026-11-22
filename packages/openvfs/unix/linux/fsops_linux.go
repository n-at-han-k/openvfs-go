package linux

import (
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
)

// Shims for the handful of calls whose shape differs between Linux and macOS.

// statMode widens st_mode, which is 32 bit on Linux and 16 bit on macOS.
func statMode(st *unix.Stat_t) uint32 { return st.Mode }

func fillStatfs(out *gofuse.StatfsOut, st *unix.Statfs_t) {
	out.Blocks = st.Blocks
	out.Bfree = st.Bfree
	out.Bavail = st.Bavail
	out.Files = st.Files
	out.Ffree = st.Ffree
	out.Bsize = uint32(st.Bsize)
	out.NameLen = uint32(st.Namelen)
	out.Frsize = uint32(st.Frsize)
}

func mknodAt(dirfd int, rel string, mode uint32, dev uint32) error {
	return unix.Mknodat(dirfd, rel, mode, int(dev))
}

func mkfifoAt(dirfd int, rel string, mode uint32) error {
	return unix.Mkfifoat(dirfd, rel, mode)
}
