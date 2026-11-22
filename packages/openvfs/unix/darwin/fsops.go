//go:build darwin

package darwin

import (
	"sync"

	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
)

// Shims for the handful of calls whose shape differs between Linux and macOS.

// statMode widens st_mode, which is 32 bit on Linux and 16 bit on macOS.
func statMode(st *unix.Stat_t) uint32 { return uint32(st.Mode) }

func fillStatfs(out *gofuse.StatfsOut, st *unix.Statfs_t) {
	out.Blocks = st.Blocks
	out.Bfree = st.Bfree
	out.Bavail = uint64(st.Bavail)
	out.Files = st.Files
	out.Ffree = st.Ffree
	out.Bsize = uint32(st.Bsize)
	// struct statfs on macOS carries neither f_namemax nor f_frsize.
	out.NameLen = 255
	out.Frsize = uint32(st.Bsize)
}

// cwdMu serialises the fchdir() below, which is a process wide side effect.
var cwdMu sync.Mutex

// withRootCwd runs fn with the process working directory set to dirfd, which is
// how upstream reaches the file below the mount throughout. macOS has no
// mknodat() or mkfifoat(), so the two calls that need it fall back to the
// upstream approach rather than to a path that would re-enter the mount.
func withRootCwd(dirfd int, fn func() error) error {
	cwdMu.Lock()
	defer cwdMu.Unlock()
	if err := unix.Fchdir(dirfd); err != nil {
		return err
	}
	return fn()
}

func mknodAt(dirfd int, rel string, mode uint32, dev uint32) error {
	return withRootCwd(dirfd, func() error {
		return unix.Mknod("./"+rel, mode, int(dev))
	})
}

func mkfifoAt(dirfd int, rel string, mode uint32) error {
	return withRootCwd(dirfd, func() error {
		return unix.Mkfifo("./"+rel, mode)
	})
}
