package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
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
	channels, err := c.GetChannels(context.Background(), nil)
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

// newUnlimitedTestClient creates a Client with unlimited rate limiter for fast tests.
func newUnlimitedTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	api := goslack.New("xoxp-test-token", goslack.OptionAPIURL(srv.URL+"/"))
	return NewClientWithAPIUnlimited(api)
}

func TestNewClientWithAPIUnlimited(t *testing.T) {
	api := goslack.New("xoxp-test")
	c := NewClientWithAPIUnlimited(api)
	assert.NotNil(t, c.api)
	assert.NotNil(t, c.rateLimiter)
	assert.Nil(t, c.rateLimiter.gate, "unlimited limiter should have nil gate")
}

func TestAuthTest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"user_id": "U12345",
			"user":    "testuser",
			"team_id": "T67890",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	resp, err := c.AuthTest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "U12345", resp.UserID)
	assert.Equal(t, "testuser", resp.User)
	assert.Equal(t, "T67890", resp.TeamID)
}

func TestAuthTestError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "invalid_auth",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	resp, err := c.AuthTest(context.Background())
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid_auth")
}

func TestSearchMessages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search.messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"messages": map[string]any{
				"total": 42,
				"paging": map[string]any{
					"page":  1,
					"pages": 2,
					"count": 100,
					"total": 42,
				},
				"matches": []map[string]any{
					{
						"type":    "message",
						"text":    "found message",
						"ts":      "1700000001.000000",
						"channel": map[string]any{"id": "C001", "name": "general"},
					},
				},
			},
		})
	})

	c := newUnlimitedTestClient(t, mux)
	result, err := c.SearchMessages(context.Background(), "test query", 1)
	require.NoError(t, err)
	assert.Equal(t, 42, result.Total)
	assert.Equal(t, 1, result.Page)
	assert.Equal(t, 2, result.Pages)
	assert.Len(t, result.Messages, 1)
}

func TestSearchMessagesError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search.messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "not_authed",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	result, err := c.SearchMessages(context.Background(), "query", 1)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestGetUserInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"user": map[string]any{
				"id":        "U12345",
				"name":      "alice",
				"real_name": "Alice Smith",
			},
		})
	})

	c := newUnlimitedTestClient(t, mux)
	user, err := c.GetUserInfo(context.Background(), "U12345")
	require.NoError(t, err)
	assert.Equal(t, "U12345", user.ID)
	assert.Equal(t, "alice", user.Name)
	assert.Equal(t, "Alice Smith", user.RealName)
}

func TestGetUserInfoError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "user_not_found",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	user, err := c.GetUserInfo(context.Background(), "UBAD")
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "user_not_found")
}

func TestGetEmoji(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"emoji": map[string]any{
				"shipit":   "https://example.com/shipit.png",
				"thumbsup": "alias:+1",
			},
		})
	})

	c := newUnlimitedTestClient(t, mux)
	emojis, err := c.GetEmoji(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/shipit.png", emojis["shipit"])
	assert.Equal(t, "alias:+1", emojis["thumbsup"])
}

func TestGetEmojiError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/emoji.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "not_authed",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	emojis, err := c.GetEmoji(context.Background())
	assert.Error(t, err)
	assert.Nil(t, emojis)
}

func TestAPIStatsAndReset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"user_id": "U001",
			"user":    "u",
			"team_id": "T001",
		})
	})
	mux.HandleFunc("/users.info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"user": map[string]any{"id": "U001", "name": "a"},
		})
	})

	c := newUnlimitedTestClient(t, mux)

	// Make some requests across different tiers
	_, _ = c.AuthTest(context.Background())            // Tier2
	_, _ = c.AuthTest(context.Background())            // Tier2
	_, _ = c.GetUserInfo(context.Background(), "U001") // Tier4

	counts, retries := c.APIStats()
	assert.Equal(t, 2, counts[Tier2])
	assert.Equal(t, 1, counts[Tier4])
	assert.Equal(t, 0, retries)

	// Reset and verify
	c.ResetAPIStats()
	counts, retries = c.APIStats()
	assert.Equal(t, 0, counts[Tier2])
	assert.Equal(t, 0, counts[Tier4])
	assert.Equal(t, 0, retries)
}

func TestSetLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "test: ", 0)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth.test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"user_id": "U001",
			"user":    "u",
			"team_id": "T001",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	c.SetLogger(logger)
	assert.Equal(t, logger, c.logger)
	assert.Equal(t, logger, c.rateLimiter.logger)

	_, err := c.AuthTest(context.Background())
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "auth.test")
}

func TestLogfNoLogger(t *testing.T) {
	c := &Client{}
	// Should not panic when logger is nil
	c.logf("test %s", "message")
}

func TestGetUsersWithOnPageCallback(t *testing.T) {
	callCount := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := callCount.Add(1)

		var members []map[string]any
		nextCursor := ""

		if page == 1 {
			members = []map[string]any{
				{"id": "U001", "name": "alice"},
				{"id": "U002", "name": "bob"},
			}
			nextCursor = "page2"
		} else {
			members = []map[string]any{
				{"id": "U003", "name": "carol"},
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

	c := newUnlimitedTestClient(t, mux)
	var pageCounts []int
	users, err := c.GetUsers(context.Background(), func(fetched int) {
		pageCounts = append(pageCounts, fetched)
	})
	require.NoError(t, err)
	assert.Len(t, users, 3)
	assert.Equal(t, []int{2, 3}, pageCounts)
}

func TestGetChannelsWithOnPageCallback(t *testing.T) {
	callCount := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := callCount.Add(1)

		var channels []map[string]any
		nextCursor := ""

		if page == 1 {
			channels = []map[string]any{
				{"id": "C001", "name": "general", "is_channel": true},
			}
			nextCursor = "page2"
		} else {
			channels = []map[string]any{
				{"id": "C002", "name": "random", "is_channel": true},
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

	c := newUnlimitedTestClient(t, mux)
	var pageCounts []int
	channels, err := c.GetChannels(context.Background(), []string{"public_channel"}, func(fetched int) {
		pageCounts = append(pageCounts, fetched)
	})
	require.NoError(t, err)
	assert.Len(t, channels, 2)
	assert.Equal(t, []int{1, 2}, pageCounts)
}

func TestGetChannelsDefaultTypes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"channels": []map[string]any{},
			"response_metadata": map[string]any{
				"next_cursor": "",
			},
		})
	})

	c := newUnlimitedTestClient(t, mux)
	// Pass empty types — should default to public_channel, private_channel
	channels, err := c.GetChannels(context.Background(), []string{})
	require.NoError(t, err)
	assert.Empty(t, channels)
}

func TestGetConversationHistoryError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "channel_not_found",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	resp, err := c.GetConversationHistory(context.Background(), HistoryOptions{ChannelID: "CBAD"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetConversationRepliesError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"error": "thread_not_found",
		})
	})

	c := newUnlimitedTestClient(t, mux)
	msgs, err := c.GetConversationReplies(context.Background(), "C001", "1700000001.000000")
	assert.Error(t, err)
	assert.Empty(t, msgs)
}

func TestHandleErrorNilError(t *testing.T) {
	c := &Client{rateLimiter: NewUnlimitedRateLimiter()}
	// Should not panic on nil error
	c.handleError(Tier2, nil)
}
