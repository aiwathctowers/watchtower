// Package sessions manages concurrency limiting for Claude CLI calls.
package sessions

import (
	"context"
	"fmt"
	"sync"
)

// Worker represents a slot in the concurrency pool.
type Worker struct{}

// SessionPool manages a fixed number of worker slots to limit parallel
// Claude CLI invocations. Each call acquires a slot and releases it when done.
type SessionPool struct {
	workers chan *Worker
	mu      sync.Mutex
	closed  bool
}

// NewSessionPool creates a pool with the given number of worker slots.
func NewSessionPool(size int) *SessionPool {
	if size <= 0 {
		size = 1
	}
	workers := make(chan *Worker, size)
	for i := 0; i < size; i++ {
		workers <- &Worker{}
	}
	return &SessionPool{
		workers: workers,
	}
}

// Acquire waits for a free worker slot. Returns error if pool is closed.
func (p *SessionPool) Acquire(ctx context.Context) (*Worker, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("session pool is closed")
	}
	p.mu.Unlock()

	select {
	case w := <-p.workers:
		return w, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("acquire timeout: %w", ctx.Err())
	}
}

// Release returns a worker slot to the pool.
func (p *SessionPool) Release(w *Worker) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	select {
	case p.workers <- w:
	default:
	}
}

// Close closes the pool and stops accepting new acquire requests.
func (p *SessionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true
	close(p.workers)
	for range p.workers {
	}
}

// Size returns the pool capacity.
func (p *SessionPool) Size() int {
	return cap(p.workers)
}
