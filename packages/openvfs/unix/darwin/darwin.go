//go:build darwin

// Package darwin is the OpenVFS backend on macOS: the openvfsfuse layer, which
// mounts over the sync folder and answers the kernel's filesystem calls.
//
// It owns the macOS half of the backend -- no-follow xattrs via a descriptor,
// KERN_PROCARGS2 for process identity, the fchdir fallback for mknod and
// mkfifo, the 16 bit st_mode and no syslog -- installed into the unix package,
// which holds the parts the two Unixes share verbatim (upstream builds one
// libfuse implementation for both).
package darwin

import (
	"context"

	"example.com/project/packages/openvfs"
	"example.com/project/packages/openvfs/unix"
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

// Args is the parsed command line of the openvfsfuse daemon.
type Args = openvfs.Args

// ProcessArgs is processArgs() of src/openvfsfuse/main.cpp.
var ProcessArgs = openvfs.ProcessArgs

// ServeAPI is the standalone process' side of the socket API.
var ServeAPI = openvfs.ServeAPI

// Main is main() of src/openvfsfuse/main.cpp.
func Main(argv []string) int { return unix.Main(argv) }

// Initialize is initializeOpenVFSFuse(): it checks the registration, starts the
// socket thread and mounts.
func Initialize(ctx context.Context, args Args, ready chan<- error) error {
	return unix.InitializeOpenVFSFuse(ctx, args, ready)
}

// Run registers the sync root and mounts it in one call.
func Run(ctx context.Context, root, owner, socketPath string, ready chan<- error) error {
	return unix.Run(ctx, root, owner, socketPath, ready)
}

// RunStat is main() of src/vfsstat/main.cpp.
func RunStat(argv []string) int { return unix.RunStat(argv) }
