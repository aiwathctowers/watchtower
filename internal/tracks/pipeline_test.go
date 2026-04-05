package tracks

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGenerator struct {
	response string
}

func (m *mockGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", nil
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func testConfig() *config.Config {
	return &config.Config{
		Digest: config.DigestConfig{
			Enabled: true,
		},
	}
}

// --- Track v3 DB tests ---

func TestUpsertTrackNew(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		Text:       "API redesign discussion",
		Context:    "Team is discussing API v2",
		Priority:   "high",
		Tags:       `["api","v2"]`,
		ChannelIDs: `["C1","C2"]`,
		SourceRefs: `[{"digest_id":1,"topic_id":42,"channel_id":"C1","timestamp":1000.0}]`,
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	track, err := database.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "API redesign discussion", track.Text)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "", track.ReadAt) // unread
	assert.False(t, track.HasUpdates)
}

func TestUpsertTrackUpdate(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		Text:     "Old title",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Mark as read first
	require.NoError(t, database.MarkTrackRead(int(id)))

	// Now update — should set has_updates=1 because it was read
	_, err = database.UpsertTrack(db.Track{
		ID:       int(id),
		Text:     "Updated title",
		Priority: "high",
	})
	require.NoError(t, err)

	track, err := database.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Updated title", track.Text)
	assert.True(t, track.HasUpdates)
}

func TestMarkTrackRead(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		Text:     "Test",
		Priority: "low",
	})
	require.NoError(t, err)

	// Set has_updates
	require.NoError(t, database.SetTrackHasUpdates(int(id)))

	// Mark as read — should clear has_updates
	require.NoError(t, database.MarkTrackRead(int(id)))

	track, err := database.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.NotEmpty(t, track.ReadAt)
	assert.False(t, track.HasUpdates)
}

func TestGetTracksFilter(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{Text: "High priority", Priority: "high", ChannelIDs: `["C1"]`})
	require.NoError(t, err)
	_, err = database.UpsertTrack(db.Track{Text: "Low priority", Priority: "low", ChannelIDs: `["C2"]`})
	require.NoError(t, err)

	// Filter by priority
	tracks, err := database.GetTracks(db.TrackFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "High priority", tracks[0].Text)

	// Filter by channel
	tracks, err = database.GetTracks(db.TrackFilter{ChannelID: "C2"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Low priority", tracks[0].Text)

	// Filter by has_updates
	hasUpdates := true
	tracks, err = database.GetTracks(db.TrackFilter{HasUpdates: &hasUpdates})
	require.NoError(t, err)
	assert.Len(t, tracks, 0)
}

func TestGetAllActiveTracks(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{Text: "Track 1", Priority: "high"})
	require.NoError(t, err)
	_, err = database.UpsertTrack(db.Track{Text: "Track 2", Priority: "low"})
	require.NoError(t, err)

	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}

func TestGetTrackCount(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{Text: "A", Priority: "high"})
	require.NoError(t, err)
	_, err = database.UpsertTrack(db.Track{Text: "B", Priority: "medium"})
	require.NoError(t, err)

	total, _, err := database.GetTrackCount()
	require.NoError(t, err)
	assert.Equal(t, 2, total)
}

// --- Pipeline tests ---

func TestAccumulatedUsage(t *testing.T) {
	pipe := &Pipeline{}
	in, out, cost, overhead := pipe.AccumulatedUsage()
	assert.Equal(t, 0, in)
	assert.Equal(t, 0, out)
	assert.Equal(t, 0.0, cost)
	assert.Equal(t, 0, overhead)

	pipe.totalInputTokens.Add(1000)
	pipe.totalOutputTokens.Add(500)
	pipe.totalAPITokens.Add(8000)
	in, out, cost, overhead = pipe.AccumulatedUsage()
	assert.Equal(t, 1000, in)
	assert.Equal(t, 500, out)
	assert.Equal(t, 0.0, cost) // cost always 0
	assert.Equal(t, 8000, overhead)
}

func TestSetPromptStore(t *testing.T) {
	pipe := &Pipeline{}
	assert.Nil(t, pipe.promptStore)
	pipe.SetPromptStore(nil)
	assert.Nil(t, pipe.promptStore)
}

func TestProgressCallback(t *testing.T) {
	pipe := &Pipeline{}
	// No panic when OnProgress is nil
	pipe.progress(0, 10, "test")

	var calledDone, calledTotal int
	var calledStatus string
	pipe.OnProgress = func(done, total int, status string) {
		calledDone = done
		calledTotal = total
		calledStatus = status
	}
	pipe.progress(5, 10, "halfway")
	assert.Equal(t, 5, calledDone)
	assert.Equal(t, 10, calledTotal)
	assert.Equal(t, "halfway", calledStatus)
}

func TestLanguageInstruction(t *testing.T) {
	pipe := &Pipeline{cfg: &config.Config{}}
	// Default (empty language)
	assert.Contains(t, pipe.languageInstruction(), "language most commonly used")

	// English (should also use default)
	pipe.cfg.Digest.Language = "English"
	assert.Contains(t, pipe.languageInstruction(), "language most commonly used")

	// Non-English
	pipe.cfg.Digest.Language = "Russian"
	assert.Contains(t, pipe.languageInstruction(), "Russian")
	assert.Contains(t, pipe.languageInstruction(), "IMPORTANT")
}

func TestLoadCaches(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertUser(db.User{ID: "U1", Name: "alice", DisplayName: "Alice Display"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U2", Name: "bob_no_display"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	pipe := New(database, testConfig(), nil, log.Default())
	pipe.loadCaches()

	assert.Equal(t, "general", pipe.channelName("C1"))
	assert.Equal(t, "Alice Display", pipe.userNames["U1"])
	assert.Equal(t, "bob_no_display", pipe.userNames["U2"])
}

func TestChannelNameCacheMiss(t *testing.T) {
	pipe := &Pipeline{}
	pipe.channelNames = map[string]string{"C1": "general"}
	pipe.userNames = map[string]string{"U1": "alice"}

	assert.Equal(t, "general", pipe.channelName("C1"))
	assert.Equal(t, "alice", pipe.userNames["U1"])
	assert.Equal(t, "C999", pipe.channelName("C999"))
	assert.Equal(t, "", pipe.userNames["U999"])
}

func TestGetPromptFallback(t *testing.T) {
	pipe := &Pipeline{cfg: testConfig()}
	// No prompt store — should return default
	tmpl, version := pipe.getPrompt("tracks.extract")
	assert.NotEmpty(t, tmpl)
	assert.Equal(t, 0, version)
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"newlines", "hello\nworld\r!", "hello world !"},
		{"backticks", "```code```", "` ` `code` ` `"},
		{"section markers", "=== SECTION === and --- divider ---", "= = = SECTION = = = and - - - divider - - -"},
		{"clean text", "nothing to sanitize", "nothing to sanitize"},
		{"combined", "```\n===\n---", "` ` ` = = = - - -"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitize(tt.input))
		})
	}
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain json", `{"items": []}`, `{"items": []}`},
		{"json fences", "```json\n{\"items\": []}\n```", `{"items": []}`},
		{"plain fences", "```\n{\"items\": []}\n```", `{"items": []}`},
		{"leading text", "Here is the result:\n{\"items\": []}", `{"items": []}`},
		{"trailing text", "{\"items\": []} done!", `{"items": []}`},
		{"nested braces", "text {\"a\": {\"b\": 1}} more", `{"a": {"b": 1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, cleanJSON(tt.input))
		})
	}
}

func TestValidatePriority(t *testing.T) {
	assert.Equal(t, "high", validatePriority("high"))
	assert.Equal(t, "medium", validatePriority("medium"))
	assert.Equal(t, "low", validatePriority("low"))
	assert.Equal(t, "medium", validatePriority("invalid"))
	assert.Equal(t, "medium", validatePriority(""))
}

func TestFormatActiveTracksForPrompt(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{
		Text:       "Track 1",
		Context:    "In progress",
		Priority:   "high",
		ChannelIDs: `["C1"]`,
	})
	require.NoError(t, err)

	pipe := New(database, testConfig(), nil, log.Default())
	result, err := pipe.FormatActiveTracksForPrompt()
	require.NoError(t, err)
	assert.Contains(t, result, "Track 1")
	assert.Contains(t, result, "high")
}

func TestFormatActiveTracksForPromptEmpty(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), nil, log.Default())
	result, err := pipe.FormatActiveTracksForPrompt()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestParseResult(t *testing.T) {
	raw := `{
		"items": [{"text": "Test track", "priority": "high", "context": "Some context"}]
	}`

	result, err := parseResult(raw)
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "Test track", result.Items[0].Text)
}

func TestParseResultInvalid(t *testing.T) {
	_, err := parseResult("bad json {{{")
	assert.Error(t, err)
}

func TestRunNoTopics(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{response: `{}`}, log.Default())

	created, updated, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, created)
	assert.Equal(t, 0, updated)
}

func TestRunDisabled(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{Digest: config.DigestConfig{Enabled: false}}
	pipe := New(database, cfg, &mockGenerator{response: `{}`}, log.Default())

	created, updated, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, created)
	assert.Equal(t, 0, updated)
}

func TestRunWithDigestsAndAI(t *testing.T) {
	database := testDB(t)

	// Seed workspace with current user
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test"}))
	require.NoError(t, database.SetCurrentUserID("U1"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U1", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U2", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Insert a channel digest with a topic in the time window.
	now := time.Now()
	from := float64(now.Add(-2 * time.Hour).Unix())
	to := float64(now.Unix())
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: from, PeriodTo: to,
		Summary: "Discussion about API PR review", MessageCount: 5, Model: "test",
	})
	require.NoError(t, err)

	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 0, 'API PR Review', 'Bob asked Alice to review the API pull request.', '[]',
		'[{"text":"Review API PR","assignee":"@alice","status":"open"}]', '[]', '[]')`)
	require.NoError(t, err)

	// AI response — batch format.
	ts := fmt.Sprintf("%d.000000", now.Add(-30*time.Minute).Unix())
	aiResponse := `[{
		"channel_id": "C1",
		"items": [{
			"text": "Review API PR",
			"context": "Bob asked Alice to review the API pull request.",
			"category": "code_review",
			"ownership": "mine",
			"priority": "medium",
			"requester": {"name": "@bob", "user_id": "U2"},
			"participants": [{"name":"Bob","user_id":"U2","stance":"requester"}],
			"source_refs": [{"ts":"` + ts + `","author":"@bob","text":"can you review the API PR?"}],
			"tags": ["api"]
		}]
	}]`

	gen := &mockGenerator{response: aiResponse}
	cfg := testConfig()
	cfg.AI.Workers = 1
	pipe := New(database, cfg, gen, log.Default())

	created, _, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)

	// Verify track was stored
	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "Review API PR", tracks[0].Text)
	assert.Equal(t, "code_review", tracks[0].Category)
	assert.Equal(t, "mine", tracks[0].Ownership)
	assert.Equal(t, "U1", tracks[0].AssigneeUserID)
	assert.Contains(t, tracks[0].ChannelIDs, "C1")
}

// Verify GetUnlinkedTopics returns only topics not linked to any track via source_refs.
func TestGetUnlinkedTopics(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Create digest with two topics
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "Test", MessageCount: 5, Model: "test",
	})
	require.NoError(t, err)

	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 0, 'Topic A', 'Summary A', '[]', '[]', '[]', '[]')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 1, 'Topic B', 'Summary B', '[]', '[]', '[]', '[]')`)
	require.NoError(t, err)

	// Both should be unlinked
	topics, err := database.GetUnlinkedTopics(14)
	require.NoError(t, err)
	assert.Len(t, topics, 2)

	// Now create a track that links to topic 1 (id=1)
	_, err = database.UpsertTrack(db.Track{
		Text:       "Linked track",
		Priority:   "medium",
		SourceRefs: `[{"digest_id":1,"topic_id":1,"channel_id":"C1","timestamp":1000.0}]`,
	})
	require.NoError(t, err)

	// Only topic 2 should be unlinked now
	topics, err = database.GetUnlinkedTopics(14)
	require.NoError(t, err)
	assert.Len(t, topics, 1)
	assert.Equal(t, "Topic B", topics[0].Title)
}

// --- Batch & optimization tests ---

func TestGroupDigestBatches(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		result := groupDigestBatches(nil, 10, 50)
		assert.Nil(t, result)
	})

	t.Run("single batch", func(t *testing.T) {
		entries := []digestEntry{
			{channelID: "C1", topicCount: 2},
			{channelID: "C2", topicCount: 3},
		}
		batches := groupDigestBatches(entries, 10, 50)
		assert.Len(t, batches, 1)
		assert.Len(t, batches[0], 2)
	})

	t.Run("split by channels", func(t *testing.T) {
		entries := []digestEntry{
			{channelID: "C1", topicCount: 1},
			{channelID: "C2", topicCount: 1},
			{channelID: "C3", topicCount: 1},
		}
		batches := groupDigestBatches(entries, 2, 100)
		assert.Len(t, batches, 2)
		assert.Len(t, batches[0], 2)
		assert.Len(t, batches[1], 1)
	})

	t.Run("split by topics", func(t *testing.T) {
		entries := []digestEntry{
			{channelID: "C1", topicCount: 30},
			{channelID: "C2", topicCount: 25},
			{channelID: "C3", topicCount: 10},
		}
		batches := groupDigestBatches(entries, 10, 50)
		// C1 (30) fits alone, C2 (25) would exceed 50 with C1, so new batch.
		// C2 (25) + C3 (10) = 35 <= 50, so same batch.
		assert.Len(t, batches, 2)
		assert.Len(t, batches[0], 1) // C1
		assert.Equal(t, "C1", batches[0][0].channelID)
		assert.Len(t, batches[1], 2) // C2 + C3
		assert.Equal(t, "C2", batches[1][0].channelID)
		assert.Equal(t, "C3", batches[1][1].channelID)
	})
}

func TestParseBatchTracksResult(t *testing.T) {
	t.Run("valid array", func(t *testing.T) {
		raw := `[{"channel_id":"C1","items":[{"text":"Do thing","priority":"high","context":"ctx"}]},{"channel_id":"C2","items":[]}]`
		results, err := parseBatchTracksResult(raw)
		require.NoError(t, err)
		assert.Len(t, results, 1) // C2 filtered out (empty items)
		assert.Equal(t, "C1", results[0].ChannelID)
		assert.Len(t, results[0].Items, 1)
	})

	t.Run("empty array", func(t *testing.T) {
		results, err := parseBatchTracksResult("[]")
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("markdown fences", func(t *testing.T) {
		raw := "```json\n[{\"channel_id\":\"C1\",\"items\":[{\"text\":\"Test\",\"priority\":\"medium\",\"context\":\"c\"}]}]\n```"
		results, err := parseBatchTracksResult(raw)
		require.NoError(t, err)
		assert.Len(t, results, 1)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := parseBatchTracksResult("bad json [[[")
		assert.Error(t, err)
	})
}

func TestFormatExistingTracksTruncation(t *testing.T) {
	database := testDB(t)

	// Create 35 tracks with varying priorities.
	for i := 0; i < 35; i++ {
		priority := "low"
		if i < 5 {
			priority = "high"
		} else if i < 15 {
			priority = "medium"
		}
		_, err := database.UpsertTrack(db.Track{
			Text:       fmt.Sprintf("Track %d", i),
			Context:    fmt.Sprintf("Context for track %d with some details", i),
			Priority:   priority,
			ChannelIDs: `["C_OTHER"]`,
		})
		require.NoError(t, err)
	}

	pipe := New(database, testConfig(), nil, log.Default())
	result := pipe.formatExistingTracks("U1")

	// Should contain truncation notice.
	assert.Contains(t, result, "Showing top 30 of 35")

	// Count track entries.
	lines := strings.Split(result, "\n")
	trackLines := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "#") {
			trackLines++
		}
	}
	assert.Equal(t, 30, trackLines)

	// Should include context snippet.
	assert.Contains(t, result, "Context for track")
}

func TestFormatExistingTracksIncludesBatchChannels(t *testing.T) {
	database := testDB(t)

	// Create a track from a channel that would be in the batch.
	_, err := database.UpsertTrack(db.Track{
		Text:       "Existing track from batch channel",
		Context:    "This track was created in a previous run",
		Priority:   "high",
		ChannelIDs: `["C_BATCH"]`,
	})
	require.NoError(t, err)

	pipe := New(database, testConfig(), nil, log.Default())
	result := pipe.formatExistingTracks("U1")

	// Track from batch channel must be included (previously it was excluded).
	assert.Contains(t, result, "Existing track from batch channel")
}

// routingMockGenerator routes responses based on whether the userMessage contains batch markers.
type routingMockGenerator struct {
	individualResponse string
	batchResponse      string
}

func (m *routingMockGenerator) Generate(_ context.Context, _, userMessage, _ string) (string, *digest.Usage, string, error) {
	resp := m.individualResponse
	if strings.Contains(userMessage, "=== CHANNEL DIGESTS ===") {
		resp = m.batchResponse
	}
	return resp, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0}, "mock-session", nil
}

func TestBatchTracksIntegration(t *testing.T) {
	database := testDB(t)

	// Seed workspace.
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test"}))
	require.NoError(t, database.SetCurrentUserID("U1"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U1", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U2", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "backend", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C2", Name: "frontend", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C3", Name: "infra", Type: "public"}))

	now := time.Now()
	from := float64(now.Add(-2 * time.Hour).Unix())
	to := float64(now.Unix())

	// C1: digest with 3 topics (high activity) — has action_items to pass relevance filter
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: from, PeriodTo: to,
		Summary: "Backend review discussions", MessageCount: 10, Model: "test",
	})
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
			VALUES (1, ?, ?, 'Topic summary', '[]', '[{"text":"review code"}]', '[]', '[]')`, i, fmt.Sprintf("Backend topic %d", i))
		require.NoError(t, err)
	}

	// C2: digest with 1 topic (low activity) — has @mention of U1 to pass relevance filter
	_, err = database.UpsertDigest(db.Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: from, PeriodTo: to,
		Summary: "Frontend small discussion", MessageCount: 2, Model: "test",
	})
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (2, 0, 'Frontend task', 'Small channel task', '[]', '[]', '[]', '["<@U1> please check"]')`)
	require.NoError(t, err)

	// C3: digest with 1 topic — has action_items to pass relevance filter
	_, err = database.UpsertDigest(db.Digest{
		ChannelID: "C3", Type: "channel",
		PeriodFrom: from, PeriodTo: to,
		Summary: "Infra question", MessageCount: 1, Model: "test",
	})
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (3, 0, 'Infra check', 'Infrastructure question', '[]', '[{"text":"check infra"}]', '[]', '[]')`)
	require.NoError(t, err)

	// All channels go through unified batch processing now.
	batchResponse := `[{"channel_id":"C1","items":[{"text":"Review backend code","context":"Bob asked for review","category":"code_review","ownership":"mine","priority":"medium"}]},{"channel_id":"C2","items":[{"text":"Frontend task","context":"From small channel","category":"task","ownership":"mine","priority":"low"}]},{"channel_id":"C3","items":[{"text":"Infra check","context":"Infrastructure question","category":"info_request","ownership":"mine","priority":"medium"}]}]`

	gen := &routingMockGenerator{
		individualResponse: batchResponse, // not used, but required by struct
		batchResponse:      batchResponse,
	}

	cfg := testConfig()
	cfg.AI.Workers = 2
	pipe := New(database, cfg, gen, log.Default())

	created, _, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, created) // all 3 from single batch

	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 3)

	// Verify we got tracks from all channels.
	texts := map[string]bool{}
	for _, tr := range tracks {
		texts[tr.Text] = true
	}
	assert.True(t, texts["Review backend code"])
	assert.True(t, texts["Frontend task"])
	assert.True(t, texts["Infra check"])
}

func TestStoreTrackItems(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test"}))
	require.NoError(t, database.SetCurrentUserID("U1"))

	pipe := New(database, testConfig(), nil, log.Default())

	items := []aiItem{
		{
			Text:      "Test track 1",
			Context:   "Context 1",
			Priority:  "high",
			Category:  "task",
			Ownership: "mine",
		},
		{
			Text:      "Test track 2",
			Context:   "Context 2",
			Priority:  "medium",
			Category:  "code_review",
			Ownership: "mine",
		},
	}

	usage := &digest.Usage{InputTokens: 200, OutputTokens: 100, CostUSD: 0}
	stored := pipe.storeTrackItems(items, "U1", "C1", "general", usage, 1, 1000, 2000)
	assert.Equal(t, 2, stored)

	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}

func TestPriorityOrder(t *testing.T) {
	assert.Equal(t, 0, priorityOrder("high"))
	assert.Equal(t, 1, priorityOrder("medium"))
	assert.Equal(t, 2, priorityOrder("low"))
	assert.Equal(t, 2, priorityOrder("unknown"))
}

// --- Channel scoring & topic dedup tests ---

func TestScoreChannel(t *testing.T) {
	existingTrackChannels := map[string]bool{"C1": true}
	starredChannels := map[string]bool{"C2": true}
	relatedUsers := map[string]bool{"U99": true}

	t.Run("no signals = 0", func(t *testing.T) {
		topics := []db.DigestTopic{{Title: "Test", ActionItems: "[]", Situations: "[]", KeyMessages: "[]"}}
		score := scoreChannel("C_NEW", topics, "U1", nil, nil, nil)
		assert.Equal(t, 0, score)
	})

	t.Run("existing tracks = 3", func(t *testing.T) {
		topics := []db.DigestTopic{{Title: "Test", ActionItems: "[]", Situations: "[]", KeyMessages: "[]"}}
		score := scoreChannel("C1", topics, "U1", existingTrackChannels, nil, nil)
		assert.Equal(t, 3, score)
	})

	t.Run("starred channel = 2", func(t *testing.T) {
		topics := []db.DigestTopic{{Title: "Test", ActionItems: "[]", Situations: "[]", KeyMessages: "[]"}}
		score := scoreChannel("C2", topics, "U1", nil, starredChannels, nil)
		assert.Equal(t, 2, score)
	})

	t.Run("user mention = 2", func(t *testing.T) {
		topics := []db.DigestTopic{{Title: "Test", ActionItems: "[]", Situations: "[]", KeyMessages: `["<@U1> please review"]`}}
		score := scoreChannel("C_NEW", topics, "U1", nil, nil, nil)
		assert.Equal(t, 2, score)
	})

	t.Run("related user in situations = 1", func(t *testing.T) {
		topics := []db.DigestTopic{{Title: "Test", ActionItems: "[]", Situations: `[{"topic":"x","participants":[{"user_id":"U99"}]}]`, KeyMessages: "[]"}}
		score := scoreChannel("C_NEW", topics, "U1", nil, nil, relatedUsers)
		assert.Equal(t, 1, score)
	})

	t.Run("action items = 1", func(t *testing.T) {
		topics := []db.DigestTopic{{Title: "Test", ActionItems: `[{"text":"do something"}]`, Situations: "[]", KeyMessages: "[]"}}
		score := scoreChannel("C_NEW", topics, "U1", nil, nil, nil)
		assert.Equal(t, 1, score)
	})

	t.Run("multiple signals additive", func(t *testing.T) {
		topics := []db.DigestTopic{{
			Title:       "Test",
			ActionItems: `[{"text":"do it"}]`,
			Situations:  `[{"topic":"x","participants":[{"user_id":"U99"}]}]`,
			KeyMessages: `["<@U1> review this"]`,
		}}
		// All signals matching: existing(3) + starred(2) + mention(2) + related(1) + action(1) = 9
		all := map[string]bool{"C1": true}
		score := scoreChannel("C1", topics, "U1", all, all, relatedUsers)
		assert.Equal(t, 9, score)
	})
}

func TestFormatActiveTracksForPromptLimit(t *testing.T) {
	database := testDB(t)

	// Create 35 tracks to test the limit of 30.
	for i := 0; i < 35; i++ {
		priority := "low"
		if i < 5 {
			priority = "high"
		} else if i < 15 {
			priority = "medium"
		}
		_, err := database.UpsertTrack(db.Track{
			Text:     fmt.Sprintf("Track %d", i),
			Priority: priority,
		})
		require.NoError(t, err)
	}

	pipe := New(database, testConfig(), nil, log.Default())
	result, err := pipe.FormatActiveTracksForPrompt()
	require.NoError(t, err)

	// Should contain truncation notice.
	assert.Contains(t, result, "Showing 30 of 35")

	// Count track lines (compact format: #ID [priority] text).
	lines := strings.Split(strings.TrimSpace(result), "\n")
	trackLines := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "#") {
			trackLines++
		}
	}
	assert.Equal(t, 30, trackLines)

	// Should NOT contain "Context:" (compact format).
	assert.NotContains(t, result, "Context:")

	// High priority tracks should come first.
	firstTrackLine := ""
	for _, l := range lines {
		if strings.HasPrefix(l, "#") {
			firstTrackLine = l
			break
		}
	}
	assert.Contains(t, firstTrackLine, "[high]")
}

func TestTokenizeText(t *testing.T) {
	tokens := tokenizeText("DDoS post-mortem на web-версии", "инцидент стабилизирован")
	assert.Contains(t, tokens, "ddos")
	assert.Contains(t, tokens, "post")
	assert.Contains(t, tokens, "morte") // "mortem" pseudo-stemmed to 5 chars
	assert.Contains(t, tokens, "web")
	assert.Contains(t, tokens, "верси") // "версии" pseudo-stemmed to 5 chars
	assert.Contains(t, tokens, "инцид") // "инцидент" pseudo-stemmed to 5 chars
	assert.Contains(t, tokens, "стаби") // "стабилизирован" pseudo-stemmed
	// Short tokens (<3 chars) excluded.
	assert.NotContains(t, tokens, "на")
	// Same root, different forms → same stem.
	t1 := tokenizeText("инциденту")
	t2 := tokenizeText("инцидента")
	t3 := tokenizeText("инцидент")
	assert.Contains(t, t1, "инцид")
	assert.Contains(t, t2, "инцид")
	assert.Contains(t, t3, "инцид")
}

func TestJaccardSimilarity(t *testing.T) {
	// Text+context combined, as used in real pipeline.
	a := tokenizeText("DDoS post-mortem на web-версии продукта", "31.03 инцидент на web-версии сервиса")
	b := tokenizeText("Убедиться в проведении пост-мортема по DDoS-инциденту на web-версии",
		"31.03 ночью зафиксирован инцидент на web-версии сервиса")
	score := jaccardSimilarity(a, b)
	assert.Greater(t, score, textSimilarityThreshold, "DDoS-related texts should exceed threshold")

	// Completely unrelated texts.
	c := tokenizeText("Обновить документацию по API эндпоинтам", "Новые эндпоинты не задокументированы")
	score2 := jaccardSimilarity(a, c)
	assert.Less(t, score2, 0.05, "Unrelated texts should have very low similarity")

	// Empty sets.
	assert.Equal(t, 0.0, jaccardSimilarity(map[string]struct{}{}, a))
}

func TestFindSimilarTrack(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test"}))
	require.NoError(t, database.SetCurrentUserID("U1"))

	// Create existing track about DDoS post-mortem (with context, as in production).
	_, err := database.UpsertTrack(db.Track{
		AssigneeUserID: "U1",
		Text:           "Запросить root-cause анализ / постмортем по DDoS-инциденту на web-версии",
		Context:        "31.03 был зафиксирован DDoS-инцидент, затронувший исключительно web-версию сервиса. Савелий Власенко закрыл инцидент без указания причины",
		Priority:       "medium",
		ChannelIDs:     `["C1"]`,
	})
	require.NoError(t, err)

	// Create unrelated track.
	_, err = database.UpsertTrack(db.Track{
		AssigneeUserID: "U1",
		Text:           "Обновить документацию API",
		Context:        "Новые эндпоинты не задокументированы",
		Priority:       "low",
		ChannelIDs:     `["C2"]`,
	})
	require.NoError(t, err)

	cfg := testConfig()
	pipe := New(database, cfg, &mockGenerator{}, log.Default())

	// Pre-load cache (normally done in RunForWindow).
	allActive, _ := database.GetAllActiveTracks()
	pipe.allActiveTracksRef = allActive

	// Similar text should match (realistic dupe from production).
	id, score := pipe.findSimilarTrack("U1",
		"Убедиться в проведении постмортема DDoS-инцидента на веб-версии: подтвердить root cause",
		"31.03 около 01:05 Савелий Власенко сообщил о стабилизации инцидента, затронувшего только веб-версию")
	assert.Greater(t, id, 0, "Should find similar track")
	assert.GreaterOrEqual(t, score, textSimilarityThreshold)

	// Completely different text should not match.
	id2, _ := pipe.findSimilarTrack("U1",
		"Согласовать бюджет на Q2",
		"Финансовый отдел запросил план расходов")
	assert.Equal(t, 0, id2, "Should not find similar track for unrelated text")

	// Different user should not match.
	id3, _ := pipe.findSimilarTrack("U999",
		"Убедиться в проведении постмортема DDoS-инцидента на веб-версии",
		"31.03 инцидент на веб-версии сервиса")
	assert.Equal(t, 0, id3, "Should not match tracks from different user")
}

func TestTextSimilarityDedupInStoreTrackItems(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test"}))
	require.NoError(t, database.SetCurrentUserID("U1"))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "incidents", Type: "public"}))

	// Create existing track (with realistic context for similarity matching).
	_, err := database.UpsertTrack(db.Track{
		AssigneeUserID: "U1",
		Text:           "Запросить root-cause анализ / постмортем по DDoS-инциденту на web-версии",
		Context:        "31.03 был зафиксирован DDoS-инцидент, затронувший исключительно web-версию сервиса. Савелий Власенко закрыл инцидент без указания причины",
		Priority:       "medium",
		ChannelIDs:     `["C1"]`,
	})
	require.NoError(t, err)

	cfg := testConfig()
	pipe := New(database, cfg, &mockGenerator{}, log.Default())

	// Pre-load cache.
	allActive, _ := database.GetAllActiveTracks()
	pipe.allActiveTracksRef = allActive

	now := time.Now()
	from := float64(now.Add(-2 * time.Hour).Unix())
	to := float64(now.Unix())

	// Try to store a near-duplicate — should merge instead of creating new.
	items := []aiItem{
		{
			Text:      "Убедиться в проведении постмортема DDoS-инцидента на веб-версии: подтвердить root cause",
			Context:   "31.03 около 01:05 Савелий Власенко сообщил о стабилизации инцидента, затронувшего только веб-версию",
			Priority:  "medium",
			Category:  "task",
			Ownership: "mine",
		},
	}

	stored := pipe.storeTrackItems(items, "U1", "C1", "incidents", nil, 1, from, to)
	assert.Equal(t, 1, stored)

	// Should still be 1 track total (merged), not 2.
	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 1, "Duplicate should be merged, not created as new")

	// The track text should be updated to the new version.
	assert.Contains(t, tracks[0].Text, "постмортема")
}

func TestTopicDedupBySourceRefs(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test"}))
	require.NoError(t, database.SetCurrentUserID("U1"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U1", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	now := time.Now()
	from := float64(now.Add(-2 * time.Hour).Unix())
	to := float64(now.Unix())

	// Create digest with 2 topics.
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: from, PeriodTo: to,
		Summary: "Test", MessageCount: 5, Model: "test",
	})
	require.NoError(t, err)

	// Topic 1 (id=1) with action_items to pass relevance filter
	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 0, 'Already tracked', 'Summary A', '[]', '[{"text":"do"}]', '[]', '[]')`)
	require.NoError(t, err)
	// Topic 2 (id=2) with action_items
	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 1, 'New topic', 'Summary B', '[]', '[{"text":"check"}]', '[]', '[]')`)
	require.NoError(t, err)

	// Create existing track linked to topic 1.
	_, err = database.UpsertTrack(db.Track{
		Text:       "Existing track",
		Priority:   "medium",
		SourceRefs: `[{"digest_id":1,"topic_id":1,"channel_id":"C1","timestamp":1000.0}]`,
		ChannelIDs: `["C1"]`,
	})
	require.NoError(t, err)

	// Mock AI that returns a track for the new topic.
	aiResponse := `[{"channel_id":"C1","items":[{"text":"New track from topic B","context":"From new topic","priority":"medium","category":"task","ownership":"mine"}]}]`

	cfg := testConfig()
	cfg.AI.Workers = 1
	pipe := New(database, cfg, &mockGenerator{response: aiResponse}, log.Default())

	created, _, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created) // Only 1 (from new topic), not 2

	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 2) // 1 existing + 1 new
}
