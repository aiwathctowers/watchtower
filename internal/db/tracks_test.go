package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertTrack_New(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{
		Title:    "API redesign discussion",
		Priority: "high",
		Tags:     `["api"]`,
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "API redesign discussion", track.Title)
	assert.Equal(t, "high", track.Priority)
}

func TestUpsertTrack_Update(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{Title: "Old title", Priority: "low"})
	require.NoError(t, err)

	// Mark as read, then update — should set has_updates
	require.NoError(t, db.MarkTrackRead(int(id)))

	_, err = db.UpsertTrack(Track{
		ID: int(id), Title: "New title", Priority: "high",
	})
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "New title", track.Title)
	assert.Equal(t, "high", track.Priority)
	assert.True(t, track.HasUpdates)
}

func TestGetTracks_Filters(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(Track{Title: "High task", Priority: "high", ChannelIDs: `["C1"]`})
	require.NoError(t, err)
	_, err = db.UpsertTrack(Track{Title: "Low task", Priority: "low", ChannelIDs: `["C2"]`})
	require.NoError(t, err)

	// Filter by priority
	tracks, err := db.GetTracks(TrackFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "High task", tracks[0].Title)

	// Filter by channel
	tracks, err = db.GetTracks(TrackFilter{ChannelID: "C2"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Low task", tracks[0].Title)

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
	assert.Len(t, tracks, 2)
}

func TestSetTrackHasUpdates(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{Title: "task", Priority: "medium"})
	require.NoError(t, err)

	err = db.SetTrackHasUpdates(int(id))
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.True(t, track.HasUpdates)
}

func TestMarkTrackRead(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{Title: "task", Priority: "medium"})
	require.NoError(t, err)
	require.NoError(t, db.SetTrackHasUpdates(int(id)))

	err = db.MarkTrackRead(int(id))
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.NotEmpty(t, track.ReadAt)
	assert.False(t, track.HasUpdates)
}

func TestGetAllActiveTracks(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(Track{Title: "A", Priority: "high"})
	require.NoError(t, err)
	_, err = db.UpsertTrack(Track{Title: "B", Priority: "low"})
	require.NoError(t, err)

	tracks, err := db.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}

func TestGetTrackCount(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(Track{Title: "A", Priority: "high"})
	require.NoError(t, err)

	id2, err := db.UpsertTrack(Track{Title: "B", Priority: "medium"})
	require.NoError(t, err)
	require.NoError(t, db.SetTrackHasUpdates(int(id2)))

	total, updated, err := db.GetTrackCount()
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Equal(t, 1, updated)
}

func TestGetUnlinkedTopics(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "Test", MessageCount: 5, Model: "test",
	})
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 0, 'Topic A', 'Summary A', '[]', '[]', '[]', '[]')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO digest_topics (digest_id, idx, title, summary, decisions, action_items, situations, key_messages)
		VALUES (1, 1, 'Topic B', 'Summary B', '[]', '[]', '[]', '[]')`)
	require.NoError(t, err)

	// Both should be unlinked
	topics, err := db.GetUnlinkedTopics(14)
	require.NoError(t, err)
	assert.Len(t, topics, 2)

	// Link topic 1 via a track's source_refs
	_, err = db.UpsertTrack(Track{
		Title: "Linked", Priority: "medium",
		SourceRefs: `[{"digest_id":1,"topic_id":1,"channel_id":"C1","timestamp":1000.0}]`,
	})
	require.NoError(t, err)

	topics, err = db.GetUnlinkedTopics(14)
	require.NoError(t, err)
	assert.Len(t, topics, 1)
	assert.Equal(t, "Topic B", topics[0].Title)
}

func TestGetTrackByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := db.GetTrackByID(999)
	assert.Error(t, err)
}
