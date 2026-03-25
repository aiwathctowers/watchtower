package tracks

import (
	"context"
	"encoding/json"
	"log"
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
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}, "mock-session", nil
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
			Model:   "test-model",
		},
	}
}

// --- Track v3 DB tests ---

func TestUpsertTrackNew(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		Title:         "API redesign discussion",
		Narrative:     "Team is discussing API v2",
		CurrentStatus: "Under review",
		Priority:      "high",
		Tags:          `["api","v2"]`,
		ChannelIDs:    `["C1","C2"]`,
		SourceRefs:    `[{"digest_id":1,"topic_id":42,"channel_id":"C1","timestamp":1000.0}]`,
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	track, err := database.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "API redesign discussion", track.Title)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "", track.ReadAt) // unread
	assert.False(t, track.HasUpdates)
}

func TestUpsertTrackUpdate(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		Title:    "Old title",
		Priority: "medium",
	})
	require.NoError(t, err)

	// Mark as read first
	require.NoError(t, database.MarkTrackRead(int(id)))

	// Now update — should set has_updates=1 because it was read
	_, err = database.UpsertTrack(db.Track{
		ID:       int(id),
		Title:    "Updated title",
		Priority: "high",
	})
	require.NoError(t, err)

	track, err := database.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Updated title", track.Title)
	assert.True(t, track.HasUpdates)
}

func TestMarkTrackRead(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		Title:    "Test",
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

	_, err := database.UpsertTrack(db.Track{Title: "High priority", Priority: "high", ChannelIDs: `["C1"]`})
	require.NoError(t, err)
	_, err = database.UpsertTrack(db.Track{Title: "Low priority", Priority: "low", ChannelIDs: `["C2"]`})
	require.NoError(t, err)

	// Filter by priority
	tracks, err := database.GetTracks(db.TrackFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "High priority", tracks[0].Title)

	// Filter by channel
	tracks, err = database.GetTracks(db.TrackFilter{ChannelID: "C2"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Low priority", tracks[0].Title)

	// Filter by has_updates
	hasUpdates := true
	tracks, err = database.GetTracks(db.TrackFilter{HasUpdates: &hasUpdates})
	require.NoError(t, err)
	assert.Len(t, tracks, 0)
}

func TestGetAllActiveTracks(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{Title: "Track 1", Priority: "high"})
	require.NoError(t, err)
	_, err = database.UpsertTrack(db.Track{Title: "Track 2", Priority: "low"})
	require.NoError(t, err)

	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}

func TestGetTrackCount(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{Title: "A", Priority: "high"})
	require.NoError(t, err)
	_, err = database.UpsertTrack(db.Track{Title: "B", Priority: "medium"})
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
	pipe.totalCostMicro.Add(10000) // 0.01 USD
	pipe.totalAPITokens.Add(8000)
	in, out, cost, overhead = pipe.AccumulatedUsage()
	assert.Equal(t, 1000, in)
	assert.Equal(t, 500, out)
	assert.InDelta(t, 0.01, cost, 0.0001)
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
	tmpl, version := pipe.getPrompt("tracks.create")
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

func TestFormatExistingTracks(t *testing.T) {
	pipe := &Pipeline{
		channelNames: map[string]string{"C1": "general"},
	}
	tracks := []db.Track{
		{ID: 1, Title: "Track one", CurrentStatus: "In progress", Priority: "high", ChannelIDs: `["C1"]`},
		{ID: 2, Title: "Track two", Priority: "low", ChannelIDs: `[]`},
	}
	result := pipe.formatExistingTracks(tracks)
	assert.Contains(t, result, "Track one")
	assert.Contains(t, result, "In progress")
	assert.Contains(t, result, "high")
	assert.Contains(t, result, "Track two")
}

func TestFormatUnlinkedTopics(t *testing.T) {
	pipe := &Pipeline{
		channelNames: map[string]string{"C1": "general"},
	}
	topics := []db.UnlinkedTopic{
		{TopicID: 1, DigestID: 10, ChannelID: "C1", ChannelName: "general", Title: "API discussion", Summary: "Team discussed API v2"},
		{TopicID: 2, DigestID: 11, ChannelID: "C1", ChannelName: "general", Title: "Bug triage", Summary: "3 bugs found"},
	}
	result := pipe.formatUnlinkedTopics(topics)
	assert.Contains(t, result, "API discussion")
	assert.Contains(t, result, "#general")
	assert.Contains(t, result, "Bug triage")
}

func TestFormatActiveTracksForPrompt(t *testing.T) {
	database := testDB(t)

	_, err := database.UpsertTrack(db.Track{
		Title:         "Track 1",
		CurrentStatus: "In progress",
		Priority:      "high",
		ChannelIDs:    `["C1"]`,
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

func TestMergeSourceRefs(t *testing.T) {
	existing := `[{"digest_id":1,"topic_id":10,"channel_id":"C1","timestamp":1000.0}]`
	newRefs := `[{"digest_id":2,"topic_id":20,"channel_id":"C2","timestamp":2000.0}]`

	merged := mergeSourceRefs(existing, newRefs)
	var refs []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(merged), &refs))
	assert.Len(t, refs, 2)
}

func TestMergeSourceRefsDeduplicate(t *testing.T) {
	existing := `[{"digest_id":1,"topic_id":10,"channel_id":"C1","timestamp":1000.0}]`
	newRefs := `[{"digest_id":1,"topic_id":10,"channel_id":"C1","timestamp":1000.0}]`

	merged := mergeSourceRefs(existing, newRefs)
	var refs []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(merged), &refs))
	assert.Len(t, refs, 1) // deduplicated
}

func TestMergeSourceRefsEmptyExisting(t *testing.T) {
	newRefs := `[{"digest_id":1,"topic_id":10,"channel_id":"C1","timestamp":1000.0}]`
	merged := mergeSourceRefs("", newRefs)
	// json.Marshal re-serializes float64(1000) as "1000", so check parsed content
	var refs []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(merged), &refs))
	assert.Len(t, refs, 1)

	merged = mergeSourceRefs("[]", newRefs)
	require.NoError(t, json.Unmarshal([]byte(merged), &refs))
	assert.Len(t, refs, 1)
}

func TestParseTrackResult(t *testing.T) {
	raw := `{
		"new_tracks": [{"title": "Test track", "priority": "high", "source_topic_ids": [1, 2]}],
		"updated_tracks": [{"track_id": 5, "title": "Updated", "new_source_topic_ids": [3]}]
	}`

	result, err := parseTrackResult(raw)
	require.NoError(t, err)
	assert.Len(t, result.NewTracks, 1)
	assert.Equal(t, "Test track", result.NewTracks[0].Title)
	assert.Len(t, result.UpdatedTracks, 1)
	assert.Equal(t, 5, result.UpdatedTracks[0].TrackID)
}

func TestParseTrackResultInvalid(t *testing.T) {
	_, err := parseTrackResult("bad json {{{")
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

func TestRunWithTopicsAndAI(t *testing.T) {
	database := testDB(t)

	// Seed channel and digest with topic (must be recent for GetUnlinkedTopics filter)
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	now := float64(time.Now().Unix())
	_, err := database.UpsertDigest(db.Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: now - 3600, PeriodTo: now,
		Summary: "Test digest", MessageCount: 10, Model: "test",
	})
	require.NoError(t, err)

	// Insert a topic for this digest
	_, err = database.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 0, 'API Discussion', 'Team discussed API v2 design', '[]', '[]', '[]', '[]')`)
	require.NoError(t, err)

	// AI response that creates a new track
	aiResponse := `{
		"new_tracks": [{
			"title": "API v2 design discussion",
			"narrative": "The team is actively discussing API v2 design.",
			"current_status": "Under discussion",
			"participants": [{"user_id":"U1","name":"alice","role":"driver"}],
			"timeline": [{"date":"2026-03-25","event":"Initial discussion","channel_id":"C1"}],
			"key_messages": [],
			"priority": "medium",
			"tags": ["api"],
			"source_topic_ids": [1]
		}],
		"updated_tracks": []
	}`

	gen := &mockGenerator{response: aiResponse}
	pipe := New(database, testConfig(), gen, log.Default())

	created, updated, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, created)
	assert.Equal(t, 0, updated)

	// Verify track was stored
	tracks, err := database.GetAllActiveTracks()
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "API v2 design discussion", tracks[0].Title)
	assert.Equal(t, "medium", tracks[0].Priority)

	// Verify source_refs
	var refs []struct {
		DigestID  int    `json:"digest_id"`
		TopicID   int    `json:"topic_id"`
		ChannelID string `json:"channel_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(tracks[0].SourceRefs), &refs))
	assert.Len(t, refs, 1)
	assert.Equal(t, 1, refs[0].DigestID)
	assert.Equal(t, 1, refs[0].TopicID)
	assert.Equal(t, "C1", refs[0].ChannelID)
}

func TestBuildSourceRefs(t *testing.T) {
	pipe := &Pipeline{}
	lookup := map[int]db.UnlinkedTopic{
		1: {TopicID: 1, DigestID: 10, ChannelID: "C1", PeriodTo: 1000.0},
		2: {TopicID: 2, DigestID: 11, ChannelID: "C2", PeriodTo: 2000.0},
	}

	result := pipe.buildSourceRefs([]int{1, 2}, lookup)

	var refs []struct {
		DigestID  int     `json:"digest_id"`
		TopicID   int     `json:"topic_id"`
		ChannelID string  `json:"channel_id"`
		Timestamp float64 `json:"timestamp"`
	}
	require.NoError(t, json.Unmarshal([]byte(result), &refs))
	assert.Len(t, refs, 2)
}

func TestCollectChannelIDs(t *testing.T) {
	pipe := &Pipeline{}
	lookup := map[int]db.UnlinkedTopic{
		1: {TopicID: 1, ChannelID: "C1"},
		2: {TopicID: 2, ChannelID: "C1"},
		3: {TopicID: 3, ChannelID: "C2"},
	}

	ids := pipe.collectChannelIDs([]int{1, 2, 3}, lookup)
	assert.Contains(t, ids, "C1")
	assert.Contains(t, ids, "C2")
	assert.Len(t, ids, 2) // deduplicated
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
		Title:      "Linked track",
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
