package daemon

import (
	"context"
	"time"
)

// WatchWake returns a channel that fires when the system appears to have woken
// from sleep. It uses a time-jump heuristic: if the wall-clock gap since the
// last check exceeds twice the poll interval, we assume the machine was asleep.
func WatchWake(ctx context.Context, pollInterval time.Duration) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		defer close(ch)
		checkInterval := pollInterval / 2
		if checkInterval < 5*time.Second {
			checkInterval = 5 * time.Second
		}
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		lastCheck := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(lastCheck)
				lastCheck = now
				if elapsed > 2*pollInterval {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	return ch
}
