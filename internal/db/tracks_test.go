package db

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertTrack_New(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{
		Text:     "Review the API redesign PR",
		Category: "code_review",
		Priority: "high",
		Tags:     `["api"]`,
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Review the API redesign PR", track.Text)
	assert.Equal(t, "code_review", track.Category)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "mine", track.Ownership)
}

func TestUpsertTrack_Update(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{Text: "Old text", Priority: "low"})
	require.NoError(t, err)

	// Mark as read, then update — should set has_updates
	require.NoError(t, db.MarkTrackRead(int(id)))

	_, err = db.UpsertTrack(Track{
		ID: int(id), Text: "New text", Priority: "high",
	})
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "New text", track.Text)
	assert.Equal(t, "high", track.Priority)
	assert.True(t, track.HasUpdates)
}

func TestGetTracks_Filters(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(Track{Text: "High task", Priority: "high", ChannelIDs: `["C1"]`, Ownership: "mine"})
	require.NoError(t, err)
	_, err = db.UpsertTrack(Track{Text: "Low task", Priority: "low", ChannelIDs: `["C2"]`, Ownership: "delegated"})
	require.NoError(t, err)

	// Filter by priority
	tracks, err := db.GetTracks(TrackFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "High task", tracks[0].Text)

	// Filter by channel
	tracks, err = db.GetTracks(TrackFilter{ChannelID: "C2"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Low task", tracks[0].Text)

	// Filter by ownership
	tracks, err = db.GetTracks(TrackFilter{Ownership: "delegated"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Low task", tracks[0].Text)

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

	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium"})
	require.NoError(t, err)

	err = db.SetTrackHasUpdates(int(id))
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.True(t, track.HasUpdates)
}

func TestMarkTrackRead(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium"})
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

	_, err := db.UpsertTrack(Track{Text: "A", Priority: "high"})
	require.NoError(t, err)
	_, err = db.UpsertTrack(Track{Text: "B", Priority: "low"})
	require.NoError(t, err)

	tracks, err := db.GetAllActiveTracks()
	require.NoError(t, err)
	assert.Len(t, tracks, 2)
}

func TestGetTrackCount(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(Track{Text: "A", Priority: "high"})
	require.NoError(t, err)

	id2, err := db.UpsertTrack(Track{Text: "B", Priority: "medium"})
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
		Text:       "Linked",
		Priority:   "medium",
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

func TestUpdateTrackFromExtraction(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{
		Text:             "Original task",
		Priority:         "medium",
		ChannelIDs:       `["C1"]`,
		RelatedDigestIDs: `[1]`,
	})
	require.NoError(t, err)

	// Mark as read so we can verify has_updates gets set
	require.NoError(t, db.MarkTrackRead(int(id)))

	_, err = db.UpdateTrackFromExtraction(int(id), Track{
		Text:             "Updated task",
		Priority:         "high",
		ChannelIDs:       `["C2"]`,
		RelatedDigestIDs: `[2]`,
	})
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Updated task", track.Text)
	assert.Equal(t, "high", track.Priority)
	assert.True(t, track.HasUpdates)
	// Channel IDs should be merged: C1 + C2
	assert.Contains(t, track.ChannelIDs, `"C1"`)
	assert.Contains(t, track.ChannelIDs, `"C2"`)
	// Digest IDs should be merged: 1 + 2
	assert.Contains(t, track.RelatedDigestIDs, "1")
	assert.Contains(t, track.RelatedDigestIDs, "2")
}

func TestFindTracksByFingerprint(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertTrack(Track{
		Text:           "Deploy v2 API",
		AssigneeUserID: "U1",
		Fingerprint:    `["deploy","v2-api"]`,
		Priority:       "high",
	})
	require.NoError(t, err)

	_, err = db.UpsertTrack(Track{
		Text:           "Fix login bug",
		AssigneeUserID: "U1",
		Fingerprint:    `["login","bug"]`,
		Priority:       "medium",
	})
	require.NoError(t, err)

	// Should find first track
	tracks, err := db.FindTracksByFingerprint("U1", []string{"deploy"})
	require.NoError(t, err)
	assert.Len(t, tracks, 1)
	assert.Equal(t, "Deploy v2 API", tracks[0].Text)

	// Wrong user — no match
	tracks, err = db.FindTracksByFingerprint("U2", []string{"deploy"})
	require.NoError(t, err)
	assert.Len(t, tracks, 0)

	// Empty fingerprint — nil result
	tracks, err = db.FindTracksByFingerprint("U1", nil)
	require.NoError(t, err)
	assert.Nil(t, tracks)
}

func TestHasTracksForUser(t *testing.T) {
	db := openTestDB(t)

	has, err := db.HasTracksForUser("U1")
	require.NoError(t, err)
	assert.False(t, has)

	_, err = db.UpsertTrack(Track{Text: "task", AssigneeUserID: "U1", Priority: "medium"})
	require.NoError(t, err)

	has, err = db.HasTracksForUser("U1")
	require.NoError(t, err)
	assert.True(t, has)
}

func TestUpdateTrackOwnership(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium"})
	require.NoError(t, err)

	err = db.UpdateTrackOwnership(int(id), "delegated")
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "delegated", track.Ownership)
}

func TestMergeJSONArrays(t *testing.T) {
	// Merge string arrays
	result := mergeJSONArrays(`["a","b"]`, `["b","c"]`)
	assert.Contains(t, result, `"a"`)
	assert.Contains(t, result, `"b"`)
	assert.Contains(t, result, `"c"`)

	// Merge int arrays
	result = mergeJSONArrays(`[1,2]`, `[2,3]`)
	assert.Contains(t, result, "1")
	assert.Contains(t, result, "2")
	assert.Contains(t, result, "3")

	// Empty arrays
	result = mergeJSONArrays(`[]`, `[]`)
	assert.Equal(t, "[]", result)

	// One empty
	result = mergeJSONArrays(`[]`, `["a"]`)
	assert.Contains(t, result, `"a"`)
}

func TestMarkTrackRead_CascadeDigests(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	// Create a digest
	digestID, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000, PeriodTo: 2000,
		Summary: "Test", MessageCount: 5, Model: "test",
	})
	require.NoError(t, err)

	// Create a track linked to that digest
	trackID, err := db.UpsertTrack(Track{
		Text:             "task",
		Priority:         "medium",
		RelatedDigestIDs: `[` + fmt.Sprintf("%d", digestID) + `]`,
	})
	require.NoError(t, err)

	// Mark track read — should cascade to digest
	require.NoError(t, db.MarkTrackRead(int(trackID)))

	// Verify digest got marked read
	digest, err := db.GetDigestByID(int(digestID))
	require.NoError(t, err)
	assert.True(t, digest.ReadAt.Valid)
}
