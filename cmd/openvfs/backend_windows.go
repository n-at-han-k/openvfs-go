//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// The openvfsfuse daemon is the Unix backend; upstream builds it only there.
// On Windows the cloud filter is the provider, so there is nothing to mount.

type backendArgs = struct{}

func processArgs(argv []string, stdout, stderr io.Writer) (backendArgs, bool) {
	fmt.Fprintln(stderr, "openvfs is not available on Windows; use the cloud filter backend")
	return backendArgs{}, false
}

func initialize(ctx context.Context, args backendArgs, ready chan<- error) error {
	return errors.New("openvfs is not available on Windows")
}
