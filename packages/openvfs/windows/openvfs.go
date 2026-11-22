//go:build windows

package windows

import (
	"context"
	"log"

	"example.com/project/src/directory"
)

// The entry points the openvfs package dispatches to. The cloud filter has no
// mount and no socket API of its own: Windows is the provider, and the plugin
// in this package is driven by the desktop client instead.

// Run reports that the fuse filesystem has no Windows counterpart.
func Run(ctx context.Context, root, owner, socketPath string, onChange func(directory.Event)) {
	log.Printf("openvfs: the fuse filesystem is not available on Windows; the cloud filter is the provider")
}

// ServeAPI reports that the socket API has no Windows counterpart.
func ServeAPI(ctx context.Context, socketPath string, onChange func(directory.Event)) {
	log.Printf("openvfs: the socket API is not available on Windows; the cloud filter has no such protocol")
}

// Main reports that the openvfsfuse daemon has no Windows counterpart.
func Main(ctx context.Context, argv []string) int {
	log.Printf("openvfs: the fuse daemon is not available on Windows")
	return -1
}

// RunStat reports that vfsstat has no Windows counterpart, because placeholder
// state there belongs to the cloud filter rather than to extended attributes.
func RunStat(argv []string) int {
	log.Printf("openvfs_stat: placeholder state on Windows is owned by the cloud filter")
	return -1
}
