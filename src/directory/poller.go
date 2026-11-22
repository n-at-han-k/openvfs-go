package directory

import (
	"context"
	"time"
)

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
