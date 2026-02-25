package sync

import (
	"context"
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

// testSetup creates an in-memory DB, mock Slack server, and orchestrator for testing.
type testSetup struct {
	db   *db.DB
	orch *Orchestrator
	srv  *httptest.Server
}

func newTestSetup(t *testing.T, mux *http.ServeMux) *testSetup {
	t.Helper()

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
			SyncThreads:        true,
		},
	}

	orch := NewOrchestrator(database, slackClient, cfg)
	orch.SetLogger(log.New(os.Stderr, "[test] ", 0))

	return &testSetup{db: database, orch: orch, srv: srv}
}

// baseMux creates a mock Slack API server with metadata endpoints
// (team.info, users.list, conversations.list) but no conversations.history.
// Use defaultMux() for a fully functional mock, or call baseMux() and add
// your own conversations.history handler.
func baseMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"team": map[string]any{
				"id":     "T024BE7LD",
				"name":   "my-company",
				"domain": "my-company",
			},
		})
	})

	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"members": []map[string]any{
				{
					"id": "U001", "name": "alice", "real_name": "Alice Smith",
					"is_bot": false, "deleted": false,
					"profile": map[string]any{"display_name": "alice", "email": "alice@example.com"},
				},
				{
					"id": "U002", "name": "bob", "real_name": "Bob Jones",
					"is_bot": false, "deleted": false,
					"profile": map[string]any{"display_name": "bob", "email": "bob@example.com"},
				},
				{
					"id": "U003", "name": "slackbot", "real_name": "Slackbot",
					"is_bot": true, "deleted": false,
					"profile": map[string]any{"display_name": "Slackbot"},
				},
			},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"channels": []map[string]any{
				{
					"id": "C001", "name": "general", "is_channel": true, "is_member": true,
					"num_members": 50, "is_archived": false,
					"topic": map[string]any{"value": "General chat"},
					"purpose": map[string]any{"value": "Company-wide announcements"},
				},
				{
					"id": "C002", "name": "engineering", "is_channel": true, "is_member": true,
					"num_members": 20, "is_archived": false,
					"topic": map[string]any{"value": "Engineering discussion"},
					"purpose": map[string]any{"value": ""},
				},
				{
					"id": "C003", "name": "old-project", "is_channel": true, "is_member": false,
					"num_members": 5, "is_archived": true,
					"topic": map[string]any{"value": ""},
					"purpose": map[string]any{"value": ""},
				},
			},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	return mux
}

// defaultMux creates a mock Slack API server with standard responses,
// including an empty conversations.history and conversations.replies handler.
func defaultMux() *http.ServeMux {
	mux := baseMux()

	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": []any{},
			"has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": []any{},
			"has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	return mux
}

func TestNewOrchestrator(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	assert.NotNil(t, ts.orch)
	assert.NotNil(t, ts.orch.db)
	assert.NotNil(t, ts.orch.slackClient)
	assert.NotNil(t, ts.orch.config)
	assert.NotNil(t, ts.orch.logger)
}

func TestRunFullSync(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.Run(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// Verify workspace was synced
	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, "T024BE7LD", ws.ID)
	assert.Equal(t, "my-company", ws.Name)
	assert.Equal(t, "my-company", ws.Domain)

	// Verify users were synced
	users, err := ts.db.GetUsers(db.UserFilter{})
	require.NoError(t, err)
	assert.Len(t, users, 3)

	alice, err := ts.db.GetUserByName("alice")
	require.NoError(t, err)
	require.NotNil(t, alice)
	assert.Equal(t, "Alice Smith", alice.RealName)
	assert.False(t, alice.IsBot)

	bot, err := ts.db.GetUserByName("slackbot")
	require.NoError(t, err)
	require.NotNil(t, bot)
	assert.True(t, bot.IsBot)

	// Verify channels were synced
	channels, err := ts.db.GetChannels(db.ChannelFilter{})
	require.NoError(t, err)
	assert.Len(t, channels, 3)

	general, err := ts.db.GetChannelByName("general")
	require.NoError(t, err)
	require.NotNil(t, general)
	assert.Equal(t, "public", general.Type)
	assert.True(t, general.IsMember)
	assert.Equal(t, 50, general.NumMembers)
	assert.Equal(t, "General chat", general.Topic)

	archived, err := ts.db.GetChannelByName("old-project")
	require.NoError(t, err)
	require.NotNil(t, archived)
	assert.True(t, archived.IsArchived)
	assert.False(t, archived.IsMember)
}

func TestSyncMetadataWorkspaceUpsert(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background())
	require.NoError(t, err)

	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, "T024BE7LD", ws.ID)
	assert.True(t, ws.SyncedAt.Valid)

	// Run again — should update, not fail
	err = ts.orch.syncMetadata(context.Background())
	require.NoError(t, err)

	ws2, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	assert.Equal(t, ws.ID, ws2.ID)
}

func TestSyncDetectsDeletedUsers(t *testing.T) {
	ts := newTestSetup(t, defaultMux())

	// Pre-populate a user that won't be in the API response
	err := ts.db.UpsertUser(db.User{
		ID:   "U999",
		Name: "departed",
	})
	require.NoError(t, err)

	err = ts.orch.syncMetadata(context.Background())
	require.NoError(t, err)

	departed, err := ts.db.GetUserByID("U999")
	require.NoError(t, err)
	require.NotNil(t, departed)
	assert.True(t, departed.IsDeleted, "user not returned by API should be marked deleted")
}

func TestSyncChannelTypes(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"team": map[string]any{"id": "T001", "name": "test", "domain": "test"},
		})
	})

	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"channels": []map[string]any{
				{
					"id": "C001", "name": "general", "is_channel": true,
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""},
				},
				{
					"id": "G001", "name": "secret", "is_channel": false, "is_group": true, "is_private": true,
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""},
				},
				{
					"id": "D001", "name": "", "is_im": true, "user": "U001",
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""},
				},
				{
					"id": "G002", "name": "group-dm", "is_mpim": true,
					"topic": map[string]any{"value": ""}, "purpose": map[string]any{"value": ""},
				},
			},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.syncMetadata(context.Background())
	require.NoError(t, err)

	ch, err := ts.db.GetChannelByID("C001")
	require.NoError(t, err)
	assert.Equal(t, "public", ch.Type)

	ch, err = ts.db.GetChannelByID("G001")
	require.NoError(t, err)
	assert.Equal(t, "private", ch.Type)

	ch, err = ts.db.GetChannelByID("D001")
	require.NoError(t, err)
	assert.Equal(t, "dm", ch.Type)
	assert.Equal(t, "U001", ch.DMUserID.String)

	ch, err = ts.db.GetChannelByID("G002")
	require.NoError(t, err)
	assert.Equal(t, "group_dm", ch.Type)
}

func TestSyncContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"team": map[string]any{"id": "T001", "name": "test", "domain": "test"},
		})
	})
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		// Don't respond — the context should already be canceled
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": []map[string]any{},
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	ts := newTestSetup(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := ts.orch.Run(ctx, SyncOptions{})
	assert.Error(t, err)
}

func TestSyncTeamInfoError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "invalid_auth",
		})
	})

	ts := newTestSetup(t, mux)
	err := ts.orch.Run(context.Background(), SyncOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata sync")
}

func TestSyncOptionsDefaults(t *testing.T) {
	opts := SyncOptions{}
	assert.False(t, opts.Full)
	assert.Empty(t, opts.Channels)
	assert.Equal(t, 0, opts.Workers)
}

func TestIsNonFatalError(t *testing.T) {
	tests := []struct {
		err      string
		expected bool
	}{
		{"channel_not_found", true},
		{"account_inactive", true},
		{"is_archived", true},
		{"not_in_channel", true},
		{"missing_scope", true},
		{"access_denied", true},
		{"some random db error", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.err, func(t *testing.T) {
			if tt.err == "" {
				assert.False(t, isNonFatalError(nil))
			} else {
				assert.Equal(t, tt.expected, isNonFatalError(errFromString(tt.err)))
			}
		})
	}
}

func TestSlackChannelType(t *testing.T) {
	tests := []struct {
		name     string
		channel  goslack.Channel
		expected string
	}{
		{
			name:     "public channel",
			channel:  goslack.Channel{},
			expected: "public",
		},
		{
			name: "private channel",
			channel: goslack.Channel{
				GroupConversation: goslack.GroupConversation{
					Conversation: goslack.Conversation{IsPrivate: true},
				},
			},
			expected: "private",
		},
		{
			name: "DM",
			channel: goslack.Channel{
				GroupConversation: goslack.GroupConversation{
					Conversation: goslack.Conversation{IsIM: true},
				},
			},
			expected: "dm",
		},
		{
			name: "group DM",
			channel: goslack.Channel{
				GroupConversation: goslack.GroupConversation{
					Conversation: goslack.Conversation{IsMpIM: true},
				},
			},
			expected: "group_dm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, slackChannelType(tt.channel))
		})
	}
}

func TestUserProfileJSONStored(t *testing.T) {
	ts := newTestSetup(t, defaultMux())
	err := ts.orch.syncMetadata(context.Background())
	require.NoError(t, err)

	alice, err := ts.db.GetUserByName("alice")
	require.NoError(t, err)
	require.NotNil(t, alice)

	// Verify profile JSON was stored and is valid JSON
	assert.NotEmpty(t, alice.ProfileJSON)
	var profile map[string]any
	err = json.Unmarshal([]byte(alice.ProfileJSON), &profile)
	assert.NoError(t, err, "profile_json should be valid JSON")
	assert.Equal(t, "alice@example.com", profile["email"])
}

// integrationMux creates a mock Slack API server where conversations.history returns
// different messages per channel, and conversations.replies returns thread replies.
func integrationMux(channelMessages map[string][]map[string]any, threadReplies map[string][]map[string]any) *http.ServeMux {
	mux := baseMux()

	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		channelID := r.FormValue("channel")
		msgs, ok := channelMessages[channelID]
		if !ok {
			msgs = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": msgs,
			"has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		channelID := r.FormValue("channel")
		threadTS := r.FormValue("ts")
		key := channelID + "|" + threadTS
		replies, ok := threadReplies[key]
		if !ok {
			replies = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": replies,
			"has_more": false,
			"response_metadata": map[string]any{"next_cursor": ""},
		})
	})

	return mux
}

// TestIntegrationSyncFlow runs a full sync with canned Slack API responses
// (workspace info, users, channels, messages, thread replies) and verifies
// that the database contains the expected data after sync completes.
func TestIntegrationSyncFlow(t *testing.T) {
	// Define messages per channel
	channelMessages := map[string][]map[string]any{
		"C001": {
			{
				"type": "message", "user": "U001",
				"text": "Deploying v2.3 to production",
				"ts":   "1740567600.000100",
				"reply_count": 2, "thread_ts": "1740567600.000100",
			},
			{
				"type": "message", "user": "U002",
				"text": "Monitoring dashboards now",
				"ts":   "1740567600.000200",
			},
		},
		"C002": {
			{
				"type": "message", "user": "U001",
				"text": "New design mockups ready for review",
				"ts":   "1740567600.000300",
			},
		},
		// C003 (old-project) is archived and won't be synced by default
	}

	// Define thread replies: parent message + 2 replies
	threadReplies := map[string][]map[string]any{
		"C001|1740567600.000100": {
			{
				"type": "message", "user": "U001",
				"text": "Deploying v2.3 to production",
				"ts":   "1740567600.000100",
				"thread_ts": "1740567600.000100",
				"reply_count": 2,
			},
			{
				"type": "message", "user": "U002",
				"text": "Looks good, I'll keep an eye on metrics",
				"ts":   "1740567600.000150",
				"thread_ts": "1740567600.000100",
			},
			{
				"type": "message", "user": "U003",
				"text": "No breaking changes in my service",
				"ts":   "1740567600.000160",
				"thread_ts": "1740567600.000100",
			},
		},
	}

	mux := integrationMux(channelMessages, threadReplies)
	ts := newTestSetup(t, mux)

	err := ts.orch.Run(context.Background(), SyncOptions{})
	require.NoError(t, err)

	// --- Verify workspace ---
	ws, err := ts.db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, ws)
	assert.Equal(t, "T024BE7LD", ws.ID)
	assert.Equal(t, "my-company", ws.Name)
	assert.Equal(t, "my-company", ws.Domain)

	// --- Verify users ---
	users, err := ts.db.GetUsers(db.UserFilter{})
	require.NoError(t, err)
	assert.Len(t, users, 3)

	alice, err := ts.db.GetUserByName("alice")
	require.NoError(t, err)
	require.NotNil(t, alice)
	assert.Equal(t, "U001", alice.ID)
	assert.Equal(t, "Alice Smith", alice.RealName)

	// --- Verify channels ---
	channels, err := ts.db.GetChannels(db.ChannelFilter{})
	require.NoError(t, err)
	assert.Len(t, channels, 3)

	general, err := ts.db.GetChannelByName("general")
	require.NoError(t, err)
	require.NotNil(t, general)
	assert.Equal(t, "C001", general.ID)
	assert.True(t, general.IsMember)

	engineering, err := ts.db.GetChannelByName("engineering")
	require.NoError(t, err)
	require.NotNil(t, engineering)
	assert.Equal(t, "C002", engineering.ID)

	// --- Verify messages in #general (C001) ---
	c001Msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, c001Msgs, 4) // 2 history messages + 2 thread replies

	// Verify a specific message
	found := false
	for _, m := range c001Msgs {
		if m.TS == "1740567600.000100" {
			found = true
			assert.Equal(t, "U001", m.UserID)
			assert.Equal(t, "Deploying v2.3 to production", m.Text)
		}
	}
	assert.True(t, found, "expected to find the deployment message")

	// --- Verify messages in #engineering (C002) ---
	c002Msgs, err := ts.db.GetMessagesByChannel("C002", 100)
	require.NoError(t, err)
	assert.Len(t, c002Msgs, 1)
	assert.Equal(t, "New design mockups ready for review", c002Msgs[0].Text)

	// --- Verify thread replies were synced ---
	threadMsgs, err := ts.db.GetThreadReplies("C001", "1740567600.000100")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(threadMsgs), 2, "thread should have at least 2 replies")

	replyFound := false
	for _, m := range threadMsgs {
		if m.TS == "1740567600.000150" {
			replyFound = true
			assert.Equal(t, "U002", m.UserID)
			assert.Equal(t, "Looks good, I'll keep an eye on metrics", m.Text)
		}
	}
	assert.True(t, replyFound, "expected to find thread reply from bob")

	// --- Verify sync state ---
	syncState, err := ts.db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, syncState)
	assert.True(t, syncState.IsInitialSyncComplete)
	assert.Greater(t, syncState.MessagesSynced, 0)

	// --- Verify archived channel (C003) was NOT synced for messages ---
	c003Msgs, err := ts.db.GetMessagesByChannel("C003", 100)
	require.NoError(t, err)
	assert.Len(t, c003Msgs, 0, "archived channel should not have messages synced")
}

// TestIntegrationSyncWithChannelFilter verifies that passing --channels
// limits the sync to only the specified channels.
func TestIntegrationSyncWithChannelFilter(t *testing.T) {
	channelMessages := map[string][]map[string]any{
		"C001": {
			{"type": "message", "user": "U001", "text": "General msg", "ts": "1740567600.000100"},
		},
		"C002": {
			{"type": "message", "user": "U002", "text": "Engineering msg", "ts": "1740567600.000200"},
		},
	}

	mux := integrationMux(channelMessages, nil)
	ts := newTestSetup(t, mux)

	// Sync only "general" channel
	err := ts.orch.Run(context.Background(), SyncOptions{
		Channels: []string{"general"},
	})
	require.NoError(t, err)

	// General should have messages
	c001Msgs, err := ts.db.GetMessagesByChannel("C001", 100)
	require.NoError(t, err)
	assert.Len(t, c001Msgs, 1)

	// Engineering should NOT have messages (wasn't requested)
	c002Msgs, err := ts.db.GetMessagesByChannel("C002", 100)
	require.NoError(t, err)
	assert.Len(t, c002Msgs, 0)
}

// errFromString creates an error with the given string.
type stringError string

func (e stringError) Error() string { return string(e) }

func errFromString(s string) error { return stringError(s) }
