//go:build linux || darwin

package fuse

import (
	"context"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/hanwen/go-fuse/v2/fs"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
)

// The libfuse facing half of src/openvfsfuse/openvfsfuse.cpp, on top of
// go-fuse. go-fuse's node API replaces libfuse's path based callbacks, so
// Lookup is new, chmod/chown/truncate/utimens arrive as one Setattr, and
// read/write/flush come from the loopback file.

func openRootHandle(path string) (int, error) {
	return unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
}

func closeRootHandle(handle int) { unix.Close(handle) }

func setUmaskZero() { unix.Umask(0) }

func callerOf(ctx context.Context) *callerInfo {
	caller, ok := gofuse.FromContext(ctx)
	if !ok || caller == nil {
		return nil
	}
	return &callerInfo{Pid: caller.Pid, Uid: caller.Uid, Gid: caller.Gid}
}

// node is one path in the mounted tree. rel is the path relative to the root
// descriptor; upstream's internal path is "./" + rel.
type node struct {
	fs.Inode
	v   *VFSFuseContext
	rel string
}

// relPath is the path handed to the *at() syscalls. "." is the root itself.
func (n *node) relPath() string {
	if n.rel == "" {
		return "."
	}
	return n.rel
}

// internal is VFSFuseContext::getInternalPath() of this node, which is the name
// every operation is logged under. FUSE names the root "/", so the root's
// internal path is "./".
func (n *node) internal() string {
	return n.v.getInternalPath("/" + n.rel)
}

func (n *node) childRel(name string) string {
	if n.rel == "" {
		return name
	}
	return n.rel + "/" + name
}

func (n *node) childInternal(name string) string {
	return n.v.getInternalPath("/" + n.childRel(name))
}

// external is getExternalPath() of this node.
func (n *node) external() string {
	return n.v.getExternalPath(n.internal())
}

func (n *node) lstat(rel string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(n.v.rootHandle, rel, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

func (n *node) newChild(ctx context.Context, rel string, st unix.Stat_t) *fs.Inode {
	return n.NewInode(ctx, &node{v: n.v, rel: rel}, fs.StableAttr{Mode: statMode(&st), Ino: st.Ino})
}

func statAttr(out *gofuse.Attr, st *unix.Stat_t) {
	out.FromStat((*syscall.Stat_t)(unsafe.Pointer(st)))
}

// Lookup has no upstream counterpart: the libfuse high level API derives it
// from getattr. It is the node API's equivalent of that lstat.
func (n *node) Lookup(ctx context.Context, name string, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	rel := n.childRel(name)
	st, err := n.lstat(rel)
	if err != nil {
		return nil, asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return n.newChild(ctx, rel, st), 0
}

func (n *node) Getattr(ctx context.Context, f fs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	path := n.internal()
	rel := n.relPath()
	st, err := n.lstat(rel)
	openvfsfuseLog(callerOf(ctx), path, "getattr", rc(err), "getattr inode: %d", st.Ino)

	if err != nil {
		return asErrno(err)
	}
	statAttr(&out.Attr, &st)
	// check virtual size if size is 0 and path is not a dir
	if statMode(&st)&unix.S_IFMT != unix.S_IFDIR && st.Size == 0 {
		if attr, ok := placeHolderFromAttributesAt(n.v.rootHandle, rel, n.external()); ok {
			out.Size = attr.Size
		}
	}
	return 0
}

func (n *node) Access(ctx context.Context, mask uint32) syscall.Errno {
	path := n.internal()
	err := unix.Faccessat(n.v.rootHandle, n.relPath(), mask, 0)
	openvfsfuseLog(callerOf(ctx), path, "access", rc(err), "")
	return asErrno(err)
}

func (n *node) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	path := n.internal()
	buf := make([]byte, unix.PathMax)
	res, err := unix.Readlinkat(n.v.rootHandle, n.relPath(), buf)
	openvfsfuseLog(callerOf(ctx), path, "readlink", rc(err), "readlink")

	if err != nil {
		return nil, asErrno(err)
	}
	return buf[:res], 0
}

func (n *node) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	path := n.internal()

	fd, err := unix.Openat(n.v.rootHandle, n.relPath(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		openvfsfuseLog(callerOf(ctx), path, "readdir", -1, "")
		return nil, asErrno(err)
	}

	// The stream fills st_ino and st_mode from d_ino and d_type, as upstream
	// does by hand, and closes the descriptor when it is released.
	stream, errno := fs.NewLoopbackDirStreamFd(fd)
	if errno != 0 {
		unix.Close(fd)
		openvfsfuseLog(callerOf(ctx), path, "readdir", -1, "")
		return nil, errno
	}
	openvfsfuseLog(callerOf(ctx), path, "readdir", 0, "")

	return stream, 0
}

// lchownToCaller is the lchown() upstream performs after creating an entry, so
// the new name belongs to the process that asked for it rather than to the
// fuse server.
func (n *node) lchownToCaller(ctx context.Context, rel string) {
	caller := callerOf(ctx)
	if caller == nil {
		return
	}
	_ = unix.Fchownat(n.v.rootHandle, rel, int(caller.Uid), int(caller.Gid), unix.AT_SYMLINK_NOFOLLOW)
}

func (n *node) Mknod(ctx context.Context, name string, mode uint32, dev uint32, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	path := n.childInternal(name)
	rel := n.childRel(name)
	caller := callerOf(ctx)

	var err error
	switch mode & unix.S_IFMT {
	case unix.S_IFREG:
		var fd int
		fd, err = unix.Openat(n.v.rootHandle, rel, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC, mode)
		openvfsfuseLog(caller, path, "mknod", rc(err), "mknod %o S_IFREG (normal file creation)", mode)
		if err == nil {
			err = unix.Close(fd)
		}
	case unix.S_IFIFO:
		err = mkfifoAt(n.v.rootHandle, rel, mode)
		openvfsfuseLog(caller, path, "mkfifo", rc(err), "mkfifo %o S_IFFIFO (fifo creation)", mode)
	default:
		err = mknodAt(n.v.rootHandle, rel, mode, dev)
		if mode&unix.S_IFMT == unix.S_IFCHR {
			openvfsfuseLog(caller, path, "mknod", rc(err), "mknod %o (character device creation)", mode)
		} else {
			openvfsfuseLog(caller, path, "mknod", rc(err), "mknod %o", mode)
		}
	}

	if err != nil {
		return nil, asErrno(err)
	}
	n.lchownToCaller(ctx, rel)

	st, err := n.lstat(rel)
	if err != nil {
		return nil, asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return n.newChild(ctx, rel, st), 0
}

func (n *node) Mkdir(ctx context.Context, name string, mode uint32, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	path := n.childInternal(name)
	rel := n.childRel(name)

	err := unix.Mkdirat(n.v.rootHandle, rel, mode)
	openvfsfuseLog(callerOf(ctx), path, "mkdir", rc(err), "mkdir mode %o", mode)

	if err != nil {
		return nil, asErrno(err)
	}
	n.lchownToCaller(ctx, rel)

	st, err := n.lstat(rel)
	if err != nil {
		return nil, asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return n.newChild(ctx, rel, st), 0
}

func (n *node) Unlink(ctx context.Context, name string) syscall.Errno {
	path := n.childInternal(name)
	err := unix.Unlinkat(n.v.rootHandle, n.childRel(name), 0)
	openvfsfuseLog(callerOf(ctx), path, "unlink", rc(err), "")
	return asErrno(err)
}

func (n *node) Rmdir(ctx context.Context, name string) syscall.Errno {
	path := n.childInternal(name)
	err := unix.Unlinkat(n.v.rootHandle, n.childRel(name), unix.AT_REMOVEDIR)
	openvfsfuseLog(callerOf(ctx), path, "rmdir", rc(err), "")
	return asErrno(err)
}

func (n *node) Symlink(ctx context.Context, target, name string, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	to := n.childInternal(name)
	rel := n.childRel(name)

	err := unix.Symlinkat(target, n.v.rootHandle, rel)
	openvfsfuseLog(callerOf(ctx), to, "symlink", rc(err), "symlink from %s to %s", target, to)

	if err != nil {
		return nil, asErrno(err)
	}
	n.lchownToCaller(ctx, rel)

	st, err := n.lstat(rel)
	if err != nil {
		return nil, asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return n.newChild(ctx, rel, st), 0
}

func (n *node) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *gofuse.EntryOut) (*fs.Inode, syscall.Errno) {
	other, ok := target.(*node)
	if !ok {
		return nil, syscall.EINVAL
	}
	to := n.childInternal(name)
	rel := n.childRel(name)

	err := unix.Linkat(other.v.rootHandle, other.relPath(), n.v.rootHandle, rel, 0)
	openvfsfuseLog(callerOf(ctx), to, "link", rc(err), "hard link from %s to %s", other.internal(), to)

	if err != nil {
		return nil, asErrno(err)
	}
	n.lchownToCaller(ctx, rel)

	st, err := n.lstat(rel)
	if err != nil {
		return nil, asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return n.newChild(ctx, rel, st), 0
}

func (n *node) Rename(ctx context.Context, oldName string, newParent *fs.Inode, newName string, flags uint32) syscall.Errno {
	other, ok := newParent.Operations().(*node)
	if !ok {
		return syscall.EINVAL
	}
	// Upstream discards the flags and always does a plain rename(2).
	_ = flags
	from := n.childInternal(oldName)
	to := other.childInternal(newName)

	err := unix.Renameat(n.v.rootHandle, n.childRel(oldName), other.v.rootHandle, other.childRel(newName))
	openvfsfuseLog(callerOf(ctx), from, "rename", rc(err), "rename %s to %s", from, to)

	return asErrno(err)
}

// Setattr covers upstream's chmod, chown, truncate and utimens callbacks, which
// the node API funnels into one entry point.
func (n *node) Setattr(ctx context.Context, f fs.FileHandle, in *gofuse.SetAttrIn, out *gofuse.AttrOut) syscall.Errno {
	path := n.internal()
	rel := n.relPath()
	caller := callerOf(ctx)

	if mode, ok := in.GetMode(); ok {
		err := unix.Fchmodat(n.v.rootHandle, rel, mode, 0)
		openvfsfuseLog(caller, path, "chmod", rc(err), "chmod to %o", mode)
		if err != nil {
			return asErrno(err)
		}
	}

	uid, hasUID := in.GetUID()
	gid, hasGID := in.GetGID()
	if hasUID || hasGID {
		u, g := -1, -1
		if hasUID {
			u = int(uid)
		}
		if hasGID {
			g = int(gid)
		}
		err := unix.Fchownat(n.v.rootHandle, rel, u, g, unix.AT_SYMLINK_NOFOLLOW)
		username, userOK := getusername(uint32(u))
		groupname, groupOK := getgroupname(uint32(g))
		if userOK && groupOK {
			openvfsfuseLog(caller, path, "chown", rc(err), "chown to %d:%d %s:%s", u, g, username, groupname)
		} else {
			openvfsfuseLog(caller, path, "chown", rc(err), "chown to %d:%d", u, g)
		}
		if err != nil {
			return asErrno(err)
		}
	}

	if size, ok := in.GetSize(); ok {
		// f is set when the truncation goes through an open descriptor and nil
		// for a path-based truncate(). Truncating through a descriptor must not
		// be re-checked against the file's mode, so use it when it is there.
		var err error
		if vf, isVFS := f.(*vfsFile); isVFS {
			err = unix.Ftruncate(vf.fd, int64(size))
		} else {
			fd, openErr := unix.Openat(n.v.rootHandle, rel, unix.O_WRONLY|unix.O_CLOEXEC, 0)
			if openErr != nil {
				err = openErr
			} else {
				err = unix.Ftruncate(fd, int64(size))
				unix.Close(fd)
			}
		}
		if err != nil {
			openvfsfuseLog(caller, path, "truncate", -1, "truncate to %d bytes", size)
			return asErrno(err)
		}
		openvfsfuseLog(caller, path, "truncate", 0, "truncate to %d bytes", size)
	}

	atime, hasATime := in.GetATime()
	mtime, hasMTime := in.GetMTime()
	if hasATime || hasMTime {
		// UTIME_OMIT for a time the kernel did not send, as libfuse passes on.
		ts := []unix.Timespec{
			{Sec: 0, Nsec: unixUtimeOmit},
			{Sec: 0, Nsec: unixUtimeOmit},
		}
		if hasATime {
			ts[0] = unix.NsecToTimespec(atime.UnixNano())
		}
		if hasMTime {
			ts[1] = unix.NsecToTimespec(mtime.UnixNano())
		}
		err := unix.UtimesNanoAt(n.v.rootHandle, rel, ts, unix.AT_SYMLINK_NOFOLLOW)
		openvfsfuseLog(caller, path, "utimens", rc(err), "")
		if err != nil {
			return asErrno(err)
		}
	}

	st, err := n.lstat(rel)
	if err != nil {
		return asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return 0
}

func (n *node) Statfs(ctx context.Context, out *gofuse.StatfsOut) syscall.Errno {
	path := n.internal()
	var st unix.Statfs_t
	err := unix.Fstatfs(n.v.rootHandle, &st)
	openvfsfuseLog(callerOf(ctx), path, "statfs", rc(err), "")

	if err != nil {
		return asErrno(err)
	}
	fillStatfs(out, &st)
	return 0
}

// vfsFile is the per-open state. LoopbackFile already forwards read, write,
// flush, fsync and the rest to the descriptor the way upstream's pread/pwrite
// and release callbacks do; the descriptor is kept alongside it so Setattr can
// mirror the ftruncate() path.
type vfsFile struct {
	*fs.LoopbackFile
	fd int
}

func newVFSFile(fd int) *vfsFile {
	return &vfsFile{LoopbackFile: fs.NewLoopbackFileFromOS(os.NewFile(uintptr(fd), "")), fd: fd}
}

func (n *node) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	path := n.internal()
	rel := n.relPath()
	caller := callerOf(ctx)

	opener := getcallername(caller)

	openvfsfuseLog(caller, path, "open", 0, "%d %s by %s", flags, path, opener)

	var callerPid uint32
	if caller != nil {
		callerPid = caller.Pid
	}

	// The desktop client must not be blocked from accessing the file
	// to be able to overwrite it.
	if callerBelongsToProcess(callerPid, n.v.jobs.DesktopClientPID()) {
		openvfsfuseLog(caller, path, "open", 0, "Desktop client tries to access, bypassing!")
	} else {
		attribs, haveAttribs := placeHolderFromAttributesAt(n.v.rootHandle, rel, n.external())
		if haveAttribs && attribs.State == StateDehydrated {
			// the file is virtual. It will be hydrated if the calling instance
			// is not on the ignore list
			if errno := n.v.requestHydration(caller, path, attribs); errno != 0 {
				return nil, 0, syscall.Errno(errno)
			}
		}
	}
	// File is not dehydrated and it is just going to be opened
	fd, err := unix.Openat(n.v.rootHandle, rel, int(flags)|unix.O_CLOEXEC, 0)

	if err != nil {
		return nil, 0, asErrno(err)
	}

	return newVFSFile(fd), 0, 0
}

func (n *node) Create(ctx context.Context, name string, flags uint32, mode uint32, out *gofuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	path := n.childInternal(name)
	rel := n.childRel(name)
	caller := callerOf(ctx)

	fd, err := unix.Openat(n.v.rootHandle, rel, int(flags)|unix.O_CLOEXEC, mode)
	if err != nil {
		openvfsfuseLog(caller, path, "create", -1, "create %o", mode)
		return nil, nil, 0, asErrno(err)
	}

	if caller != nil {
		if chownErr := unix.Fchown(fd, int(caller.Uid), int(caller.Gid)); chownErr != nil {
			cleanupErr := unix.Unlinkat(n.v.rootHandle, rel, 0)
			unix.Close(fd)
			openvfsfuseLog(caller, path, "create", -1, "set ownership after create %o", mode)
			if cleanupErr != nil {
				openvfsfuseLog(caller, path, "create", -1, "failed to remove incomplete file: errno %d", errnoValue(cleanupErr))
			}
			return nil, nil, 0, asErrno(chownErr)
		}
	}
	openvfsfuseLog(caller, path, "create", 0, "create %o", mode)

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, nil, 0, asErrno(err)
	}
	statAttr(&out.Attr, &st)
	return n.newChild(ctx, rel, st), newVFSFile(fd), 0, 0
}

// Release logs the close; the descriptor itself is closed by the file handle,
// which is what upstream's close(fi->fh) does.
func (n *node) Release(ctx context.Context, f fs.FileHandle) syscall.Errno {
	openvfsfuseLog(callerOf(ctx), n.internal(), "close", 0, "")
	if r, ok := f.(fs.FileReleaser); ok {
		return r.Release(ctx)
	}
	return 0
}

func (n *node) Fsync(ctx context.Context, f fs.FileHandle, flags uint32) syscall.Errno {
	openvfsfuseLog(callerOf(ctx), n.internal(), "fsync", 0, "")

	// TODO:: immplement
	return 0
}

/* xattr operations are optional and can safely be left unimplemented */

func (n *node) Setxattr(ctx context.Context, attr string, data []byte, flags uint32) syscall.Errno {
	return asErrno(xattrSetAt(n.v.rootHandle, n.relPath(), attr, data, int(flags)))
}

func (n *node) Getxattr(ctx context.Context, attr string, dest []byte) (uint32, syscall.Errno) {
	/**
	 * if size is zero, getxattr returns the required size of the buffer.
	 * See https://linux.die.net/man/2/getxattr
	 */
	copyDataResult := func(s []byte) (uint32, syscall.Errno) {
		if len(dest) == 0 {
			return uint32(len(s)), 0
		} else if len(s) <= len(dest) {
			copy(dest, s)
			return uint32(len(s)), 0
		}
		return 0, syscall.ERANGE
	}

	path := n.internal()
	rel := n.relPath()
	if attr == XAttrRealSize {
		st, err := n.lstat(rel)
		if err != nil {
			return 0, asErrno(err)
		}
		return copyDataResult([]byte(strconv.FormatInt(st.Size, 10)))
	}
	ret, err := xattrGetAt(n.v.rootHandle, rel, attr, dest)
	if isErrNoAttr(err) && attr == XAttrData {
		// return the default value for the attributes
		openvfsfuseLog(callerOf(ctx), path, "getxattr", 0, "Returning default data for %s", attr)
		return copyDataResult(DefaultData())
	}
	if err != nil {
		return 0, asErrno(err)
	}
	return uint32(ret), 0
}

func (n *node) Listxattr(ctx context.Context, dest []byte) (uint32, syscall.Errno) {
	ret, err := xattrListAt(n.v.rootHandle, n.relPath(), dest)
	if err != nil {
		return 0, asErrno(err)
	}
	return uint32(ret), 0
}

func (n *node) Removexattr(ctx context.Context, attr string) syscall.Errno {
	return asErrno(xattrRemoveAt(n.v.rootHandle, n.relPath(), attr))
}

// mountAndServe is the fuse_main() call at the end of initializeOpenVFSFuse().
func mountAndServe(ctx context.Context, c *VFSFuseContext, args Args, report func(error) error) error {
	// use the system inodes instead of the libfuse generated, and do not let
	// the kernel cache anything: openVFSfuse_init()'s fuse_config.
	zero := time.Duration(0)
	opts := &fs.Options{
		MountOptions: gofuse.MountOptions{
			Name:  "openvfs",
			Debug: args.DebugEnabled,
		},
		EntryTimeout:    &zero,
		AttrTimeout:     &zero,
		NegativeTimeout: &zero,
	}

	server, err := fs.Mount(args.MountPoint, &node{v: c}, opts)
	if err != nil {
		return report(err)
	}
	_ = report(nil)
	if ctx != nil {
		go func() {
			<-ctx.Done()
			_ = server.Unmount()
		}()
	}
	server.Wait()
	return nil
}
