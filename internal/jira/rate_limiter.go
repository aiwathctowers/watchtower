package jira

import (
	"context"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter for Jira API requests.
type RateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a rate limiter with 8 requests/second.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		tokens:     8,
		maxTokens:  8,
		refillRate: 8,
		lastRefill: time.Now(),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	for {
		rl.mu.Lock()
		rl.refill()
		if rl.tokens >= 1 {
			rl.tokens--
			rl.mu.Unlock()
			return nil
		}
		rl.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (rl *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens += elapsed * rl.refillRate
	if rl.tokens > rl.maxTokens {
		rl.tokens = rl.maxTokens
	}
	rl.lastRefill = now
}

// BackoffDuration returns exponential backoff duration for a given attempt (0-indexed).
// 1s, 2s, 4s.
func BackoffDuration(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 1 * time.Second
	case 1:
		return 2 * time.Second
	default:
		return 4 * time.Second
	}
}
