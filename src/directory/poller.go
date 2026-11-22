package directory

import (
	"context"
	"time"
)

// RunPoller emits a poll event on every tick through the same callback the
// watcher uses. It does not scan; main.go handles the event and starts it.
func RunPoller(ctx context.Context, interval time.Duration, onChange func(Event)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			onChange(Event{Kind: Poll})
		}
	}
}
