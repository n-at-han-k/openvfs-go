//go:build windows

package main

import (
	"context"
	"errors"
)

// The openvfs backend is Unix only; on Windows the cloud filter provider in
// packages/windows takes its place.

func serveAPI(ctx context.Context, socketPath string, ready chan<- error) error {
	err := errors.New("the openvfs socket API is not available on Windows")
	if ready != nil {
		ready <- err
	}
	return err
}

func runBackend(ctx context.Context, root, owner, socketPath string, ready chan<- error) error {
	return errors.New("the openvfs backend is not available on Windows")
}
