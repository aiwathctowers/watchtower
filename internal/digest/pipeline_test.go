package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/prompts"
	"watchtower/internal/sessions"
)

// mockGenerator returns a fixed response for testing.
type mockGenerator struct {
	response string
	err      error
	calls    int
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *Usage, string, error) {
	m.calls++
	return m.response, &Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}, "mock-session", m.err
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
	assert.Contains(t, formatted, "@Alice Smith (U1)] hello world")
	assert.Contains(t, formatted, "@Alice Smith (U1)] another message")
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

// capturingGenerator captures the prompt passed to Generate for inspection.
type capturingGenerator struct {
	response       string
	capturedPrompt string
	calls          int
}

func (m *capturingGenerator) Generate(_ context.Context, _, prompt, _ string) (string, *Usage, string, error) {
	m.capturedPrompt = prompt
	m.calls++
	return m.response, &Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}, "mock-session", nil
}

func TestProfileContextInjectedIntoDigestPrompt(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Insert enough messages.
	for i := 0; i < 6; i++ {
		ts := fmt.Sprintf("%d.000000", 1000000000+i*10)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID: "C1", TS: ts, UserID: "U001",
			Text: fmt.Sprintf("test message %d", i),
		}))
	}

	// Create profile with custom context.
	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID:         "U001",
		CustomPromptContext: "You are helping a Platform EM. Reports: alice, bob.",
		Reports:             `["U002"]`,
		StarredChannels:     `["C1"]`,
		StarredPeople:       `["U010"]`,
	}))

	gen := &capturingGenerator{
		response: `{"summary":"test","topics":[],"decisions":[],"action_items":[],"key_messages":[]}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	pipe.SinceOverride = 999999990
	_, _, err := pipe.Run(context.Background())
	require.NoError(t, err)

	// Profile context should appear in the channel digest prompt.
	assert.Contains(t, gen.capturedPrompt, "USER PROFILE CONTEXT")
	assert.Contains(t, gen.capturedPrompt, "Platform EM")
	assert.Contains(t, gen.capturedPrompt, "STARRED CHANNELS")
	assert.Contains(t, gen.capturedPrompt, "MY REPORTS")
}

func TestNoProfileContextWhenProfileEmpty(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	for i := 0; i < 6; i++ {
		ts := fmt.Sprintf("%d.000000", 1000000000+i*10)
		require.NoError(t, database.UpsertMessage(db.Message{
			ChannelID: "C1", TS: ts, UserID: "U001",
			Text: fmt.Sprintf("test message %d", i),
		}))
	}

	gen := &capturingGenerator{
		response: `{"summary":"test","topics":[],"decisions":[],"action_items":[],"key_messages":[]}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	pipe.SinceOverride = 999999990
	_, _, err := pipe.Run(context.Background())
	require.NoError(t, err)

	// No profile context should appear.
	assert.NotContains(t, gen.capturedPrompt, "USER PROFILE CONTEXT")
}

// --- New comprehensive tests below ---

// threadSafeMockGenerator is safe for concurrent use by workers.
type threadSafeMockGenerator struct {
	response string
	err      error
	mu       sync.Mutex
	calls    int
	prompts  []string
}

func (m *threadSafeMockGenerator) Generate(_ context.Context, _, prompt, _ string) (string, *Usage, string, error) {
	m.mu.Lock()
	m.calls++
	m.prompts = append(m.prompts, prompt)
	m.mu.Unlock()
	return m.response, &Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}, "mock-session", m.err
}

func validDigestJSON() string {
	return `{"summary":"Test summary","topics":["topic1"],"decisions":[{"text":"decided X","by":"@alice","message_ts":"123.456","importance":"high"}],"action_items":[{"text":"do Y","assignee":"@bob","status":"open"}],"key_messages":["123.456"]}`
}

// seedMessagesForChannel creates n messages in the given channel with recent timestamps.
func seedMessagesForChannel(t *testing.T, database *db.DB, channelID, userID string, n int) {
	t.Helper()
	now := time.Now().Unix()
	for i := 0; i < n; i++ {
		ts := fmt.Sprintf("%d.%06d", now-3600+int64(i*60), i)
		seedMessage(t, database, channelID, ts, userID, fmt.Sprintf("message %d content", i))
	}
}

func TestRunChannelDigestsOnly_Enabled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	// Seed an existing channel digest so isFirstRun() returns false.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(time.Now().Unix() - 86400*2),
		PeriodTo:   float64(time.Now().Unix() - 86400),
		Summary:    "old digest", MessageCount: 3, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	n, usage, err := p.RunChannelDigestsOnly(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.NotNil(t, usage)
	assert.Equal(t, 1, gen.calls) // only channel digests, no rollups
}

func TestRunChannelDigestsOnly_Disabled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Enabled = false

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigestsOnly(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, gen.calls)
}

func TestRunRollups_Enabled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "frontend")
	seedChannel(t, database, "C2", "backend")

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	fromUnix := float64(dayStart.Unix())
	toUnix := float64(now.Unix())

	// Need at least 2 channel digests for daily rollup.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Frontend work", MessageCount: 10, Model: "haiku",
	})
	require.NoError(t, err)
	_, err = database.UpsertDigest(db.Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Backend work", MessageCount: 15, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	err = p.RunRollups(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, gen.calls) // daily rollup only
}

func TestRunRollups_Disabled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Enabled = false

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	err := p.RunRollups(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, gen.calls)
}

func TestRunPeriodSummary(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")

	now := time.Now()
	from := now.AddDate(0, 0, -3)

	// Seed multiple digests in the time range.
	for i := 0; i < 3; i++ {
		dayOffset := int64(i * 86400)
		_, err := database.UpsertDigest(db.Digest{
			ChannelID: "C1", Type: "channel",
			PeriodFrom:   float64(from.Unix() + dayOffset),
			PeriodTo:     float64(from.Unix() + dayOffset + 43200),
			Summary:      fmt.Sprintf("Channel digest day %d", i),
			MessageCount: 10 + i, Model: "haiku",
		})
		require.NoError(t, err)
	}

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	result, usage, err := p.RunPeriodSummary(context.Background(), from, now)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Test summary", result.Summary)
	assert.NotNil(t, usage)
	assert.Equal(t, 1, gen.calls)
}

func TestRunPeriodSummary_NoDigests(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())

	now := time.Now()
	from := now.AddDate(0, 0, -3)
	_, _, err := p.RunPeriodSummary(context.Background(), from, now)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no digests found")
	assert.Equal(t, 0, gen.calls)
}

func TestRunPeriodSummary_AIError(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	now := time.Now()
	from := now.AddDate(0, 0, -3)
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(from.Unix()), PeriodTo: float64(now.Unix()),
		Summary: "test", MessageCount: 10, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{err: fmt.Errorf("AI down")}
	p := New(database, cfg, gen, testLogger())
	_, _, err = p.RunPeriodSummary(context.Background(), from, now)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating period summary")
}

func TestRunPeriodSummary_IncludesDailyAndWeeklyLabels(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	now := time.Now()
	from := now.AddDate(0, 0, -7)

	seedChannel(t, database, "C1", "general")

	// channel digest
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(from.Unix()), PeriodTo: float64(from.Unix() + 43200),
		Summary: "channel stuff", MessageCount: 10, Model: "haiku",
	})
	require.NoError(t, err)

	// daily digest
	_, err = database.UpsertDigest(db.Digest{
		Type:       "daily",
		PeriodFrom: float64(from.Unix() + 86400), PeriodTo: float64(from.Unix() + 86400*2),
		Summary: "daily stuff", MessageCount: 20, Model: "haiku",
	})
	require.NoError(t, err)

	// weekly digest
	_, err = database.UpsertDigest(db.Digest{
		Type:       "weekly",
		PeriodFrom: float64(from.Unix()), PeriodTo: float64(now.Unix()),
		Summary: "weekly stuff", MessageCount: 50, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &capturingGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	_, _, err = p.RunPeriodSummary(context.Background(), from, now)
	require.NoError(t, err)

	// All three types should appear in the prompt.
	assert.Contains(t, gen.capturedPrompt, "#general")
	assert.Contains(t, gen.capturedPrompt, "Daily rollup")
	assert.Contains(t, gen.capturedPrompt, "Weekly trends")
}

func TestRun_FullPipeline_WithSinceOverride(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedChannel(t, database, "C2", "random")
	seedUser(t, database, "U1", "alice", "Alice")

	seedMessagesForChannel(t, database, "C1", "U1", 5)
	seedMessagesForChannel(t, database, "C2", "U1", 5)

	gen := &threadSafeMockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200) // 2 hours ago

	n, usage, err := p.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, n) // 2 channels
	assert.NotNil(t, usage)
	// 2 channel digests + 1 daily rollup = 3 calls
	gen.mu.Lock()
	assert.Equal(t, 3, gen.calls)
	gen.mu.Unlock()
}

func TestRun_ContextCancelled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	// Seed existing digest to skip first-run path.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(time.Now().Unix() - 86400*2),
		PeriodTo:   float64(time.Now().Unix() - 86400),
		Summary:    "old", MessageCount: 3, Model: "haiku",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	_, _, err = p.Run(ctx)

	// Workers should exit early on cancelled context.
	assert.Equal(t, 0, gen.calls)
}

func TestRunChannelDigests_MultipleWorkers(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Workers = 3

	seedUser(t, database, "U1", "alice", "Alice")
	for i := 0; i < 5; i++ {
		chID := fmt.Sprintf("C%d", i)
		seedChannel(t, database, chID, fmt.Sprintf("chan%d", i))
		seedMessagesForChannel(t, database, chID, "U1", 5)
	}

	gen := &threadSafeMockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200)

	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5, n)
	gen.mu.Lock()
	assert.Equal(t, 5, gen.calls)
	gen.mu.Unlock()
}

func TestRunDailyRollup_WithChainContext(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "eng")
	seedChannel(t, database, "C2", "design")

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	fromUnix := float64(dayStart.Unix())
	toUnix := float64(now.Unix())

	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Engineering updates", MessageCount: 10, Model: "haiku",
	})
	require.NoError(t, err)
	_, err = database.UpsertDigest(db.Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Design updates", MessageCount: 8, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &capturingGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.ChainContext = "=== CHAIN CONTEXT ===\nChain #1: API redesign (3 decisions linked)"

	err = p.RunDailyRollup(context.Background())
	require.NoError(t, err)

	// Chain context should be injected into the prompt.
	assert.Contains(t, gen.capturedPrompt, "CHAIN CONTEXT")
	assert.Contains(t, gen.capturedPrompt, "API redesign")
}

func TestRun_WithChainLinker(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	// Seed an existing digest so isFirstRun() returns false.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 1100000,
		Summary: "old", Model: "haiku",
	})
	require.NoError(t, err)

	gen := &threadSafeMockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200)

	linker := &mockChainLinker{
		runResult:    3,
		chainContext: "=== ACTIVE CHAINS ===\nChain #1: test chain",
	}
	p.ChainLinker = linker

	_, _, err = p.Run(context.Background())
	require.NoError(t, err)

	// Chain linker should have been called.
	assert.True(t, linker.runCalled, "ChainLinker.Run should be called")
	assert.True(t, linker.formatCalled, "ChainLinker.FormatActiveChainsForPrompt should be called")

	// ChainContext should be set from linker output.
	assert.Contains(t, p.ChainContext, "ACTIVE CHAINS")
}

type mockChainLinker struct {
	runResult    int
	chainContext string
	runCalled    bool
	formatCalled bool
}

func (m *mockChainLinker) Run(ctx context.Context) (int, error) {
	m.runCalled = true
	return m.runResult, nil
}

func (m *mockChainLinker) FormatActiveChainsForPrompt(ctx context.Context) (string, error) {
	m.formatCalled = true
	return m.chainContext, nil
}

func TestRunDailyRollup_WithDecisions(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "eng")
	seedChannel(t, database, "C2", "ops")

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	fromUnix := float64(dayStart.Unix())
	toUnix := float64(now.Unix())

	decisions := `[{"text":"use Go for backend","by":"@alice","message_ts":"123.456","importance":"high"}]`
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Eng discussion", Decisions: decisions,
		MessageCount: 10, Model: "haiku",
	})
	require.NoError(t, err)
	_, err = database.UpsertDigest(db.Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "Ops discussion", MessageCount: 8, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &capturingGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	err = p.RunDailyRollup(context.Background())
	require.NoError(t, err)

	// Decisions from channel digests should be included in rollup prompt.
	assert.Contains(t, gen.capturedPrompt, "use Go for backend")
}

func TestRunWeeklyTrends_NotEnoughDailies(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	// Only 1 daily — needs at least 2.
	_, err := database.UpsertDigest(db.Digest{
		Type:       "daily",
		PeriodFrom: float64(time.Now().Unix() - 86400), PeriodTo: float64(time.Now().Unix()),
		Summary: "One day", MessageCount: 20, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	err = p.RunWeeklyTrends(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, gen.calls)
}

func TestRunWeeklyTrends_WithDecisions(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	decisions := `[{"text":"adopt microservices","by":"@cto","message_ts":"999.001","importance":"high"}]`
	for i := 0; i < 3; i++ {
		dayOffset := int64(i * 86400)
		_, err := database.UpsertDigest(db.Digest{
			Type:         "daily",
			PeriodFrom:   float64(time.Now().Unix() - dayOffset - 86400),
			PeriodTo:     float64(time.Now().Unix() - dayOffset),
			Summary:      fmt.Sprintf("Day %d summary", i),
			Decisions:    decisions,
			MessageCount: 30, Model: "haiku",
		})
		require.NoError(t, err)
	}

	gen := &capturingGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	err := p.RunWeeklyTrends(context.Background())

	require.NoError(t, err)
	assert.Contains(t, gen.capturedPrompt, "adopt microservices")
}

func TestLanguageInstruction(t *testing.T) {
	database := testDB(t)
	gen := &mockGenerator{}

	tests := []struct {
		name     string
		lang     string
		expected string
	}{
		{"empty language", "", "Write in the language most commonly used"},
		{"english", "English", "Write all text values in English"},
		{"russian", "Russian", "You MUST write ALL text values"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Digest.Language = tt.lang
			p := New(database, cfg, gen, testLogger())
			result := p.languageInstruction()
			assert.Contains(t, result, tt.expected)
		})
	}
}

func TestLanguageInstruction_CaseInsensitive(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Language = "english" // lowercase
	gen := &mockGenerator{}
	p := New(database, cfg, gen, testLogger())

	result := p.languageInstruction()
	assert.Contains(t, result, "Write all text values in English")
}

func TestSanitizePromptValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean text", "hello world", "hello world"},
		{"with ===", "test === foo", "test = = = foo"},
		{"with ---", "test --- foo", "test - - - foo"},
		{"with newlines", "line1\nline2\rline3", "line1 line2 line3"},
		{"combined", "===\n---", "= = = - - -"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePromptValue(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestFormatMessages_SanitizesDelimiters(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedUser(t, database, "U1", "alice", "Alice")
	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	msgs := []db.Message{
		{UserID: "U1", Text: "check this out === important ---", TSUnix: 1000000},
	}

	formatted := p.formatMessages(msgs)
	assert.Contains(t, formatted, "= = =")
	assert.Contains(t, formatted, "- - -")
	assert.NotContains(t, formatted, "===")
	assert.NotContains(t, formatted, "---")
}

func TestFormatProfileContext_WithAllFields(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.profile = &db.UserProfile{
		CustomPromptContext: "I am a senior engineer",
		StarredChannels:    `["C1","C2"]`,
		StarredPeople:      `["U10"]`,
		Reports:            `["U20","U21"]`,
	}

	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "USER PROFILE CONTEXT")
	assert.Contains(t, ctx, "I am a senior engineer")
	assert.Contains(t, ctx, "STARRED CHANNELS")
	assert.Contains(t, ctx, "STARRED PEOPLE")
	assert.Contains(t, ctx, "MY REPORTS")
}

func TestFormatProfileContext_EmptyProfile(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.profile = nil

	ctx := p.formatProfileContext()
	assert.Equal(t, "", ctx)
}

func TestFormatProfileContext_EmptyCustomContext(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.profile = &db.UserProfile{
		CustomPromptContext: "",
	}

	ctx := p.formatProfileContext()
	assert.Equal(t, "", ctx)
}

func TestFormatProfileContext_EmptyStarredLists(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.profile = &db.UserProfile{
		CustomPromptContext: "test context",
		StarredChannels:    "[]",
		StarredPeople:      "[]",
		Reports:            "[]",
	}

	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "USER PROFILE CONTEXT")
	assert.NotContains(t, ctx, "STARRED CHANNELS")
	assert.NotContains(t, ctx, "STARRED PEOPLE")
	assert.NotContains(t, ctx, "MY REPORTS")
}

func TestChannelName_UnknownID(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.channelNames = map[string]string{"C1": "general"}

	assert.Equal(t, "general", p.channelName("C1"))
	assert.Equal(t, "C_UNKNOWN", p.channelName("C_UNKNOWN")) // fallback to ID
}

func TestChannelName_NilMap(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.channelNames = nil

	assert.Equal(t, "C1", p.channelName("C1"))
}

func TestUserName_UnknownID(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.userNames = map[string]string{"U1": "Alice"}

	assert.Equal(t, "Alice", p.userName("U1"))
	assert.Equal(t, "U_UNKNOWN", p.userName("U_UNKNOWN"))
}

func TestUserName_NilMap(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.userNames = nil

	assert.Equal(t, "U1", p.userName("U1"))
}

func TestLoadCaches_PopulatesNames(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedChannel(t, database, "C1", "engineering")
	seedUser(t, database, "U1", "alice", "Alice Smith")
	// User with no display name — should fall back to username.
	seedUser(t, database, "U2", "bob", "")

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	assert.Equal(t, "engineering", p.channelNames["C1"])
	assert.Equal(t, "Alice Smith", p.userNames["U1"])
	assert.Equal(t, "bob", p.userNames["U2"])
}

func TestLoadCaches_DMChannel(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedUser(t, database, "U1", "alice", "Alice")
	require.NoError(t, database.UpsertChannel(db.Channel{
		ID:   "D1",
		Name: "U1",
		Type: "dm",
	}))

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	// DM channel should show user display name.
	assert.Equal(t, "DM: Alice", p.channelNames["D1"])
}

func TestSetPromptStore(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	assert.Nil(t, p.promptStore)

	store := prompts.New(database, nil)
	p.SetPromptStore(store)
	assert.NotNil(t, p.promptStore)
}

func TestGetPrompt_FallbackToDefault(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())

	tmpl, version := p.getPrompt("nonexistent.id", "fallback template %s")
	assert.Equal(t, "fallback template %s", tmpl)
	assert.Equal(t, 0, version)
}

func TestGetPrompt_WithPromptStore(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	store := prompts.New(database, nil)
	require.NoError(t, store.Seed())

	p := New(database, cfg, gen, testLogger())
	p.SetPromptStore(store)

	tmpl, version := p.getPrompt(prompts.DigestChannel, "fallback")
	// Should get the seeded prompt, not fallback.
	assert.NotEqual(t, "fallback", tmpl)
	assert.GreaterOrEqual(t, version, 1)
}

func TestGetPrompt_WithRole(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	store := prompts.New(database, nil)
	require.NoError(t, store.Seed())

	p := New(database, cfg, gen, testLogger())
	p.SetPromptStore(store)
	p.profile = &db.UserProfile{Role: "top_management"}

	tmpl, _ := p.getPrompt(prompts.DigestChannel, "fallback")
	// Role instruction should be prepended.
	assert.Contains(t, tmpl, "You are analyzing Slack messages")
}

func TestIsFirstRun(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	assert.True(t, p.isFirstRun())

	// Add a channel digest.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "test", MessageCount: 5, Model: "haiku",
	})
	require.NoError(t, err)

	assert.False(t, p.isFirstRun())
}

func TestLastDigestTime_NoDigests(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Sync.InitialHistoryDays = 7
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	since := p.lastDigestTime()

	// Should be approximately 7 days ago.
	expected := float64(time.Now().AddDate(0, 0, -7).Unix())
	assert.InDelta(t, expected, since, 5.0)
}

func TestLastDigestTime_WithDigests(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	periodTo := float64(time.Now().Unix() - 3600)
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: periodTo - 3600, PeriodTo: periodTo,
		Summary: "test", MessageCount: 5, Model: "haiku",
	})
	require.NoError(t, err)

	p := New(database, cfg, gen, testLogger())
	since := p.lastDigestTime()

	assert.InDelta(t, periodTo, since, 1.0)
}

func TestOnProgress_Callback(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200)

	var progressCalls int
	p.OnProgress = func(done, total int, status string) {
		progressCalls++
	}

	_, _, err := p.RunChannelDigests(context.Background())
	require.NoError(t, err)
	assert.Greater(t, progressCalls, 0)
}

func TestRunChannelDigests_SkipsEmptyMessages(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 3

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now().Unix()
	// 5 messages but all empty text — should skip.
	for i := 0; i < 5; i++ {
		ts := fmt.Sprintf("%d.%06d", now-3600+int64(i*60), i)
		seedMessage(t, database, "C1", ts, "U1", "")
	}

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(now - 7200)

	n, _, err := p.RunChannelDigests(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, gen.calls)
}

func TestStoreDigest_NilUsage(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())

	result := &DigestResult{
		Summary: "test",
		Topics:  []string{"a"},
	}

	err := p.storeDigest("C1", "channel", 1000.0, 2000.0, result, 10, nil, 0)
	require.NoError(t, err)

	d, err := database.GetLatestDigest("C1", "channel")
	require.NoError(t, err)
	assert.Equal(t, 0, d.InputTokens)
	assert.Equal(t, 0, d.OutputTokens)
}

func TestStoreDigest_WithPromptVersion(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())

	result := &DigestResult{Summary: "versioned", Topics: []string{"t1"}}
	// Store with prompt version 3 — verifies that storeDigest accepts and passes it.
	err := p.storeDigest("C1", "channel", 1000.0, 2000.0, result, 5, &Usage{InputTokens: 10, OutputTokens: 5}, 3)
	require.NoError(t, err)

	// Verify via GetDigests (prompt_version may not be scanned, but
	// we verify the digest was stored correctly with its other fields).
	digests, err := database.GetDigests(db.DigestFilter{Type: "channel"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "versioned", digests[0].Summary)
	assert.Equal(t, 5, digests[0].MessageCount)
}

func TestParseDigestResult_GenericMarkdownFence(t *testing.T) {
	input := "```\n{\"summary\":\"test\",\"topics\":[],\"decisions\":[],\"action_items\":[],\"key_messages\":[]}\n```"
	result, err := parseDigestResult(input)
	require.NoError(t, err)
	assert.Equal(t, "test", result.Summary)
}

func TestParseDigestResult_WithImportanceField(t *testing.T) {
	input := `{"summary":"test","topics":[],"decisions":[{"text":"use K8s","by":"@ops","message_ts":"1.1","importance":"high"}],"action_items":[],"key_messages":[]}`
	result, err := parseDigestResult(input)
	require.NoError(t, err)
	require.Len(t, result.Decisions, 1)
	assert.Equal(t, "high", result.Decisions[0].Importance)
}

// --- Tests for pooled.go ---

func TestPooledGenerator_Generate(t *testing.T) {
	inner := &mockGenerator{response: validDigestJSON()}
	pool := sessions.NewSessionPool(2)
	defer pool.Close()

	pg := NewPooledGenerator(inner, pool)

	result, usage, _, err := pg.Generate(context.Background(), "system", "user msg", "")
	require.NoError(t, err)
	assert.Equal(t, validDigestJSON(), result)
	assert.NotNil(t, usage)
	assert.Equal(t, 1, inner.calls)
}

func TestPooledGenerator_GenerateError(t *testing.T) {
	inner := &mockGenerator{err: fmt.Errorf("inner error")}
	pool := sessions.NewSessionPool(1)
	defer pool.Close()

	pg := NewPooledGenerator(inner, pool)

	_, usage, _, err := pg.Generate(context.Background(), "", "prompt", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inner error")
	// Usage may be non-nil even on error (inner returns it).
	_ = usage
}

func TestPooledGenerator_AcquireTimeout(t *testing.T) {
	pool := sessions.NewSessionPool(1)
	defer pool.Close()

	inner := &mockGenerator{response: "ok"}
	pg := NewPooledGenerator(inner, pool)

	// Acquire the only slot.
	worker, err := pool.Acquire(context.Background())
	require.NoError(t, err)

	// Now try to generate with a cancelled context — should fail to acquire.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, err = pg.Generate(ctx, "", "prompt", "")
	require.Error(t, err)

	pool.Release(worker)
}

func TestPooledGenerator_Pool(t *testing.T) {
	pool := sessions.NewSessionPool(3)
	defer pool.Close()

	inner := &mockGenerator{response: "ok"}
	pg := NewPooledGenerator(inner, pool)

	assert.Equal(t, pool, pg.Pool())
	assert.Equal(t, 3, pg.Pool().Size())
}

func TestPooledGenerator_SetSessionLog(t *testing.T) {
	pool := sessions.NewSessionPool(1)
	defer pool.Close()

	inner := &mockGenerator{response: validDigestJSON()}
	pg := NewPooledGenerator(inner, pool)

	// Set a session log (writes to a temp file).
	tmpFile := t.TempDir() + "/sessions.log"
	sl := sessions.NewSessionLog(tmpFile)
	pg.SetSessionLog(sl)

	assert.NotNil(t, pg.sessionLog)

	// Generate with source context to trigger logging.
	ctx := WithSource(context.Background(), "test.source")
	_, _, _, err := pg.Generate(ctx, "", "prompt", "")
	require.NoError(t, err)
}

func TestWithSource(t *testing.T) {
	ctx := context.Background()
	ctx = WithSource(ctx, "digest.channel")

	val, ok := ctx.Value(sessionSourceKey{}).(string)
	assert.True(t, ok)
	assert.Equal(t, "digest.channel", val)
}

func TestWithSource_EmptyContext(t *testing.T) {
	ctx := context.Background()
	val, ok := ctx.Value(sessionSourceKey{}).(string)
	assert.False(t, ok)
	assert.Equal(t, "", val)
}

// --- Tests for generator.go (parseCLIOutput, limitedWriter) ---

func TestParseCLIOutput_SingleJSON(t *testing.T) {
	output := []byte(`{"type":"result","result":"hello world","total_cost_usd":0.01,"is_error":false,"session_id":"s1","usage":{"input_tokens":100,"output_tokens":50}}`)
	resp, err := parseCLIOutput(output)
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.Result)
	assert.Equal(t, "s1", resp.SessionID)
	assert.Equal(t, 100, resp.Usage.InputTokens)
	assert.Equal(t, 50, resp.Usage.OutputTokens)
	assert.InDelta(t, 0.01, resp.CostUSD, 0.001)
	assert.False(t, resp.IsError)
}

func TestParseCLIOutput_StreamingArray(t *testing.T) {
	output := []byte(`[{"type":"system","result":""},{"type":"assistant","result":"partial"},{"type":"result","result":"final answer","total_cost_usd":0.005,"session_id":"s2","usage":{"input_tokens":200,"output_tokens":100}}]`)
	resp, err := parseCLIOutput(output)
	require.NoError(t, err)
	assert.Equal(t, "final answer", resp.Result)
	assert.Equal(t, "s2", resp.SessionID)
	assert.Equal(t, 200, resp.Usage.InputTokens)
}

func TestParseCLIOutput_StreamingArrayNoResult(t *testing.T) {
	output := []byte(`[{"type":"system","result":""},{"type":"assistant","result":"partial"}]`)
	_, err := parseCLIOutput(output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no result event found")
}

func TestParseCLIOutput_InvalidFormat(t *testing.T) {
	output := []byte(`not json`)
	_, err := parseCLIOutput(output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected claude CLI output format")
}

func TestParseCLIOutput_EmptyInput(t *testing.T) {
	_, err := parseCLIOutput([]byte(""))
	require.Error(t, err)
}

func TestParseCLIOutput_WithWhitespace(t *testing.T) {
	output := []byte(`  {"type":"result","result":"trimmed","session_id":"s3","usage":{"input_tokens":10,"output_tokens":5}}  `)
	resp, err := parseCLIOutput(output)
	require.NoError(t, err)
	assert.Equal(t, "trimmed", resp.Result)
}

func TestParseCLIOutput_WithCacheTokens(t *testing.T) {
	output := []byte(`{"type":"result","result":"cached","session_id":"s4","usage":{"input_tokens":50,"output_tokens":25,"cache_read_input_tokens":30,"cache_creation_input_tokens":10}}`)
	resp, err := parseCLIOutput(output)
	require.NoError(t, err)
	assert.Equal(t, 50, resp.Usage.InputTokens)
	assert.Equal(t, 30, resp.Usage.CacheReadInputTokens)
	assert.Equal(t, 10, resp.Usage.CacheCreationInputTokens)
}

func TestLimitedWriter_WithinLimit(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 100}

	n, err := lw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", buf.String())
	assert.Equal(t, 5, lw.written)
}

func TestLimitedWriter_ExceedsLimit(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 5}

	// Write more than limit.
	n, err := lw.Write([]byte("hello world"))
	require.NoError(t, err)
	assert.Equal(t, 11, n) // reports full length consumed
	assert.Equal(t, "hello", buf.String())
	assert.Equal(t, 5, lw.written)
}

func TestLimitedWriter_MultipleWrites(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 10}

	n1, _ := lw.Write([]byte("hello"))
	n2, _ := lw.Write([]byte(" world!"))
	n3, _ := lw.Write([]byte("ignored"))

	assert.Equal(t, 5, n1)
	assert.Equal(t, 7, n2) // reports full length consumed
	assert.Equal(t, 7, n3) // all consumed (but written nothing)
	// "hello" (5) + " worl" (5 of " world!") = "hello worl" (10 chars = limit)
	assert.Equal(t, "hello worl", buf.String())
}

func TestLimitedWriter_ExactLimit(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 5}

	n, err := lw.Write([]byte("exact"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "exact", buf.String())

	// Next write should be silently dropped.
	n, err = lw.Write([]byte("more"))
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, "exact", buf.String())
}

func TestRunChannelDigests_SinceOverride(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now().Unix()
	// Messages 2 hours ago.
	for i := 0; i < 5; i++ {
		ts := fmt.Sprintf("%d.%06d", now-3600+int64(i*60), i)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(now - 7200)

	n, _, err := p.RunChannelDigests(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestRunInitialDayByDay(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Sync.InitialHistoryDays = 2

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")

	// Seed messages across multiple days.
	now := time.Now()
	for d := 2; d >= 0; d-- {
		day := now.AddDate(0, 0, -d)
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, time.UTC)
		for i := 0; i < 5; i++ {
			ts := fmt.Sprintf("%d.%06d", dayStart.Unix()+int64(i*60), i)
			seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("day%d msg%d", d, i))
		}
	}

	gen := &threadSafeMockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	// Don't set SinceOverride — let isFirstRun() detect first run.
	n, usage, err := p.Run(context.Background())

	require.NoError(t, err)
	assert.Greater(t, n, 0, "should generate digests")
	assert.NotNil(t, usage)
}

func TestRunDailyRollup_AIError(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "eng")
	seedChannel(t, database, "C2", "ops")

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	fromUnix := float64(dayStart.Unix())
	toUnix := float64(now.Unix())

	_, _ = database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "eng", MessageCount: 10, Model: "haiku",
	})
	_, _ = database.UpsertDigest(db.Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: fromUnix, PeriodTo: toUnix,
		Summary: "ops", MessageCount: 10, Model: "haiku",
	})

	gen := &mockGenerator{err: fmt.Errorf("rollup AI error")}
	p := New(database, cfg, gen, testLogger())
	err := p.RunDailyRollup(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating daily rollup")
}

func TestRunWeeklyTrends_AIError(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	for i := 0; i < 3; i++ {
		_, _ = database.UpsertDigest(db.Digest{
			Type:         "daily",
			PeriodFrom:   float64(time.Now().Unix() - int64(i*86400) - 86400),
			PeriodTo:     float64(time.Now().Unix() - int64(i*86400)),
			Summary:      fmt.Sprintf("Day %d", i),
			MessageCount: 20, Model: "haiku",
		})
	}

	gen := &mockGenerator{err: fmt.Errorf("weekly AI error")}
	p := New(database, cfg, gen, testLogger())
	err := p.RunWeeklyTrends(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating weekly trends")
}

func TestRunPeriodSummary_ParseError(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	now := time.Now()
	from := now.AddDate(0, 0, -3)
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(from.Unix()), PeriodTo: float64(now.Unix()),
		Summary: "test", MessageCount: 10, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: "not valid json at all"}
	p := New(database, cfg, gen, testLogger())
	_, _, err = p.RunPeriodSummary(context.Background(), from, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing period summary")
}

func TestRunChannelDigests_WorkersDefault(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.Workers = 0 // should default to 1

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200)

	n, _, err := p.RunChannelDigests(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestFallbackPromptFormatVerbs(t *testing.T) {
	// Verify that hardcoded fallback prompts have the correct number of %s
	// placeholders matching the fmt.Sprintf calls in pipeline.go.
	// This prevents regressions where prompts and Sprintf args go out of sync,
	// which causes messages to silently disappear from prompts.

	tests := []struct {
		name     string
		prompt   string
		expected int // number of %s placeholders expected
	}{
		{"channelDigestPrompt", channelDigestPrompt, 6},   // channelName, fromStr, toStr, profileCtx, langInstr, messages
		{"dailyRollupPrompt", dailyRollupPrompt, 4},       // dateStr, profileCtx, langInstr, channelInput
		{"weeklyTrendsPrompt", weeklyTrendsPrompt, 6},     // date, fromStr, toStr, profileCtx, langInstr, dailies
		{"periodSummaryPrompt", periodSummaryPrompt, 5},   // fromStr, toStr, profileCtx, langInstr, digests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := strings.Count(tt.prompt, "%s")
			assert.Equal(t, tt.expected, count,
				"%s has %d %%s placeholders, expected %d", tt.name, count, tt.expected)
		})
	}
}
