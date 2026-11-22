//go:build linux || darwin

package unix

import (
	"example.com/project/packages/openvfs"

	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Transpiled from src/openvfsfuse/openvfsfuse.{h,cpp}. The parts that talk to
// libfuse live in openvfsfuse_unix.go and openvfsfuse_windows.go.
//
// This work is based on the nice work of Rémi Flament - remipouak at gmail.com
// called loggedFS: https://github.com/rflament/loggedfs

// VFSFuseContext holds the state the fuse callbacks share. Upstream reaches it
// through a singleton because the libfuse callbacks are free functions; here
// every node carries a pointer to it, and the singleton exists only for the
// logging helper, which upstream also reaches statically.
type VFSFuseContext struct {
	mountPoint            string
	owner                 string
	rootHandle            int
	appsNoHydrateFull     []string
	appsNoHydrateEndsWith []string
	hydrationTimeout      time.Duration
	debugEnabled          bool

	jobs         *openvfs.SharedMap
	socketThread *openvfs.SocketThread
	// FUSE dispatches requests from several threads, so the transfer id needs
	// to be atomic.
	transferID atomic.Int32
}

var instance atomic.Pointer[VFSFuseContext]

func NewVFSFuseContext(args openvfs.Args) (*VFSFuseContext, error) {
	// The root handle is opened before mounting, so a path resolved against it
	// reaches the directory below the mount rather than the mount.
	handle, err := openRootHandle(args.MountPoint)
	if err != nil {
		return nil, err
	}
	c := &VFSFuseContext{
		mountPoint:            args.MountPoint,
		owner:                 args.Owner,
		rootHandle:            handle,
		appsNoHydrateFull:     args.AppsNoHydrateFull,
		appsNoHydrateEndsWith: args.AppsNoHydrateEndsWith,
		hydrationTimeout:      args.HydrationTimeout,
		debugEnabled:          args.DebugEnabled,
		jobs:                  openvfs.NewSharedMap(),
	}
	c.transferID.Store(12)
	instance.Store(c)
	return c, nil
}

func (c *VFSFuseContext) Close() {
	instance.CompareAndSwap(c, nil)
	closeRootHandle(c.rootHandle)
}

/*
 * We are an overlay file system, using the same path internally and externally.
 * To trick fuse and prevent it from intercepting our calls to the underlying
 * file system, upstream chdirs to the root and uses relative paths internally.
 * Resolving the same relative path against the root handle does this without
 * the process-wide side effect, so getInternalPath only produces the name the
 * operation is logged under.
 */
func (c *VFSFuseContext) getInternalPath(path string) string {
	return "." + path
}

func (c *VFSFuseContext) getExternalPath(path string) string {
	// ./foo
	if !filepath.IsAbs(path) {
		// openvfs.Join cleans the result, where upstream's operator/ leaves the "/./"
		// in place; both name the same file.
		return filepath.Join(c.mountPoint, path)
	}
	// original_path /foo
	return c.mountPoint + path
}

func (c *VFSFuseContext) MountPoint() string { return c.mountPoint }

func (c *VFSFuseContext) Owner() string { return c.owner }

func (c *VFSFuseContext) blockedToOpen(app string) bool {
	for _, name := range c.appsNoHydrateFull {
		if app == name {
			return true
		}
	}
	for _, suffix := range c.appsNoHydrateEndsWith {
		if len(app) >= len(suffix) && app[len(app)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

func (c *VFSFuseContext) DebugEnabled() bool { return c.debugEnabled }

// HydrationTimeout is how long open() waits for the desktop client to hydrate
// a file.
func (c *VFSFuseContext) HydrationTimeout() time.Duration { return c.hydrationTimeout }

// callerInfo is fuse_get_context(): who asked for this operation. Upstream
// reads it from libfuse's thread local state; the Go bindings carry it on the
// request instead, so it is passed down explicitly.
type callerInfo struct {
	Pid uint32
	Uid uint32
	Gid uint32
}

// callerBelongsToProcess answers whether a fuse caller is a thread of the
// desktop client.
//
// FUSE reports the *thread* id of the caller in fuse_context::pid, while the
// desktop client announces its process id over the socket API. A client that
// writes the content it downloaded from a worker thread therefore fails the
// identity check in openVFSfuse_open(), and its own write is treated as a
// foreign access that needs hydrating -- which deadlocks the very hydration it
// is answering.
//
// Asking the OS whether the thread belongs to the process is left to
// callerIsThreadOfProcess, which differs per platform.
func callerBelongsToProcess(callerThreadID uint32, processID int64) bool {
	if callerThreadID == 0 || processID <= 0 {
		return false
	}
	if int64(callerThreadID) == processID {
		return true
	}
	return callerIsThreadOfProcess(callerThreadID, processID)
}

// getcallername returns the name of the process which accessed the file system.
func getcallername(caller *callerInfo) string {
	if caller == nil || caller.Pid == 0 {
		return "pid: 0"
	}
	return getcallernameByPid(caller.Pid)
}

// openvfsfuseLog is openvfsfuse_log().
func openvfsfuseLog(caller *callerInfo, path, action string, returncode int, format string, v ...any) {
	c := instance.Load()
	if c == nil || !c.DebugEnabled() {
		return
	}
	// FIXME - whitelist of pathes that are not logged at all?
	if path == "./.OpenCloudSync.log" {
		return
	}

	buf := fmt.Sprintf(format, v...)
	successCode := "❌"
	if returncode >= 0 {
		successCode = "✅"
	}
	var message string
	if caller != nil {
		message = fmt.Sprintf("[ pid = %d %s uuid = %d] %s %s %s %s",
			caller.Pid, getcallername(caller), caller.Uid, action, buf, path, successCode)
	} else {
		message = fmt.Sprintf("[ openvfsfuse ] %s %s %s %s", action, buf, path, successCode)
	}
	fmt.Println(message)
	logToSyslog(message)
}

// rc maps an error to the returncode upstream passes to the logger: the syscall
// result, which is -1 on failure and 0 (or better) otherwise.
func rc(err error) int {
	if err != nil {
		return -1
	}
	return 0
}

// requestHydration is the body of openVFSfuse_open() between the placeholder
// check and the open(): ask the desktop client to hydrate the file and block
// until it answers. It returns the errno to fail the open with, or 0.
//
// It is shared by every platform's open callback, because the protocol is the
// part that does not depend on the fuse binding.
func (c *VFSFuseContext) requestHydration(caller *callerInfo, path string, attribs PlaceHolderAttributes) int {
	// ignore list of apps that must not cause a hydration
	opener := getcallername(caller)
	if c.blockedToOpen(opener) {
		openvfsfuseLog(caller, path, "open", 0, "blocked hydration for not permitted app %s", opener)
		return errPerm
	}

	// Create message to send to worker thread 1
	msgData := &openvfs.MsgData{
		Msg:       "V2/HYDRATE_FILE",
		File:      c.getExternalPath(attribs.AbsolutePath),
		FileID:    attribs.FileID,
		Requester: opener,
		// attention: transferID is global and can change!
		ID: int(c.transferID.Add(1)),
	}

	openvfsfuseLog(caller, path, "open", 0, "Requesting hydration %s: %d", path, msgData.ID)

	// Register the job *before* handing the request over. PostMsg() only
	// queues, so the socket thread may well have sent the request and received
	// the answer before this thread runs again. A waiter must never be able to
	// observe the absence of a job it just posted.
	c.jobs.Insert(msgData.ID, openvfs.HydJob{State: openvfs.HydRunning})

	// push hydration request to the thread that handles the communication to the client
	if !c.socketThread.PostMsg(msgData) {
		c.jobs.Remove(msgData.ID)
		openvfsfuseLog(caller, path, "open", 0, "Could not queue hydration request %d, shutting down", msgData.ID)
		return errIO
	}

	// The socketThread now talks to the client, which downloads the file for
	// us. Block here until the client answers or the timeout expires.
	result := c.jobs.WaitForJob(msgData.ID, c.HydrationTimeout())

	// remove the job regardless of the result
	c.jobs.Remove(msgData.ID)

	switch result {
	case openvfs.HydResultSucceeded:
		openvfsfuseLog(caller, path, "open", 0, "Successfully finished job %d", msgData.ID)
		return 0
	case openvfs.HydResultFailed:
		openvfsfuseLog(caller, path, "open", 0, "Failed job %d", msgData.ID)
		return errIO
	case openvfs.HydResultTimedOut:
		openvfsfuseLog(caller, path, "open", 0, "TIMEOUT - no answer from client for job %d", msgData.ID)
		return errTimedOut
	case openvfs.HydResultLost:
		openvfsfuseLog(caller, path, "open", 0, "Job queue does not have job %d", msgData.ID)
		return errIO
	}
	return 0
}

// InitializeOpenVFSFuse is initializeOpenVFSFuse(). fuse_main() blocks until
// the mount is torn down, so ctx stands in for the signal that unmounts it and
// ready reports the outcome of the setup to a caller that mounted in a
// goroutine. Both are nil in the standalone path.
func InitializeOpenVFSFuse(ctx context.Context, openVFSArgs openvfs.Args, ready chan<- error) error {
	report := func(err error) error {
		if ready != nil {
			select {
			case ready <- err:
			default:
			}
		}
		return err
	}

	if !platformInstalled() {
		return report(errNoPlatform)
	}

	contextInstance, err := NewVFSFuseContext(openVFSArgs)
	if err != nil {
		return report(err)
	}
	defer contextInstance.Close()

	// First, check if the mount point has a xattr that shows that it's ours
	if info := RegistrationFromAttributes(contextInstance.MountPoint()); info.IsOK() {
		if info.Owner != contextInstance.Owner() {
			err := fmt.Errorf("mount point %s is not owned by %s", contextInstance.MountPoint(), contextInstance.Owner())
			fmt.Fprintln(os.Stderr, err)
			return report(err)
		}
	} else {
		err := fmt.Errorf("no registration info found for mount point %s %s", contextInstance.MountPoint(), info.Error)
		fmt.Fprintln(os.Stderr, err)
		return report(err)
	}

	setUmaskZero()

	if openVFSArgs.SocketPath == "" {
		// upstream asserts
		return report(fmt.Errorf("no socket path given"))
	}
	contextInstance.socketThread = openvfs.NewSocketThread("openvfs.SocketThread", contextInstance.jobs)
	if !contextInstance.socketThread.CreateThread(openVFSArgs.SocketPath) {
		fmt.Fprintln(os.Stderr, "Unable to create the socket thread")
		return report(fmt.Errorf("unable to create the socket thread"))
	}
	defer contextInstance.socketThread.ExitThread()

	fmt.Printf("openVFSfuse starting: PID:%d", os.Getpid())
	fmt.Printf(", Socket:%s\n", openVFSArgs.SocketPath)
	fmt.Print("fuse command: ")
	for _, s := range openVFSArgs.FuseArgv {
		fmt.Print(s + " ")
	}
	fmt.Println()

	openvfsfuseLog(nil, "/path", "_init", 1, "**** INIT called")

	if err := mountAndServe(ctx, contextInstance, openVFSArgs, report); err != nil {
		return err
	}

	fmt.Println("openVFSfuse closing.")
	return nil
}

// Main is main() of src/openvfsfuse/main.cpp.
func Main(argv []string) int {
	if openvfsfuseArgs, ok := openvfs.ProcessArgs(argv, os.Stdout, os.Stderr); ok {
		fmt.Printf("openVFSfuse starting at%s.\n", openvfsfuseArgs.MountPoint)
		if err := InitializeOpenVFSFuse(context.Background(), openvfsfuseArgs, nil); err != nil {
			return -1
		}
		return 0
	}
	return -1
}

// Run has no upstream counterpart: it registers the root and mounts it in one
// call, which is what the Go daemon in src/main.go needs.
func Run(ctx context.Context, root, owner, socketPath string, ready chan<- error) error {
	args, ok := openvfs.ProcessArgs([]string{"openvfs", "-o", owner, "-s", socketPath, root}, os.Stdout, os.Stderr)
	if !ok {
		return os.ErrInvalid
	}
	if info := RegisterFilesystem(args.MountPoint, owner); !info.IsOK() {
		return fmt.Errorf("%s", info.Error)
	}
	if cfg, err := openvfs.ParseConfig(openvfs.DefaultConfig); err == nil {
		args.AppsNoHydrateFull = cfg.IgnoreApps.ByName
		args.AppsNoHydrateEndsWith = cfg.IgnoreApps.EndsWith
		if cfg.HydrationTimeoutSeconds > 0 {
			args.HydrationTimeout = time.Duration(cfg.HydrationTimeoutSeconds) * time.Second
		}
	}
	return InitializeOpenVFSFuse(ctx, args, ready)
}
