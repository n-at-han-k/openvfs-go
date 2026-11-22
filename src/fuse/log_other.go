//go:build !linux

package fuse

// Upstream guards the syslog() call with #ifndef __APPLE__, so there is no
// system log on macOS. Windows has no syslog either.

func logToSyslog(message string) {}
