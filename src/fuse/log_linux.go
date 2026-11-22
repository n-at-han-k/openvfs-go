package fuse

import (
	"log/syslog"
	"sync"
)

// Upstream mirrors every log line into syslog(LOG_INFO, ...) on everything but
// Apple.

var (
	syslogOnce   sync.Once
	syslogWriter *syslog.Writer
)

func logToSyslog(message string) {
	syslogOnce.Do(func() {
		syslogWriter, _ = syslog.New(syslog.LOG_INFO|syslog.LOG_USER, "openvfsfuse")
	})
	if syslogWriter != nil {
		_ = syslogWriter.Info(message)
	}
}
