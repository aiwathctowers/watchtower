package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleTrack(channelID, userID, text string) Track {
	return Track{
		ChannelID:        channelID,
		AssigneeUserID:   userID,
		Text:             text,
		Status:           "inbox",
		Priority:         "medium",
		DueDate:          0,
		PeriodFrom:       1000000,
		PeriodTo:         2000000,
		SourceMessageTS:  "1234567890.123456",
		Participants:     "[]",
		SourceRefs:       "[]",
		Tags:             "[]",
		SubItems:         "[]",
		RelatedDigestIDs: "[]",
		DecisionOptions:  "[]",
	}
}

func TestUpsertTrack_New(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "review PR"))
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Check history logged
	history, err := db.GetTrackHistory(int(id))
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "created", history[0].Event)
}

func TestUpsertTrack_DefaultOwnership(t *testing.T) {
	db := openTestDB(t)

	trk := sampleTrack("C1", "U1", "review PR")
	trk.Ownership = ""
	id, err := db.UpsertTrack(trk)
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "mine", track.Ownership)
}

func TestUpsertTrack_Update_InboxStatus(t *testing.T) {
	db := openTestDB(t)

	trk := sampleTrack("C1", "U1", "review PR")
	trk.Priority = "low"
	id, err := db.UpsertTrack(trk)
	require.NoError(t, err)

	// Upsert same track with different priority
	trk.Priority = "high"
	trk.Context = "updated context"
	id2, err := db.UpsertTrack(trk)
	require.NoError(t, err)
	assert.Equal(t, id, id2)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "updated context", track.Context)

	// History should have created + priority_changed + context_updated
	history, err := db.GetTrackHistory(int(id))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(history), 2)
}

func TestUpsertTrack_Update_ActiveStatus_MetadataOnly(t *testing.T) {
	db := openTestDB(t)

	trk := sampleTrack("C1", "U1", "review PR")
	id, err := db.UpsertTrack(trk)
	require.NoError(t, err)

	// Move to active
	err = db.AcceptTrack(int(id))
	require.NoError(t, err)

	// Re-extract same track — should only update metadata, not priority/context
	trk.Priority = "high"
	trk.Context = "new context"
	trk.Tags = `["tag1"]`
	id2, err := db.UpsertTrack(trk)
	require.NoError(t, err)
	assert.Equal(t, id, id2)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "medium", track.Priority) // preserved
	assert.Equal(t, `["tag1"]`, track.Tags)   // metadata updated
}

func TestGetTracks_Filters(t *testing.T) {
	db := openTestDB(t)

	t1 := sampleTrack("C1", "U1", "task 1")
	t1.Priority = "high"
	_, err := db.UpsertTrack(t1)
	require.NoError(t, err)

	t2 := sampleTrack("C2", "U2", "task 2")
	t2.Priority = "low"
	t2.SourceMessageTS = "9999999999.000001"
	id2, err := db.UpsertTrack(t2)
	require.NoError(t, err)
	require.NoError(t, db.UpdateTrackStatus(int(id2), "done"))

	t3 := sampleTrack("C1", "U1", "task 3")
	t3.Ownership = "delegated"
	t3.SourceMessageTS = "9999999999.000002"
	_, err = db.UpsertTrack(t3)
	require.NoError(t, err)

	// Filter by assignee
	tracks, err := db.GetTracks(TrackFilter{AssigneeUserID: "U1"})
	require.NoError(t, err)
	assert.Len(t, tracks, 2)

	// Filter by status
	tracks, err = db.GetTracks(TrackFilter{Status: "done"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)

	// Filter by channel
	tracks, err = db.GetTracks(TrackFilter{ChannelID: "C1"})
	require.NoError(t, err)
	assert.Len(t, tracks, 2)

	// Filter by priority
	tracks, err = db.GetTracks(TrackFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)

	// Filter by ownership
	tracks, err = db.GetTracks(TrackFilter{Ownership: "delegated"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)

	// With limit
	tracks, err = db.GetTracks(TrackFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)

	// Filter by has_updates
	boolTrue := true
	boolFalse := false
	tracks, err = db.GetTracks(TrackFilter{HasUpdates: &boolTrue})
	require.NoError(t, err)
	assert.Len(t, tracks, 0)
	tracks, err = db.GetTracks(TrackFilter{HasUpdates: &boolFalse})
	require.NoError(t, err)
	assert.Len(t, tracks, 3)
}

func TestUpdateTrackStatus(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	// Move to active
	err = db.UpdateTrackStatus(int(id), "active")
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "active", track.Status)
	assert.False(t, track.CompletedAt.Valid)

	// Move to done — completed_at should be set
	err = db.UpdateTrackStatus(int(id), "done")
	require.NoError(t, err)

	track, err = db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "done", track.Status)
	assert.True(t, track.CompletedAt.Valid)
}

func TestUpdateTrackStatus_InvalidStatus(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.UpdateTrackStatus(int(id), "invalid")
	assert.Error(t, err)
}

func TestUpdateTrackStatus_NotFound(t *testing.T) {
	db := openTestDB(t)

	err := db.UpdateTrackStatus(999, "active")
	assert.Error(t, err)
}

func TestAcceptTrack(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.AcceptTrack(int(id))
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "active", track.Status)
}

func TestAcceptTrack_NotInbox(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)
	require.NoError(t, db.UpdateTrackStatus(int(id), "done"))

	err = db.AcceptTrack(int(id))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in inbox status")
}

func TestAcceptTrack_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.AcceptTrack(999)
	assert.Error(t, err)
}

func TestSnoozeTrack(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.SnoozeTrack(int(id), 9999999999)
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "snoozed", track.Status)
	assert.Equal(t, 9999999999.0, track.SnoozeUntil)
	assert.Equal(t, "inbox", track.PreSnoozeStatus)
}

func TestSnoozeTrack_FromActive(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)
	require.NoError(t, db.AcceptTrack(int(id)))

	err = db.SnoozeTrack(int(id), 9999999999)
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "snoozed", track.Status)
	assert.Equal(t, "active", track.PreSnoozeStatus)
}

func TestSnoozeTrack_InvalidStatus(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)
	require.NoError(t, db.UpdateTrackStatus(int(id), "done"))

	err = db.SnoozeTrack(int(id), 9999999999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be snoozed")
}

func TestSetTrackHasUpdates(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.SetTrackHasUpdates(int(id), true)
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.True(t, track.HasUpdates)

	// Check history
	history, err := db.GetTrackHistory(int(id))
	require.NoError(t, err)
	var hasUpdateEvent bool
	for _, h := range history {
		if h.Event == "update_detected" {
			hasUpdateEvent = true
		}
	}
	assert.True(t, hasUpdateEvent)
}

func TestSetTrackHasUpdates_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.SetTrackHasUpdates(999, true)
	assert.Error(t, err)
}

func TestMarkTrackUpdateRead(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)
	require.NoError(t, db.SetTrackHasUpdates(int(id), true))

	err = db.MarkTrackUpdateRead(int(id))
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.False(t, track.HasUpdates)
}

func TestMarkTrackUpdateRead_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.MarkTrackUpdateRead(999)
	assert.Error(t, err)
}

func TestGetTrackAssignee(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	assignee, err := db.GetTrackAssignee(int(id))
	require.NoError(t, err)
	assert.Equal(t, "U1", assignee)
}

func TestGetTrackAssignee_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetTrackAssignee(999)
	assert.Error(t, err)
}

func TestCountOpenTracks(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(sampleTrack("C1", "U1", "task1"))
	require.NoError(t, err)

	t2 := sampleTrack("C1", "U1", "task2")
	t2.SourceMessageTS = "9999999999.000001"
	id2, err := db.UpsertTrack(t2)
	require.NoError(t, err)
	require.NoError(t, db.AcceptTrack(int(id2)))

	// Done track — should not count
	t3 := sampleTrack("C1", "U1", "task3")
	t3.SourceMessageTS = "9999999999.000002"
	id3, err := db.UpsertTrack(t3)
	require.NoError(t, err)
	require.NoError(t, db.UpdateTrackStatus(int(id3), "done"))

	count, err := db.CountOpenTracks("U1")
	require.NoError(t, err)
	assert.Equal(t, 2, count) // inbox + active
}

func TestHasTracksForUser(t *testing.T) {
	db := openTestDB(t)

	has, err := db.HasTracksForUser("U1")
	require.NoError(t, err)
	assert.False(t, has)

	_, err = db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	has, err = db.HasTracksForUser("U1")
	require.NoError(t, err)
	assert.True(t, has)

	has, err = db.HasTracksForUser("U2")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestDeleteTracksForWindow(t *testing.T) {
	db := openTestDB(t)

	// Inbox track in window — should be deleted
	_, err := db.UpsertTrack(sampleTrack("C1", "U1", "inbox task"))
	require.NoError(t, err)

	// Active track in window — should NOT be deleted
	t2 := sampleTrack("C1", "U1", "active task")
	t2.SourceMessageTS = "9999999999.000001"
	id2, err := db.UpsertTrack(t2)
	require.NoError(t, err)
	require.NoError(t, db.AcceptTrack(int(id2)))

	deleted, err := db.DeleteTracksForWindow("U1", 1000000, 2000000)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	tracks, err := db.GetTracks(TrackFilter{AssigneeUserID: "U1"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "active", tracks[0].Status)
}

func TestGetTrackHistory(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	// Accept then update status
	require.NoError(t, db.AcceptTrack(int(id)))
	require.NoError(t, db.UpdateTrackStatus(int(id), "done"))

	history, err := db.GetTrackHistory(int(id))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(history), 3) // created, accepted, status_changed
}

func TestUpdateTrackContext(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.UpdateTrackContext(int(id), "new context here")
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "new context here", track.Context)
}

func TestUpdateTrackContext_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.UpdateTrackContext(999, "ctx")
	assert.Error(t, err)
}

func TestUpdateLastCheckedTS(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.UpdateLastCheckedTS(int(id), "1234567890.999999")
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "1234567890.999999", track.LastCheckedTS)
}

func TestUpdateLastCheckedTS_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.UpdateLastCheckedTS(999, "123.456")
	assert.Error(t, err)
}

func TestGetTracksForUpdateCheck(t *testing.T) {
	db := openTestDB(t)

	// Track with source_message_ts — should be returned
	_, err := db.UpsertTrack(sampleTrack("C1", "U1", "task with ts"))
	require.NoError(t, err)

	// Track without source_message_ts — should not be returned
	t2 := sampleTrack("C1", "U1", "task without ts")
	t2.SourceMessageTS = ""
	t2.Text = "no source ts"
	_, err = db.UpsertTrack(t2)
	require.NoError(t, err)

	// Done track — should not be returned
	t3 := sampleTrack("C1", "U1", "done task")
	t3.SourceMessageTS = "8888888888.000001"
	id3, err := db.UpsertTrack(t3)
	require.NoError(t, err)
	require.NoError(t, db.UpdateTrackStatus(int(id3), "done"))

	tracks, err := db.GetTracksForUpdateCheck()
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "task with ts", tracks[0].Text)
}

func TestFindRelatedDigestIDs(t *testing.T) {
	db := openTestDB(t)

	// Insert digests
	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "channel C1", Model: "haiku",
	})
	require.NoError(t, err)

	_, err = db.UpsertDigest(Digest{
		ChannelID: "", Type: "daily",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "daily", Model: "haiku",
	})
	require.NoError(t, err)

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "channel C2", Model: "haiku",
	})
	require.NoError(t, err)

	// Find for C1 with overlapping window
	ids, err := db.FindRelatedDigestIDs("C1", 1500000, 1800000)
	require.NoError(t, err)
	assert.Len(t, ids, 2) // C1 channel + daily (empty channel_id matches)
}

func TestGetExistingTracksForChannel(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(sampleTrack("C1", "U1", "task in C1"))
	require.NoError(t, err)

	t2 := sampleTrack("C2", "U1", "task in C2")
	t2.SourceMessageTS = "9999999999.000001"
	_, err = db.UpsertTrack(t2)
	require.NoError(t, err)

	tracks, err := db.GetExistingTracksForChannel("C1", "U1")
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "task in C1", tracks[0].Text)
}

func TestGetExistingTracksExcludingChannel(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(sampleTrack("C1", "U1", "task in C1"))
	require.NoError(t, err)

	t2 := sampleTrack("C2", "U1", "task in C2")
	t2.SourceMessageTS = "9999999999.000001"
	_, err = db.UpsertTrack(t2)
	require.NoError(t, err)

	tracks, err := db.GetExistingTracksExcludingChannel("C1", "U1")
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "task in C2", tracks[0].Text)
}

func TestUpdateTrackBallOn(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	err = db.UpdateTrackBallOn(int(id), "U2")
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "U2", track.BallOn)

	// Same value — should no-op
	err = db.UpdateTrackBallOn(int(id), "U2")
	require.NoError(t, err)
}

func TestUpdateTrackBallOn_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.UpdateTrackBallOn(999, "U1")
	assert.Error(t, err)
}

func TestUpdateTrackSubItems(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	newSubItems := `[{"text":"sub task 1","isDone":false}]`
	err = db.UpdateTrackSubItems(int(id), newSubItems)
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, newSubItems, track.SubItems)
}

func TestUpdateTrackSubItems_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.UpdateTrackSubItems(999, "[]")
	assert.Error(t, err)
}

func TestUpdateTrackFromExtraction(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(sampleTrack("C1", "U1", "task"))
	require.NoError(t, err)

	changed, err := db.UpdateTrackFromExtraction(int(id), Track{
		Priority: "high",
		Context:  "new context",
	})
	require.NoError(t, err)
	assert.True(t, changed)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "new context", track.Context)
}

func TestUpdateTrackFromExtraction_NoChange(t *testing.T) {
	db := openTestDB(t)

	trk := sampleTrack("C1", "U1", "task")
	trk.Priority = "medium"
	trk.Context = "original"
	id, err := db.UpsertTrack(trk)
	require.NoError(t, err)

	// Same values — no change
	changed, err := db.UpdateTrackFromExtraction(int(id), Track{
		Priority: "medium",
		Context:  "original",
	})
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestUpdateTrackFromExtraction_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.UpdateTrackFromExtraction(999, Track{Priority: "high"})
	assert.Error(t, err)
}

func TestSummarizeSubItemsChange(t *testing.T) {
	// Items completed
	old := `[{"text":"task A","isDone":false},{"text":"task B","isDone":false}]`
	new1 := `[{"text":"task A","isDone":true},{"text":"task B","isDone":false}]`
	result := summarizeSubItemsChange(old, new1)
	assert.Contains(t, result, "completed: task A")

	// Items added
	new2 := `[{"text":"task A","isDone":false},{"text":"task B","isDone":false},{"text":"task C","isDone":false}]`
	result = summarizeSubItemsChange(old, new2)
	assert.Contains(t, result, "added: task C")

	// Items removed
	new3 := `[{"text":"task A","isDone":false}]`
	result = summarizeSubItemsChange(old, new3)
	assert.Contains(t, result, "removed: task B")

	// No meaningful change
	result = summarizeSubItemsChange(old, old)
	assert.Contains(t, result, "2 items")
}

func TestReactivateSnoozedTracks(t *testing.T) {
	db := openTestDB(t)

	// Create and snooze a track with snooze_until in the past
	id1, err := db.UpsertTrack(sampleTrack("C1", "U1", "snoozed task from inbox"))
	require.NoError(t, err)
	// Snooze until a past time (epoch 1 = way in the past)
	require.NoError(t, db.SnoozeTrack(int(id1), 1))

	// Create and snooze another track from active status
	t2 := sampleTrack("C1", "U1", "snoozed task from active")
	t2.SourceMessageTS = "9999999999.000001"
	id2, err := db.UpsertTrack(t2)
	require.NoError(t, err)
	require.NoError(t, db.AcceptTrack(int(id2)))
	require.NoError(t, db.SnoozeTrack(int(id2), 1))

	// Create a snoozed track with future snooze_until — should NOT be reactivated
	t3 := sampleTrack("C1", "U1", "future snoozed task")
	t3.SourceMessageTS = "9999999999.000002"
	id3, err := db.UpsertTrack(t3)
	require.NoError(t, err)
	require.NoError(t, db.SnoozeTrack(int(id3), 9999999999))

	count, err := db.ReactivateSnoozedTracks()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Check restored statuses
	track1, err := db.GetTrackByID(int(id1))
	require.NoError(t, err)
	assert.Equal(t, "inbox", track1.Status)
	assert.Equal(t, 0.0, track1.SnoozeUntil)

	track2, err := db.GetTrackByID(int(id2))
	require.NoError(t, err)
	assert.Equal(t, "active", track2.Status) // restored to pre_snooze_status
	assert.Equal(t, 0.0, track2.SnoozeUntil)

	// Future snoozed track should remain snoozed
	track3, err := db.GetTrackByID(int(id3))
	require.NoError(t, err)
	assert.Equal(t, "snoozed", track3.Status)
}

func TestReactivateSnoozedTracks_None(t *testing.T) {
	db := openTestDB(t)

	count, err := db.ReactivateSnoozedTracks()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestFormatSnoozeUntil(t *testing.T) {
	assert.Equal(t, "", formatSnoozeUntil(0))
	assert.Equal(t, "", formatSnoozeUntil(-1))

	// A known timestamp
	result := formatSnoozeUntil(1750000000)
	assert.NotEmpty(t, result)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hel...", truncate("hello", 3))
	assert.Equal(t, "", truncate("", 5))
}

func TestSummarizeDigestLinked(t *testing.T) {
	result := summarizeDigestLinked(`[1,2]`, `[1,2,3,4]`)
	assert.Contains(t, result, "linked #3, #4")

	result = summarizeDigestLinked(`[]`, `[1]`)
	assert.Contains(t, result, "linked #1")

	result = summarizeDigestLinked(`[1,2]`, `[1,2]`)
	assert.Contains(t, result, "2 digests")
}

func TestUpdateTrackFromExtraction_AllFields(t *testing.T) {
	db := openTestDB(t)

	trk := sampleTrack("C1", "U1", "task with all fields")
	trk.DecisionSummary = "old summary"
	trk.RelatedDigestIDs = "[1]"
	trk.SubItems = `[{"text":"task A","isDone":false}]`
	trk.Ownership = "mine"
	trk.BallOn = "U1"
	id, err := db.UpsertTrack(trk)
	require.NoError(t, err)

	// Update all field branches that are normally uncovered
	changed, err := db.UpdateTrackFromExtraction(int(id), Track{
		DueDate:          1700000000,
		DecisionSummary:  "new decision summary",
		RelatedDigestIDs: "[1,2,3]",
		SubItems:         `[{"text":"task A","isDone":true},{"text":"task B","isDone":false}]`,
		Ownership:        "delegated",
		BallOn:           "U2",
	})
	require.NoError(t, err)
	assert.True(t, changed)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, 1700000000.0, track.DueDate)
	assert.Equal(t, "new decision summary", track.DecisionSummary)
	assert.Equal(t, "[1,2,3]", track.RelatedDigestIDs)
	assert.Contains(t, track.SubItems, "task B")
	assert.Equal(t, "delegated", track.Ownership)
	assert.Equal(t, "U2", track.BallOn)

	// Verify history entries were logged
	history, err := db.GetTrackHistory(int(id))
	require.NoError(t, err)
	// Should have events for: due_date_changed, decision_evolved,
	// digest_linked, sub_items_updated, ownership_changed, ball_on_changed
	eventTypes := make(map[string]bool)
	for _, h := range history {
		eventTypes[h.Event] = true
	}
	assert.True(t, eventTypes["due_date_changed"], "should log due_date_changed")
	assert.True(t, eventTypes["decision_evolved"], "should log decision_evolved")
	assert.True(t, eventTypes["digest_linked"], "should log digest_linked")
	assert.True(t, eventTypes["sub_items_updated"], "should log sub_items_updated")
	assert.True(t, eventTypes["ownership_changed"], "should log ownership_changed")
	assert.True(t, eventTypes["ball_on_changed"], "should log ball_on_changed")
}

func TestUpdateTrackFromExtraction_DueDateOnly(t *testing.T) {
	db := openTestDB(t)

	trk := sampleTrack("C1", "U1", "task with due date")
	trk.DueDate = 1600000000
	id, err := db.UpsertTrack(trk)
	require.NoError(t, err)

	// Update due date
	changed, err := db.UpdateTrackFromExtraction(int(id), Track{
		DueDate: 1700000000,
	})
	require.NoError(t, err)
	assert.True(t, changed)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, 1700000000.0, track.DueDate)
}

func TestGetTracksFilterByUnixRange(t *testing.T) {
	db := openTestDB(t)

	// Create tracks with different period ranges
	t1 := sampleTrack("C1", "U1", "early track")
	t1.PeriodFrom = 1000000
	t1.PeriodTo = 1500000
	_, err := db.UpsertTrack(t1)
	require.NoError(t, err)

	t2 := sampleTrack("C1", "U1", "late track")
	t2.PeriodFrom = 2000000
	t2.PeriodTo = 2500000
	_, err = db.UpsertTrack(t2)
	require.NoError(t, err)

	// Filter by FromUnix — only tracks with period_from >= 1500000
	tracks, err := db.GetTracks(TrackFilter{FromUnix: 1500000})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "late track", tracks[0].Text)

	// Filter by ToUnix — only tracks with period_to <= 1500000
	tracks, err = db.GetTracks(TrackFilter{ToUnix: 1500000})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "early track", tracks[0].Text)

	// Combined FromUnix + ToUnix
	tracks, err = db.GetTracks(TrackFilter{FromUnix: 900000, ToUnix: 2600000})
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}
