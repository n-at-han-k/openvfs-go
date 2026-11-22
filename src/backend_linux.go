//go:build linux

package main

import (
	"context"
	"log"

	"example.com/project/packages/openvfs/unix/linux"
	"example.com/project/src/directory"
)

// Importing the OS package is what installs the platform half of the backend.

func serveAPI(ctx context.Context, socketPath string, onChange func(directory.Event)) {
	ready := make(chan error, 1)
	if err := linux.ServeAPI(ctx, socketPath, ready); err != nil {
		log.Printf("socket api: %v", err)
	}
	if err := <-ready; err != nil {
		log.Printf("socket api: %v", err)
	}
}

func runBackend(ctx context.Context, root, owner, socketPath string, onChange func(directory.Event)) {
	ready := make(chan error, 1)
	if err := linux.Run(ctx, root, owner, socketPath, ready); err != nil {
		log.Printf("openvfs: %v", err)
	}
	if err := <-ready; err != nil {
		log.Printf("openvfs: %v", err)
	}
}
