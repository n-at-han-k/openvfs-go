//go:build linux

package main

import (
	"context"
	"io"

	"example.com/project/packages/openvfs/unix/linux"
)

// Importing the OS package is what installs the platform half of the backend.

type backendArgs = linux.Args

func processArgs(argv []string, stdout, stderr io.Writer) (backendArgs, bool) {
	return linux.ProcessArgs(argv, stdout, stderr)
}

func initialize(ctx context.Context, args backendArgs, ready chan<- error) error {
	return linux.Initialize(ctx, args, ready)
}
