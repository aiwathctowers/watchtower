package slack

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Rate limit tiers matching Slack API documentation.
const (
	Tier2 = 2 // 20 requests/min: users.list, conversations.list, team.info
	Tier3 = 3 // 50 requests/min: conversations.history, conversations.replies
)

// RateLimiter enforces Slack API rate limits using per-tier token buckets
// and handles 429 backoff with jitter.
type RateLimiter struct {
	limiters map[int]*rate.Limiter

	mu       sync.Mutex
	backoffs map[int]time.Time // tier -> backoff-until timestamp
}

// NewRateLimiter creates a rate limiter with per-tier token bucket limits.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiters: map[int]*rate.Limiter{
			Tier2: rate.NewLimiter(rate.Every(time.Minute/20), 1), // 20/min
			Tier3: rate.NewLimiter(rate.Every(time.Minute/50), 1), // 50/min
		},
		backoffs: make(map[int]time.Time),
	}
}

// Wait blocks until the rate limiter allows a request for the given tier.
// It respects both the token bucket rate and any active 429 backoff.
func (rl *RateLimiter) Wait(ctx context.Context, tier int) error {
	// Check for active backoff first.
	rl.mu.Lock()
	until, hasBackoff := rl.backoffs[tier]
	rl.mu.Unlock()

	if hasBackoff && time.Now().Before(until) {
		delay := time.Until(until)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	limiter, ok := rl.limiters[tier]
	if !ok {
		return nil
	}
	return limiter.Wait(ctx)
}

// HandleRateLimit sets a backoff for the given tier after receiving a 429 response.
// It adds jitter (0-25% of retryAfter) to avoid thundering herd.
func (rl *RateLimiter) HandleRateLimit(tier int, retryAfter time.Duration) {
	jitter := time.Duration(rand.Int64N(int64(retryAfter) / 4))
	until := time.Now().Add(retryAfter + jitter)

	rl.mu.Lock()
	rl.backoffs[tier] = until
	rl.mu.Unlock()
}
