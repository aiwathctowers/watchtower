package sessions

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSessionPool(t *testing.T) {
	p := NewSessionPool(5)
	if p.Size() != 5 {
		t.Errorf("expected size 5, got %d", p.Size())
	}
	p.Close()
}

func TestSessionPoolAcquireRelease(t *testing.T) {
	p := NewSessionPool(2)
	defer p.Close()

	ctx := context.Background()

	// Acquire first worker
	w1, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}
	if w1 == nil {
		t.Fatal("worker 1 is nil")
	}

	// Acquire second worker
	w2, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}
	if w2 == nil {
		t.Fatal("worker 2 is nil")
	}

	// Acquire with timeout (should block)
	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = p.Acquire(ctx2)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// Release and retry
	p.Release(w1)
	w1b, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	if w1b == nil {
		t.Fatal("released worker is nil")
	}

	p.Release(w2)
	p.Release(w1b)
}

func TestSessionPoolClose(t *testing.T) {
	p := NewSessionPool(1)

	ctx := context.Background()
	w, err := p.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	p.Close()

	// Should reject new acquires
	_, err = p.Acquire(context.Background())
	if err == nil {
		t.Fatal("expected error after close")
	}

	// Release should be safe (no panic)
	p.Release(w)
}

func TestSessionPoolConcurrency(t *testing.T) {
	p := NewSessionPool(3)
	defer p.Close()

	const workers = 10
	const iterations = 5

	done := make(chan struct{})

	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			ctx := context.Background()
			for j := 0; j < iterations; j++ {
				w, err := p.Acquire(ctx)
				if err != nil {
					t.Errorf("acquire failed: %v", err)
					return
				}
				// Simulate work
				time.Sleep(1 * time.Millisecond)
				p.Release(w)
			}
		}()
	}

	for i := 0; i < workers; i++ {
		<-done
	}
}

func TestSessionPoolZeroSize(t *testing.T) {
	p := NewSessionPool(0)
	defer p.Close()

	if p.Size() != 1 {
		t.Errorf("expected minimum size 1, got %d", p.Size())
	}
}

func TestSessionPoolDoubleClose(t *testing.T) {
	p := NewSessionPool(2)
	p.Close()
	p.Close() // should not panic
}

func TestSessionPoolNegativeSize(t *testing.T) {
	p := NewSessionPool(-5)
	defer p.Close()
	if p.Size() != 1 {
		t.Errorf("expected minimum size 1, got %d", p.Size())
	}
}

// --- SessionLog tests ---

func TestNewSessionLog(t *testing.T) {
	sl := NewSessionLog("/tmp/test-session.jsonl")
	if sl == nil {
		t.Fatal("expected non-nil SessionLog")
	}
}

func TestSessionLogWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.jsonl")
	sl := NewSessionLog(path)

	sl.Log(SessionEvent{
		Timestamp:  "2026-01-01T00:00:00Z",
		SessionID:  "sess-1",
		Action:     "created",
		Source:     "digest.channel",
		DurationMS: 1500,
	})
	sl.Log(SessionEvent{
		Timestamp:  "2026-01-01T00:00:01Z",
		SessionID:  "sess-1",
		Action:     "reused",
		Source:     "analysis.user",
		DurationMS: 800,
	})

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var events []SessionEvent
	for scanner.Scan() {
		var ev SessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].SessionID != "sess-1" || events[0].Action != "created" {
		t.Errorf("event 0 mismatch: %+v", events[0])
	}
	if events[1].Action != "reused" || events[1].Source != "analysis.user" {
		t.Errorf("event 1 mismatch: %+v", events[1])
	}
}

func TestSessionLogInvalidPath(t *testing.T) {
	sl := NewSessionLog("/nonexistent/dir/sessions.jsonl")
	// Should not panic, just silently fail
	sl.Log(SessionEvent{Timestamp: "t", SessionID: "s", Action: "created"})
}

func TestSessionLogConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.jsonl")
	sl := NewSessionLog(path)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 5; j++ {
				sl.Log(SessionEvent{
					Timestamp: "t",
					SessionID: "s",
					Action:    "created",
					Source:    "test",
				})
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	count := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		count++
	}
	if count != 50 {
		t.Errorf("expected 50 lines, got %d", count)
	}
}
