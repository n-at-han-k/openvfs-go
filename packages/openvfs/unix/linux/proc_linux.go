package linux

import (
	"fmt"
	"os"
	"strconv"
)

// The Linux arm of getcallername() and callerBelongsToProcess().

// getcallernameByPid returns the name of the process which accessed the file
// system.
func getcallernameByPid(pid uint32) string {
	name, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to locate process name for: %d error: %v\n", pid, err)
		return "unknown: pid: " + strconv.FormatUint(uint64(pid), 10)
	}
	return name
}

// callerIsThreadOfProcess answers whether a caller thread id belongs to the
// given process.
//
// A thread of the client is visible as /proc/<pid>/task/<tid>, so one stat()
// answers the question without reading or parsing anything.
func callerIsThreadOfProcess(callerThreadID uint32, processID int64) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d/task/%d", processID, callerThreadID))
	return err == nil
}
