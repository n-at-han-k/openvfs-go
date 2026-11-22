// Package openvfs runs the OpenVFS virtual filesystem for the current
// platform. On Linux and macOS that is the openvfsfuse layer, which mounts over
// the sync folder; on Windows it is the cloud filter provider.
//
// Everything platform specific is chosen at build time by the run_* files, so
// callers never select a backend themselves.
package openvfs

import (
	"context"

	"example.com/project/src/directory"
)

// Run serves the sync folder at root until ctx is cancelled, reporting what
// happens to it through onChange.
//
// It mirrors directory.RunWatcher and directory.RunPoller so that a caller can
// start all three the same way and funnel every event into one channel.
func Run(ctx context.Context, root, owner, socketPath string, onChange func(directory.Event)) {
	run(ctx, root, owner, socketPath, onChange)
}

// ServeAPI serves the socket API the virtual filesystem talks to, until ctx is
// cancelled. It is the side of the protocol the desktop client implements.
func ServeAPI(ctx context.Context, socketPath string, onChange func(directory.Event)) {
	serveAPI(ctx, socketPath, onChange)
}

// Main is the openvfs daemon's entry point: it parses the upstream command
// line and serves the mount until ctx is cancelled. It returns the process exit
// code, which is -1 on any failure as upstream's main() is.
func Main(ctx context.Context, argv []string) int { return mainCmd(ctx, argv) }

// RunStat is the vfsstat entry point: it reports the placeholder state of a
// file or of every entry in a directory. It returns the process exit code.
func RunStat(argv []string) int { return runStat(argv) }
