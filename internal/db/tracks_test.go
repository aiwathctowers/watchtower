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

	// BEHAVIOR TRACKS-06: manual upsert with ID snapshots prior state.
	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "Old text", states[0].Text)
	assert.Equal(t, "manual", states[0].Source)
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

	// BEHAVIOR TRACKS-06: extraction update snapshots prior narrative state.
	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "Original task", states[0].Text)
	assert.Equal(t, "extraction", states[0].Source)
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
	// Fresh elements come first, then existing-only elements.
	// "c" is fresh-only → first; "b" exists in both → second (kept from fresh); "a" is existing-only → last.
	result := mergeJSONArrays(`["a","b"]`, `["c","b"]`)
	assert.Equal(t, `["c","b","a"]`, result)

	// Fresh int arrays — same ordering rule.
	result = mergeJSONArrays(`[1,2]`, `[3,2]`)
	assert.Equal(t, `[3,2,1]`, result)

	// Empty arrays
	result = mergeJSONArrays(`[]`, `[]`)
	assert.Equal(t, "[]", result)

	// Existing empty: only fresh remain.
	result = mergeJSONArrays(`[]`, `["a"]`)
	assert.Equal(t, `["a"]`, result)

	// Fresh empty: existing preserved as-is.
	result = mergeJSONArrays(`["a","b"]`, `[]`)
	assert.Equal(t, `["a","b"]`, result)
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

// --- TRACKS-06: track state history guards ---

// BEHAVIOR TRACKS-06: snapshot is written before extraction overwrites narrative fields.
func TestTracks06_StateSnapshotOnExtractionUpdate(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{
		Text: "Original text", Context: "Original context",
		Priority: "medium", Category: "task",
	})
	require.NoError(t, err)

	// Freshly inserted tracks have no history.
	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	assert.Len(t, states, 0)

	_, err = db.UpdateTrackFromExtraction(int(id), Track{
		Text: "Updated text", Context: "Updated context",
		Priority: "high", Category: "task",
		Model: "haiku-4-5", PromptVersion: 7,
	})
	require.NoError(t, err)

	states, err = db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "Original text", states[0].Text)
	assert.Equal(t, "Original context", states[0].Context)
	assert.Equal(t, "medium", states[0].Priority)
	assert.Equal(t, "extraction", states[0].Source)
}

// BEHAVIOR TRACKS-06: identical narrative re-extraction does NOT write a snapshot.
func TestTracks06_NoSnapshotWhenNarrativeUnchanged(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{
		Text: "Same", Context: "Same",
		Priority: "medium", Category: "task",
	})
	require.NoError(t, err)

	// Re-extract identical narrative; only model + tokens differ.
	_, err = db.UpdateTrackFromExtraction(int(id), Track{
		Text: "Same", Context: "Same",
		Priority: "medium", Category: "task",
		Model: "different-model", InputTokens: 999,
	})
	require.NoError(t, err)

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	assert.Len(t, states, 0, "no snapshot when narrative unchanged")
}

// BEHAVIOR TRACKS-06: manual priority change snapshots prior state with source='manual'.
func TestTracks06_StateSnapshotOnManualPriorityChange(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium", Category: "task"})
	require.NoError(t, err)

	require.NoError(t, db.UpdateTrackPriority(int(id), "high"))

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "medium", states[0].Priority)
	assert.Equal(t, "manual", states[0].Source)

	// No-op repeat: setting the same priority should not snapshot.
	require.NoError(t, db.UpdateTrackPriority(int(id), "high"))
	states, err = db.GetTrackStates(int(id))
	require.NoError(t, err)
	assert.Len(t, states, 1, "no-op priority update should not snapshot")
}

// BEHAVIOR TRACKS-06: manual ownership change snapshots prior state with source='manual'.
func TestTracks06_StateSnapshotOnManualOwnershipChange(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{
		Text: "task", Ownership: "mine",
		Priority: "medium", Category: "task",
	})
	require.NoError(t, err)

	require.NoError(t, db.UpdateTrackOwnership(int(id), "delegated"))

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "mine", states[0].Ownership)
	assert.Equal(t, "manual", states[0].Source)
}

// BEHAVIOR TRACKS-06: manual sub_items change snapshots prior state with source='manual'.
func TestTracks06_StateSnapshotOnManualSubItemsChange(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{
		Text: "task", SubItems: `[]`,
		Priority: "medium", Category: "task",
	})
	require.NoError(t, err)

	require.NoError(t, db.UpdateTrackSubItems(int(id), `[{"text":"step 1","done":false}]`))

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "[]", states[0].SubItems)
	assert.Equal(t, "manual", states[0].Source)
}

// BEHAVIOR TRACKS-06: full update via UpsertTrack(ID>0) snapshots with source='manual'.
func TestTracks06_StateSnapshotOnUpsertWithID(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{
		Text: "Original", Context: "v1",
		Priority: "low", Category: "task",
	})
	require.NoError(t, err)

	_, err = db.UpsertTrack(Track{
		ID: int(id), Text: "Rewritten", Context: "v2",
		Priority: "high", Category: "task",
	})
	require.NoError(t, err)

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, "Original", states[0].Text)
	assert.Equal(t, "v1", states[0].Context)
	assert.Equal(t, "low", states[0].Priority)
	assert.Equal(t, "manual", states[0].Source)
}

// BEHAVIOR TRACKS-06: brand-new tracks (insert path) have no history row.
func TestTracks06_NoSnapshotOnInsert(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{Text: "fresh", Priority: "medium", Category: "task"})
	require.NoError(t, err)

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	assert.Len(t, states, 0)
}

// BEHAVIOR TRACKS-06: history is capped at 30 most recent rows per track.
func TestTracks06_HistoryCapAt30(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium", Category: "task"})
	require.NoError(t, err)

	// Alternate between two distinct values to guarantee a real change every step.
	priorities := []string{"high", "low"}
	for i := 0; i < 35; i++ {
		require.NoError(t, db.UpdateTrackPriority(int(id), priorities[i%2]))
	}

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	assert.Len(t, states, 30, "history is capped at 30 most recent rows")
}

// BEHAVIOR TRACKS-06: GetTrackStates returns rows newest first.
func TestTracks06_GetTrackStatesOrdersDescByCreatedAt(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium", Category: "task"})
	require.NoError(t, err)

	require.NoError(t, db.UpdateTrackPriority(int(id), "high"))   // snapshot of: medium
	require.NoError(t, db.UpdateTrackPriority(int(id), "low"))    // snapshot of: high
	require.NoError(t, db.UpdateTrackPriority(int(id), "medium")) // snapshot of: low

	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 3)
	// Newest first → low, high, medium.
	assert.Equal(t, "low", states[0].Priority)
	assert.Equal(t, "high", states[1].Priority)
	assert.Equal(t, "medium", states[2].Priority)
}

// BEHAVIOR TRACKS-06: deleting a track cascades and removes its history rows.
func TestTracks06_HistoryCascadesOnTrackDelete(t *testing.T) {
	db := openTestDB(t)
	id, err := db.UpsertTrack(Track{Text: "task", Priority: "medium", Category: "task"})
	require.NoError(t, err)

	require.NoError(t, db.UpdateTrackPriority(int(id), "high"))
	states, err := db.GetTrackStates(int(id))
	require.NoError(t, err)
	require.Len(t, states, 1)

	_, err = db.Exec("DELETE FROM tracks WHERE id = ?", id)
	require.NoError(t, err)

	states, err = db.GetTrackStates(int(id))
	require.NoError(t, err)
	assert.Len(t, states, 0, "history cascades when parent track deleted")
}
