package slack

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter()
	assert.NotNil(t, rl.limiter)
	assert.NotNil(t, rl.gate)
	assert.True(t, rl.backoff.IsZero())
}

func TestWaitRespectsContext(t *testing.T) {
	rl := NewRateLimiter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := rl.Wait(ctx, Tier2)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWaitUnknownTierPassesThrough(t *testing.T) {
	rl := NewRateLimiter()
	err := rl.Wait(context.Background(), 99)
	assert.NoError(t, err)
	rl.Done(99)
}

func TestWaitRespectsBackoff(t *testing.T) {
	rl := NewRateLimiter()

	// Set a short backoff.
	backoffDuration := 50 * time.Millisecond
	rl.HandleRateLimit(Tier2, backoffDuration)

	start := time.Now()
	err := rl.Wait(context.Background(), Tier2)
	elapsed := time.Since(start)

	require.NoError(t, err)
	rl.Done(Tier2)
	// Should have waited at least the backoff duration (which includes jitter up to 25%).
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), backoffDuration.Milliseconds())
}

func TestHandleRateLimitSetsBackoff(t *testing.T) {
	rl := NewRateLimiter()

	rl.HandleRateLimit(Tier3, 2*time.Second)

	rl.mu.Lock()
	until := rl.backoff
	rl.mu.Unlock()

	assert.False(t, until.IsZero())
	// Should be at least 2 seconds from now (before jitter check, it's at least 2s).
	assert.True(t, until.After(time.Now().Add(1*time.Second)),
		"backoff should be at least 1 second in the future")
}

func TestBackoffContextCancellation(t *testing.T) {
	rl := NewRateLimiter()

	// Set a long backoff.
	rl.HandleRateLimit(Tier2, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := rl.Wait(ctx, Tier2)
	elapsed := time.Since(start)

	assert.Error(t, err)
	// Should have been cancelled quickly, not waiting 10 seconds.
	assert.Less(t, elapsed, 1*time.Second)
}

func TestTierConstants(t *testing.T) {
	assert.Equal(t, 2, Tier2)
	assert.Equal(t, 3, Tier3)
}

func TestUnlimitedRateLimiter(t *testing.T) {
	rl := NewUnlimitedRateLimiter()

	// Should be able to make many rapid calls without blocking
	start := time.Now()
	for i := 0; i < 100; i++ {
		err := rl.Wait(context.Background(), Tier2)
		require.NoError(t, err)
		err = rl.Wait(context.Background(), Tier3)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 1*time.Second, "unlimited limiter should not throttle")
}

func TestBackoffExpires(t *testing.T) {
	rl := NewRateLimiter()

	// Set a very short backoff
	rl.HandleRateLimit(Tier2, 10*time.Millisecond)

	// Wait for backoff to expire
	time.Sleep(50 * time.Millisecond)

	// Should succeed without significant delay
	start := time.Now()
	err := rl.Wait(context.Background(), Tier2)
	elapsed := time.Since(start)

	require.NoError(t, err)
	rl.Done(Tier2)
	assert.Less(t, elapsed, 500*time.Millisecond, "expired backoff should not cause delay")
}
