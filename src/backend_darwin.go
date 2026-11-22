//go:build darwin

package main

import (
	"context"

	"example.com/project/packages/openvfs/unix/darwin"
)

// Importing the OS package is what installs the platform half of the backend.

func serveAPI(ctx context.Context, socketPath string, ready chan<- error) error {
	return darwin.ServeAPI(ctx, socketPath, ready)
}

func runBackend(ctx context.Context, root, owner, socketPath string, ready chan<- error) error {
	return darwin.Run(ctx, root, owner, socketPath, ready)
}
