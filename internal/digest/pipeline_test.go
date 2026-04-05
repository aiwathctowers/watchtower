package digest

import (
	"context"
	"database/sql"
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
	mu       sync.Mutex
	calls    int
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *Usage, string, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return m.response, &Usage{Model: "test-model", InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", m.err
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
			name:  "clean JSON with topics",
			input: `{"summary":"test","topics":[{"title":"Topic A","summary":"about A","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}`,
			want: DigestResult{
				Summary: "test",
				Topics:  []Topic{{Title: "Topic A", Summary: "about A", Decisions: []Decision{}, ActionItems: []ActionItem{}, KeyMessages: []string{}}},
			},
		},
		{
			name:  "wrapped in markdown fences",
			input: "```json\n{\"summary\":\"test\",\"topics\":[]}\n```",
			want:  DigestResult{Summary: "test", Topics: []Topic{}},
		},
		{
			name:  "with preamble text",
			input: "Here is the analysis:\n{\"summary\":\"test\",\"topics\":[]}",
			want:  DigestResult{Summary: "test", Topics: []Topic{}},
		},
		{
			name:  "with decisions and action items in topic",
			input: `{"summary":"deploy discussed","topics":[{"title":"Deployment","summary":"deploy plan","decisions":[{"text":"deploy Friday","by":"@alice","message_ts":"1000.001"}],"action_items":[{"text":"write tests","assignee":"@bob","status":"open"}],"situations":[],"key_messages":["1000.001"]}]}`,
			want: DigestResult{
				Summary: "deploy discussed",
				Topics: []Topic{{
					Title:       "Deployment",
					Summary:     "deploy plan",
					Decisions:   []Decision{{Text: "deploy Friday", By: "@alice", MessageTS: "1000.001"}},
					ActionItems: []ActionItem{{Text: "write tests", Assignee: "@bob", Status: "open"}},
					KeyMessages: []string{"1000.001"},
				}},
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
			assert.Equal(t, len(tt.want.Topics), len(got.Topics))
			for i := range tt.want.Topics {
				assert.Equal(t, tt.want.Topics[i].Title, got.Topics[i].Title)
				assert.Equal(t, tt.want.Topics[i].Summary, got.Topics[i].Summary)
			}
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

	mockResp := `{"summary":"Team discussed deployment","topics":[{"title":"Deployment","summary":"deployment discussion","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}`
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
	assert.Equal(t, "test-model", digests[0].Model)
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

	// With unified batching, a single channel (even below min_messages) goes
	// through a single-entry batch → uses individual channel prompt.
	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, gen.calls, "single-entry batch uses individual channel prompt")
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

func TestRunChannelDigests_SkipsDMs(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.BatchMaxChannels = 1 // force individual processing

	// 1:1 DM should be skipped, group DM should be kept.
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "CDM", Name: "dm-user", Type: "dm"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "CGDM", Name: "group-chat", Type: "group_dm"}))
	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)
	// Use 35 messages per channel (medium tier ≥30) so BatchMaxChannels=1 forces individual processing.
	for i := range 35 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "CDM", ts, "U1", fmt.Sprintf("dm msg %d", i))
		seedMessage(t, database, "CGDM", ts, "U1", fmt.Sprintf("group dm msg %d", i))
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("public msg %d", i))
	}

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, n, "should skip 1:1 DM but keep group DM and public channel")
}

func TestRunChannelDigests_SkipsBotOnly(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C_ALERTS", "alerts")
	seedChannel(t, database, "C_GENERAL", "general")
	require.NoError(t, database.UpsertUser(db.User{ID: "UBOT", Name: "alertbot", IsBot: true}))
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)
	for i := range 10 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		// Alerts channel: all messages from bot.
		seedMessage(t, database, "C_ALERTS", ts, "UBOT", fmt.Sprintf("alert %d", i))
		// General channel: messages from human.
		seedMessage(t, database, "C_GENERAL", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n, "should skip bot-only channel, only digest general")
}

func TestRunChannelDigests_BotHeavyWithHumanReply(t *testing.T) {
	// Bot-heavy channel (≥90% bot msgs) with a human reply should be processed
	// but only with human messages + surrounding context, not all bot alerts.
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C_ALERTS", "alerts")
	require.NoError(t, database.UpsertUser(db.User{ID: "UBOT", Name: "alertbot", IsBot: true}))
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)
	// 19 bot messages + 1 human reply (95% bot = bot-heavy).
	for i := range 19 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_ALERTS", ts, "UBOT", fmt.Sprintf("alert %d", i))
	}
	ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(19*60), 0)
	seedMessage(t, database, "C_ALERTS", ts, "U1", "looking into this")

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.RunChannelDigests(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, n, "should process bot-heavy channel because human replied")
	assert.Equal(t, 1, gen.calls)
}

func TestRunChannelDigests_BotHeavyExtractsContext(t *testing.T) {
	// Verify extractHumanContext keeps human msg + thread + surrounding bot msgs.
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C_ALERTS", "alerts")
	require.NoError(t, database.UpsertUser(db.User{ID: "UBOT", Name: "alertbot", IsBot: true}))
	seedUser(t, database, "U1", "alice", "Alice")

	p := New(database, cfg, nil, testLogger())
	p.loadCaches()

	// Simulate 20 messages: 19 bot + 1 human at position 10.
	var msgs []db.Message
	for i := range 20 {
		userID := "UBOT"
		text := fmt.Sprintf("alert %d", i)
		if i == 10 {
			userID = "U1"
			text = "investigating this"
		}
		msgs = append(msgs, db.Message{
			ChannelID: "C_ALERTS",
			TS:        fmt.Sprintf("%d.000000", 1000+i),
			UserID:    userID,
			Text:      text,
			TSUnix:    float64(1000 + i),
		})
	}

	result := p.extractHumanContext(msgs)
	// Should keep: human msg (pos 10) + 3 before (7,8,9) + 3 after (11,12,13) = 7 messages.
	assert.Equal(t, 7, len(result), "should extract human msg + 3 before + 3 after")

	// Verify human message is included.
	found := false
	for _, m := range result {
		if m.UserID == "U1" {
			found = true
			break
		}
	}
	assert.True(t, found, "human message must be in result")
}

func TestRunChannelDigests_BotHeavyThreadContext(t *testing.T) {
	// When human replies in a thread, keep the entire thread.
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C_ALERTS", "alerts")
	require.NoError(t, database.UpsertUser(db.User{ID: "UBOT", Name: "alertbot", IsBot: true}))
	seedUser(t, database, "U1", "alice", "Alice")

	p := New(database, cfg, nil, testLogger())
	p.loadCaches()

	threadParentTS := "1005.000000"
	var msgs []db.Message
	// 15 unrelated bot alerts.
	for i := range 15 {
		msgs = append(msgs, db.Message{
			ChannelID: "C_ALERTS",
			TS:        fmt.Sprintf("%d.000000", 1000+i),
			UserID:    "UBOT",
			Text:      fmt.Sprintf("alert %d", i),
			TSUnix:    float64(1000 + i),
		})
	}
	// Thread parent (bot alert at position 5).
	msgs[5].ReplyCount = 2
	// Thread reply from bot.
	msgs = append(msgs, db.Message{
		ChannelID: "C_ALERTS",
		TS:        "1020.000000",
		UserID:    "UBOT",
		Text:      "auto-resolved",
		ThreadTS:  sql.NullString{String: threadParentTS, Valid: true},
		TSUnix:    1020,
	})
	// Thread reply from human.
	msgs = append(msgs, db.Message{
		ChannelID: "C_ALERTS",
		TS:        "1021.000000",
		UserID:    "U1",
		Text:      "confirmed fix",
		ThreadTS:  sql.NullString{String: threadParentTS, Valid: true},
		TSUnix:    1021,
	})

	result := p.extractHumanContext(msgs)

	// Must include: thread parent + both thread replies + surrounding context.
	tsSet := make(map[string]bool)
	for _, m := range result {
		tsSet[m.TS] = true
	}
	assert.True(t, tsSet[threadParentTS], "thread parent must be included")
	assert.True(t, tsSet["1020.000000"], "bot thread reply must be included")
	assert.True(t, tsSet["1021.000000"], "human thread reply must be included")
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

	rollupResp := `{"summary":"Active day: frontend fixed bugs, backend deployed API v2","topics":[{"title":"Deployment","summary":"API v2 deployed","decisions":[],"action_items":[],"situations":[],"key_messages":[]},{"title":"Bugfix","summary":"frontend bugs fixed","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}`
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

	weeklyResp := `{"summary":"Productive week with deployments and bug fixes","topics":[{"title":"Deployment","summary":"multiple deploys","decisions":[],"action_items":[],"situations":[],"key_messages":[]},{"title":"Performance","summary":"perf improvements","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}`
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

	formatted := p.formatMessages(msgs, nil)
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
		Summary: "test summary",
		Topics: []Topic{
			{Title: "topic1", Summary: "about topic 1", Decisions: []Decision{{Text: "decided X", By: "@alice"}}},
			{Title: "topic2", Summary: "about topic 2", ActionItems: []ActionItem{{Text: "do Y", Assignee: "@bob", Status: "open"}}},
		},
	}

	err := p.storeDigest("C1", "channel", 1000.0, 2000.0, result, 42, &Usage{InputTokens: 500, OutputTokens: 200, CostUSD: 0}, 0)
	require.NoError(t, err)

	d, err := database.GetLatestDigest("C1", "channel")
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, "test summary", d.Summary)
	assert.Equal(t, 42, d.MessageCount)

	// Verify legacy JSON fields (aggregated from topics)
	var topicTitles []string
	require.NoError(t, json.Unmarshal([]byte(d.Topics), &topicTitles))
	assert.Equal(t, []string{"topic1", "topic2"}, topicTitles)

	var decisions []Decision
	require.NoError(t, json.Unmarshal([]byte(d.Decisions), &decisions))
	assert.Len(t, decisions, 1)
	assert.Equal(t, "decided X", decisions[0].Text)

	// Verify digest_topics table
	dbTopics, err := database.GetDigestTopics(d.ID)
	require.NoError(t, err)
	assert.Len(t, dbTopics, 2)
	assert.Equal(t, "topic1", dbTopics[0].Title)
	assert.Equal(t, "topic2", dbTopics[1].Title)
}

// capturingGenerator captures the prompt passed to Generate for inspection.
type capturingGenerator struct {
	response             string
	capturedPrompt       string
	capturedSystemPrompt string
	mu                   sync.Mutex
	calls                int
}

func (m *capturingGenerator) Generate(_ context.Context, systemPrompt, prompt, _ string) (string, *Usage, string, error) {
	m.mu.Lock()
	m.capturedSystemPrompt = systemPrompt
	m.capturedPrompt = systemPrompt + "\n" + prompt // combined for backward-compatible assertions
	m.calls++
	m.mu.Unlock()
	return m.response, &Usage{Model: "test-model", InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", nil
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
		response: `{"summary":"test","topics":[]}`,
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
		response: `{"summary":"test","topics":[]}`,
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
	return m.response, &Usage{Model: "test-model", InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", m.err
}

func validDigestJSON() string {
	return `{"summary":"Test summary","topics":[{"title":"topic1","summary":"about topic 1","decisions":[{"text":"decided X","by":"@alice","message_ts":"123.456","importance":"high"}],"action_items":[{"text":"do Y","assignee":"@bob","status":"open"}],"situations":[],"key_messages":["123.456"]}]}`
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

	// Seed an existing channel digest so lastDigestTime() returns a recent time.
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
	cfg.Digest.BatchMaxChannels = 1 // force individual processing

	seedChannel(t, database, "C1", "general")
	seedChannel(t, database, "C2", "random")
	seedUser(t, database, "U1", "alice", "Alice")

	// Use 35 messages per channel (medium tier ≥30) so BatchMaxChannels=1 forces individual processing.
	seedMessagesForChannel(t, database, "C1", "U1", 35)
	seedMessagesForChannel(t, database, "C2", "U1", 35)

	gen := &threadSafeMockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200) // 2 hours ago

	n, usage, err := p.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, n) // 2 channels
	assert.NotNil(t, usage)
	// 2 channel digests (+ 1 daily rollup if both land on the same UTC day)
	gen.mu.Lock()
	assert.GreaterOrEqual(t, gen.calls, 2)
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
	_, _, _ = p.Run(ctx)

	// Workers should exit early on cancelled context.
	assert.Equal(t, 0, gen.calls)
}

func TestRunChannelDigests_MultipleWorkers(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.AI.Workers = 3
	cfg.Digest.BatchMaxChannels = 1 // force individual processing to test worker parallelism

	seedUser(t, database, "U1", "alice", "Alice")
	for i := 0; i < 5; i++ {
		chID := fmt.Sprintf("C%d", i)
		seedChannel(t, database, chID, fmt.Sprintf("chan%d", i))
		// Use 35 messages per channel (medium tier ≥30) so BatchMaxChannels=1 forces individual processing.
		seedMessagesForChannel(t, database, chID, "U1", 35)
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
	p.TrackContext = "=== ACTIVE TRACKS ===\nTrack #1: API redesign (high priority)"

	err = p.RunDailyRollup(context.Background())
	require.NoError(t, err)

	// Track context should be injected into the prompt.
	assert.Contains(t, gen.capturedPrompt, "ACTIVE TRACKS")
	assert.Contains(t, gen.capturedPrompt, "API redesign")
}

func TestRun_WithTrackLinker(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	// Seed an existing digest so lastDigestTime() returns a recent time.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 1100000,
		Summary: "old", Model: "haiku",
	})
	require.NoError(t, err)

	gen := &threadSafeMockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200)

	linker := &mockTrackLinker{
		created:      2,
		updated:      1,
		trackContext: "=== ACTIVE TRACKS ===\nTrack #1: test track",
	}
	p.TrackLinker = linker

	_, _, err = p.Run(context.Background())
	require.NoError(t, err)

	// Track linker should have been called.
	assert.True(t, linker.runCalled, "TrackLinker.Run should be called")
	assert.True(t, linker.formatCalled, "TrackLinker.FormatActiveTracksForPrompt should be called")

	// TrackContext should be set from linker output.
	assert.Contains(t, p.TrackContext, "ACTIVE TRACKS")
}

type mockTrackLinker struct {
	created      int
	updated      int
	trackContext string
	runCalled    bool
	formatCalled bool
}

func (m *mockTrackLinker) Run(ctx context.Context) (int, int, error) {
	m.runCalled = true
	return m.created, m.updated, nil
}

func (m *mockTrackLinker) FormatActiveTracksForPrompt() (string, error) {
	m.formatCalled = true
	return m.trackContext, nil
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

	formatted := p.formatMessages(msgs, nil)
	assert.Contains(t, formatted, "= = =")
	assert.Contains(t, formatted, "- - -")
	assert.NotContains(t, formatted, "===")
	assert.NotContains(t, formatted, "---")
}

func TestFormatMessages_ThreadGrouping(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedUser(t, database, "U1", "alice", "Alice")
	seedUser(t, database, "U2", "bob", "Bob")
	seedUser(t, database, "U3", "carol", "Carol")
	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	msgs := []db.Message{
		{TS: "1000.001", UserID: "U1", Text: "start of thread", TSUnix: 1000, ReplyCount: 2},
		{TS: "1001.001", UserID: "U2", Text: "reply one", TSUnix: 1001, ThreadTS: sql.NullString{String: "1000.001", Valid: true}},
		{TS: "1002.001", UserID: "U3", Text: "top level msg", TSUnix: 1002},
		{TS: "1003.001", UserID: "U3", Text: "reply two", TSUnix: 1003, ThreadTS: sql.NullString{String: "1000.001", Valid: true}},
	}

	formatted := p.formatMessages(msgs, nil)

	// Parent should appear first, then replies indented, then top-level.
	lines := strings.Split(strings.TrimSpace(formatted), "\n")
	assert.Equal(t, 4, len(lines), "expected 4 lines: parent + 2 replies + 1 top-level")
	assert.Contains(t, lines[0], "start of thread")
	assert.Contains(t, lines[1], "↳")
	assert.Contains(t, lines[1], "reply one")
	assert.Contains(t, lines[2], "↳")
	assert.Contains(t, lines[2], "reply two")
	assert.Contains(t, lines[3], "top level msg")
	assert.NotContains(t, lines[3], "↳")
}

func TestFormatMessages_OrphanThreadReplies(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedUser(t, database, "U1", "alice", "Alice")
	seedUser(t, database, "U2", "bob", "Bob")
	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	// Replies whose parent is NOT in the batch.
	msgs := []db.Message{
		{TS: "2001.001", UserID: "U1", Text: "orphan reply 1", TSUnix: 2001, ThreadTS: sql.NullString{String: "999.001", Valid: true}},
		{TS: "2002.001", UserID: "U2", Text: "orphan reply 2", TSUnix: 2002, ThreadTS: sql.NullString{String: "999.001", Valid: true}},
		{TS: "2003.001", UserID: "U1", Text: "normal msg", TSUnix: 2003},
	}

	formatted := p.formatMessages(msgs, nil)
	lines := strings.Split(strings.TrimSpace(formatted), "\n")
	assert.Equal(t, 3, len(lines))
	assert.Contains(t, lines[0], "↳")
	assert.Contains(t, lines[0], "orphan reply 1")
	assert.Contains(t, lines[1], "↳")
	assert.Contains(t, lines[1], "orphan reply 2")
	assert.Contains(t, lines[2], "normal msg")
	assert.NotContains(t, lines[2], "↳")
}

func TestFormatMessages_SelfReferencingParent(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	seedUser(t, database, "U1", "alice", "Alice")
	seedUser(t, database, "U2", "bob", "Bob")
	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	// Parent with self-referencing thread_ts (ts == thread_ts, as from full sync).
	msgs := []db.Message{
		{TS: "1000.001", UserID: "U1", Text: "parent msg", TSUnix: 1000, ReplyCount: 1,
			ThreadTS: sql.NullString{String: "1000.001", Valid: true}},
		{TS: "1001.001", UserID: "U2", Text: "child reply", TSUnix: 1001,
			ThreadTS: sql.NullString{String: "1000.001", Valid: true}},
	}

	formatted := p.formatMessages(msgs, nil)
	lines := strings.Split(strings.TrimSpace(formatted), "\n")
	assert.Equal(t, 2, len(lines))
	assert.Contains(t, lines[0], "parent msg")
	assert.NotContains(t, lines[0], "↳")
	assert.Contains(t, lines[1], "↳")
	assert.Contains(t, lines[1], "child reply")
}

func TestFormatProfileContext_WithAllFields(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	gen := &mockGenerator{}

	p := New(database, cfg, gen, testLogger())
	p.profile = &db.UserProfile{
		CustomPromptContext: "I am a senior engineer",
		StarredChannels:     `["C1","C2"]`,
		StarredPeople:       `["U10"]`,
		Reports:             `["U20","U21"]`,
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
		StarredChannels:     "[]",
		StarredPeople:       "[]",
		Reports:             "[]",
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

func TestLastStepFields(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())
	p.SinceOverride = float64(time.Now().Unix() - 7200)

	var lastMsgCount int
	var lastFrom, lastTo time.Time
	p.OnProgress = func(done, total int, status string) {
		lastMsgCount = p.LastStepMessageCount
		lastFrom = p.LastStepPeriodFrom
		lastTo = p.LastStepPeriodTo
	}

	n, _, err := p.RunChannelDigests(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// LastStep* should reflect the last channel processed
	assert.Equal(t, 5, lastMsgCount)
	assert.False(t, lastFrom.IsZero())
	assert.False(t, lastTo.IsZero())
	assert.True(t, lastTo.After(lastFrom) || lastTo.Equal(lastFrom))
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
		Topics:  []Topic{{Title: "a", Summary: "topic a"}},
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

	result := &DigestResult{Summary: "versioned", Topics: []Topic{{Title: "t1", Summary: "topic"}}}
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
	input := "```\n{\"summary\":\"test\",\"topics\":[]}\n```"
	result, err := parseDigestResult(input)
	require.NoError(t, err)
	assert.Equal(t, "test", result.Summary)
}

func TestParseDigestResult_WithImportanceField(t *testing.T) {
	input := `{"summary":"test","topics":[{"title":"Infra","summary":"infra topic","decisions":[{"text":"use K8s","by":"@ops","message_ts":"1.1","importance":"high"}],"action_items":[],"situations":[],"key_messages":[]}]}`
	result, err := parseDigestResult(input)
	require.NoError(t, err)
	require.Len(t, result.Topics, 1)
	require.Len(t, result.Topics[0].Decisions, 1)
	assert.Equal(t, "high", result.Topics[0].Decisions[0].Importance)
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

func TestRunFirstRun_SingleWindow(t *testing.T) {
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
	n, usage, err := p.Run(context.Background())

	require.NoError(t, err)
	assert.Greater(t, n, 0, "should generate digests")
	assert.NotNil(t, usage)
	// Single window: all messages processed in one batch per channel (not 3 day-by-day calls).
	// No daily rollup since only 1 channel exists (needs ≥2).
	gen.mu.Lock()
	callCount := gen.calls
	gen.mu.Unlock()
	assert.Equal(t, 1, callCount, "should make exactly 1 AI call for single channel")
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
	cfg.AI.Workers = 0 // should default to DefaultAIWorkers

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
		{"channelDigestPrompt", channelDigestPrompt, 7},           // channelName, fromStr, toStr, profileCtx, langInstr, previousCtx, messages
		{"channelBatchDigestPrompt", channelBatchDigestPrompt, 6}, // fromStr, toStr, profileCtx, langInstr, prevCtxNote, channelBlocks
		{"dailyRollupPrompt", dailyRollupPrompt, 5},               // dateStr, profileCtx, langInstr, previousCtx, channelInput
		{"weeklyTrendsPrompt", weeklyTrendsPrompt, 7},             // date, fromStr, toStr, profileCtx, langInstr, previousCtx, dailies
		{"periodSummaryPrompt", periodSummaryPrompt, 5},           // fromStr, toStr, profileCtx, langInstr, digests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := strings.Count(tt.prompt, "%s")
			assert.Equal(t, tt.expected, count,
				"%s has %d %%s placeholders, expected %d", tt.name, count, tt.expected)
		})
	}
}

// validDigestJSONWithRunningSummary returns a valid JSON response that includes
// a running_summary field for testing channel memory round-trip.
func validDigestJSONWithRunningSummary() string {
	return `{"summary":"Test summary","topics":[{"title":"topic1","summary":"about topic 1","decisions":[],"action_items":[],"situations":[],"key_messages":[]}],"running_summary":{"active_topics":[{"topic":"Migration","status":"in_progress","started":"2026-03-18","last_update":"2026-03-21","key_participants":["U1"],"summary":"Working on migration"}],"recent_decisions":[],"channel_dynamics":"Active channel","open_questions":["When to deploy?"]}}`
}

func TestChannelMemory_RunningSummaryStoredAndLoaded(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	// Seed an existing digest so lastDigestTime() returns a recent time.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(time.Now().Unix() - 86400*2),
		PeriodTo:   float64(time.Now().Unix() - 86400),
		Summary:    "old digest", MessageCount: 3, Model: "haiku",
	})
	require.NoError(t, err)

	// First run: AI returns a running_summary
	gen := &mockGenerator{response: validDigestJSONWithRunningSummary()}
	p := New(database, cfg, gen, testLogger())
	n, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify running_summary was stored in DB
	result, err := database.GetLatestRunningSummaryWithAge("C1", "channel")
	require.NoError(t, err)
	require.NotNil(t, result, "running summary should be stored")
	assert.Contains(t, result.Summary, "Migration")
	assert.Less(t, result.AgeDays, 1.0)
}

func TestChannelMemory_GracefulDegradation_NoContext(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	seedChannel(t, database, "C1", "general")
	seedUser(t, database, "U1", "alice", "Alice")
	seedMessagesForChannel(t, database, "C1", "U1", 5)

	// Seed an existing digest WITHOUT running_summary
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(time.Now().Unix() - 86400*2),
		PeriodTo:   float64(time.Now().Unix() - 86400),
		Summary:    "old digest", MessageCount: 3, Model: "haiku",
	})
	require.NoError(t, err)

	gen := &mockGenerator{response: validDigestJSON()}
	p := New(database, cfg, gen, testLogger())

	// loadPreviousContext should return empty string (no crash)
	ctx := p.loadPreviousContext("C1", "channel")
	assert.Empty(t, ctx, "should return empty string when no running summary exists")

	// Pipeline should still work fine
	n, _, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestChannelMemory_TTL_ExpiredAfter30Days(t *testing.T) {
	database := testDB(t)

	// Insert a digest with running_summary but created_at 35 days ago
	oldTime := time.Now().Add(-35 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	_, err := database.Exec(`INSERT INTO digests (channel_id, type, period_from, period_to, summary, running_summary, message_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"C1", "channel", float64(time.Now().Unix()-86400*36), float64(time.Now().Unix()-86400*35),
		"old summary", `{"active_topics":[],"channel_dynamics":"test"}`, 10, oldTime)
	require.NoError(t, err)

	cfg := testConfig()
	p := New(database, cfg, &mockGenerator{}, testLogger())

	ctx := p.loadPreviousContext("C1", "channel")
	assert.Empty(t, ctx, "should not load context older than 30 days")
}

func TestChannelMemory_TTL_OutdatedWarningAfter7Days(t *testing.T) {
	database := testDB(t)

	// Insert a digest with running_summary created 10 days ago
	oldTime := time.Now().Add(-10 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	_, err := database.Exec(`INSERT INTO digests (channel_id, type, period_from, period_to, summary, running_summary, message_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"C1", "channel", float64(time.Now().Unix()-86400*11), float64(time.Now().Unix()-86400*10),
		"old summary", `{"active_topics":[],"channel_dynamics":"test"}`, 10, oldTime)
	require.NoError(t, err)

	cfg := testConfig()
	p := New(database, cfg, &mockGenerator{}, testLogger())

	ctx := p.loadPreviousContext("C1", "channel")
	assert.Contains(t, ctx, "PREVIOUS CONTEXT", "should load context between 7-30 days")
	assert.Contains(t, ctx, "outdated", "should include outdated warning")
}

func TestChannelMemory_ResetRunningSummary(t *testing.T) {
	database := testDB(t)

	// Insert two digests with running summaries
	for _, ch := range []string{"C1", "C2"} {
		_, err := database.UpsertDigest(db.Digest{
			ChannelID: ch, Type: "channel",
			PeriodFrom: float64(time.Now().Unix() - 86400),
			PeriodTo:   float64(time.Now().Unix()),
			Summary:    "test", MessageCount: 5, Model: "haiku",
			RunningSummary: `{"active_topics":[]}`,
		})
		require.NoError(t, err)
	}

	// Reset only C1
	affected, err := database.ResetRunningSummary("C1", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	// C1 should be empty, C2 should still have summary
	r1, err := database.GetLatestRunningSummaryWithAge("C1", "channel")
	require.NoError(t, err)
	assert.Nil(t, r1, "C1 running summary should be cleared")

	r2, err := database.GetLatestRunningSummaryWithAge("C2", "channel")
	require.NoError(t, err)
	require.NotNil(t, r2, "C2 running summary should still exist")

	// Reset all
	affected, err = database.ResetRunningSummary("", "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected) // only C2 left

	r2, err = database.GetLatestRunningSummaryWithAge("C2", "channel")
	require.NoError(t, err)
	assert.Nil(t, r2, "C2 running summary should be cleared")
}

func TestChannelMemory_LoadPreviousContext_Fresh(t *testing.T) {
	database := testDB(t)

	// Insert a fresh digest with running_summary
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: float64(time.Now().Unix() - 3600),
		PeriodTo:   float64(time.Now().Unix()),
		Summary:    "recent", MessageCount: 5, Model: "haiku",
		RunningSummary: `{"active_topics":[{"topic":"Deploy"}],"channel_dynamics":"Active"}`,
	})
	require.NoError(t, err)

	cfg := testConfig()
	p := New(database, cfg, &mockGenerator{}, testLogger())

	ctx := p.loadPreviousContext("C1", "channel")
	assert.Contains(t, ctx, "PREVIOUS CONTEXT")
	assert.Contains(t, ctx, "Deploy")
	assert.NotContains(t, ctx, "outdated", "fresh context should not have outdated warning")
}

// --- Batch digest tests ---

func TestGroupIntoBatches(t *testing.T) {
	mkEntry := func(id string, visible int) batchEntry {
		return batchEntry{channelID: id, channelName: id, visibleCount: visible}
	}

	t.Run("empty", func(t *testing.T) {
		result := groupIntoBatches(nil, 10, 50)
		assert.Nil(t, result)
	})

	t.Run("single batch when no limit", func(t *testing.T) {
		entries := []batchEntry{mkEntry("C1", 3), mkEntry("C2", 4), mkEntry("C3", 2)}
		result := groupIntoBatches(entries, 0, 0)
		require.Len(t, result, 1)
		assert.Len(t, result[0], 3)
	})

	t.Run("split by maxChannels", func(t *testing.T) {
		entries := []batchEntry{mkEntry("C1", 2), mkEntry("C2", 2), mkEntry("C3", 2)}
		result := groupIntoBatches(entries, 2, 100)
		require.Len(t, result, 2)
		assert.Len(t, result[0], 2)
		assert.Len(t, result[1], 1)
	})

	t.Run("split by maxMessages", func(t *testing.T) {
		entries := []batchEntry{mkEntry("C1", 4), mkEntry("C2", 4), mkEntry("C3", 3)}
		result := groupIntoBatches(entries, 10, 5)
		require.Len(t, result, 3) // each entry alone is ≤5, but pairs exceed
		assert.Len(t, result[0], 1)
		assert.Len(t, result[1], 1)
		assert.Len(t, result[2], 1)
	})

	t.Run("combined limits", func(t *testing.T) {
		entries := []batchEntry{mkEntry("C1", 2), mkEntry("C2", 2), mkEntry("C3", 2), mkEntry("C4", 2)}
		result := groupIntoBatches(entries, 3, 50)
		require.Len(t, result, 2)
		assert.Len(t, result[0], 3)
		assert.Len(t, result[1], 1)
	})
}

func TestParseBatchDigestResult(t *testing.T) {
	t.Run("valid array", func(t *testing.T) {
		raw := `[
			{"channel_id":"C1","summary":"Test summary","topics":[{"title":"t1","summary":"s1","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]},
			{"channel_id":"C2","summary":"Another","topics":[]}
		]`
		results, err := parseBatchDigestResult(raw)
		require.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "C1", results[0].ChannelID)
		assert.Equal(t, "C2", results[1].ChannelID)
	})

	t.Run("empty array", func(t *testing.T) {
		results, err := parseBatchDigestResult("[]")
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("filters empty channel_id", func(t *testing.T) {
		raw := `[{"channel_id":"","summary":"skip"},{"channel_id":"C1","summary":"keep","topics":[]}]`
		results, err := parseBatchDigestResult(raw)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "C1", results[0].ChannelID)
	})

	t.Run("filters empty summary", func(t *testing.T) {
		raw := `[{"channel_id":"C1","summary":"","topics":[]},{"channel_id":"C2","summary":"ok","topics":[]}]`
		results, err := parseBatchDigestResult(raw)
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "C2", results[0].ChannelID)
	})

	t.Run("markdown fences", func(t *testing.T) {
		raw := "```json\n[{\"channel_id\":\"C1\",\"summary\":\"test\",\"topics\":[]}]\n```"
		results, err := parseBatchDigestResult(raw)
		require.NoError(t, err)
		assert.Len(t, results, 1)
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := parseBatchDigestResult("not json")
		assert.Error(t, err)
	})
}

// sourceFromContext extracts the source label set by WithSource.
func sourceFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionSourceKey{}).(string); ok {
		return v
	}
	return ""
}

// multiMockGenerator returns different responses based on the source context key.
type multiMockGenerator struct {
	mu        sync.Mutex
	responses map[string]string // source → response
	calls     map[string]int
	fallback  string
}

func (m *multiMockGenerator) Generate(ctx context.Context, _, _, _ string) (string, *Usage, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls == nil {
		m.calls = make(map[string]int)
	}
	source := sourceFromContext(ctx)
	m.calls[source]++
	if resp, ok := m.responses[source]; ok {
		return resp, &Usage{Model: "test-model", InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", nil
	}
	return m.fallback, &Usage{Model: "test-model", InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", nil
}

func TestBatchDigestIntegration(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 5
	cfg.Digest.BatchMaxChannels = 5
	cfg.Digest.BatchMaxMessages = 6 // forces big channel (6 msgs) into its own batch

	seedChannel(t, database, "C_BIG", "big-channel")
	seedChannel(t, database, "C_SMALL1", "small-one")
	seedChannel(t, database, "C_SMALL2", "small-two")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)

	// Big channel: 6 messages (above threshold)
	for i := range 6 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_BIG", ts, "U1", fmt.Sprintf("big message %d", i))
	}
	// Small channels: 2 messages each (below threshold of 5)
	for i := range 2 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_SMALL1", ts, "U1", fmt.Sprintf("small1 msg %d", i))
	}
	for i := range 2 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_SMALL2", ts, "U1", fmt.Sprintf("small2 msg %d", i))
	}

	// With unified batching: big channel (6 msgs) goes to single-entry batch → individual prompt.
	// Small channels (2 msgs each) are grouped into one batch → batch prompt.
	individualResp := validDigestJSON()
	batchResp := `[
		{"channel_id":"C_SMALL1","summary":"Small1 summary","topics":[{"title":"t1","summary":"s1","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]},
		{"channel_id":"C_SMALL2","summary":"Small2 summary","topics":[{"title":"t2","summary":"s2","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}
	]`

	gen := &multiMockGenerator{
		responses: map[string]string{
			"digest.channel":       individualResp,
			"digest.channel_batch": batchResp,
		},
	}

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	n, _, err := p.runChannelDigestsForWindow(context.Background(), float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 3, n, "should generate 3 digests: 1 individual + 2 batch")

	// Verify batch LLM was called exactly once, individual once
	gen.mu.Lock()
	assert.Equal(t, 1, gen.calls["digest.channel_batch"], "batch should be called once")
	assert.Equal(t, 1, gen.calls["digest.channel"], "individual should be called once for single-entry batch")
	gen.mu.Unlock()

	// Verify all 3 digests are in DB
	digests, err := database.GetDigests(db.DigestFilter{Type: "channel"})
	require.NoError(t, err)
	assert.Len(t, digests, 3)
}

func TestBatchSkipsUnnoteworthyChannels(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 5

	seedChannel(t, database, "C_SMALL1", "small-one")
	seedChannel(t, database, "C_SMALL2", "small-two")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)

	// Both channels have 2 messages
	for i := range 2 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_SMALL1", ts, "U1", fmt.Sprintf("msg %d", i))
	}
	for i := range 2 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_SMALL2", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	// AI returns only 1 channel as noteworthy, skips the other
	batchResp := `[{"channel_id":"C_SMALL1","summary":"Something important","topics":[{"title":"important","summary":"yes","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}]`

	gen := &multiMockGenerator{
		responses: map[string]string{
			"digest.channel_batch": batchResp,
		},
	}

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	n, _, err := p.runChannelDigestsForWindow(context.Background(), float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 1, n, "should generate 1 digest (AI skipped one channel)")

	digests, err := database.GetDigests(db.DigestFilter{Type: "channel"})
	require.NoError(t, err)
	assert.Len(t, digests, 1)
	assert.Equal(t, "Something important", digests[0].Summary)
}

func TestUnifiedBatching_LargeChannelAlone(t *testing.T) {
	// A channel with many messages exceeds BatchMaxMessages, so it goes into
	// its own single-entry batch → individual prompt.
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 5
	cfg.Digest.BatchMaxChannels = 5
	cfg.Digest.BatchMaxMessages = 30 // low limit to force separation

	seedChannel(t, database, "C_BIG", "big-channel")
	seedChannel(t, database, "C_SMALL", "small-channel")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-2 * time.Hour)

	// Big channel: 25 visible messages (exceeds BatchMaxMessages=30 when combined)
	for i := range 25 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*10), 0)
		seedMessage(t, database, "C_BIG", ts, "U1", fmt.Sprintf("msg %d", i))
	}
	// Small channel: 20 visible messages
	for i := range 20 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C_SMALL", ts, "U1", fmt.Sprintf("small msg %d", i))
	}

	gen := &multiMockGenerator{
		responses: map[string]string{
			"digest.channel": validDigestJSON(),
		},
	}

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	n, _, err := p.runChannelDigestsForWindow(context.Background(), float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Both should use individual prompt since they end up in separate single-entry batches
	// (25+20=45 > 30, so they can't share a batch; each alone is ≤30)
	gen.mu.Lock()
	assert.Equal(t, 2, gen.calls["digest.channel"], "both channels should use individual prompt")
	assert.Equal(t, 0, gen.calls["digest.channel_batch"], "no batch calls needed")
	gen.mu.Unlock()
}

func TestUnifiedBatching_ThreeChannelsBatched(t *testing.T) {
	// Three channels with 20 messages each should be batched together.
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 5
	cfg.Digest.BatchMaxChannels = 5
	cfg.Digest.BatchMaxMessages = 300

	seedChannel(t, database, "C1", "chan-one")
	seedChannel(t, database, "C2", "chan-two")
	seedChannel(t, database, "C3", "chan-three")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)

	for _, chID := range []string{"C1", "C2", "C3"} {
		for i := range 20 {
			ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
			seedMessage(t, database, chID, ts, "U1", fmt.Sprintf("msg %d", i))
		}
	}

	batchResp := `[
		{"channel_id":"C1","summary":"C1 summary","topics":[{"title":"t1","summary":"s1","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]},
		{"channel_id":"C2","summary":"C2 summary","topics":[{"title":"t2","summary":"s2","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]},
		{"channel_id":"C3","summary":"C3 summary","topics":[{"title":"t3","summary":"s3","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}
	]`

	gen := &multiMockGenerator{
		responses: map[string]string{
			"digest.channel_batch": batchResp,
		},
	}

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	n, _, err := p.runChannelDigestsForWindow(context.Background(), float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	gen.mu.Lock()
	assert.Equal(t, 1, gen.calls["digest.channel_batch"], "all three channels should be in one batch")
	assert.Equal(t, 0, gen.calls["digest.channel"], "no individual calls")
	gen.mu.Unlock()
}

func TestUnifiedBatching_SingleChannelFallback(t *testing.T) {
	// When batch has exactly 1 channel, it should use the individual channel prompt.
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 5
	cfg.Digest.BatchMaxChannels = 5
	cfg.Digest.BatchMaxMessages = 300

	seedChannel(t, database, "C1", "only-channel")
	seedUser(t, database, "U1", "alice", "Alice")

	now := time.Now()
	since := now.Add(-1 * time.Hour)

	for i := range 10 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
		seedMessage(t, database, "C1", ts, "U1", fmt.Sprintf("msg %d", i))
	}

	gen := &multiMockGenerator{
		responses: map[string]string{
			"digest.channel": validDigestJSON(),
		},
	}

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	n, _, err := p.runChannelDigestsForWindow(context.Background(), float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	gen.mu.Lock()
	assert.Equal(t, 1, gen.calls["digest.channel"], "single channel should use individual prompt")
	assert.Equal(t, 0, gen.calls["digest.channel_batch"], "batch prompt should not be used")
	gen.mu.Unlock()
}

func TestTieredBatching_HighMediumLow(t *testing.T) {
	// Verify tiered batching: high-activity channels get individual prompts,
	// medium channels are batched normally, low-activity channels are batched aggressively.
	database := testDB(t)
	cfg := testConfig()
	cfg.Digest.MinMessages = 1
	cfg.Digest.BatchMaxChannels = 3
	cfg.Digest.BatchMaxMessages = 800

	seedUser(t, database, "U1", "alice", "Alice")

	// 1 high-activity channel (>200 msgs) → individual prompt
	seedChannel(t, database, "C_HIGH", "high-activity")
	// 2 medium-activity channels (30-200 msgs) → batched together (maxCh=3)
	seedChannel(t, database, "C_MED1", "med-one")
	seedChannel(t, database, "C_MED2", "med-two")
	// 8 low-activity channels (<30 msgs) → batched aggressively (maxCh=3*3=9 per batch)
	for i := range 8 {
		seedChannel(t, database, fmt.Sprintf("C_LOW%d", i), fmt.Sprintf("low-%d", i))
	}

	now := time.Now()
	since := now.Add(-2 * time.Hour)

	// High: 210 messages (above DefaultBatchHighActivityThreshold=200)
	for i := range 210 {
		ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*3), 0)
		seedMessage(t, database, "C_HIGH", ts, "U1", fmt.Sprintf("high msg %d", i))
	}
	// Medium: 50 messages each (between 30 and 200)
	for _, ch := range []string{"C_MED1", "C_MED2"} {
		for i := range 50 {
			ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*60), 0)
			seedMessage(t, database, ch, ts, "U1", fmt.Sprintf("med msg %d", i))
		}
	}
	// Low: 10 messages each (below DefaultBatchLowActivityThreshold=30)
	for li := range 8 {
		ch := fmt.Sprintf("C_LOW%d", li)
		for i := range 10 {
			ts := fmt.Sprintf("%d.%06d", since.Unix()+int64(i*120+li), 0)
			seedMessage(t, database, ch, ts, "U1", fmt.Sprintf("low msg %d", i))
		}
	}

	// Build a generic batch response that returns results for all channels in the batch.
	// The multiMockGenerator uses source-based routing: individual → "digest.channel", batch → "digest.channel_batch".
	// For batch calls, we return all possible channels — parseBatchDigestResult filters by non-empty channel_id+summary.
	var allBatchItems []string
	for _, id := range []string{"C_MED1", "C_MED2"} {
		allBatchItems = append(allBatchItems, fmt.Sprintf(
			`{"channel_id":"%s","summary":"%s summary","topics":[{"title":"t","summary":"s","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}`, id, id))
	}
	for i := range 8 {
		allBatchItems = append(allBatchItems, fmt.Sprintf(
			`{"channel_id":"C_LOW%d","summary":"Low%d","topics":[{"title":"t","summary":"s","decisions":[],"action_items":[],"situations":[],"key_messages":[]}]}`, i, i))
	}
	batchResp := "[" + strings.Join(allBatchItems, ",") + "]"

	gen := &multiMockGenerator{
		responses: map[string]string{
			"digest.channel":       validDigestJSON(),
			"digest.channel_batch": batchResp,
		},
	}

	p := New(database, cfg, gen, testLogger())
	p.loadCaches()

	n, _, err := p.runChannelDigestsForWindow(context.Background(), float64(since.Unix()), float64(now.Unix()))
	require.NoError(t, err)
	assert.Equal(t, 11, n, "should generate 11 digests: 1 high + 2 med + 8 low")

	gen.mu.Lock()
	// High-activity channel → individual prompt.
	assert.Equal(t, 1, gen.calls["digest.channel"], "high-activity channel should use individual prompt")
	// Medium (2 channels in 1 batch) + Low (8 channels, maxCh*3=9 → 1 batch) = 2 batch calls.
	assert.Equal(t, 2, gen.calls["digest.channel_batch"], "should have 2 batch calls: 1 medium + 1 low")
	gen.mu.Unlock()
}
