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

	// Get the track ID for the batch response.
	allTracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, allTracks, 1)
	trackID := allTracks[0].ID

	gen := &mockGenerator{
		response: fmt.Sprintf(`{"results": [{"track_id": %d, "has_update": true, "updated_context": "Bob says this is now urgent, need by EOD", "status_hint": "active", "ball_on": ""}]}`, trackID),
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

	// Get the track ID for the batch response.
	allTracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, allTracks, 1)
	trackID := allTracks[0].ID

	gen := &mockGenerator{
		response: fmt.Sprintf(`{"results": [{"track_id": %d, "has_update": true, "updated_context": "Denis opened access for the IP on prod EU", "status_hint": "done", "ball_on": ""}]}`, trackID),
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

func (m *capturingGenerator) Generate(_ context.Context, _, prompt, _ string) (string, *digest.Usage, string, error) {
	m.mu.Lock()
	m.capturedPrompt = prompt
	m.mu.Unlock()
	return m.response, &digest.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}, "mock-session", nil
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

// --- Additional tests for coverage ---

// errGenerator returns an error from Generate.
type errGenerator struct {
	err error
}

func (m *errGenerator) Generate(_ context.Context, _, _, _ string) (string, *digest.Usage, string, error) {
	return "", nil, "", m.err
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

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 100))
	assert.Equal(t, "hel...", truncate("hello world", 3))
	assert.Equal(t, "", truncate("", 10))
	// Unicode
	assert.Equal(t, "при...", truncate("привет мир", 3))
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

func TestJsonOrEmpty(t *testing.T) {
	assert.Equal(t, "[]", jsonOrEmpty(nil))
	assert.Equal(t, "[]", jsonOrEmpty(json.RawMessage("null")))
	assert.Equal(t, "[]", jsonOrEmpty(json.RawMessage("")))
	assert.Equal(t, "[]", jsonOrEmpty(json.RawMessage("not valid json")))
	assert.Equal(t, `["a","b"]`, jsonOrEmpty(json.RawMessage(`["a","b"]`)))
	assert.Equal(t, `{"x":1}`, jsonOrEmpty(json.RawMessage(`{"x":1}`)))
}

func TestParseResultInvalid(t *testing.T) {
	_, err := parseResult("not json at all {{{ bad")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing tracks JSON")
}

func TestParseBatchUpdateResult(t *testing.T) {
	raw := `{"results": [{"track_id": 1, "has_update": true, "updated_context": "done", "status_hint": "done", "ball_on": "U2"}]}`
	result, err := parseBatchUpdateResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	assert.Equal(t, 1, result.Results[0].TrackID)
	assert.True(t, result.Results[0].HasUpdate)
	assert.Equal(t, "done", result.Results[0].StatusHint)
	assert.Equal(t, "U2", result.Results[0].BallOn)
}

func TestParseBatchUpdateResultInvalid(t *testing.T) {
	_, err := parseBatchUpdateResult("bad json {{{")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing batch update check JSON")
}

func TestAccumulatedUsage(t *testing.T) {
	pipe := &Pipeline{}
	in, out, cost := pipe.AccumulatedUsage()
	assert.Equal(t, 0, in)
	assert.Equal(t, 0, out)
	assert.Equal(t, 0.0, cost)

	pipe.totalInputTokens.Add(1000)
	pipe.totalOutputTokens.Add(500)
	pipe.totalCostMicro.Add(10000) // 0.01 USD
	in, out, cost = pipe.AccumulatedUsage()
	assert.Equal(t, 1000, in)
	assert.Equal(t, 500, out)
	assert.InDelta(t, 0.01, cost, 0.0001)
}

func TestReactivateSnoozed(t *testing.T) {
	database := testDB(t)

	pipe := New(database, testConfig(), &mockGenerator{response: `{"items":[]}`}, log.Default())
	n, err := pipe.ReactivateSnoozed(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestSetPromptStore(t *testing.T) {
	pipe := &Pipeline{}
	assert.Nil(t, pipe.promptStore)
	// SetPromptStore just assigns, verify it doesn't panic
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

func TestChannelNameUserNameCacheMiss(t *testing.T) {
	pipe := &Pipeline{}
	pipe.channelNames = map[string]string{"C1": "general"}
	pipe.userNames = map[string]string{"U1": "alice"}

	// Hit
	assert.Equal(t, "general", pipe.channelName("C1"))
	assert.Equal(t, "alice", pipe.userName("U1"))

	// Miss — returns ID
	assert.Equal(t, "C999", pipe.channelName("C999"))
	assert.Equal(t, "U999", pipe.userName("U999"))
}

func TestRunDigestDisabled(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{Digest: config.DigestConfig{Enabled: false}}
	pipe := New(database, cfg, &mockGenerator{response: `{"items":[]}`}, log.Default())

	n, err := pipe.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestCheckForUpdatesDigestDisabled(t *testing.T) {
	database := testDB(t)
	cfg := &config.Config{Digest: config.DigestConfig{Enabled: false}}
	pipe := New(database, cfg, &mockGenerator{response: `{"items":[]}`}, log.Default())

	n, err := pipe.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestCheckForUpdatesNoTracks(t *testing.T) {
	database := testDB(t)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))

	pipe := New(database, testConfig(), &mockGenerator{response: `{"items":[]}`}, log.Default())
	n, err := pipe.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestFormatMessages(t *testing.T) {
	pipe := &Pipeline{}
	pipe.channelNames = map[string]string{}
	pipe.userNames = map[string]string{"U1": "alice", "U2": "bob"}

	msgs := []db.Message{
		{TS: "1000000001.000000", TSUnix: 1000000001, UserID: "U1", Text: "hello world"},
		{TS: "1000000002.000000", TSUnix: 1000000002, UserID: "U2", Text: "reply here", ThreadTS: nullString("1000000001.000000")},
		{TS: "1000000003.000000", TSUnix: 1000000003, UserID: "U1", Text: "", IsDeleted: false}, // empty text, skipped
		{TS: "1000000004.000000", TSUnix: 1000000004, UserID: "U1", Text: "deleted", IsDeleted: true}, // deleted, skipped
	}

	formatted := pipe.formatMessages(msgs)
	assert.Contains(t, formatted, "@alice")
	assert.Contains(t, formatted, "hello world")
	assert.Contains(t, formatted, "[thread reply]")
	assert.Contains(t, formatted, "@bob")
	assert.NotContains(t, formatted, "deleted")
}

func TestPipelineRunForWindowWithAllFields(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U002",
		Text: "@alice deploy to staging",
	}))

	gen := &mockGenerator{
		response: `{
			"items": [
				{
					"text": "Deploy to staging",
					"context": "Bob asked Alice to deploy",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": "high",
					"due_date": "2026-03-20",
					"requester": {"name": "@bob", "user_id": "U002"},
					"category": "task",
					"blocking": "Release blocked",
					"tags": ["deploy", "staging"],
					"decision_summary": "Agreed to deploy today",
					"decision_options": [{"option": "deploy now"}],
					"participants": [{"name": "@bob", "user_id": "U002", "stance": "requestor"}],
					"source_refs": [{"ts": "1000000001.000000", "author": "@bob", "text": "deploy please"}],
					"sub_items": [{"text": "run tests", "status": "open"}],
					"ownership": "mine",
					"ball_on": "U001",
					"owner_user_id": "U001"
				}
			]
		}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)

	track := tracks[0]
	assert.Equal(t, "Deploy to staging", track.Text)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "task", track.Category)
	assert.Equal(t, "Release blocked", track.Blocking)
	assert.Equal(t, "@bob", track.RequesterName)
	assert.Equal(t, "U002", track.RequesterUserID)
	assert.Equal(t, "Agreed to deploy today", track.DecisionSummary)
	assert.Equal(t, "mine", track.Ownership)
	assert.Equal(t, "U001", track.BallOn)
	assert.True(t, track.DueDate > 0)
	assert.Contains(t, track.Tags, "deploy")
	assert.Contains(t, track.SubItems, "run tests")
}

func TestPipelinePriorityValidation(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "test",
	}))

	// Invalid priority and empty priority should both default to "medium"
	gen := &mockGenerator{
		response: `{
			"items": [
				{
					"text": "invalid priority",
					"context": "ctx",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": "URGENT_CRITICAL"
				},
				{
					"text": "empty priority",
					"context": "ctx2",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": ""
				}
			]
		}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 2)
	for _, track := range tracks {
		assert.Equal(t, "medium", track.Priority)
	}
}

func TestPipelineCategoryValidation(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "test",
	}))

	gen := &mockGenerator{
		response: `{
			"items": [
				{
					"text": "invalid category",
					"context": "ctx",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": "low",
					"category": "totally_invalid_category"
				}
			]
		}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "task", tracks[0].Category) // defaults to "task"
}

func TestPipelineOwnershipValidation(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "test",
	}))

	gen := &mockGenerator{
		response: `{
			"items": [
				{
					"text": "invalid ownership",
					"context": "ctx",
					"channel_id": "C1",
					"channel_name": "#general",
					"source_message_ts": "1000000001.000000",
					"priority": "medium",
					"ownership": "invalid_ownership_value"
				}
			]
		}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	require.Len(t, tracks, 1)
	assert.Equal(t, "mine", tracks[0].Ownership) // defaults to "mine"
}

func TestProcessChannelAIError(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "test message",
	}))

	gen := &errGenerator{err: fmt.Errorf("AI service unavailable")}
	pipe := New(database, testConfig(), gen, log.Default())

	// RunForWindow should propagate the error from processChannel
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	// The error is logged but RunForWindow doesn't fail — it logs the error per channel
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestPipelineContextCancelled(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "test",
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	gen := &mockGenerator{response: `{"items": []}`}
	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(ctx, "U001", 1000000000, 1000000100)
	require.NoError(t, err) // doesn't return error, just skips
	assert.Equal(t, 0, n)
}

func TestFormatExistingItems(t *testing.T) {
	database := testDB(t)

	pipe := New(database, testConfig(), nil, log.Default())
	pipe.channelNames = map[string]string{"C1": "general"}
	pipe.userNames = map[string]string{}

	// No tracks → empty string
	assert.Equal(t, "", pipe.formatExistingItems("C1", "U001"))

	// Add tracks with various fields
	_, err := database.UpsertTrack(db.Track{
		ChannelID:       "C1",
		AssigneeUserID:  "U001",
		Text:            "track with fields",
		Context:         "some context here",
		Status:          "inbox",
		Priority:        "medium",
		PeriodFrom:      1000,
		PeriodTo:        2000,
		DecisionSummary: "decision made",
		Tags:            `["backend"]`,
		RelatedDigestIDs: "[1,2]",
	})
	require.NoError(t, err)

	result := pipe.formatExistingItems("C1", "U001")
	assert.Contains(t, result, "EXISTING TRACKS")
	assert.Contains(t, result, "track with fields")
	assert.Contains(t, result, "decision made")
	assert.Contains(t, result, `["backend"]`)
	assert.Contains(t, result, "[1,2]")
	assert.Contains(t, result, "some context here")
}

func TestFormatCrossChannelItems(t *testing.T) {
	database := testDB(t)

	pipe := New(database, testConfig(), nil, log.Default())
	pipe.channelNames = map[string]string{"C1": "general", "C2": "devops"}
	pipe.userNames = map[string]string{}

	// No tracks → empty string
	assert.Equal(t, "", pipe.formatCrossChannelItems("C1", "U001"))

	// Add a track in C2
	_, err := database.UpsertTrack(db.Track{
		ChannelID:      "C2",
		AssigneeUserID: "U001",
		Text:           "cross channel track",
		Context:        "some cross context",
		Status:         "inbox",
		Priority:       "high",
		PeriodFrom:     1000,
		PeriodTo:       2000,
	})
	require.NoError(t, err)

	result := pipe.formatCrossChannelItems("C1", "U001")
	assert.Contains(t, result, "EXISTING TRACKS FROM OTHER CHANNELS")
	assert.Contains(t, result, "cross channel track")
	assert.Contains(t, result, "devops")
	assert.Contains(t, result, "some cross context")

	// Exclude C2 — should return empty
	result = pipe.formatCrossChannelItems("C2", "U001")
	assert.Equal(t, "", result)
}

func TestFormatDigestDecisions(t *testing.T) {
	database := testDB(t)

	pipe := New(database, testConfig(), nil, log.Default())

	// No decisions → empty string
	result := pipe.formatDigestDecisions("C1", 1000, 2000)
	assert.Equal(t, "", result)
}

func TestProfileContextWithPeersAndManager(t *testing.T) {
	p := &Pipeline{}
	p.profile = &db.UserProfile{
		CustomPromptContext: "Test context",
		Reports:            `["U002","U003"]`,
		Peers:              `["U010","U011"]`,
		Manager:            "U020",
		StarredChannels:    `["C1"]`,
		StarredPeople:      `["U030"]`,
		Role:               "Engineering Manager",
	}

	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "USER PROFILE CONTEXT")
	assert.Contains(t, ctx, "Test context")
	assert.Contains(t, ctx, "OWNERSHIP RULES")
	assert.Contains(t, ctx, `["U002","U003"]`)
	assert.Contains(t, ctx, "MY PEERS")
	assert.Contains(t, ctx, `["U010","U011"]`)
	assert.Contains(t, ctx, "MY MANAGER")
	assert.Contains(t, ctx, "U020")
	assert.Contains(t, ctx, "STARRED CHANNELS")
	assert.Contains(t, ctx, "STARRED PEOPLE")
}

func TestProfileContextEmptyReportsAndPeers(t *testing.T) {
	p := &Pipeline{}
	p.profile = &db.UserProfile{
		CustomPromptContext: "Test context",
		Reports:            "[]",
		Peers:              "[]",
		StarredChannels:    "[]",
		StarredPeople:      "[]",
		Role:               "Software Engineer",
	}

	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "USER PROFILE CONTEXT")
	// Empty arrays should NOT produce the specific MY REPORTS/MY PEERS sections with user_ids
	assert.NotContains(t, ctx, "MY REPORTS (user_ids)")
	assert.NotContains(t, ctx, "MY PEERS (user_ids)")
	assert.NotContains(t, ctx, "MY MANAGER (user_id)")
	assert.NotContains(t, ctx, "STARRED CHANNELS:")
	assert.NotContains(t, ctx, "STARRED PEOPLE:")
}

func TestCategoryWeightingPM(t *testing.T) {
	p := &Pipeline{}
	p.profile = &db.UserProfile{
		CustomPromptContext: "context",
		Role:                "Product Manager",
	}
	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "decision_needed, approval")
}

func TestCategoryWeightingLead(t *testing.T) {
	p := &Pipeline{}
	p.profile = &db.UserProfile{
		CustomPromptContext: "context",
		Role:                "Tech Lead",
	}
	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "decision_needed, code_review")
}

func TestCategoryWeightingDefault(t *testing.T) {
	p := &Pipeline{}
	p.profile = &db.UserProfile{
		CustomPromptContext: "context",
		Role:                "Intern",
	}
	ctx := p.formatProfileContext()
	assert.Contains(t, ctx, "code_review, bug_fix, task")
}

func TestPipelineWithChainContext(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000001.000000", UserID: "U001",
		Text: "test message",
	}))

	gen := &capturingGenerator{
		response: `{"items": []}`,
	}

	pipe := New(database, testConfig(), gen, log.Default())
	pipe.ChainContext = "=== ACTIVE CHAINS ===\nChain #1: Migration project"

	_, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)

	captured := gen.getCapturedPrompt()
	assert.Contains(t, captured, "ACTIVE CHAINS")
	assert.Contains(t, captured, "Migration project")
	assert.Contains(t, captured, "chain_id")
}

func TestPipelineNoMessagesInWindow(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))

	gen := &mockGenerator{response: `{"items": []}`}
	pipe := New(database, testConfig(), gen, log.Default())

	// No channels or messages at all
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestPipelineExistingIDOwnerMismatch(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T1", Name: "test", Domain: "test"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice", DisplayName: "Alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob", DisplayName: "Bob"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	// Create track owned by U002 (different user).
	otherID, err := database.UpsertTrack(db.Track{
		ChannelID:      "C1",
		AssigneeUserID: "U002",
		Text:           "bob's track",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     1000000000,
		PeriodTo:       1000000100,
	})
	require.NoError(t, err)

	require.NoError(t, database.UpsertMessage(db.Message{
		ChannelID: "C1", TS: "1000000050.000000", UserID: "U001",
		Text: "some message",
	}))

	// AI tries to update bob's track with existing_id — should be ignored and create a new track.
	gen := &mockGenerator{
		response: fmt.Sprintf(`{
			"items": [
				{
					"existing_id": %d,
					"text": "try to hijack",
					"context": "hijack attempt",
					"source_message_ts": "1000000050.000000",
					"priority": "high"
				}
			]
		}`, otherID),
	}

	pipe := New(database, testConfig(), gen, log.Default())
	n, err := pipe.RunForWindow(context.Background(), "U001", 1000000000, 1000000100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Should have created a new track, not updated bob's
	tracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U001"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "try to hijack", tracks[0].Text)

	// Bob's track should be unchanged
	bobTracks, err := database.GetTracks(db.TrackFilter{AssigneeUserID: "U002"})
	require.NoError(t, err)
	assert.Len(t, bobTracks, 1)
	assert.Equal(t, "bob's track", bobTracks[0].Text)
}

func TestGetPromptFallback(t *testing.T) {
	pipe := &Pipeline{
		cfg: testConfig(),
	}
	// No prompt store — should return default
	tmpl, version := pipe.getPrompt("tracks.extract")
	assert.NotEmpty(t, tmpl)
	assert.Equal(t, 0, version)
}

func TestLoadCaches(t *testing.T) {
	database := testDB(t)

	require.NoError(t, database.UpsertUser(db.User{ID: "U1", Name: "alice", DisplayName: "Alice Display"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U2", Name: "bob_no_display"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C1", Name: "general", Type: "public"}))

	pipe := New(database, testConfig(), nil, log.Default())
	pipe.loadCaches()

	assert.Equal(t, "Alice Display", pipe.userName("U1"))
	assert.Equal(t, "bob_no_display", pipe.userName("U2")) // falls back to Name when DisplayName is empty
	assert.Equal(t, "general", pipe.channelName("C1"))
}

func TestDayWindowDifferentTimezones(t *testing.T) {
	// Test with UTC
	utcTime, _ := time.Parse(time.RFC3339, "2026-03-18T15:00:00Z")
	from, to := DayWindow(utcTime)
	assert.True(t, to > from)
	assert.True(t, to-from >= 82800) // at least ~23h

	// Window should start at midnight
	startTime := time.Unix(int64(from), 0).UTC()
	assert.Equal(t, 0, startTime.Hour())
	assert.Equal(t, 0, startTime.Minute())
}
