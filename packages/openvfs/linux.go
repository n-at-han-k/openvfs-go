//go:build linux

package openvfs

import "example.com/project/packages/openvfs/unix/linux"

// On Linux the filesystem is openvfsfuse, in packages/openvfs/unix/linux.

var (
	run      = linux.Run
	serveAPI = linux.ServeAPI
	mainCmd  = linux.Main
	runStat  = linux.RunStat
)
