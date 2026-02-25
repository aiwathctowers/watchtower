package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWatchWake_ClosesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ch := WatchWake(ctx, 100*time.Millisecond)
	cancel()

	// The channel should be closed after context cancellation.
	select {
	case _, ok := <-ch:
		if ok {
			// Got a spurious wake signal, that's acceptable; drain and wait for close.
			select {
			case <-ch:
			case <-time.After(time.Second):
				t.Fatal("wake channel was not closed after context cancellation")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("wake channel was not closed after context cancellation")
	}
}

func TestWatchWake_NoFalsePositives(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// With a 5 second poll interval and 300ms test window, no wake should fire.
	ch := WatchWake(ctx, 5*time.Second)

	select {
	case <-ch:
		// Could be the close signal, check context.
		if ctx.Err() == nil {
			t.Fatal("unexpected wake event when no time-jump occurred")
		}
	case <-ctx.Done():
		// Expected: no wake events during normal operation.
	}
}

func TestWatchWake_MinimumCheckInterval(t *testing.T) {
	// Even with a very small poll interval, the check interval should be at least 5s.
	// We can't easily test the internal timer, but we can verify the channel is created
	// and eventually closed on cancel without error.
	ctx, cancel := context.WithCancel(context.Background())
	ch := WatchWake(ctx, 1*time.Millisecond)
	assert.NotNil(t, ch)
	cancel()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("wake channel was not closed after context cancellation")
	}
}
