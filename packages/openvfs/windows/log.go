//go:build windows

package windows

import (
	"fmt"
	"log"
)

// The Q_LOGGING_CATEGORY declarations of the three cfapi translation units.
// Qt filters per category and level at runtime; Enabled does the same job for
// the categories declared with a level other than the default.

type loggingCategory struct {
	name string
}

var (
	// Q_LOGGING_CATEGORY(lcCfApi, "sync.vfs.cfapi", QtInfoMsg)
	lcCfApi = &loggingCategory{"sync.vfs.cfapi"}
	// Q_LOGGING_CATEGORY(lcCfApiWrapper, "sync.vfs.cfapi.wrapper", QtDebugMsg)
	lcCfApiWrapper = &loggingCategory{"sync.vfs.cfapi.wrapper"}
	// Q_LOGGING_CATEGORY(lcCfApiHydrationDevice, "sync.vfs.cfapi.hydrationdevice", QtDebugMsg)
	lcCfApiHydrationDevice = &loggingCategory{"sync.vfs.cfapi.hydrationdevice"}
)

func (c *loggingCategory) emit(level, format string, v ...any) {
	log.Printf("[%s] %s %s", level, c.name, fmt.Sprintf(format, v...))
}

func (c *loggingCategory) Debug(format string, v ...any)    { c.emit("debug", format, v...) }
func (c *loggingCategory) Info(format string, v ...any)     { c.emit("info", format, v...) }
func (c *loggingCategory) Warning(format string, v ...any)  { c.emit("warning", format, v...) }
func (c *loggingCategory) Critical(format string, v ...any) { c.emit("critical", format, v...) }

// logWarning is for the few places that warn outside a category.
func logWarning(format string, v ...any) {
	log.Printf("[warning] %s", fmt.Sprintf(format, v...))
}
