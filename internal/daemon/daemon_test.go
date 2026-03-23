package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	goslack "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/sync"
)

func TestMain(m *testing.M) {
	// Allow sub-second poll intervals in tests for fast execution.
	minPollInterval = 1 * time.Millisecond
	os.Exit(m.Run())
}

func testMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"team": map[string]any{
				"id":     "T024BE7LD",
				"name":   "test-workspace",
				"domain": "test-workspace",
			},
		})
	})

	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": []any{},
		})
	})

	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"channels": []any{},
		})
	})

	mux.HandleFunc("/search.messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"messages": map[string]any{
				"matches": []any{},
				"paging":  map[string]any{"count": 100, "total": 0, "page": 1, "pages": 0},
				"total":   0,
			},
		})
	})

	return mux
}

func newTestOrchestrator(t *testing.T, syncCount *atomic.Int32) (*sync.Orchestrator, *httptest.Server) {
	t.Helper()

	mux := testMux()

	// Wrap mux to count how many times search.messages is called (proxy for sync runs).
	// We use search.messages because team.info is cached after the first call.
	countingMux := http.NewServeMux()
	countingMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search.messages" {
			syncCount.Add(1)
		}
		mux.ServeHTTP(w, r)
	})

	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Pre-populate workspace so sync takes the incremental (search) path
	err = database.UpsertWorkspace(db.Workspace{ID: "T024BE7LD", Name: "test-workspace", Domain: "test-workspace"})
	require.NoError(t, err)

	srv := httptest.NewServer(countingMux)
	t.Cleanup(srv.Close)

	api := goslack.New("xoxp-test-token", goslack.OptionAPIURL(srv.URL+"/"))
	slackClient := watchtowerslack.NewClientWithAPIUnlimited(api)

	cfg := &config.Config{
		ActiveWorkspace: "test-workspace",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-workspace": {SlackToken: "xoxp-test-token"},
		},
		Sync: config.SyncConfig{
			Workers:            1,
			InitialHistoryDays: 1,
			SyncThreads:        false,
			PollInterval:       100 * time.Millisecond,
			SyncOnWake:         false,
		},
	}

	orch := sync.NewOrchestrator(database, slackClient, cfg)
	orch.SetLogger(log.New(os.Stderr, "[test] ", 0))

	return orch, srv
}

func TestDaemon_RunsInitialSync(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second, // long interval so only initial sync fires
			SyncOnWake:   false,
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := d.Run(ctx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, syncCount.Load(), int32(1), "daemon should run at least one initial sync")
}

func TestDaemon_PollTriggersSync(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: 50 * time.Millisecond,
			SyncOnWake:   false,
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := d.Run(ctx)
	assert.NoError(t, err)
	// With 50ms interval and 300ms timeout, we should get initial sync + at least 2-3 poll syncs.
	assert.GreaterOrEqual(t, syncCount.Load(), int32(2), "daemon should run multiple syncs from polling")
}

func TestDaemon_WakeEventTriggersSync(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second, // long so only wake fires, not poll
			SyncOnWake:   false,            // we inject wake channel manually
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))

	// Inject a fake wake channel.
	wakeCh := make(chan struct{}, 1)
	d.wakeCh = wakeCh

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Send a wake signal after a brief delay.
	go func() {
		time.Sleep(100 * time.Millisecond)
		wakeCh <- struct{}{}
	}()

	err := d.Run(ctx)
	assert.NoError(t, err)
	// Initial sync + wake-triggered sync = at least 2.
	assert.GreaterOrEqual(t, syncCount.Load(), int32(2), "wake event should trigger an additional sync")
}

func TestDaemon_GracefulShutdown(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: 50 * time.Millisecond,
			SyncOnWake:   false,
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Let the daemon run briefly, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "daemon should shut down cleanly on context cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not shut down within timeout")
	}
}

func TestWakeChannel_NilWhenDisabled(t *testing.T) {
	d := &Daemon{}
	ch := d.wakeChannel()
	assert.Nil(t, ch, "wake channel should be nil when not configured")
}
