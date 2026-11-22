//go:build windows

package main

import "fmt"

// Upstream builds vfsstat only where the openvfs backend runs; on Windows the
// placeholder state belongs to the cloud filter, not to the user.openvfs.*
// attributes this tool reports.
func runStat(argv []string) int {
	fmt.Println("openvfs_stat is not available on Windows; placeholder state is owned by the cloud filter")
	return -1
}
