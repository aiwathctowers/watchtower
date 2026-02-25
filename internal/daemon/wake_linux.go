//go:build linux

package daemon

import (
	"context"
	"time"
)

// WatchWake returns a channel that fires when the system appears to have woken
// from sleep. On Linux we use the same time-jump heuristic as macOS: if the
// wall-clock gap since the last check exceeds twice the poll interval, we
// assume the machine was asleep.
//
// A D-Bus subscription to org.freedesktop.login1.Manager.PrepareForSleep
// would be more reliable, but it adds a heavy dependency (godbus) that is not
// worth pulling in for a fallback platform. The time-jump approach works well
// enough for typical laptop sleep/wake cycles.
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
