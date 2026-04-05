package briefing

import (
	"context"
	"io"
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
			Enabled:  true,
			Language: "English",
		},
		Briefing: config.BriefingConfig{
			Enabled: true,
			Hour:    8,
		},
	}
}

func TestPipelineRunForDate(t *testing.T) {
	database := testDB(t)

	// Setup workspace and current user.
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))

	// Setup channel digest so briefing has data to work with.
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location())
	dayEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	_, err := database.UpsertDigest(db.Digest{
		ChannelID:    "C1",
		Type:         "channel",
		PeriodFrom:   float64(dayStart.Unix()),
		PeriodTo:     float64(dayEnd.Unix()),
		Summary:      "Discussion about new feature release",
		Topics:       `["release","feature"]`,
		Decisions:    `[{"text":"Ship v2.0 on Friday","by":"@bob","message_ts":"123.456","importance":"high"}]`,
		ActionItems:  `[]`,
		MessageCount: 15,
	})
	require.NoError(t, err)

	gen := &mockGenerator{
		response: `{
			"attention": [
				{"text": "Review deploy checklist before Friday", "source_type": "digest", "source_id": "1", "priority": "high", "reason": "Release deadline approaching"}
			],
			"your_day": [
				{"text": "Check PR reviews", "track_id": 0, "priority": "medium", "status": "active", "ownership": "mine"}
			],
			"what_happened": [
				{"text": "v2.0 release scheduled for Friday", "digest_id": 1, "channel_name": "#general", "item_type": "decision", "importance": "high"}
			],
			"team_pulse": [
				{"text": "Bob driving release actively", "user_id": "U002", "signal_type": "highlight", "detail": "High activity in release channel"}
			],
			"coaching": [
				{"text": "Follow up with Bob on deploy checklist", "related_user_id": "U002", "category": "process"}
			]
		}`,
	}

	cfg := testConfig()
	logger := log.New(io.Discard, "", 0)
	pipe := New(database, cfg, gen, logger)

	today := time.Now().Format("2006-01-02")
	id, err := pipe.RunForDate(context.Background(), today)
	require.NoError(t, err)
	assert.Greater(t, id, 0)

	// Verify stored briefing.
	b, err := database.GetBriefing("U001", today)
	require.NoError(t, err)
	require.NotNil(t, b)

	assert.Equal(t, "U001", b.UserID)
	assert.Equal(t, today, b.Date)
	assert.Contains(t, b.Attention, "deploy checklist")
	assert.Contains(t, b.YourDay, "PR reviews")
	assert.Contains(t, b.WhatHappened, "v2.0")
	assert.Contains(t, b.TeamPulse, "Bob")
	assert.Contains(t, b.Coaching, "deploy checklist")
	assert.Equal(t, 100, b.InputTokens)
	assert.Equal(t, 50, b.OutputTokens)

	// Running again should skip (already exists for same date).
	id2, err := pipe.RunForDate(context.Background(), today)
	require.NoError(t, err)
	assert.Equal(t, id, id2)
}

func TestPipelineRunDisabled(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()
	cfg.Briefing.Enabled = false

	pipe := New(database, cfg, &mockGenerator{}, log.New(io.Discard, "", 0))
	id, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, id)
}

func TestPipelineRunNoUser(t *testing.T) {
	database := testDB(t)
	cfg := testConfig()

	pipe := New(database, cfg, &mockGenerator{}, log.New(io.Discard, "", 0))
	id, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, id)
}

func TestPipelineRunNoData(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))

	cfg := testConfig()
	pipe := New(database, cfg, &mockGenerator{}, log.New(io.Discard, "", 0))
	id, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, id) // No digests or tracks — nothing to generate.
}

func TestParseBriefingResult(t *testing.T) {
	input := `{
		"attention": [{"text": "att", "source_type": "track", "source_id": "1", "priority": "high", "reason": "urgent"}],
		"your_day": [{"text": "task", "track_id": 1, "priority": "medium", "status": "active", "ownership": "mine"}],
		"what_happened": [{"text": "decision", "digest_id": 1, "channel_name": "#ch", "item_type": "decision", "importance": "high"}],
		"team_pulse": [{"text": "signal", "user_id": "U1", "signal_type": "highlight", "detail": "good"}],
		"coaching": [{"text": "tip", "related_user_id": "U2", "category": "communication"}]
	}`

	result, err := parseBriefingResult(input)
	require.NoError(t, err)
	assert.Len(t, result.Attention, 1)
	assert.Len(t, result.YourDay, 1)
	assert.Len(t, result.WhatHappened, 1)
	assert.Len(t, result.TeamPulse, 1)
	assert.Len(t, result.Coaching, 1)
	assert.Equal(t, "high", result.Attention[0].Priority)
	assert.Equal(t, "track", result.Attention[0].SourceType)
}

func TestParseBriefingResultWithMarkdownFences(t *testing.T) {
	input := "```json\n{\"attention\": [], \"your_day\": [], \"what_happened\": [], \"team_pulse\": [], \"coaching\": []}\n```"

	result, err := parseBriefingResult(input)
	require.NoError(t, err)
	assert.Empty(t, result.Attention)
}

func TestDBBriefingCRUD(t *testing.T) {
	database := testDB(t)

	// Upsert.
	id, err := database.UpsertBriefing(db.Briefing{
		WorkspaceID:  "T1",
		UserID:       "U001",
		Date:         "2026-03-22",
		Role:         "engineer",
		Attention:    `[{"text":"test"}]`,
		YourDay:      "[]",
		WhatHappened: "[]",
		TeamPulse:    "[]",
		Coaching:     "[]",
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Get by user+date.
	b, err := database.GetBriefing("U001", "2026-03-22")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "engineer", b.Role)
	assert.Contains(t, b.Attention, "test")

	// Get by ID.
	b2, err := database.GetBriefingByID(int(id))
	require.NoError(t, err)
	require.NotNil(t, b2)
	assert.Equal(t, b.ID, b2.ID)

	// List.
	list, err := database.GetRecentBriefings("U001", 10)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// Mark read.
	err = database.MarkBriefingRead(int(id))
	require.NoError(t, err)
	b, _ = database.GetBriefing("U001", "2026-03-22")
	assert.True(t, b.ReadAt.Valid)

	// Get for non-existent date.
	b3, err := database.GetBriefing("U001", "2099-01-01")
	require.NoError(t, err)
	assert.Nil(t, b3)
}

// --- Tests for Task #5: briefing adaptation ---

func TestGatherTracks_NoTracks(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, log.New(io.Discard, "", 0))

	ctx, hasReal := pipe.gatherTracks()
	assert.False(t, hasReal)
	assert.Contains(t, ctx, "No active tracks")
}

func TestGatherTracks_WithTracks(t *testing.T) {
	database := testDB(t)
	_, err := database.UpsertTrack(db.Track{
		Text:         "API redesign",
		Context:      "Under review",
		Priority:     "high",
		Participants: `[{"user_id":"U1","name":"alice","role":"driver"}]`,
		ChannelIDs:   `["C1"]`,
	})
	require.NoError(t, err)

	pipe := New(database, testConfig(), &mockGenerator{}, log.New(io.Discard, "", 0))

	ctx, hasReal := pipe.gatherTracks()
	assert.True(t, hasReal)
	assert.Contains(t, ctx, "API redesign")
	assert.Contains(t, ctx, "high")
}

func TestAttentionItem(t *testing.T) {
	input := `{
		"attention": [{"text": "track needs attention", "source_type": "track", "source_id": "1", "priority": "high", "reason": "stale"}],
		"your_day": [],
		"what_happened": [],
		"team_pulse": [],
		"coaching": []
	}`
	result, err := parseBriefingResult(input)
	require.NoError(t, err)
	require.Len(t, result.Attention, 1)
	assert.Equal(t, "track", result.Attention[0].SourceType)
	assert.Equal(t, "high", result.Attention[0].Priority)
}

func TestGatherTasks_NoTasks(t *testing.T) {
	database := testDB(t)
	pipe := New(database, testConfig(), &mockGenerator{}, log.New(io.Discard, "", 0))

	ctx, hasReal := pipe.gatherTasks()
	assert.False(t, hasReal)
	assert.Contains(t, ctx, "No active tasks")
}

func TestGatherTasks_WithTasks(t *testing.T) {
	database := testDB(t)
	_, err := database.CreateTask(db.Task{
		Text:       "Review PR #42",
		Intent:     "Check API changes",
		Status:     "todo",
		Priority:   "high",
		Ownership:  "mine",
		DueDate:    "2020-01-01", // overdue
		SourceType: "manual",
	})
	require.NoError(t, err)

	_, err = database.CreateTask(db.Task{
		Text:       "Deploy v2",
		Status:     "in_progress",
		Priority:   "medium",
		Ownership:  "mine",
		Blocking:   "release pipeline",
		SourceType: "manual",
	})
	require.NoError(t, err)

	// Done task should not appear
	_, err = database.CreateTask(db.Task{
		Text:       "Old task",
		Status:     "done",
		Priority:   "low",
		Ownership:  "mine",
		SourceType: "manual",
	})
	require.NoError(t, err)

	pipe := New(database, testConfig(), &mockGenerator{}, log.New(io.Discard, "", 0))

	ctx, hasReal := pipe.gatherTasks()
	assert.True(t, hasReal)
	assert.Contains(t, ctx, "Review PR #42")
	assert.Contains(t, ctx, "OVERDUE")
	assert.Contains(t, ctx, "Check API changes")
	assert.Contains(t, ctx, "Deploy v2")
	assert.Contains(t, ctx, "Blocking: release pipeline")
	assert.NotContains(t, ctx, "Old task")
}

func TestBriefingHasDataWithTasksOnly(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))

	// Create a task but no digests/tracks
	_, err := database.CreateTask(db.Task{
		Text:       "Urgent task",
		Status:     "todo",
		Priority:   "high",
		Ownership:  "mine",
		SourceType: "manual",
	})
	require.NoError(t, err)

	gen := &mockGenerator{
		response: `{
			"attention": [{"text": "Do urgent task", "source_type": "task", "source_id": "1", "priority": "high", "reason": "overdue"}],
			"your_day": [{"text": "Urgent task", "task_id": 1, "priority": "high", "status": "todo", "ownership": "mine"}],
			"what_happened": [],
			"team_pulse": [],
			"coaching": []
		}`,
	}

	cfg := testConfig()
	pipe := New(database, cfg, gen, log.New(io.Discard, "", 0))

	today := time.Now().Format("2006-01-02")
	id, err := pipe.RunForDate(context.Background(), today)
	require.NoError(t, err)
	assert.Greater(t, id, 0) // Should generate — tasks count as data
}

func TestAttentionItemSuggestTask(t *testing.T) {
	input := `{
		"attention": [{"text": "track needs task", "source_type": "track", "source_id": "1", "priority": "high", "reason": "no task", "suggest_task": true}],
		"your_day": [],
		"what_happened": [],
		"team_pulse": [],
		"coaching": []
	}`
	result, err := parseBriefingResult(input)
	require.NoError(t, err)
	require.Len(t, result.Attention, 1)
	assert.True(t, result.Attention[0].SuggestTask)
}

func TestYourDayItemTaskID(t *testing.T) {
	input := `{
		"attention": [],
		"your_day": [{"text": "Do the task", "task_id": 42, "priority": "high", "status": "todo", "ownership": "mine"}],
		"what_happened": [],
		"team_pulse": [],
		"coaching": []
	}`
	result, err := parseBriefingResult(input)
	require.NoError(t, err)
	require.Len(t, result.YourDay, 1)
	assert.Equal(t, 42, result.YourDay[0].TaskID)
}
