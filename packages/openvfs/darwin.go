//go:build darwin

package openvfs

import "example.com/project/packages/openvfs/unix/darwin"

// On macOS the filesystem is openvfsfuse, in packages/openvfs/unix/darwin.

var (
	run      = darwin.Run
	serveAPI = darwin.ServeAPI
	mainCmd  = darwin.Main
	runStat  = darwin.RunStat
)
