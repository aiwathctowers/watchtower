package jira

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_Wait(t *testing.T) {
	rl := NewRateLimiter()

	// Should be able to consume all 8 tokens immediately.
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		require.NoError(t, rl.Wait(ctx))
	}

	// 9th should require waiting (tokens depleted).
	start := time.Now()
	require.NoError(t, rl.Wait(ctx))
	elapsed := time.Since(start)
	// Should have waited some time for token refill.
	assert.Greater(t, elapsed, 10*time.Millisecond)
}

func TestRateLimiter_Wait_CancelledContext(t *testing.T) {
	rl := NewRateLimiter()

	// Drain all tokens.
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		require.NoError(t, rl.Wait(ctx))
	}

	// Cancel context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rl.Wait(ctx)
	assert.Error(t, err)
}

func TestBackoffDuration(t *testing.T) {
	assert.Equal(t, 1*time.Second, BackoffDuration(0))
	assert.Equal(t, 2*time.Second, BackoffDuration(1))
	assert.Equal(t, 4*time.Second, BackoffDuration(2))
	assert.Equal(t, 4*time.Second, BackoffDuration(5)) // capped at 4s
}
