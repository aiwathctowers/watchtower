package digest

import (
	"context"
	"time"

	"watchtower/internal/sessions"
)

// PooledGenerator wraps a Generator with a SessionPool to limit concurrency.
// Each call acquires a worker slot, calls the inner generator (which creates
// a fresh one-shot session), and releases the slot back to the pool.
type PooledGenerator struct {
	inner      Generator
	pool       *sessions.SessionPool
	sessionLog *sessions.SessionLog
}

// NewPooledGenerator creates a generator that limits concurrency via the pool.
func NewPooledGenerator(inner Generator, pool *sessions.SessionPool) *PooledGenerator {
	return &PooledGenerator{inner: inner, pool: pool}
}

// SetSessionLog enables structured logging of generation events.
func (pg *PooledGenerator) SetSessionLog(sl *sessions.SessionLog) {
	pg.sessionLog = sl
}

// Pool returns the underlying session pool.
func (pg *PooledGenerator) Pool() *sessions.SessionPool {
	return pg.pool
}

// Generate acquires a worker slot from the pool, calls the inner generator,
// and releases the slot. Each call is independent — no session reuse.
func (pg *PooledGenerator) Generate(ctx context.Context, systemPrompt, userMessage, _ string) (string, *Usage, string, error) {
	worker, err := pg.pool.Acquire(ctx)
	if err != nil {
		return "", nil, "", err
	}
	defer pg.pool.Release(worker)

	start := time.Now()

	result, usage, sessionID, err := pg.inner.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return "", usage, "", err
	}

	// Log generation event.
	if pg.sessionLog != nil && sessionID != "" {
		source := "unknown"
		if s, ok := ctx.Value(sessionSourceKey{}).(string); ok && s != "" {
			source = s
		}
		pg.sessionLog.Log(sessions.SessionEvent{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			SessionID:  sessionID,
			Action:     "created",
			Source:     source,
			DurationMS: time.Since(start).Milliseconds(),
		})
	}

	return result, usage, sessionID, nil
}

// sessionSourceKey is the context key for the caller source label.
type sessionSourceKey struct{}

// WithSource returns a context that carries a source label for session logging.
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sessionSourceKey{}, source)
}
