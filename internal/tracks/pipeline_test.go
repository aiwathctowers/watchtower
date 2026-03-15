package tracks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
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

func (m *mockGenerator) Generate(_ context.Context, _, _ string) (string, *digest.Usage, error) {
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}, nil
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

func TestPipelineRunForWindow(t *testing.T) {
	database := testDB(t)

	// Setup workspace with current user
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))

	// Setup users
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))

	// Setup channel
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Insert messages
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002",
		Text: "@alice can you review the PR?",
	}))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000002.000000", UserID: "U002",
		Text: "The deploy looks good",
	}))

	gen := &mockGenerator{
		response: `{
			"items": [
				{
					"text": "Review the PR",
					"context": "Bob asked Alice to review a pull request",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": "medium"
				}
			]
		}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify stored track
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "Review the PR", tracks[0].Text)
	assert.Equal(t, "medium", tracks[0].Priority)
	assert.Equal(t, "inbox", tracks[0].Status)
	assert.Equal(t, "C1", tracks[0].ChannelID)
}

func TestPipelineNoCurrentUser(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))

	gen := &mockGenerator{response: `{"items": []}`}
	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestPipelineEmptyResult(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002", Text: "hello",
	}))

	gen := &mockGenerator{response: `{"items": []}`}
	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestParseResult(t *testing.T) {
	raw := `{"items": [{"text": "do X", "context": "Y asked", "channel_id": "C1", "channel_name": "#test", "source_message_ts": "123.456", "priority": "high", "due_date": "2025-03-15"}]}`
	result, err := parseResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "do X", result.Items[0].Text)
	assert.Equal(t, "high", result.Items[0].Priority)
	assert.Equal(t, "2025-03-15", result.Items[0].DueDate)
}

func TestParseResultMarkdownFences(t *testing.T) {
	raw := "```json\n" + `{"items": [{"text": "test", "context": "ctx", "channel_id": "C1", "channel_name": "#a", "source_message_ts": "1.0", "priority": "low"}]}` + "\n```"
	result, err := parseResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "test", result.Items[0].Text)
}

func TestTrackStatusUpdate(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "test track",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     1000,
		PeriodTo:       2000,
	})
	require.NoError(t, err)

	// Mark as done
	require.NoError(t, database.UpdateTrackStatus(int(id), "done"))

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "done", tracks[0].Status)
	assert.True(t, tracks[0].CompletedAt.Valid)
}

func TestDeleteTracksForWindow(t *testing.T) {
	database := testDB(t)

	// Insert open track
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "old track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Insert done track (should NOT be deleted)
	id2, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "done track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.UpdateTrackStatus(int(id2), "done"))

	deleted, err := database.DeleteTracksForWindow("U001", 1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted) // only the open one

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "done track", tracks[0].Text)
}

func TestAcceptTrack(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "review PR",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Accept: inbox → active
	require.NoError(t, database.AcceptTrack(int(id)))

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "active", tracks[0].Status)

	// Accept again should fail (not in inbox)
	err = database.AcceptTrack(int(id))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in inbox")
}

func TestSnoozeAndReactivate(t *testing.T) {
	database := testDB(t)

	// Create inbox track and snooze it
	id1, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "inbox track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Create active track and snooze it
	id2, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "active track",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.AcceptTrack(int(id2)))

	// Snooze both — set snooze_until to the past so reactivation triggers immediately
	pastTS := float64(1000000000) // well in the past
	require.NoError(t, database.SnoozeTrack(int(id1), pastTS))
	require.NoError(t, database.SnoozeTrack(int(id2), pastTS))

	// Both should be snoozed
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001", Status: "snoozed"})
	require.NoError(t, err)
	assert.Len(t, tracks, 2)

	// Reactivate
	n, err := database.ReactivateSnoozedTracks()
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// Check they returned to their pre-snooze statuses
	tracks, err = database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 2)

	statusByText := map[string]string{}
	for _, track := range tracks {
		statusByText[track.Text] = track.Status
	}
	assert.Equal(t, "inbox", statusByText["inbox track"])
	assert.Equal(t, "active", statusByText["active track"])
}

func TestSnoozeFutureNotReactivated(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "future snooze",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Snooze far in the future
	futureTS := float64(9999999999)
	require.NoError(t, database.SnoozeTrack(int(id), futureTS))

	// Should not reactivate
	n, err := database.ReactivateSnoozedTracks()
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "snoozed", tracks[0].Status)
}

func TestHasUpdatesFlag(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "track updates",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000",
	})
	require.NoError(t, err)

	// Initially no updates
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.False(t, tracks[0].HasUpdates)

	// Set has_updates
	require.NoError(t, database.SetTrackHasUpdates(int(id), true))

	tracks, err = database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.True(t, tracks[0].HasUpdates)

	// Filter by has_updates
	hasUpdates := true
	tracks, err = database.GetTracks(db.TrackFilter{AssigneeUserID: "U001", HasUpdates: &hasUpdates})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)

	noUpdates := false
	tracks, err = database.GetTracks(db.TrackFilter{AssigneeUserID: "U001", HasUpdates: &noUpdates})
	require.NoError(t, err)
	assert.Len(t, tracks, 0)

	// Mark as read
	require.NoError(t, database.MarkTrackUpdateRead(int(id)))
	tracks, err = database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.False(t, tracks[0].HasUpdates)
}

func TestGetTracksForUpdateCheck(t *testing.T) {
	database := testDB(t)

	// Track with source_message_ts — should be returned
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "with ts",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000",
	})
	require.NoError(t, err)

	// Track without source_message_ts — should NOT be returned
	_, err = database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "without ts",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Done track — should NOT be returned
	id3, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "done track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000003.000000",
	})
	require.NoError(t, err)
	require.NoError(t, database.UpdateTrackStatus(int(id3), "done"))

	tracks, err := database.GetTracksForUpdateCheck()
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "with ts", tracks[0].Text)
}

func TestDeleteWindowPreservesActive(t *testing.T) {
	database := testDB(t)

	// Inbox track — will be deleted
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "inbox track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Active track — should NOT be deleted
	id2, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "active track",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.AcceptTrack(int(id2)))

	deleted, err := database.DeleteTracksForWindow("U001", 1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "active track", tracks[0].Text)
	assert.Equal(t, "active", tracks[0].Status)
}

func TestCheckForUpdates(t *testing.T) {
	database := testDB(t)

	// Setup
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Original message that generated the track
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002",
		Text: "@alice review the PR please",
	}))

	// Create track for this message
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "review PR",
		Status: "active", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000", SourceChannelName: "general",
	})
	require.NoError(t, err)

	// Add a thread reply (new update)
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000005.000000", UserID: "U002",
		Text:     "actually this is now urgent, we need it by EOD",
		ThreadTS: nullString("1000000001.000000"),
	}))

	gen := &mockGenerator{
		response: `{"has_update": true, "updated_context": "Bob says this is now urgent, need by EOD", "status_hint": "active"}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify has_updates flag is set
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.True(t, tracks[0].HasUpdates)
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func TestCheckForUpdatesChannelMessage(t *testing.T) {
	database := testDB(t)

	// Setup
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "denis"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "dev-europe", Type: "public"}))

	// Original message that generated the track
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "need to whitelist IP 136.226.198.1 for EY consultants",
	}))

	// Create track
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "whitelist IP for EY",
		Status: "active", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
		SourceMessageTS: "1000000001.000000", SourceChannelName: "dev-europe",
	})
	require.NoError(t, err)

	// Denis posts a CHANNEL message (not a thread reply!) confirming the task is done
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000010.000000", UserID: "U002",
		Text: "opened access for ip 136.226.198.1/32 on prod EU",
		// No ThreadTS — this is a standalone channel message
	}))

	gen := &mockGenerator{
		response: `{"has_update": true, "updated_context": "Denis opened access for the IP on prod EU", "status_hint": "done"}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Verify track is marked done
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "done", tracks[0].Status)
	assert.True(t, tracks[0].HasUpdates)
	assert.True(t, tracks[0].CompletedAt.Valid)
}

func TestDayWindow(t *testing.T) {
	// Two times on the same day should produce the same window.
	t1, _ := time.Parse(time.RFC3339, "2026-03-12T13:00:00+03:00")
	t2, _ := time.Parse(time.RFC3339, "2026-03-12T13:15:00+03:00")

	from1, to1 := DayWindow(t1)
	from2, to2 := DayWindow(t2)

	assert.Equal(t, from1, from2, "same day should have same from")
	assert.Equal(t, to1, to2, "same day should have same to")

	// Window should span from midnight to next midnight.
	// Not asserting exactly 86400s because DST transitions can be 23h or 25h.
	assert.True(t, to1-from1 >= 82800 && to1-from1 <= 90000, "window should be ~24h")

	// Different day should produce different window.
	t3, _ := time.Parse(time.RFC3339, "2026-03-13T01:00:00+03:00")
	from3, _ := DayWindow(t3)
	assert.NotEqual(t, from1, from3, "different days should have different from")
}

func TestExistingIDParsing(t *testing.T) {
	raw := `{"items": [
		{"existing_id": 42, "text": "updated item", "context": "new ctx", "source_message_ts": "1.0", "priority": "high"},
		{"existing_id": null, "text": "new item", "context": "ctx", "source_message_ts": "2.0", "priority": "medium"}
	]}`
	result, err := parseResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	// First item: existing_id = 42
	require.NotNil(t, result.Items[0].ExistingID)
	assert.Equal(t, 42, *result.Items[0].ExistingID)

	// Second item: existing_id = null (new item)
	assert.Nil(t, result.Items[1].ExistingID)
}

func TestUpdateTrackFromExtraction(t *testing.T) {
	database := testDB(t)

	// Create original track.
	id, err := database.UpsertTrack(db.Track{
		ChannelID:       "C1",
		AssigneeUserID:  "U001",
		Text:            "original task",
		Context:         "old context",
		Status:          "active",
		Priority:        "low",
		PeriodFrom:      1000,
		PeriodTo:        2000,
		DecisionSummary: "old decision",
	})
	require.NoError(t, err)

	// Update with new extraction data.
	changed, err := database.UpdateTrackFromExtraction(int(id), db.Track{
		Context:         "updated context with new info",
		Priority:        "high",
		DecisionSummary: "evolved decision",
		Tags:            `["infra"]`,
	})
	require.NoError(t, err)
	assert.True(t, changed)

	// Verify changes.
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "updated context with new info", tracks[0].Context)
	assert.Equal(t, "high", tracks[0].Priority)
	assert.Equal(t, "active", tracks[0].Status) // preserved
	assert.Equal(t, "evolved decision", tracks[0].DecisionSummary)
	assert.Equal(t, `["infra"]`, tracks[0].Tags)

	// Verify history.
	history, err := database.GetTrackHistory(int(id))
	require.NoError(t, err)
	require.True(t, len(history) >= 3) // re_extracted, priority_changed, decision_evolved

	events := make(map[string]bool)
	for _, h := range history {
		events[h.Event] = true
	}
	assert.True(t, events["re_extracted"])
	assert.True(t, events["priority_changed"])
	assert.True(t, events["decision_evolved"])
}

func TestUpdateNoChange(t *testing.T) {
	database := testDB(t)

	id, err := database.UpsertTrack(db.Track{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "task",
		Context:        "context",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     1000,
		PeriodTo:       2000,
	})
	require.NoError(t, err)

	// Update with same values → no change.
	changed, err := database.UpdateTrackFromExtraction(int(id), db.Track{
		Context:  "context",
		Priority: "medium",
	})
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestPipelineExistingIDUpdate(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Create existing track.
	existingID, err := database.UpsertTrack(db.Track{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "prepare migration plan",
		Context:        "old context about migration",
		Status:         "active",
		Priority:       "medium",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	// Insert message.
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000050.000000", UserID: "U002",
		Text: "migration vendor confirmed Q2 timeline",
	}))

	// AI returns existing_id pointing to existing track.
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"text": "prepare migration plan",
					"context": "vendor confirmed Q2 timeline, plan needs update",
					"source_message_ts": "1000000050.000000",
					"priority": "high"
				}
			]
		}`, existingID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Should have updated the existing track, not created a new one.
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "vendor confirmed Q2 timeline, plan needs update", tracks[0].Context)
	assert.Equal(t, "high", tracks[0].Priority)
	assert.Equal(t, "active", tracks[0].Status) // preserved
}

func TestDeleteTracksForWindowRangeBased(t *testing.T) {
	database := testDB(t)

	// Track from earlier run with slightly different window (old sliding behavior).
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "old sliding track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Delete with a window that encompasses the old track's period.
	deleted, err := database.DeleteTracksForWindow("U001", 900, 2100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Track outside the window should NOT be deleted.
	_, err = database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "outside window",
		Status: "inbox", Priority: "medium", PeriodFrom: 3000, PeriodTo: 4000,
	})
	require.NoError(t, err)

	deleted, err = database.DeleteTracksForWindow("U001", 900, 2100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}

func TestGetExistingTracksForChannel(t *testing.T) {
	database := testDB(t)

	// Inbox track — should be returned.
	_, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "inbox track",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	// Active track — should be returned.
	id2, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "active track",
		Status: "inbox", Priority: "high", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.AcceptTrack(int(id2)))

	// Done track — should NOT be returned.
	id3, err := database.UpsertTrack(db.Track{
		ChannelID: "C1", AssigneeUserID: "U001", Text: "done track",
		Status: "inbox", Priority: "low", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)
	require.NoError(t, database.UpdateTrackStatus(int(id3), "done"))

	// Different channel — should NOT be returned.
	_, err = database.UpsertTrack(db.Track{
		ChannelID: "C2", AssigneeUserID: "U001", Text: "other channel",
		Status: "inbox", Priority: "medium", PeriodFrom: 1000, PeriodTo: 2000,
	})
	require.NoError(t, err)

	tracks, err := database.GetExistingTracksForChannel("C1", "U001")
	require.NoError(t, err)
	assert.Len(t, tracks, 2)

	texts := map[string]bool{}
	for _, track := range tracks {
		texts[track.Text] = true
	}
	assert.True(t, texts["inbox track"])
	assert.True(t, texts["active track"])
}

func TestPipelineExistingIDStatusHintDone(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "dev-europe", Type: "public"}))

	// Create existing active track (e.g., "whitelist IP for EY consultants").
	existingID, err := database.UpsertTrack(db.Track{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "whitelist IP 136.226.198.1 for EY consultants",
		Context:        "EY consultants need access before March 19 meeting",
		Status:         "active",
		Priority:       "high",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	// New message: someone confirms the task is done.
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000200.000000", UserID: "U002",
		Text: "opened access for ip 136.226.198.1/32 on prod EU",
	}))

	// AI returns existing_id with status_hint "done".
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"status_hint": "done",
					"text": "whitelist IP 136.226.198.1 for EY consultants",
					"context": "Denis opened access for IP 136.226.198.1/32 on prod EU. Task completed.",
					"source_message_ts": "1000000200.000000",
					"priority": "high"
				}
			]
		}`, existingID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000150, 1000000300)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// The existing track should be marked as done with has_updates flag.
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "done", tracks[0].Status)
	assert.True(t, tracks[0].HasUpdates)
	assert.True(t, tracks[0].CompletedAt.Valid)
	assert.Contains(t, tracks[0].Context, "Denis opened access")
}

func TestPipelineCrossChannelCompletion(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "denis", DisplayName: "Denis"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "dev-europe", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C2", Name: "devops", Type: "public"}))

	// Track created in dev-europe.
	existingID, err := database.UpsertTrack(db.Track{
		ChannelID:      "C1",
		AssigneeUserID: "U001",
		Text:           "whitelist IP 136.226.198.1 for EY consultants",
		Context:        "EY needs access before March 19 meeting",
		Status:         "active",
		Priority:       "high",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	// Completion message appears in devops channel (different channel!).
	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C2", TS: "1000000200.000000", UserID: "U002",
		Text: "opened access for ip 136.226.198.1/32 on prod EU",
	}))

	// AI recognizes cross-channel completion.
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"status_hint": "done",
					"text": "whitelist IP 136.226.198.1 for EY consultants",
					"context": "Denis confirmed access opened in devops channel",
					"source_message_ts": "1000000200.000000",
					"priority": "high"
				}
			]
		}`, existingID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000150, 1000000300)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Track from C1 should be marked done via message from C2.
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "done", tracks[0].Status)
	assert.Equal(t, "C1", tracks[0].ChannelID) // still in original channel
	assert.True(t, tracks[0].CompletedAt.Valid)
}

// Verify JSON structure matches what the AI produces
func TestAIResultStructure(t *testing.T) {
	sample := aiResult{
		Items: []aiItem{
			{
				Text:            "Review PR #42",
				Context:         "Bob needs Alice to approve",
				ChannelID:       "C123",
				ChannelName:     "#dev",
				SourceMsgTS:     "1234567890.123456",
				Priority:        "high",
				DueDate:         "2025-01-15",
				Requester:       &aiRequester{Name: "@bob", UserID: "U1"},
				Category:        "code_review",
				Blocking:        "Release v2.0 is blocked",
				Tags:            json.RawMessage(`["backend","pr"]`),
				DecisionSummary: "Team agreed this needs review before merge",
				DecisionOptions: json.RawMessage(`[]`),
				Participants:    json.RawMessage(`[{"name":"@bob","user_id":"U1","stance":"needs review"}]`),
				SourceRefs:      json.RawMessage(`[{"ts":"1234567890.123456","author":"@bob","text":"Please review PR #42"}]`),
				SubItems:        json.RawMessage(`[{"text":"Check code quality","status":"open"},{"text":"Run tests","status":"open"}]`),
			},
		},
	}
	data, err := json.Marshal(sample)
	require.NoError(t, err)

	var parsed aiResult
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, sample, parsed)
}

// capturingGenerator captures the prompt passed to Generate for inspection.
// Thread-safe: uses a mutex to protect capturedPrompt from concurrent writes.
type capturingGenerator struct {
	mu             sync.Mutex
	response       string
	capturedPrompt string
}

func (m *capturingGenerator) Generate(_ context.Context, _, prompt string) (string, *digest.Usage, error) {
	m.mu.Lock()
	m.capturedPrompt = prompt
	m.mu.Unlock()
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}, nil
}

func (m *capturingGenerator) getCapturedPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capturedPrompt
}

func TestProfileContextInjectedIntoExtractPrompt(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000050.000000", UserID: "U001",
		Text: "test message",
	}))

	// Create profile with custom context.
	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID:         "U001",
		Role:                "Engineering Manager",
		CustomPromptContext: "You are helping an EM responsible for Platform team. Reports: alice, bob.",
		Reports:             `["U002","U003"]`,
		StarredChannels:     `["C1"]`,
		StarredPeople:       `["U010"]`,
	}))

	gen := &capturingGenerator{
		response: `{"items": []}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	_, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)

	// Profile context should appear in the prompt.
	captured := gen.getCapturedPrompt()
	assert.Contains(t, captured, "USER PROFILE CONTEXT")
	assert.Contains(t, captured, "Platform team")
	assert.Contains(t, captured, "OWNERSHIP RULES")
	assert.Contains(t, captured, `["U002","U003"]`)
	assert.Contains(t, captured, "STARRED CHANNELS")
	assert.Contains(t, captured, "STARRED PEOPLE")
	assert.Contains(t, captured, "CATEGORY PRIORITY")
	assert.Contains(t, captured, "decision_needed, follow_up") // EM role
}

func TestNoProfileContextWhenProfileEmpty(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000050.000000", UserID: "U001",
		Text: "test message",
	}))

	gen := &capturingGenerator{
		response: `{"items": []}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	_, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)

	// No profile context should appear in the prompt.
	captured2 := gen.getCapturedPrompt()
	assert.NotContains(t, captured2, "USER PROFILE CONTEXT")
	assert.NotContains(t, captured2, "OWNERSHIP RULES")
	assert.NotContains(t, captured2, "CATEGORY PRIORITY")
}

func TestCategoryWeightingByRole(t *testing.T) {
	p := &Pipeline{}

	tests := []struct {
		role     string
		expected string
	}{
		{"Engineering Manager", "decision_needed, follow_up"},
		{"Tech Lead", "decision_needed, code_review"},
		{"Product Manager", "decision_needed, approval"},
		{"Software Engineer", "code_review, bug_fix, task"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			p.profile = &db.UserProfile{
				Role:                tt.role,
				CustomPromptContext: "test context",
			}
			ctx := p.formatProfileContext()
			assert.Contains(t, ctx, tt.expected)
		})
	}
}

func TestFormatRoleRulesManager(t *testing.T) {
	p := &Pipeline{}

	managerRoles := []string{"top_management", "direction_owner", "middle_management", "Engineering Manager", "CTO", "VP Engineering", "Head of Backend"}
	for _, role := range managerRoles {
		t.Run(role, func(t *testing.T) {
			p.profile = &db.UserProfile{Role: role}
			rules := p.formatRoleRules()
			assert.Contains(t, rules, "ROLE-SPECIFIC RULES")
			assert.Contains(t, rules, "DECISIONS in your area")
			assert.Contains(t, rules, "DELEGATED TASKS")
			assert.Contains(t, rules, "BLOCKERS & ESCALATIONS")
			assert.Contains(t, rules, "STRATEGIC DISCUSSIONS")
			assert.Contains(t, rules, "better to surface too much")
		})
	}
}

func TestFormatRoleRulesLead(t *testing.T) {
	p := &Pipeline{}

	leadRoles := []string{"Tech Lead", "Staff Engineer", "Principal Engineer"}
	for _, role := range leadRoles {
		t.Run(role, func(t *testing.T) {
			p.profile = &db.UserProfile{Role: role}
			rules := p.formatRoleRules()
			assert.Contains(t, rules, "ROLE-SPECIFIC RULES")
			assert.Contains(t, rules, "TECHNICAL DECISIONS")
			assert.Contains(t, rules, "CODE QUALITY SIGNALS")
			assert.NotContains(t, rules, "DELEGATED TASKS")
		})
	}
}

func TestFormatRoleRulesIC(t *testing.T) {
	p := &Pipeline{}

	icRoles := []string{"ic", "Software Engineer", "senior_ic", ""}
	for _, role := range icRoles {
		t.Run("role="+role, func(t *testing.T) {
			p.profile = &db.UserProfile{Role: role}
			rules := p.formatRoleRules()
			assert.Empty(t, rules, "IC roles should not get role-specific rules")
		})
	}
}

func TestFormatRoleRulesNoProfile(t *testing.T) {
	p := &Pipeline{}
	rules := p.formatRoleRules()
	assert.Empty(t, rules)
}

func TestRoleRulesInExtractPrompt(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000050.000000", UserID: "U001",
		Text: "test message",
	}))

	// Create profile with manager role.
	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID:         "U001",
		Role:                "middle_management",
		CustomPromptContext: "EM responsible for Platform team.",
	}))

	gen := &capturingGenerator{
		response: `{"items": []}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	_, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)

	captured := gen.getCapturedPrompt()
	assert.Contains(t, captured, "ROLE-SPECIFIC RULES")
	assert.Contains(t, captured, "DECISIONS in your area")
	assert.Contains(t, captured, "DELEGATED TASKS")
}
