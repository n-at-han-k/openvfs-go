//go:build darwin

package main

import (
	"context"
	"io"

	"example.com/project/packages/openvfs/unix/darwin"
)

// Importing the OS package is what installs the platform half of the backend.

type backendArgs = darwin.Args

func processArgs(argv []string, stdout, stderr io.Writer) (backendArgs, bool) {
	return darwin.ProcessArgs(argv, stdout, stderr)
}

func initialize(ctx context.Context, args backendArgs, ready chan<- error) error {
	return darwin.Initialize(ctx, args, ready)
}
