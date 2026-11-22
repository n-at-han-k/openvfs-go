//go:build darwin

// Package darwin is the OpenVFS filesystem on macOS: the openvfsfuse layer,
// which mounts over the sync folder and answers the kernel's filesystem calls.
//
// It supplies the macOS half of the unix backend -- no-follow xattrs through a
// descriptor, KERN_PROCARGS2 for process identity, the fchdir fallback for
// mknod and mkfifo, and the 16 bit st_mode -- and exposes the entry points the
// openvfs package dispatches to.
package darwin

import (
	"context"
	"log"
	"os"

	"example.com/project/packages/openvfs/unix"
	"example.com/project/src/directory"
)

func init() {
	unix.SetPlatform(unix.Platform{
		XattrGet:                xattrGet,
		XattrSet:                xattrSet,
		XattrList:               xattrList,
		XattrRemove:             xattrRemove,
		IsErrNoAttr:             isErrNoAttr,
		CallerName:              getcallernameByPid,
		CallerIsThreadOfProcess: callerIsThreadOfProcess,
		StatMode:                statMode,
		FillStatfs:              fillStatfs,
		MknodAt:                 mknodAt,
		MkfifoAt:                mkfifoAt,
		LogToSyslog:             logToSyslog,
	})
}

// Run registers the sync root, mounts it and serves until ctx is cancelled.
func Run(ctx context.Context, root, owner, socketPath string, onChange func(directory.Event)) {
	report(unix.Run(ctx, root, owner, socketPath, drain("openvfs")))
}

// ServeAPI serves the socket API the filesystem talks to.
func ServeAPI(ctx context.Context, socketPath string, onChange func(directory.Event)) {
	report(unix.ServeAPI(ctx, socketPath, drain("openvfs socket api")))
}

// Main is the openvfsfuse daemon's entry point.
func Main(ctx context.Context, argv []string) int {
	args, ok := unix.ProcessArgs(argv, os.Stdout, os.Stderr)
	if !ok {
		return -1
	}
	if err := unix.InitializeOpenVFSFuse(ctx, args, drain("openvfs")); err != nil {
		return -1
	}
	return 0
}

// RunStat is the vfsstat entry point.
func RunStat(argv []string) int { return unix.RunStat(argv) }

// drain reports the outcome the mount publishes once it is up, which arrives
// out of band because the mount call itself blocks.
func drain(what string) chan<- error {
	ready := make(chan error, 1)
	go func() {
		if err := <-ready; err != nil {
			log.Printf("%s: %v", what, err)
		}
	}()
	return ready
}

func report(err error) {
	if err != nil {
		log.Printf("openvfs: %v", err)
	}
}
