package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	goslack "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"
)

// threadMux creates a mock Slack API that handles conversations.history and
// conversations.replies. channelMessages feeds history, threadReplies maps
// "channelID:threadTS" to reply messages.
func threadMux(channelMessages map[string][]map[string]any, threadReplies map[string][]map[string]any) *http.ServeMux {
	mux := baseMux()

	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		channelID := r.Form.Get("channel")

		msgs, ok := channelMessages[channelID]
		if !ok {
			msgs = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          msgs,
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		channelID := r.Form.Get("channel")
		threadTS := r.Form.Get("ts")
		key := channelID + ":" + threadTS

		replies, ok := threadReplies[key]
		if !ok {
			replies = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          replies,
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	return mux
}

func TestSyncThreadsBasic(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 2, "thread_ts": "1700000001.000000",
			},
		},
	}

	threadReplies := map[string][]map[string]any{
		"C001:1700000001.000000": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 2, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000010.000000", "user": "U002", "text": "Reply 1",
				"type": "message", "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000020.000000", "user": "U001", "text": "Reply 2",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
	}

	ts := newTestSetup(t, threadMux(channelMsgs, threadReplies))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Should have parent + 2 replies = 3 messages
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 3)

	// Verify thread replies are linked correctly
	replies, err := ts.db.GetThreadReplies("C001", "1700000001.000000")
	require.NoError(t, err)
	assert.Len(t, replies, 3) // parent + 2 replies
}

func TestSyncThreadsDisabled(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 2, "thread_ts": "1700000001.000000",
			},
		},
	}

	// replies handler should not be called because sync_threads is false
	threadReplies := map[string][]map[string]any{
		"C001:1700000001.000000": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 2, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000010.000000", "user": "U002", "text": "Reply 1",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
	}

	mux := threadMux(channelMsgs, threadReplies)
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	api := goslack.New("xoxp-test-token", goslack.OptionAPIURL(srv.URL+"/"))
	slackClient := watchtowerslack.NewClientWithAPIUnlimited(api)

	cfg := &config.Config{
		ActiveWorkspace: "test-workspace",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-workspace": {SlackToken: "xoxp-test-token"},
		},
		Sync: config.SyncConfig{
			Workers:            2,
			InitialHistoryDays: 30,
			SyncThreads:        false, // disabled!
		},
	}

	orch := NewOrchestrator(database, slackClient, cfg)
	orch.SetLogger(log.New(os.Stderr, "[test] ", 0))

	err = orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Should only have the parent message from history (no thread replies synced)
	msgs, err := database.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestSyncThreadsNoThreadsToSync(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Regular message",
				"type": "message", "reply_count": 0,
			},
		},
	}

	threadReplies := map[string][]map[string]any{}

	ts := newTestSetup(t, threadMux(channelMsgs, threadReplies))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Only the regular message, no thread sync needed
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestSyncThreadsMultipleThreads(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread 1",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000005.000000", "user": "U002", "text": "Thread 2",
				"type": "message", "reply_count": 1, "thread_ts": "1700000005.000000",
			},
		},
	}

	threadReplies := map[string][]map[string]any{
		"C001:1700000001.000000": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread 1",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000002.000000", "user": "U002", "text": "Reply to thread 1",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
		"C001:1700000005.000000": {
			{
				"ts": "1700000005.000000", "user": "U002", "text": "Thread 2",
				"type": "message", "reply_count": 1, "thread_ts": "1700000005.000000",
			},
			{
				"ts": "1700000006.000000", "user": "U001", "text": "Reply to thread 2",
				"type": "message", "thread_ts": "1700000005.000000",
			},
		},
	}

	ts := newTestSetup(t, threadMux(channelMsgs, threadReplies))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// 2 parents + 2 replies = 4 messages
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 4)
}

func TestSyncThreadsNonFatalErrorSkipsThread(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
		},
	}

	mux := baseMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		channelID := r.Form.Get("channel")

		msgs, ok := channelMsgs[channelID]
		if !ok {
			msgs = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          msgs,
			"has_more":          false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "channel_not_found",
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err) // non-fatal error should not fail the sync
}

func TestSyncThreadsAlreadySynced(t *testing.T) {
	// When conversations.history returns both the parent and its reply,
	// inline thread sync (Phase 3) will still call conversations.replies
	// for the thread parent. But Phase 5 (syncThreads) should find no
	// additional work because reply_count matches actual replies in DB.
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000002.000000", "user": "U002", "text": "Reply",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
	}

	threadReplies := map[string][]map[string]any{
		"C001:1700000001.000000": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread parent",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000002.000000", "user": "U002", "text": "Reply",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
	}

	ts := newTestSetup(t, threadMux(channelMsgs, threadReplies))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	// Should have parent + 1 reply = 2 messages
	msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)

	// Verify thread structure is correct
	replies, err := ts.db.GetThreadReplies("C001", "1700000001.000000")
	require.NoError(t, err)
	assert.Len(t, replies, 2) // parent + reply
}

func TestSyncThreadsCrossChannel(t *testing.T) {
	channelMsgs := map[string][]map[string]any{
		"C001": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread in general",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
		},
		"C002": {
			{
				"ts": "1700000010.000000", "user": "U002", "text": "Thread in engineering",
				"type": "message", "reply_count": 1, "thread_ts": "1700000010.000000",
			},
		},
	}

	threadReplies := map[string][]map[string]any{
		"C001:1700000001.000000": {
			{
				"ts": "1700000001.000000", "user": "U001", "text": "Thread in general",
				"type": "message", "reply_count": 1, "thread_ts": "1700000001.000000",
			},
			{
				"ts": "1700000002.000000", "user": "U002", "text": "Reply in general",
				"type": "message", "thread_ts": "1700000001.000000",
			},
		},
		"C002:1700000010.000000": {
			{
				"ts": "1700000010.000000", "user": "U002", "text": "Thread in engineering",
				"type": "message", "reply_count": 1, "thread_ts": "1700000010.000000",
			},
			{
				"ts": "1700000011.000000", "user": "U001", "text": "Reply in engineering",
				"type": "message", "thread_ts": "1700000010.000000",
			},
		},
	}

	ts := newTestSetup(t, threadMux(channelMsgs, threadReplies))
	err := ts.orch.Run(context.Background(), SyncOptions{Full: true})
	require.NoError(t, err)

	msgs1, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, msgs1, 2)

	msgs2, err := ts.db.GetMessagesByChannel("C002", 100)
	require.NoError(t, err)
	assert.Len(t, msgs2, 2)
}

func TestGetAllThreadParentsDB(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Insert a parent message with reply_count > 0
	err = database.UpsertMessage(db.Message{
		ChannelID:  "C001",
		TS:         "1700000001.000000",
		UserID:     "U001",
		Text:       "Parent",
		ReplyCount: 2,
	})
	require.NoError(t, err)

	// Insert a regular message with no replies
	err = database.UpsertMessage(db.Message{
		ChannelID:  "C001",
		TS:         "1700000005.000000",
		UserID:     "U002",
		Text:       "No replies",
		ReplyCount: 0,
	})
	require.NoError(t, err)

	// GetAllThreadParents should return only the parent with unsynced replies
	allParents, err := database.GetAllThreadParents(1000)
	require.NoError(t, err)
	assert.Len(t, allParents, 1)
	assert.Equal(t, "1700000001.000000", allParents[0].TS)
}

func TestGetAllThreadParentsAlreadySynced(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// Insert parent with reply_count=1
	err = database.UpsertMessage(db.Message{
		ChannelID:  "C001",
		TS:         "1700000001.000000",
		UserID:     "U001",
		Text:       "Parent",
		ReplyCount: 1,
	})
	require.NoError(t, err)

	// Insert the reply (thread_ts points to parent)
	err = database.UpsertMessage(db.Message{
		ChannelID: "C001",
		TS:        "1700000002.000000",
		UserID:    "U002",
		Text:      "Reply",
		ThreadTS:  sqlNullString("1700000001.000000"),
	})
	require.NoError(t, err)

	// Should return empty since reply_count matches actual reply count
	parents, err := database.GetAllThreadParents(1000)
	require.NoError(t, err)
	assert.Len(t, parents, 0)
}

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}
