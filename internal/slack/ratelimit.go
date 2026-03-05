package slack

import (
	"context"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Rate limit tiers for stats tracking.
const (
	Tier2 = 2 // users.list, conversations.list, team.info, search.messages
	Tier3 = 3 // conversations.history, conversations.replies
	Tier4 = 4 // users.info
)

// RateLimiter enforces Slack API rate limits using a single global token bucket,
// a single global gate, and a single global backoff.
//
// Slack enforces an undocumented global rate limit (~20 req/min) across all
// API methods. Everything is global: one gate, one rate, one backoff.
type RateLimiter struct {
	limiter *rate.Limiter
	gate    chan struct{} // semaphore (capacity 1), nil in unlimited mode
	logger  *log.Logger

	mu      sync.Mutex
	backoff time.Time   // global backoff-until timestamp
	counts  map[int]int // tier -> request count (for stats)
	retries int         // total 429 count
}

// NewRateLimiter creates a rate limiter with a global rate of ~40 req/min.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Every(time.Minute/40), 3), // ~40/min, burst 3
		gate:    make(chan struct{}, 3),
		counts:  make(map[int]int),
	}
}

// NewUnlimitedRateLimiter creates a rate limiter that does not enforce any limits.
// Intended for testing only.
func NewUnlimitedRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Inf, 1),
		gate:    nil,
		counts:  make(map[int]int),
	}
}

// Wait blocks until the rate limiter allows a request.
// Acquires the global gate, waits for any active backoff, then waits for the token bucket.
func (rl *RateLimiter) Wait(ctx context.Context, tier int) error {
	if rl.gate != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case rl.gate <- struct{}{}:
		}
	}

	for {
		rl.mu.Lock()
		backoff := rl.backoff
		rl.mu.Unlock()

		if backoff.After(time.Now()) {
			select {
			case <-ctx.Done():
				rl.releaseGate()
				return ctx.Err()
			case <-time.After(time.Until(backoff)):
			}
			continue
		}

		if rl.limiter == nil {
			return nil
		}
		if err := rl.limiter.Wait(ctx); err != nil {
			rl.releaseGate()
			return err
		}
		return nil
	}
}

// Done releases the global gate and records a successful request.
// Only call this after a request was actually made.
func (rl *RateLimiter) Done(tier int) {
	rl.mu.Lock()
	rl.counts[tier]++
	rl.mu.Unlock()
	rl.releaseGate()
}

// releaseGate releases the global gate without incrementing stats.
// Used on early exits where no API request was actually made.
func (rl *RateLimiter) releaseGate() {
	if rl.gate != nil {
		<-rl.gate
	}
}

// HandleRateLimit sets a global backoff after receiving a 429 response.
func (rl *RateLimiter) HandleRateLimit(tier int, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	jitterMax := int64(retryAfter) / 4
	if jitterMax <= 0 {
		jitterMax = 1
	}
	jitter := time.Duration(rand.Int64N(jitterMax))
	backoff := retryAfter + jitter
	until := time.Now().Add(backoff)

	if rl.logger != nil {
		rl.logger.Printf("rate limited: backing off %.1fs", backoff.Seconds())
	}

	rl.mu.Lock()
	rl.backoff = until
	rl.retries++
	rl.mu.Unlock()
}

// Stats returns per-tier request counts and total 429 retry count.
func (rl *RateLimiter) Stats() (counts map[int]int, retries int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	c := make(map[int]int, len(rl.counts))
	for k, v := range rl.counts {
		c[k] = v
	}
	return c, rl.retries
}

// ResetStats clears all counters.
func (rl *RateLimiter) ResetStats() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.counts = make(map[int]int)
	rl.retries = 0
}
