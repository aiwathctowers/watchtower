package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	goslack "github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates a Client backed by the given test server.
func newTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	api := goslack.New("xoxp-test-token", goslack.OptionAPIURL(srv.URL+"/"))
	return NewClientWithAPI(api)
}

func TestGetTeamInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"ok": true,
			"team": map[string]any{
				"id":     "T024BE7LD",
				"name":   "my-company",
				"domain": "my-company",
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	c := newTestClient(t, mux)
	info, err := c.GetTeamInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "T024BE7LD", info.ID)
	assert.Equal(t, "my-company", info.Name)
	assert.Equal(t, "my-company", info.Domain)
}

func TestGetTeamInfoError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/team.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "invalid_auth",
		})
	})

	c := newTestClient(t, mux)
	_, err := c.GetTeamInfo(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_auth")
}

func TestGetUsers(t *testing.T) {
	callCount := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := callCount.Add(1)

		var members []map[string]any
		nextCursor := ""

		if page == 1 {
			members = []map[string]any{
				{"id": "U001", "name": "alice", "real_name": "Alice Smith"},
				{"id": "U002", "name": "bob", "real_name": "Bob Jones"},
			}
			nextCursor = "page2"
		} else {
			members = []map[string]any{
				{"id": "U003", "name": "carol", "real_name": "Carol White"},
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": members,
			"response_metadata": map[string]any{
				"next_cursor": nextCursor,
			},
		})
	})

	c := newTestClient(t, mux)
	users, err := c.GetUsers(context.Background())
	require.NoError(t, err)
	assert.Len(t, users, 3)
	assert.Equal(t, "U001", users[0].ID)
	assert.Equal(t, "U003", users[2].ID)
}

func TestGetChannels(t *testing.T) {
	callCount := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := callCount.Add(1)

		var channels []map[string]any
		nextCursor := ""

		if page == 1 {
			channels = []map[string]any{
				{"id": "C001", "name": "general", "is_channel": true, "is_member": true},
				{"id": "C002", "name": "random", "is_channel": true, "is_member": true},
			}
			nextCursor = "page2"
		} else {
			channels = []map[string]any{
				{"id": "C003", "name": "dev", "is_channel": true, "is_member": false},
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"channels": channels,
			"response_metadata": map[string]any{
				"next_cursor": nextCursor,
			},
		})
	})

	c := newTestClient(t, mux)
	channels, err := c.GetChannels(context.Background())
	require.NoError(t, err)
	assert.Len(t, channels, 3)
	assert.Equal(t, "C001", channels[0].ID)
	assert.Equal(t, "general", channels[0].Name)
	assert.Equal(t, "C003", channels[2].ID)
}

func TestGetConversationHistory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"has_more": true,
			"messages": []map[string]any{
				{"type": "message", "user": "U001", "text": "Hello", "ts": "1700000001.000000"},
				{"type": "message", "user": "U002", "text": "World", "ts": "1700000002.000000"},
			},
			"response_metadata": map[string]any{
				"next_cursor": "next_page_cursor",
			},
		})
	})

	c := newTestClient(t, mux)
	resp, err := c.GetConversationHistory(context.Background(), HistoryOptions{
		ChannelID: "C001",
		Limit:     100,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Messages, 2)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "next_page_cursor", resp.NextCursor)
	assert.Equal(t, "Hello", resp.Messages[0].Text)
}

func TestGetConversationHistoryDefaultLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"has_more": false,
			"messages": []map[string]any{},
			"response_metadata": map[string]any{
				"next_cursor": "",
			},
		})
	})

	c := newTestClient(t, mux)
	resp, err := c.GetConversationHistory(context.Background(), HistoryOptions{
		ChannelID: "C001",
		// Limit not set - should default to 200
	})
	require.NoError(t, err)
	assert.False(t, resp.HasMore)
}

func TestGetConversationReplies(t *testing.T) {
	callCount := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := callCount.Add(1)

		var msgs []map[string]any
		hasMore := false
		nextCursor := ""

		if page == 1 {
			msgs = []map[string]any{
				{"type": "message", "user": "U001", "text": "Thread parent", "ts": "1700000001.000000", "thread_ts": "1700000001.000000"},
				{"type": "message", "user": "U002", "text": "Reply 1", "ts": "1700000002.000000", "thread_ts": "1700000001.000000"},
			}
			hasMore = true
			nextCursor = "page2"
		} else {
			msgs = []map[string]any{
				{"type": "message", "user": "U003", "text": "Reply 2", "ts": "1700000003.000000", "thread_ts": "1700000001.000000"},
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"has_more": hasMore,
			"messages": msgs,
			"response_metadata": map[string]any{
				"next_cursor": nextCursor,
			},
		})
	})

	c := newTestClient(t, mux)
	msgs, err := c.GetConversationReplies(context.Background(), "C001", "1700000001.000000")
	require.NoError(t, err)
	assert.Len(t, msgs, 3)
	assert.Equal(t, "Thread parent", msgs[0].Text)
	assert.Equal(t, "Reply 2", msgs[2].Text)
}

func TestContextCancellationDuringGetUsers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"members": []map[string]any{},
			"response_metadata": map[string]any{
				"next_cursor": "",
			},
		})
	})

	c := newTestClient(t, mux)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetUsers(ctx)
	assert.Error(t, err)
}

func TestNewClient(t *testing.T) {
	c := NewClient("xoxp-test")
	assert.NotNil(t, c.api)
	assert.NotNil(t, c.rateLimiter)
}
