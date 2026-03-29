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
	"watchtower/internal/digest"
	"watchtower/internal/guide"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/sync"
	"watchtower/internal/tracks"
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

func TestDaemon_SetLogger(t *testing.T) {
	orch, _ := newTestOrchestrator(t, new(atomic.Int32))
	cfg := &config.Config{
		Sync: config.SyncConfig{PollInterval: time.Second},
	}
	d := New(orch, cfg)

	l := log.New(os.Stderr, "[custom] ", 0)
	d.SetLogger(l)
	assert.Equal(t, l, d.logger)
}

func TestDaemon_SetPipelines(t *testing.T) {
	orch, _ := newTestOrchestrator(t, new(atomic.Int32))
	cfg := &config.Config{
		Sync: config.SyncConfig{PollInterval: time.Second},
	}
	d := New(orch, cfg)

	// All pipeline setters should store without error.
	d.SetDigestPipeline(nil)
	d.SetTracksPipeline(nil)
	d.SetPeoplePipeline(nil)

	assert.Nil(t, d.digestPipe)
	assert.Nil(t, d.tracksPipe)
	assert.Nil(t, d.peoplePipe)
}

func TestDaemon_SetPIDPath(t *testing.T) {
	orch, _ := newTestOrchestrator(t, new(atomic.Int32))
	cfg := &config.Config{
		Sync: config.SyncConfig{PollInterval: time.Second},
	}
	d := New(orch, cfg)
	d.SetPIDPath("/tmp/test.pid")
	assert.Equal(t, "/tmp/test.pid", d.pidPath)
}

func TestDaemon_RunWithPIDFile(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	pidPath := dir + "/daemon.pid"

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))
	d.SetPIDPath(pidPath)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := d.Run(ctx)
	assert.NoError(t, err)

	// PID file should be removed after Run completes.
	_, statErr := os.Stat(pidPath)
	assert.True(t, os.IsNotExist(statErr), "PID file should be removed after daemon stops")
}

func testDaemonWithTempHome(t *testing.T) (*sync.Orchestrator, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	orch, _ := newTestOrchestrator(t, new(atomic.Int32))
	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{PollInterval: time.Second},
	}
	return orch, cfg, wsDir
}

func TestDaemon_SaveLoadPeopleTime(t *testing.T) {
	orch, cfg, _ := testDaemonWithTempHome(t)

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test] ", 0))

	now := time.Now().Truncate(time.Second)
	d.lastPeople = now
	d.saveLastPeople()

	d2 := New(orch, cfg)
	d2.SetLogger(log.New(os.Stderr, "[test2] ", 0))
	d2.loadLastPeople()

	assert.Equal(t, now.Unix(), d2.lastPeople.Unix())
}

func TestDaemon_LoadPeopleMissingFile(t *testing.T) {
	orch, cfg, _ := testDaemonWithTempHome(t)

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test] ", 0))
	d.loadLastPeople()
	assert.True(t, d.lastPeople.IsZero())
}

func TestDaemon_LoadPeopleInvalidContent(t *testing.T) {
	orch, cfg, wsDir := testDaemonWithTempHome(t)

	require.NoError(t, os.WriteFile(wsDir+"/last_people.txt", []byte("not-a-number"), 0o600))

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test] ", 0))
	d.loadLastPeople()
	assert.True(t, d.lastPeople.IsZero())
}

// mockGenerator implements digest.Generator for testing.
type mockGenerator struct{}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return `{"summary":"test","topics":[],"decisions":[],"action_items":[],"key_messages":[]}`, nil, "", nil
}

func TestDaemon_RunSyncWithDigestPipeline(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	database, err := db.Open(wsDir + "/watchtower.db")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
		Digest: config.DigestConfig{
			Enabled:     true,
			MinMessages: 1,
		},
	}

	gen := &mockGenerator{}
	pipe := digest.New(database, cfg, gen, log.New(os.Stderr, "[digest-test] ", 0))

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))
	d.SetDigestPipeline(pipe)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = d.Run(ctx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, syncCount.Load(), int32(1))
}

func TestDaemon_RunSyncWithAllPipelines(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	database, err := db.Open(wsDir + "/watchtower.db")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
		Digest: config.DigestConfig{
			Enabled:        true,
			MinMessages:    1,
			TracksInterval: 1 * time.Millisecond, // Allow tracks to run immediately
		},
	}

	gen := &mockGenerator{}
	l := log.New(os.Stderr, "[test-pipe] ", 0)

	digestPipe := digest.New(database, cfg, gen, l)
	tracksPipe := tracks.New(database, cfg, gen, l)
	peoplePipe := guide.New(database, cfg, gen, l)

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))
	d.SetDigestPipeline(digestPipe)
	d.SetTracksPipeline(tracksPipe)
	d.SetPeoplePipeline(peoplePipe)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = d.Run(ctx)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, syncCount.Load(), int32(1))
}

func TestDaemon_AutoMarkReadAfterDigests(t *testing.T) {
	// AutoMarkReadFromSlack must run AFTER digest generation so that
	// newly created digests are marked read when the user has already
	// read those messages in Slack.
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	database, err := db.Open(wsDir + "/watchtower.db")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Seed a channel with last_read ahead of the digest period.
	require.NoError(t, database.EnsureChannel("C1", "general", "public", ""))
	// Set last_read to a timestamp after the digest period_to.
	require.NoError(t, database.UpdateChannelLastRead("C1", "9999999999.000000"))

	// Insert a channel digest whose period_to is before last_read.
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:  "C1",
		Type:       "channel",
		PeriodFrom: 1000000000,
		PeriodTo:   1000086400,
		Summary:    "test digest",
		Topics:     `["testing"]`,
		Decisions:  `[{"text":"test decision","by":"@alice","importance":"high"}]`,
	})
	require.NoError(t, err)

	// Verify digest is unread initially.
	digests, err := database.GetDigests(db.DigestFilter{Type: "channel"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.False(t, digests[0].ReadAt.Valid, "digest should be unread before daemon run")

	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
		Digest: config.DigestConfig{Enabled: true, MinMessages: 1},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))
	d.SetDB(database)

	// Call runSync directly (not Run which starts a loop).
	d.runSync(context.Background())

	// Verify digest is now marked as read.
	digests, err = database.GetDigests(db.DigestFilter{Type: "channel"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.True(t, digests[0].ReadAt.Valid, "digest should be marked read after daemon runSync")
}

func TestDaemon_RunSyncContextCancelled(t *testing.T) {
	// When context is cancelled during sync, pipelines should be skipped.
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
		Digest: config.DigestConfig{Enabled: true, MinMessages: 1},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))

	// Very short context so runSync hits ctx.Err() != nil branch
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := d.Run(ctx)
	assert.NoError(t, err)
}

func TestDaemon_RunSyncWithPeopleThrottle(t *testing.T) {
	// Test that people pipeline respects the 24h throttle.
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	database, err := db.Open(wsDir + "/watchtower.db")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
		Digest: config.DigestConfig{Enabled: true, MinMessages: 1},
	}

	gen := &mockGenerator{}
	l := log.New(os.Stderr, "[test] ", 0)
	peoplePipe := guide.New(database, cfg, gen, l)

	d := New(orch, cfg)
	d.SetLogger(l)
	d.SetPeoplePipeline(peoplePipe)
	// Set lastPeople to recent time — pipeline should be skipped
	d.lastPeople = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = d.Run(ctx)
	assert.NoError(t, err)
}

func TestDaemon_MinPollInterval(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: 1 * time.Nanosecond, // too small, should use default
			SyncOnWake:   false,
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := d.Run(ctx)
	assert.NoError(t, err)
	// Should still have run at least the initial sync
	assert.GreaterOrEqual(t, syncCount.Load(), int32(1))
}

func TestDaemon_UnsnoozeExpiredTasks(t *testing.T) {
	var syncCount atomic.Int32
	orch, _ := newTestOrchestrator(t, &syncCount)

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	wsDir := dir + "/.local/share/watchtower/test-ws"
	require.NoError(t, os.MkdirAll(wsDir, 0o755))

	database, err := db.Open(wsDir + "/watchtower.db")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Create a snoozed task with expired snooze_until.
	_, err = database.CreateTask(db.Task{
		Text:        "Expired snooze",
		Status:      "snoozed",
		Priority:    "medium",
		Ownership:   "mine",
		SnoozeUntil: "2020-01-01",
		SourceType:  "manual",
	})
	require.NoError(t, err)

	// Create a snoozed task with future snooze_until.
	_, err = database.CreateTask(db.Task{
		Text:        "Future snooze",
		Status:      "snoozed",
		Priority:    "medium",
		Ownership:   "mine",
		SnoozeUntil: "2099-12-31",
		SourceType:  "manual",
	})
	require.NoError(t, err)

	cfg := &config.Config{
		ActiveWorkspace: "test-ws",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-ws": {SlackToken: "xoxp-test"},
		},
		Sync: config.SyncConfig{
			PollInterval: 10 * time.Second,
			SyncOnWake:   false,
		},
	}

	d := New(orch, cfg)
	d.SetLogger(log.New(os.Stderr, "[test-daemon] ", 0))
	d.SetDB(database)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err = d.Run(ctx)
	assert.NoError(t, err)

	// Verify: expired task should be unsnoozed.
	task1, err := database.GetTaskByID(1)
	require.NoError(t, err)
	assert.Equal(t, "todo", task1.Status)
	assert.Equal(t, "", task1.SnoozeUntil)

	// Verify: future task should still be snoozed.
	task2, err := database.GetTaskByID(2)
	require.NoError(t, err)
	assert.Equal(t, "snoozed", task2.Status)
	assert.Equal(t, "2099-12-31", task2.SnoozeUntil)
}
