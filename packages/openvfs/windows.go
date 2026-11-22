//go:build windows

package openvfs

import "example.com/project/packages/openvfs/windows"

// On Windows the filesystem is the cloud filter, in packages/openvfs/windows:
// Windows itself is the provider, so there is nothing to mount and no socket
// API to serve.

var (
	run      = windows.Run
	serveAPI = windows.ServeAPI
	mainCmd  = windows.Main
	runStat  = windows.RunStat
)
