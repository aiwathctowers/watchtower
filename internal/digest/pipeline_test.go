package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// mockGenerator returns a fixed response for testing.
type mockGenerator struct {
	response string
	err      error
	calls    int
}

func (m *mockGenerator) Generate(_ context.Context, _, _ string) (string, *Usage, error) {
	m.calls++
	return m.response, &Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}, m.err
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

func testConfig() *config.Config {
	return &config.Config{
		Digest: config.DigestConfig{
			Enabled:     true,
			Model:       "haiku",
			MinMessages: 3,
		},
	}
}

func testLogger() *log.Logger {
	return log.New(os.Stderr, "test: ", 0)
}

func seedChannel(t *testing.T, database *db.DB, id, name string) {
	t.Helper()
	err := database.UpsertChannel(db.Channel{ID: id, Name: name, Type: "public"})
	require.NoError(t, err)
}

func seedUser(t *testing.T, database *db.DB, id, name, displayName string) {
	t.Helper()
	err := database.UpsertUser(db.User{ID: id, Name: name, DisplayName: displayName})
	require.NoError(t, err)
}

func seedMessage(t *testing.T, database *db.DB, channelID, ts, userID, text string) {
	t.Helper()
	err := database.UpsertMessage(db.Message{
		ChannelID: channelID,
		TS:        ts,
		UserID:    userID,
		Text:      text,
	})
	require.NoError(t, err)
}

func TestParseDigestResult(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    DigestResult
		wantErr bool
	}{
		{
			name:  "clean JSON",
			input: `{"summary":"test","topics":["a"],"decisions":[],"action_items":[],"key_messages":[]}`,
			want:  DigestResult{Summary: "test", Topics: []string{"a"}, Decisions: []Decision{}, ActionItems: []ActionItem{}, KeyMessages: []string{}},
		},
		{
			name:  "wrapped in markdown fences",
			input: "```json\n{\"summary\":\"test\",\"topics\":[],\"decisions\":[],\"action_items\":[],\"key_messages\":[]}\n```",
			want:  DigestResult{Summary: "test", Topics: []string{}, Decisions: []Decision{}, ActionItems: []ActionItem{}, KeyMessages: []string{}},
		},
		{
			name:  "with preamble text",
			input: "Here is the analysis:\n{\"summary\":\"test\",\"topics\":[],\"decisions\":[],\"action_items\":[],\"key_messages\":[]}",
			want:  DigestResult{Summary: "test", Topics: []string{}, Decisions: []Decision{}, ActionItems: []ActionItem{}, KeyMessages: []string{}},
		},
		{
			name:  "with decisions and action items",
			input: `{"summary":"deploy discussed","topics":["deploy"],"decisions":[{"text":"deploy Friday","by":"@alice","message_ts":"1000.001"}],"action_items":[{"text":"write tests","assignee":"@bob","status":"open"}],"key_messages":["1000.001"]}`,
			want: DigestResult{
				Summary:     "deploy discussed",
				Topics:      []string{"deploy"},
				Decisions:   []Decision{{Text: "deploy Friday", By: "@alice", MessageTS: "1000.001"}},
				ActionItems: []ActionItem{{Text: "write tests", Assignee: "@bob", Status: "open"}},
				KeyMessages: []string{"1000.001"},
			},
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDigestResult(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.Summary, got.Summary)
			assert.Equal(t, tt.want.Topics, got.Topics)
			assert.Equal(t, tt.want.Decisions, got.Decisions)
			assert.Equal(t, tt.want.ActionItems, got.ActionItems)
		})
	}
}

func TestRunChannelDigests(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "engineering")
	seedUser(t, database, "U1", "alice", "Alice")

	// Seed 5 recent messages (meets min_messages=3)
	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		ts := fmt.Sprintf("%d.%06d", now-3600+int64(i*60), i)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("message %d about deployment", i))
	}

	mockResp := `{"summary":"Team discussed deployment","topics":["deployment"],"decisions":[],"action_items":[],"key_messages":[]}`
	gen := &mockGenerator{response: mockResp}

	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, gen.calls)

	// Verify stored digest
	digests, err := database.GetDigests(db.DigestFilter{Type: "channel"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "C1", digests[0].ChannelID)
	assert.Equal(t, "Team discussed deployment", digests[0].Summary)
	assert.Equal(t, 5, digests[0].MessageCount)
	assert.Equal(t, "haiku", digests[0].Model)
}

func TestRunChannelDigests_BelowMinMessages(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 10

	seedChannel(t, database, "C1", "quiet")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		ts := fmt.Sprintf("%d.%06d", now-3600+int64(i*60), i)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	gen := &mockGenerator{response: `{}`}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, gen.calls)
}

func TestRunChannelDigests_Disabled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Enabled = false

	gen := &mockGenerator{response: `{}`}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, gen.calls)
}

func TestRunChannelDigests_NoNewMessages(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	gen := &mockGenerator{response: `{}`}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, gen.calls)
}

func TestRunChannelDigests_AIError(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "engineering")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		ts := fmt.Sprintf("%d.%06d", now-3600+int64(i*60), i)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	gen := &mockGenerator{err: fmt.Errorf("AI unavailable")}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.Error(t, err) // all channels failed → returns error
	assert.Contains(t, err.Error(), "AI unavailable")
	assert.Equal(t, 0, n)
	assert.Equal(t, 1, gen.calls)
}

func TestRunDailyRollup(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "frontend")
	seedChannel(t, database, "C2", "backend")

	// Use dayStart so digests are always "today" regardless of UTC hour
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	fromUnix := float64(dayStart.Unix())
	toUnix := float64(now.Unix())

	// Pre-populate channel digests for today
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Frontend team fixed CSS bugs", MessageCount: 15, Model: "haiku",
	})
	require.NoError(t, err)

	_, err = database.UpsertDigest(db.Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Backend team deployed API v2", MessageCount: 20, Model: "haiku",
	})
	require.NoError(t, err)

	rollupResp := `{"summary":"Active day: frontend fixed bugs, backend deployed API v2","topics":["deployment","bugfix"],"decisions":[],"action_items":[],"key_messages":[]}`
	gen := &mockGenerator{response: rollupResp}

	p := New(database, cfg, gen, testLogger())
	err = p.RunDailyRollup(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, gen.calls)

	// Verify daily digest stored
	digests, err := database.GetDigests(db.DigestFilter{Type: "daily"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Contains(t, digests[0].Summary, "frontend fixed bugs")
}

func TestRunDailyRollup_NotEnoughDigests(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	// Only one channel digest — not enough for rollup
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(time.Now().Unix() - 3600), PeriodTo: float64(time.Now().Unix()),
		Summary: "Only one", MessageCount: 5, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: `{}`}
	p := New(database, cfg, gen, testLogger())
	err = p.RunDailyRollup(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, gen.calls)
}

func TestRunWeeklyTrends(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	// Pre-populate daily digests
	for i := 0; i < 3; i++ {
		dayOffset := int64(i * 86400)
		_, err := database.UpsertDigest(db.Digest{
			Type:         "daily",
			PeriodFrom:   float64(time.Now().Unix() - dayOffset - 86400),
			PeriodTo:     float64(time.Now().Unix() - dayOffset),
			Summary:      fmt.Sprintf("Day %d summary", i),
			MessageCount: 30 + i*10, Model: "haiku",
		})
		require.NoError(t, err)
	}

	weeklyResp := `{"summary":"Productive week with deployments and bug fixes","topics":["deployment","performance"],"decisions":[],"action_items":[],"key_messages":[]}`
	gen := &mockGenerator{response: weeklyResp}

	p := New(database, cfg, gen, testLogger())
	err := p.RunWeeklyTrends(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, gen.calls)

	digests, err := database.GetDigests(db.DigestFilter{Type: "weekly"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Contains(t, digests[0].Summary, "Productive week")
}

func TestFormatMessages(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedUser(t, database, "U1", "alice", "Alice Smith")

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	msgs := []db.Message{
		{UserID: "U1", Text: "hello world", TSUnix: 1000000},
		{UserID: "U1", Text: "another message", TSUnix: 1000060},
		{UserID: "U1", Text: "", TSUnix: 1000120},                         // empty — should be skipped
		{UserID: "U1", Text: "deleted", TSUnix: 1000180, IsDeleted: true}, // deleted — skipped
	}

	formatted := p.formatMessages(msgs)
	assert.Contains(t, formatted, "@Alice Smith: hello world")
	assert.Contains(t, formatted, "@Alice Smith: another message")
	assert.NotContains(t, formatted, "deleted")
	assert.NotContains(t, formatted, "empty")
}

func TestStoreDigest(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())

	result := &DigestResult{
		Summary:     "test summary",
		Topics:      []string{"topic1", "topic2"},
		Decisions:   []Decision{{Text: "decided X", By: "@alice"}},
		ActionItems: []ActionItem{{Text: "do Y", Assignee: "@bob", Status: "open"}},
	}

	err := p.storeDigest("C1", "channel", 1000.0, 2000.0, result, 42, &Usage{InputTokens: 500, OutputTokens: 200, CostUSD: 0.005}, 0)
	require.NoError(t, err)

	d, err := database.GetLatestDigest("C1", "channel")
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, "test summary", d.Summary)
	assert.Equal(t, 42, d.MessageCount)

	// Verify JSON fields
	var topics []string
	require.NoError(t, json.Unmarshal([]byte(d.Topics), &topics))
	assert.Equal(t, []string{"topic1", "topic2"}, topics)

	var decisions []Decision
	require.NoError(t, json.Unmarshal([]byte(d.Decisions), &decisions))
	assert.Len(t, decisions, 1)
	assert.Equal(t, "decided X", decisions[0].Text)
}
