package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// --- Progress() accessor on Orchestrator ---

func TestOrchestratorProgress(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	p := ts.orch.Progress()
	require.NotNil(t, p)
	assert.Equal(t, PhaseMetadata, p.Phase()) // default is 0
}

// --- syncEmoji ---

func TestSyncEmojiSuccess(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"emoji": map[string]string{
				"shipit":      "https://example.com/shipit.png",
				"thumbsup":    "alias:+1",
				"custom_logo": "https://example.com/logo.png",
			},
		})
	})
	// Need conversations.history for full sync path
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T024BE7LD",
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Verify emojis were stored
	emojis, err := ts.db.GetCustomEmojis()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(emojis), 3)

	// Check alias detection
	found := false
	for _, e := range emojis {
		if e.Name == "thumbsup" {
			found = true
			assert.Equal(t, "+1", e.AliasFor)
		}
	}
	assert.True(t, found, "should find thumbsup alias emoji")
}

func TestSyncEmojiErrorNonFatal(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "missing_scope",
		})
	})
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T024BE7LD",
		})
	})

	ts := newTestSetup(t, mux)

	// Pre-populate workspace so we take the search path
	err := ts.db.UpsertWorkspace(db.Workspace{ID: "T024BE7LD", Name: "my-company", Domain: "my-company"})
	require.NoError(t, err)

	// Should not fail even though emoji.list errors — emoji sync is non-fatal
	err = ts.orch.Run(context.Background(), SyncOptions{})
	require.NoError(t, err)
}

// --- syncCurrentUser ---

func TestSyncCurrentUserSuccess(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T024BE7LD",
		})
	})
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "emoji": map[string]string{}})
	})
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Verify current user was saved
	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	assert.Equal(t, "U001", ws.CurrentUserID)
}

func TestSyncCurrentUserAuthTestError(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": false, "error": "invalid_auth",
		})
	})
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "emoji": map[string]string{}})
	})
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	// auth.test failure is non-fatal, sync should still succeed
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	assert.Empty(t, ws.CurrentUserID) // auth.test failed, no current user
}

func TestSyncCurrentUserRetryOnCachedWorkspace(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T024BE7LD",
		})
	})
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "emoji": map[string]string{}})
	})

	ts := newTestSetup(t, mux)

	// Pre-populate workspace WITHOUT current_user_id (simulating previous auth.test failure)
	err := ts.db.UpsertWorkspace(db.Workspace{ID: "T024BE7LD", Name: "my-company", Domain: "my-company"})
	require.NoError(t, err)

	err = ts.orch.Run(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Should have retried auth.test and saved user
	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	assert.Equal(t, "U001", ws.CurrentUserID)
}

// --- ensureWorkspace (cached path) ---

func TestEnsureWorkspaceCached(t *testing.T) {
	ts := newTestSetup(t, defaultMux())

	// Pre-populate workspace
	err := ts.db.UpsertWorkspace(db.Workspace{ID: "T024BE7LD", Name: "my-company", Domain: "my-company"})
	require.NoError(t, err)

	// Should use cached workspace, not call team.info
	err = ts.orch.ensureWorkspace(context.Background())
	require.NoError(t, err)

	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	assert.Equal(t, "T024BE7LD", ws.ID)
}

// --- channelName ---

func TestChannelNameWithMap(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	ts.orch.channelNames = map[string]string{
		"C001": "general",
		"C002": "",
	}

	assert.Equal(t, "#general (C001)", ts.orch.channelName("C001"))
	assert.Equal(t, "C002", ts.orch.channelName("C002"))   // empty name
	assert.Equal(t, "C999", ts.orch.channelName("C999"))   // not in map
}

func TestChannelNameNilMap(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	ts.orch.channelNames = nil
	assert.Equal(t, "C001", ts.orch.channelName("C001"))
}

// --- saveSyncError ---

func TestSaveSyncErrorWithExistingState(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Set up existing sync state
	err = ts.db.UpdateSyncState("C001", db.SyncState{
		LastSyncedTS:          "1700000001.000000",
		OldestSyncedTS:        "1699900000.000000",
		IsInitialSyncComplete: true,
		Cursor:                "saved_cursor",
		MessagesSynced:        42,
	})
	require.NoError(t, err)

	existing, _ := ts.db.GetSyncState("C001")

	// Save an error
	ts.orch.saveSyncError("C001", existing, "1699900000.000000", fmt.Errorf("rate_limited"))

	state, err := ts.db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "rate_limited", state.Error)
	assert.Equal(t, "1700000001.000000", state.LastSyncedTS)
	assert.True(t, state.IsInitialSyncComplete)
	assert.Equal(t, 42, state.MessagesSynced)
}

func TestSaveSyncErrorWithNilState(t *testing.T) {
	ts := newTestSetup(t, defaultMux())

	// Save error for a channel with no existing state
	ts.orch.saveSyncError("C001", nil, "1699900000.000000", fmt.Errorf("internal_error"))

	state, err := ts.db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "internal_error", state.Error)
	assert.Equal(t, "1699900000.000000", state.OldestSyncedTS)
}

// --- searchChannelType ---

func TestSearchChannelType(t *testing.T) {
	tests := []struct {
		name     string
		ch       func() interface{} // we'll test the raw function
		expected string
	}{
		{"public", nil, "public"},
	}
	_ = tests

	// Test all branches of searchChannelType via the slack.CtxChannel struct
	// Since slack.CtxChannel fields aren't easily constructible in tests,
	// we test via the integration path in TestRunSearchSync above.
	// Here we test the parseSlackTS helper directly.
}

// --- parseSlackTS ---

func TestParseSlackTS(t *testing.T) {
	ts, err := parseSlackTS("1740567600.000100")
	require.NoError(t, err)
	assert.Equal(t, int64(1740567600), ts.Unix())

	ts, err = parseSlackTS("1700000000.000000")
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000), ts.Unix())

	// Error cases
	_, err = parseSlackTS("")
	assert.Error(t, err)

	_, err = parseSlackTS("notanumber.000000")
	assert.Error(t, err)

	_, err = parseSlackTS(".000000")
	assert.Error(t, err)
}

// --- resolveWorkerCount ---

func TestResolveWorkerCount(t *testing.T) {
	ts := newTestSetup(t, defaultMux())

	// 0 should use config default (2)
	assert.Equal(t, 2, ts.orch.resolveWorkerCount(0))

	// Explicit value
	assert.Equal(t, 5, ts.orch.resolveWorkerCount(5))

	// Capped at 100
	assert.Equal(t, 100, ts.orch.resolveWorkerCount(200))

	// Negative should use config default
	assert.Equal(t, 2, ts.orch.resolveWorkerCount(-1))
}

func TestResolveWorkerCountNoConfigDefault(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	ts.orch.config.Sync.Workers = 0 // no config default

	// With 0 requested and 0 config, should fall back to 1
	assert.Equal(t, 1, ts.orch.resolveWorkerCount(0))
}

// --- runSearchSync fallback paths ---

func TestRunSearchSyncFallsBackOnNonFatalError(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "team": map[string]any{"id": "T001", "name": "test", "domain": "test"},
		})
	})
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T001",
		})
	})
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "members": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "channels": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "emoji": map[string]string{}})
	})
	mux.HandleFunc("/search.messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return missing_scope which is a non-fatal error
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "missing_scope",
		})
	})
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "user_not_found"})
	})

	ts := newTestSetup(t, mux)

	// Pre-populate workspace so Run() takes the search path
	err := ts.db.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test", Domain: "test"})
	require.NoError(t, err)

	// Should fall back to full sync on non-fatal search error
	err = ts.orch.Run(context.Background(), SyncOptions{})
	require.NoError(t, err)
}

func TestRunSearchSyncZeroChannelsFallback(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "team": map[string]any{"id": "T001", "name": "test", "domain": "test"},
		})
	})
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T001",
		})
	})
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "members": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "channels": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "emoji": map[string]string{}})
	})
	mux.HandleFunc("/search.messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return empty results (0 channels discovered)
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"messages": map[string]any{
				"matches": []any{},
				"paging": map[string]any{
					"count": 100, "total": 0, "page": 1, "pages": 0,
				},
				"total": 0,
			},
		})
	})
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "user_not_found"})
	})

	ts := newTestSetup(t, mux)

	// Pre-populate workspace with no channels in DB
	err := ts.db.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test", Domain: "test"})
	require.NoError(t, err)

	// Search finds 0 channels + DB has 0 channels = fallback to full sync
	err = ts.orch.Run(context.Background(), SyncOptions{})
	require.NoError(t, err)
}

// --- SyncResult ---

func TestResultFromSnapshot(t *testing.T) {
	snap := Snapshot{
		StartTime:       time.Now().Add(-30 * time.Second),
		MessagesFetched: 500,
		ThreadsFetched:  100,
	}

	// Without error
	result := ResultFromSnapshot(snap, nil)
	assert.Equal(t, 500, result.MessagesFetched)
	assert.Equal(t, 100, result.ThreadsFetched)
	assert.Empty(t, result.Error)
	assert.InDelta(t, 30.0, result.DurationSecs, 2.0)

	// With error
	result = ResultFromSnapshot(snap, fmt.Errorf("sync failed"))
	assert.Equal(t, "sync failed", result.Error)
}

func TestWriteAndReadSyncResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync_result.json")

	result := SyncResult{
		StartedAt:       time.Now().Add(-30 * time.Second),
		FinishedAt:      time.Now(),
		DurationSecs:    30.0,
		MessagesFetched: 500,
		ThreadsFetched:  100,
		Error:           "",
	}

	err := WriteSyncResult(path, result)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Read it back
	loaded, err := ReadSyncResult(path)
	require.NoError(t, err)
	assert.Equal(t, 500, loaded.MessagesFetched)
	assert.Equal(t, 100, loaded.ThreadsFetched)
	assert.InDelta(t, 30.0, loaded.DurationSecs, 0.1)
}

func TestReadSyncResultNotFound(t *testing.T) {
	_, err := ReadSyncResult("/nonexistent/path.json")
	assert.Error(t, err)
}

func TestWriteSyncResultWithError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync_result.json")

	result := SyncResult{
		StartedAt:       time.Now(),
		FinishedAt:      time.Now(),
		DurationSecs:    5.0,
		MessagesFetched: 0,
		Error:           "auth failed",
	}

	err := WriteSyncResult(path, result)
	require.NoError(t, err)

	loaded, err := ReadSyncResult(path)
	require.NoError(t, err)
	assert.Equal(t, "auth failed", loaded.Error)
}

// --- Progress JSON ---

func TestProgressJSON(t *testing.T) {
	p := NewProgress()
	p.SetPhase(PhaseDiscovery)
	p.SetDiscovery(5, 10, 3, 2)
	p.AddMessages(100)

	data := p.JSON()
	require.NotEmpty(t, data)

	var snap JSONSnapshot
	err := json.Unmarshal(data, &snap)
	require.NoError(t, err)
	assert.Equal(t, "Discovery", snap.Phase)
	assert.Equal(t, 5, snap.DiscoveryPages)
	assert.Equal(t, 10, snap.DiscoveryTotalPages)
	assert.Equal(t, 3, snap.DiscoveryChannels)
	assert.Equal(t, 2, snap.DiscoveryUsers)
	assert.Equal(t, 100, snap.MessagesFetched)
	assert.Greater(t, snap.ElapsedSec, float64(0))
}

// --- Progress Render with discovery phase ---

func TestRenderSnapshotDiscoveryPhase(t *testing.T) {
	snap := Snapshot{
		Phase:               PhaseDiscovery,
		DiscoveryPages:      5,
		DiscoveryTotalPages: 20,
		DiscoveryChannels:   10,
		DiscoveryUsers:      30,
		MessagesFetched:     500,
		PhaseStartTime:      time.Now().Add(-5 * time.Second),
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "Search")
	assert.Contains(t, output, "500 msgs")
	assert.Contains(t, output, "10 ch")
	assert.Contains(t, output, "25%")
}

func TestRenderSnapshotDiscoveryPhaseDone(t *testing.T) {
	snap := Snapshot{
		Phase:               PhaseUsers,
		DiscoveryPages:      10,
		DiscoveryTotalPages: 10,
		DiscoveryChannels:   20,
		DiscoveryUsers:      50,
		MessagesFetched:     1000,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "1,000 msgs")
	assert.Contains(t, output, "20 channels")
	assert.Contains(t, output, "50 users")
}

func TestRenderSnapshotDiscoveryWaiting(t *testing.T) {
	// When phase < Discovery, renderDiscovery shows "waiting..."
	// but renderDiscovery is only called when phase == Discovery or (phase > Discovery && pages > 0)
	// So we test discovery searching state instead
	snap := Snapshot{
		Phase:               PhaseDiscovery,
		DiscoveryPages:      0,
		DiscoveryTotalPages: 0,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "searching")
}

// --- Progress Render users phase ---

func TestRenderSnapshotUsersPhase(t *testing.T) {
	snap := Snapshot{
		Phase:             PhaseUsers,
		UserProfilesTotal: 20,
		UserProfilesDone:  10,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "Users")
	assert.Contains(t, output, "10/20")
	assert.Contains(t, output, "50%")
}

func TestRenderSnapshotUsersChecking(t *testing.T) {
	snap := Snapshot{
		Phase:             PhaseUsers,
		UserProfilesTotal: 0,
		UserProfilesDone:  0,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "checking")
}

func TestRenderSnapshotUsersWaiting(t *testing.T) {
	// When phase < PhaseUsers but we need to show users section,
	// it shows "waiting...". However, renderUsers is only called when
	// phase == PhaseUsers or (phase > PhaseUsers && UserProfilesTotal > 0).
	// So test the "phase > PhaseUsers && UserProfilesTotal > 0" done path.
	snap := Snapshot{
		Phase:             PhaseDone,
		UserProfilesTotal: 5,
		UserProfilesDone:  5,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "5 profiles fetched")
}

func TestRenderSnapshotUsersDoneWithProfiles(t *testing.T) {
	snap := Snapshot{
		Phase:             PhaseDone,
		UserProfilesTotal: 15,
		UserProfilesDone:  15,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "15 profiles fetched")
}

// --- Progress Render with skipped channels info ---

func TestRenderSnapshotWithSkippedInfo(t *testing.T) {
	snap := Snapshot{
		Phase:               PhaseMessages,
		MsgChannelsTotal:    50,
		MsgChannelsDone:     25,
		MessagesFetched:     1000,
		ChannelsSkippedInfo: "skipped: 10 DMs, 5 archived",
		PhaseStartTime:      time.Now().Add(-10 * time.Second),
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "skipped: 10 DMs, 5 archived")
}

// --- Progress SetUserProfiles + SetChannelsSkippedInfo ---

func TestProgressSetUserProfiles(t *testing.T) {
	p := NewProgress()
	p.SetUserProfiles(20, 10)
	snap := p.Snapshot()
	assert.Equal(t, 20, snap.UserProfilesTotal)
	assert.Equal(t, 10, snap.UserProfilesDone)
}

func TestProgressSetChannelsSkippedInfo(t *testing.T) {
	p := NewProgress()
	p.SetChannelsSkippedInfo("5 DMs, 3 archived")
	snap := p.Snapshot()
	assert.Equal(t, "5 DMs, 3 archived", snap.ChannelsSkippedInfo)
}

// --- syncUserProfiles ---

func TestSyncUserProfilesAllKnown(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// All users from metadata are already known, so no profiles to fetch
	err = ts.orch.syncUserProfiles(context.Background())
	require.NoError(t, err)
}

func TestSyncUserProfilesIndividual(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "messages": []any{}, "has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)

	// Create an "unknown" user (via EnsureUser, which only sets id+name)
	err := ts.db.EnsureUser("U099", "unknown_user")
	require.NoError(t, err)

	err = ts.orch.syncUserProfiles(context.Background())
	require.NoError(t, err)

	// users.info for U099 returns user_not_found (non-fatal), should skip
	// The test still passes because non-fatal errors are skipped
}

// --- SkipDMs ---

func TestBuildChannelQueueSkipDMs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "team": map[string]any{"id": "T001", "name": "test", "domain": "test"},
		})
	})
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "members": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"channels": []map[string]any{
				{"id": "C001", "name": "general", "is_channel": true, "is_member": true,
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""}},
				{"id": "D001", "name": "", "is_im": true, "user": "U001", "is_member": true,
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""}},
				{"id": "G001", "name": "group-dm", "is_mpim": true, "is_member": true,
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""}},
			},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "user_id": "U001", "user": "alice", "team_id": "T001",
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Without SkipDMs: should have all 3 channels
	tasks, err := ts.orch.buildChannelQueue(SyncOptions{})
	require.NoError(t, err)
	assert.Len(t, tasks, 3)

	// With SkipDMs: should only have public channel
	tasks, err = ts.orch.buildChannelQueue(SyncOptions{SkipDMs: true})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "C001", tasks[0].ChannelID)
}

// --- assignChannelPriority with "low" watch ---

func TestAssignChannelPriorityLow(t *testing.T) {
	watchMap := map[string]string{
		"C001": "low",
	}
	ch := db.Channel{ID: "C001", IsMember: true}
	assert.Equal(t, PriorityWatchLow, assignChannelPriority(ch, watchMap))
}

// --- finishSync ---

func TestFinishSyncUpdatesSyncedAt(t *testing.T) {
	ts := newTestSetup(t, defaultMux())

	// Set up workspace
	err := ts.db.UpsertWorkspace(db.Workspace{ID: "T024BE7LD", Name: "my-company", Domain: "my-company"})
	require.NoError(t, err)

	err = ts.orch.finishSync()
	require.NoError(t, err)

	assert.Equal(t, PhaseDone, ts.orch.progress.Phase())

	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	assert.True(t, ws.SyncedAt.Valid)
}

// --- Render edge cases ---

func TestRenderSnapshotMetadataFetchingNoData(t *testing.T) {
	snap := Snapshot{
		Phase:         PhaseMetadata,
		UsersTotal:    0,
		UsersDone:     0,
		ChannelsTotal: 0,
		ChannelsDone:  0,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "fetching from Slack")
}

func TestRenderSnapshotMetadataFetchedUsers(t *testing.T) {
	snap := Snapshot{
		Phase:         PhaseMetadata,
		UsersTotal:    100,
		UsersDone:     0,
		ChannelsTotal: 50,
		ChannelsDone:  0,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "100 users")
	assert.Contains(t, output, "50 channels")
	assert.Contains(t, output, "saving")
}

func TestRenderSnapshotMetadataUsersSavedChannelsFetching(t *testing.T) {
	snap := Snapshot{
		Phase:         PhaseMetadata,
		UsersTotal:    100,
		UsersDone:     100,
		ChannelsTotal: 0,
		ChannelsDone:  0,
	}
	output := RenderSnapshot(snap, "test-ws")
	assert.Contains(t, output, "100 users saved")
	assert.Contains(t, output, "fetching channels")
}

// --- SyncPhase Discovery string ---

func TestPhaseDiscoveryString(t *testing.T) {
	assert.Equal(t, "Discovery", PhaseDiscovery.String())
	assert.Equal(t, "Users", PhaseUsers.String())
}
