package fuse

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// The Apple arm of getcallername() and callerBelongsToProcess().

// getcallernameByPid returns the name of the process which accessed the file
// system.
//
// Upstream calls proc_pidpath() from libproc, which needs cgo. KERN_PROCARGS2
// answers the same question through sysctl: the buffer opens with a 4 byte
// argc followed by the NUL terminated executable path.
func getcallernameByPid(pid uint32) string {
	buf, err := unix.SysctlRaw("kern.procargs2", int(pid))
	if err == nil && len(buf) > 4 {
		if end := bytes.IndexByte(buf[4:], 0); end > 0 {
			return string(buf[4 : 4+end])
		}
	}
	if err == nil {
		err = unix.EINVAL
	}
	fmt.Fprintf(os.Stderr, "Failed to locate process name for: %d error: %v\n", pid, err)
	return "unknown: pid: " + strconv.FormatUint(uint64(pid), 10)
}

// callerIsThreadOfProcess answers whether a caller thread id belongs to the
// given process. There is no /proc on macOS, so only the direct comparison the
// caller already made applies, which is what upstream does under __APPLE__.
func callerIsThreadOfProcess(callerThreadID uint32, processID int64) bool {
	return false
}
