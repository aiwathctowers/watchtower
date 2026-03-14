package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// messageMux creates a mock Slack API that includes conversations.history responses
// and an empty conversations.replies handler.
func messageMux(channelMessages map[string][]map[string]any) *http.ServeMux {
	mux := baseMux()

	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() //nolint:errcheck
		channelID := r.Form.Get("channel")
		cursor := r.Form.Get("cursor")

		msgs, ok := channelMessages[channelID]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"ok":       true,
				"messages": []any{},
			})
			return
		}

		// Simple pagination: cursor="page2" returns second half
		pageSize := len(msgs)
		hasMore := false
		nextCursor := ""
		var page []map[string]any

		if len(msgs) > 3 && cursor == "" {
			// First page: first 3 messages
			page = msgs[:3]
			hasMore = true
			nextCursor = "page2"
		} else if cursor == "page2" {
			// Second page: remaining messages
			page = msgs[3:]
		} else {
			_ = pageSize
			page = msgs
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": page,
			"has_more": hasMore,
			"response_metadata": map[string]any{
				"next_cursor": nextCursor,
			},
		})
	})

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          []any{},
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	return mux
}

func TestSyncMessagesBasic(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000003.000000", "user": "U001", "text": "Hello world", "type": "message"},
			{"ts": "1700000002.000000", "user": "U002", "text": "Hi there", "type": "message"},
			{"ts": "1700000001.000000", "user": "U001", "text": "First message", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))

	// Run full sync (metadata + messages)
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Verify messages were stored
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 3)

	// Verify content of first message (newest first in our query)
	assert.Equal(t, "Hello world", msgs[0].Text)
	assert.Equal(t, "U001", msgs[0].UserID)
}

func TestSyncMessagesPagination(t *testing.T) {
	// Create 5 messages to trigger pagination (our mock paginates at 3)
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000005.000000", "user": "U001", "text": "Msg 5", "type": "message"},
			{"ts": "1700000004.000000", "user": "U002", "text": "Msg 4", "type": "message"},
			{"ts": "1700000003.000000", "user": "U001", "text": "Msg 3", "type": "message"},
			{"ts": "1700000002.000000", "user": "U002", "text": "Msg 2", "type": "message"},
			{"ts": "1700000001.000000", "user": "U001", "text": "Msg 1", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 5)
}

func TestSyncMessagesMultipleChannels(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "General msg", "type": "message"},
		},
		"C002": {
			{"ts": "1700000001.000000", "user": "U002", "text": "Engineering msg", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs1, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs1, 1)

	msgs2, err := ts.db.GetMessagesByChannel("C002", 100)
	require.NoError(t, err)
	assert.Len(t, msgs2, 1)
}

func TestSyncMessagesChannelFilter(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "General msg", "type": "message"},
		},
		"C002": {
			{"ts": "1700000001.000000", "user": "U002", "text": "Engineering msg", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	// Sync metadata first
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Sync only "general" channel
	err = ts.orch.syncMessages(context.Background(), SyncOptions{Channels: []string{"general"}})
	require.NoError(t, err)

	msgs1, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs1, 1, "general channel should have messages")

	msgs2, err := ts.db.GetMessagesByChannel("C002", 100)
	require.NoError(t, err)
	assert.Len(t, msgs2, 0, "engineering channel should not have been synced")
}

func TestSyncMessagesIncrementalSync(t *testing.T) {
	var callCount atomic.Int32
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000002.000000", "user": "U001", "text": "New msg", "type": "message"},
		},
	}

	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() //nolint:errcheck
		callCount.Add(1)
		channelID := r.Form.Get("channel")

		msgs := channelMsgs[channelID]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          msgs,
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// First sync
	err = ts.orch.syncMessages(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Verify sync state was created
	state, err := ts.db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.True(t, state.IsInitialSyncComplete)
	assert.Equal(t, "1700000002.000000", state.LastSyncedTS)

	// Second sync should use last_synced_ts as oldest param
	err = ts.orch.syncMessages(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Should have been called more than once (once per sync, per non-archived channel)
	assert.True(t, callCount.Load() > 1)
}

func TestSyncMessagesFullMode(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "Hello", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Set up existing sync state
	err = ts.db.UpdateSyncState("C001", db.SyncState{
		LastSyncedTS:          "1700000001.000000",
		IsInitialSyncComplete: true,
		MessagesSynced:        1,
	})
	require.NoError(t, err)

	// Full sync should still fetch messages (ignoring sync state)
	err = ts.orch.syncMessages(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestSyncMessagesSyncStateTracking(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000003.000000", "user": "U001", "text": "Msg 3", "type": "message"},
			{"ts": "1700000002.000000", "user": "U001", "text": "Msg 2", "type": "message"},
			{"ts": "1700000001.000000", "user": "U001", "text": "Msg 1", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	err = ts.orch.syncMessages(context.Background(), SyncOptions{})
	require.NoError(t, err)

	state, err := ts.db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.True(t, state.IsInitialSyncComplete)
	assert.Equal(t, 3, state.MessagesSynced)
	assert.Equal(t, "1700000003.000000", state.LastSyncedTS)
}

func TestSyncMessagesWithThreadTS(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Parent msg",
				"type": "message", "reply_count": 2, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000002.000000", "user": "U002", "text": "Reply 1",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)

	// Find the parent message (ts == thread_ts, so thread_ts should be NULL)
	parent, err := ts.db.GetMessages(db.MessageOpts{ChannelID: "C001", Limit: 10})
	require.NoError(t, err)

	foundParent := false
	foundReply := false
	for _, m := range parent {
		if m.TS == "1700000001.000000" {
			foundParent = true
			assert.Equal(t, 2, m.ReplyCount)
			// Parent's thread_ts == ts, so we should store it as NULL
			assert.False(t, m.ThreadTS.Valid)
		}
		if m.TS == "1700000002.000000" {
			foundReply = true
			assert.True(t, m.ThreadTS.Valid)
			assert.Equal(t, "1700000001.000000", m.ThreadTS.String)
		}
	}
	assert.True(t, foundParent, "should find parent message")
	assert.True(t, foundReply, "should find reply message")
}

func TestSyncMessagesNonFatalErrorSkipsChannel(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() //nolint:errcheck
		channelID := r.Form.Get("channel")
		w.Header().Set("Content-Type", "application/json")

		if channelID == "C001" {
			json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "channel_not_found",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          []map[string]any{{"ts": "1700000001.000000", "user": "U001", "text": "OK", "type": "message"}},
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Should not fail even though C001 returns channel_not_found
	err = ts.orch.syncMessages(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// C002 should still have messages
	msgs, err := ts.db.GetMessagesByChannel("C002", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestSyncMessagesContextCancellation(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "msg", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = ts.orch.syncMessages(ctx, SyncOptions{})
	// Either no error (if no tasks submitted) or context cancelled — both are acceptable
	if err != nil {
		assert.Contains(t, err.Error(), "context canceled")
	}
}

func TestBuildChannelQueuePriority(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Add C001 (general) to watch list with high priority
	err = ts.db.AddWatch("channel", "C001", "general", "high")
	require.NoError(t, err)

	// Add C002 (engineering) to watch list with normal priority
	err = ts.db.AddWatch("channel", "C002", "engineering", "normal")
	require.NoError(t, err)

	tasks, err := ts.orch.buildChannelQueue(SyncOptions{})
	require.NoError(t, err)

	// Should have 2 channels (C003 is archived and should be skipped)
	assert.Len(t, tasks, 2)

	// C001 should be first (watch high), C002 second (watch normal)
	assert.Equal(t, "C001", tasks[0].ChannelID)
	assert.Equal(t, PriorityWatchHigh, tasks[0].Priority)
	assert.Equal(t, "C002", tasks[1].ChannelID)
	assert.Equal(t, PriorityWatchNormal, tasks[1].Priority)
}

func TestBuildChannelQueueSkipsArchived(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	tasks, err := ts.orch.buildChannelQueue(SyncOptions{})
	require.NoError(t, err)

	// C003 (old-project) is archived, should be excluded
	for _, task := range tasks {
		assert.NotEqual(t, "C003", task.ChannelID, "archived channel should be excluded")
	}
}

func TestBuildChannelQueueWithFilter(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	tasks, err := ts.orch.buildChannelQueue(SyncOptions{Channels: []string{"engineering"}})
	require.NoError(t, err)

	assert.Len(t, tasks, 1)
	assert.Equal(t, "C002", tasks[0].ChannelID)
}

func TestBuildChannelQueueFilterByID(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	tasks, err := ts.orch.buildChannelQueue(SyncOptions{Channels: []string{"C001"}})
	require.NoError(t, err)

	assert.Len(t, tasks, 1)
	assert.Equal(t, "C001", tasks[0].ChannelID)
}

func TestBuildChannelQueueFilterIncludesArchived(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// When explicitly filtering by name, archived channels should be included
	tasks, err := ts.orch.buildChannelQueue(SyncOptions{Channels: []string{"old-project"}})
	require.NoError(t, err)

	assert.Len(t, tasks, 1)
	assert.Equal(t, "C003", tasks[0].ChannelID)
}

func TestAssignChannelPriority(t *testing.T) {
	watchMap := map[string]string{
		"C001": "high",
		"C002": "normal",
	}

	tests := []struct {
		name     string
		channel  db.Channel
		expected TaskPriority
	}{
		{
			name:     "watch high",
			channel:  db.Channel{ID: "C001", IsMember: true},
			expected: PriorityWatchHigh,
		},
		{
			name:     "watch normal",
			channel:  db.Channel{ID: "C002", IsMember: true},
			expected: PriorityWatchNormal,
		},
		{
			name:     "member not watched",
			channel:  db.Channel{ID: "C003", IsMember: true},
			expected: PriorityMember,
		},
		{
			name:     "not member not watched",
			channel:  db.Channel{ID: "C004", IsMember: false},
			expected: PriorityRest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, assignChannelPriority(tt.channel, watchMap))
		})
	}
}

func TestComputeOldest(t *testing.T) {
	ts := newTestSetup(t, defaultMux())

	// No state = initial sync, should compute cutoff from config
	oldest := ts.orch.computeOldest(nil, false)
	assert.NotEmpty(t, oldest)

	// With state and initial sync complete = incremental
	state := &db.SyncState{
		LastSyncedTS:          "1700000001.000000",
		IsInitialSyncComplete: true,
	}
	oldest = ts.orch.computeOldest(state, false)
	assert.Equal(t, "1700000001.000000", oldest)

	// Full mode = ignore sync state, use cutoff
	oldest = ts.orch.computeOldest(state, true)
	assert.NotEqual(t, "1700000001.000000", oldest)
}

func TestSyncMessagesErrorSavesState(t *testing.T) {
	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "internal_error",
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// This should fail because all channels return errors
	err = ts.orch.syncMessages(context.Background(), SyncOptions{})
	assert.Error(t, err)

	// Sync state should have error recorded for at least one channel
	foundError := false
	for _, chID := range []string{"C001", "C002"} {
		state, stateErr := ts.db.GetSyncState(chID)
		if stateErr == nil && state != nil && state.Error != "" {
			foundError = true
			break
		}
	}
	assert.True(t, foundError, "sync error should be recorded in sync_state")
}

func TestSyncMessagesWithSubtype(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "joined #general",
				"type": "message", "subtype": "channel_join",
			},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "channel_join", msgs[0].Subtype)
}

func TestSyncMessagesRawJSONStored(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "Test", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	// Verify raw JSON was stored and is valid
	assert.NotEmpty(t, msgs[0].RawJSON)
	var raw map[string]any
	err = json.Unmarshal([]byte(msgs[0].RawJSON), &raw)
	assert.NoError(t, err, "raw_json should be valid JSON")
}

func TestSyncMessagesEmptyChannel(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {}, // No messages
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 0)
}

func TestSyncMessagesUpsertDeduplication(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "Original", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Sync first time
	err = ts.orch.syncMessages(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Change message text and sync again
	channelMsgs["C001"][0]["text"] = "Edited"
	err = ts.orch.syncMessages(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Should still have only 1 message (upsert, not duplicate)
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "Edited", msgs[0].Text)
}

func TestSyncMessagesEditedFlag(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Edited msg",
				"type": "message", "edited": map[string]any{"user": "U001", "ts": "1700000002.000000"},
			},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].IsEdited)
}

func TestSyncMessagesWorkerCount(t *testing.T) {
	// With 0 workers in opts, should use config default
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{"ts": "1700000001.000000", "user": "U001", "text": "msg", "type": "message"},
		},
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Workers=0 should use config default (2 in test config)
	err = ts.orch.syncMessages(context.Background(), SyncOptions{Workers: 0})
	require.NoError(t, err)

	// Workers=1 should work too
	err = ts.orch.syncMessages(context.Background(), SyncOptions{Workers: 1, Full: true})
	require.NoError(t, err)

	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestSyncMessagesCursorResume(t *testing.T) {
	// Set up a sync state with a cursor to simulate interrupted sync
	var requestedCursors []string

	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm() //nolint:errcheck
		cursor := r.Form.Get("cursor")
		requestedCursors = append(requestedCursors, cursor)

		w.Header().Set("Content-Type", "application/json")
		if cursor == "resume_cursor" {
			json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"ts": "1700000002.000000", "user": "U001", "text": "Resumed msg", "type": "message"},
				},
				"has_more":          false,
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"ok":                true,
				"messages":          []any{},
				"has_more":          false,
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		}
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.syncMetadata(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Set up a sync state with a cursor (simulating interrupted previous sync)
	err = ts.db.UpdateSyncState("C001", db.SyncState{
		LastSyncedTS:          "",
		IsInitialSyncComplete: false,
		Cursor:                "resume_cursor",
		MessagesSynced:        5,
	})
	require.NoError(t, err)

	err = ts.orch.syncMessages(context.Background(), SyncOptions{Channels: []string{"general"}})
	require.NoError(t, err)

	// Verify that the cursor was used
	foundResumeCursor := false
	for _, c := range requestedCursors {
		if c == "resume_cursor" {
			foundResumeCursor = true
		}
	}
	assert.True(t, foundResumeCursor, "should have used the saved cursor: %v", requestedCursors)

	// Verify message was stored
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "Resumed msg", msgs[0].Text)

	// Verify sync state was updated and cursor cleared
	state, err := ts.db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.True(t, state.IsInitialSyncComplete)
	assert.Empty(t, state.Cursor)
	assert.Equal(t, 6, state.MessagesSynced) // 5 previous + 1 new
}

func TestSyncMessagesEmptyResponse(t *testing.T) {
	// Test that channels with no messages don't cause errors
	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          []any{},
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)
}

func TestSyncMessagesLargeVolume(t *testing.T) {
	// Create 10 messages per channel to test higher volume
	msgs := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		msgs[i] = map[string]any{
			"ts":   fmt.Sprintf("170000%04d.000000", i),
			"user": "U001",
			"text": fmt.Sprintf("Message %d", i),
			"type": "message",
		}
	}

	channelMsgs := map[string][]map[string]any{
		"C001": msgs,
	}

	ts := newTestSetup(t, messageMux(channelMsgs))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	stored, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, stored, 10)
}
