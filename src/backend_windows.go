//go:build windows

package main

import (
	"context"
	"log"

	"example.com/project/src/directory"
)

// The openvfs backend is Unix only; on Windows the cloud filter provider in
// packages/windows takes its place.

func serveAPI(ctx context.Context, socketPath string, onChange func(directory.Event)) {
	log.Print("the openvfs socket API is not available on Windows")
}

func runBackend(ctx context.Context, root, owner, socketPath string, onChange func(directory.Event)) {
	log.Print("the openvfs backend is not available on Windows")
}
