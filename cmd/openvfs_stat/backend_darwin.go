//go:build darwin

package main

import "example.com/project/packages/openvfs/unix/darwin"

// runStat is main() of src/vfsstat/main.cpp. Importing the OS package is what
// installs the platform half of the backend.
func runStat(argv []string) int { return darwin.RunStat(argv) }
