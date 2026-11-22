//go:build linux

package main

import (
	"context"

	"example.com/project/packages/openvfs/unix/linux"
)

// Importing the OS package is what installs the platform half of the backend.

func serveAPI(ctx context.Context, socketPath string, ready chan<- error) error {
	return linux.ServeAPI(ctx, socketPath, ready)
}

func runBackend(ctx context.Context, root, owner, socketPath string, ready chan<- error) error {
	return linux.Run(ctx, root, owner, socketPath, ready)
}
