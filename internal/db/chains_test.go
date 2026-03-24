package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateChain(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateChain(Chain{
		Title:      "API Migration",
		Slug:       "api-migration",
		Status:     "active",
		Summary:    "Migrating from REST to gRPC",
		ChannelIDs: `["C1","C2"]`,
		FirstSeen:  1000000,
		LastSeen:   2000000,
		ItemCount:  3,
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestGetChainByID(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateChain(Chain{
		Title:      "API Migration",
		Slug:       "api-migration",
		Status:     "active",
		Summary:    "Migrating from REST to gRPC",
		ChannelIDs: `["C1"]`,
		FirstSeen:  1000000,
		LastSeen:   2000000,
		ItemCount:  3,
	})
	require.NoError(t, err)

	chain, err := db.GetChainByID(int(id))
	require.NoError(t, err)
	require.NotNil(t, chain)
	assert.Equal(t, "API Migration", chain.Title)
	assert.Equal(t, "api-migration", chain.Slug)
	assert.Equal(t, "active", chain.Status)
	assert.Equal(t, "Migrating from REST to gRPC", chain.Summary)
	assert.Equal(t, `["C1"]`, chain.ChannelIDs)
	assert.Equal(t, 1000000.0, chain.FirstSeen)
	assert.Equal(t, 2000000.0, chain.LastSeen)
	assert.Equal(t, 3, chain.ItemCount)
	assert.NotEmpty(t, chain.CreatedAt)
	assert.NotEmpty(t, chain.UpdatedAt)
}

func TestGetChainByID_NotFound(t *testing.T) {
	db := openTestDB(t)

	_, err := db.GetChainByID(999)
	assert.Error(t, err)
}

func TestUpdateChainSummary(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateChain(Chain{
		Title:      "Test Chain",
		Slug:       "test-chain",
		Status:     "active",
		Summary:    "v1",
		ChannelIDs: `["C1"]`,
		FirstSeen:  1000000,
		LastSeen:   2000000,
		ItemCount:  1,
	})
	require.NoError(t, err)

	err = db.UpdateChainSummary(int(id), "v2", 3000000, 5, `["C1","C2"]`)
	require.NoError(t, err)

	chain, err := db.GetChainByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "v2", chain.Summary)
	assert.Equal(t, 3000000.0, chain.LastSeen)
	assert.Equal(t, 5, chain.ItemCount)
	assert.Equal(t, `["C1","C2"]`, chain.ChannelIDs)
}

func TestUpdateChainStatus(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateChain(Chain{
		Title:      "Test Chain",
		Slug:       "test-chain",
		Status:     "active",
		ChannelIDs: `[]`,
		FirstSeen:  1000000,
		LastSeen:   2000000,
	})
	require.NoError(t, err)

	err = db.UpdateChainStatus(int(id), "resolved")
	require.NoError(t, err)

	chain, err := db.GetChainByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "resolved", chain.Status)
}

func TestGetActiveChains(t *testing.T) {
	db := openTestDB(t)

	// Active chain with recent activity
	_, err := db.CreateChain(Chain{
		Title:      "Recent Active",
		Slug:       "recent-active",
		Status:     "active",
		ChannelIDs: `[]`,
		FirstSeen:  1000000,
		LastSeen:   float64(4000000000), // far future
		ItemCount:  2,
	})
	require.NoError(t, err)

	// Active chain that is stale (last_seen very old)
	_, err = db.CreateChain(Chain{
		Title:      "Old Active",
		Slug:       "old-active",
		Status:     "active",
		ChannelIDs: `[]`,
		FirstSeen:  1000000,
		LastSeen:   100000, // very old
		ItemCount:  1,
	})
	require.NoError(t, err)

	// Resolved chain — should not appear
	_, err = db.CreateChain(Chain{
		Title:      "Resolved",
		Slug:       "resolved",
		Status:     "resolved",
		ChannelIDs: `[]`,
		FirstSeen:  1000000,
		LastSeen:   float64(4000000000),
		ItemCount:  1,
	})
	require.NoError(t, err)

	// staleDays=30 — only Recent Active should be returned (Old Active has last_seen too old)
	chains, err := db.GetActiveChains(30)
	require.NoError(t, err)
	require.Len(t, chains, 1)
	assert.Equal(t, "Recent Active", chains[0].Title)
}

func TestGetChains(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateChain(Chain{
		Title: "Chain A", Slug: "chain-a", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 3000000,
	})
	require.NoError(t, err)
	_, err = db.CreateChain(Chain{
		Title: "Chain B", Slug: "chain-b", Status: "resolved",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)
	_, err = db.CreateChain(Chain{
		Title: "Chain C", Slug: "chain-c", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 4000000,
	})
	require.NoError(t, err)

	// All chains
	chains, err := db.GetChains(ChainFilter{})
	require.NoError(t, err)
	assert.Len(t, chains, 3)
	// Ordered by last_seen DESC
	assert.Equal(t, "Chain C", chains[0].Title)
	assert.Equal(t, "Chain A", chains[1].Title)
	assert.Equal(t, "Chain B", chains[2].Title)

	// Filter by status
	chains, err = db.GetChains(ChainFilter{Status: "active"})
	require.NoError(t, err)
	assert.Len(t, chains, 2)

	chains, err = db.GetChains(ChainFilter{Status: "resolved"})
	require.NoError(t, err)
	assert.Len(t, chains, 1)
	assert.Equal(t, "Chain B", chains[0].Title)

	// With limit
	chains, err = db.GetChains(ChainFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, chains, 1)
}

func TestInsertAndGetChainRefs(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Test Chain", Slug: "test-chain", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	// Insert decision ref
	err = db.InsertChainRef(ChainRef{
		ChainID:     int(chainID),
		RefType:     "decision",
		DigestID:    10,
		DecisionIdx: 0,
		ChannelID:   "C1",
		Timestamp:   1500000,
	})
	require.NoError(t, err)

	// Insert track ref
	err = db.InsertChainRef(ChainRef{
		ChainID:   int(chainID),
		RefType:   "track",
		TrackID:   5,
		ChannelID: "C1",
		Timestamp: 1600000,
	})
	require.NoError(t, err)

	refs, err := db.GetChainRefs(int(chainID))
	require.NoError(t, err)
	require.Len(t, refs, 2)

	// Ordered by timestamp ASC
	assert.Equal(t, "decision", refs[0].RefType)
	assert.Equal(t, 10, refs[0].DigestID)
	assert.Equal(t, 0, refs[0].DecisionIdx)
	assert.Equal(t, 1500000.0, refs[0].Timestamp)

	assert.Equal(t, "track", refs[1].RefType)
	assert.Equal(t, 5, refs[1].TrackID)
	assert.Equal(t, 1600000.0, refs[1].Timestamp)
}

func TestInsertChainRef_IgnoreDuplicate(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	ref := ChainRef{
		ChainID:     int(chainID),
		RefType:     "decision",
		DigestID:    10,
		DecisionIdx: 0,
		ChannelID:   "C1",
		Timestamp:   1500000,
	}

	err = db.InsertChainRef(ref)
	require.NoError(t, err)

	// Insert same ref again — should be ignored (INSERT OR IGNORE)
	err = db.InsertChainRef(ref)
	require.NoError(t, err)

	refs, err := db.GetChainRefs(int(chainID))
	require.NoError(t, err)
	assert.Len(t, refs, 1)
}

func TestGetChainRefs_Empty(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Empty", Slug: "empty", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	refs, err := db.GetChainRefs(int(chainID))
	require.NoError(t, err)
	assert.Empty(t, refs)
}

func TestMarkStaleChains(t *testing.T) {
	db := openTestDB(t)

	// Active chain with old last_seen
	_, err := db.CreateChain(Chain{
		Title: "Old Chain", Slug: "old", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 1000000,
	})
	require.NoError(t, err)

	// Active chain with recent last_seen
	_, err = db.CreateChain(Chain{
		Title: "Recent Chain", Slug: "recent", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 9000000000,
	})
	require.NoError(t, err)

	// Already resolved
	_, err = db.CreateChain(Chain{
		Title: "Resolved", Slug: "resolved", Status: "resolved",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 1000000,
	})
	require.NoError(t, err)

	affected, err := db.MarkStaleChains(5000000000) // cutoff in the middle
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	// Verify
	chain, err := db.GetChains(ChainFilter{Status: "stale"})
	require.NoError(t, err)
	require.Len(t, chain, 1)
	assert.Equal(t, "Old Chain", chain[0].Title)
}

func TestGetChainItemCount(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	count, err := db.GetChainItemCount(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Add refs
	err = db.InsertChainRef(ChainRef{
		ChainID: int(chainID), RefType: "decision",
		DigestID: 1, DecisionIdx: 0, ChannelID: "C1", Timestamp: 1500000,
	})
	require.NoError(t, err)
	err = db.InsertChainRef(ChainRef{
		ChainID: int(chainID), RefType: "track",
		TrackID: 1, ChannelID: "C1", Timestamp: 1600000,
	})
	require.NoError(t, err)

	count, err = db.GetChainItemCount(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestIsDecisionChained(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: `[]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	// Not chained yet
	result := db.IsDecisionChained(10, 0)
	assert.Equal(t, 0, result)

	// Chain it
	err = db.InsertChainRef(ChainRef{
		ChainID: int(chainID), RefType: "decision",
		DigestID: 10, DecisionIdx: 0, ChannelID: "C1", Timestamp: 1500000,
	})
	require.NoError(t, err)

	result = db.IsDecisionChained(10, 0)
	assert.Equal(t, int(chainID), result)

	// Different decision_idx is not chained
	result = db.IsDecisionChained(10, 1)
	assert.Equal(t, 0, result)
}

func TestAddChannelToChain(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: `["C1"]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	// Add new channel
	err = db.AddChannelToChain(int(chainID), "C2")
	require.NoError(t, err)

	chain, err := db.GetChainByID(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, `["C1","C2"]`, chain.ChannelIDs)

	// Add duplicate — should be idempotent
	err = db.AddChannelToChain(int(chainID), "C1")
	require.NoError(t, err)

	chain, err = db.GetChainByID(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, `["C1","C2"]`, chain.ChannelIDs)
}

func TestAddChannelToChain_EmptyChannelIDs(t *testing.T) {
	db := openTestDB(t)

	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: ``, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)

	err = db.AddChannelToChain(int(chainID), "C1")
	require.NoError(t, err)

	chain, err := db.GetChainByID(int(chainID))
	require.NoError(t, err)
	assert.Equal(t, `["C1"]`, chain.ChannelIDs)
}

func TestGetUnlinkedDecisions(t *testing.T) {
	db := openTestDB(t)

	// Create a channel so the channel name lookup works
	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	// Insert a channel digest with decisions
	digestID, err := db.UpsertDigest(Digest{
		ChannelID:  "C1",
		Type:       "channel",
		PeriodFrom: 1000000,
		PeriodTo:   2000000,
		Summary:    "test digest",
		Decisions:  `[{"text":"use gRPC","by":"@alice","importance":"high"},{"text":"deploy Friday","by":"@bob","importance":"medium"}]`,
		Model:      "haiku",
	})
	require.NoError(t, err)

	// All decisions should be unlinked
	unlinked, err := db.GetUnlinkedDecisions(500000)
	require.NoError(t, err)
	assert.Len(t, unlinked, 2)
	assert.Equal(t, "use gRPC", unlinked[0].DecisionText)
	assert.Equal(t, "@alice", unlinked[0].DecisionBy)
	assert.Equal(t, "high", unlinked[0].Importance)
	assert.Equal(t, "general", unlinked[0].ChannelName)
	assert.Equal(t, "channel", unlinked[0].DigestType)

	// Link the first decision to a chain
	chainID, err := db.CreateChain(Chain{
		Title: "Test", Slug: "test", Status: "active",
		ChannelIDs: `["C1"]`, FirstSeen: 1000000, LastSeen: 2000000,
	})
	require.NoError(t, err)
	err = db.InsertChainRef(ChainRef{
		ChainID: int(chainID), RefType: "decision",
		DigestID: int(digestID), DecisionIdx: 0, ChannelID: "C1", Timestamp: 1500000,
	})
	require.NoError(t, err)

	// Now only one should be unlinked
	unlinked, err = db.GetUnlinkedDecisions(500000)
	require.NoError(t, err)
	assert.Len(t, unlinked, 1)
	assert.Equal(t, "deploy Friday", unlinked[0].DecisionText)
}

func TestGetUnlinkedDecisions_SkipsEmptyDecisions(t *testing.T) {
	db := openTestDB(t)

	// Digest with empty decisions
	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "no decisions", Decisions: "[]", Model: "haiku",
	})
	require.NoError(t, err)

	unlinked, err := db.GetUnlinkedDecisions(500000)
	require.NoError(t, err)
	assert.Empty(t, unlinked)
}

func TestGetTrackByID(t *testing.T) {
	db := openTestDB(t)

	id, err := db.UpsertTrack(Track{
		ChannelID:        "C1",
		AssigneeUserID:   "U1",
		Text:             "review PR",
		Status:           "inbox",
		Priority:         "high",
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
	})
	require.NoError(t, err)

	track, err := db.GetTrackByID(int(id))
	require.NoError(t, err)
	require.NotNil(t, track)
	assert.Equal(t, "C1", track.ChannelID)
	assert.Equal(t, "U1", track.AssigneeUserID)
	assert.Equal(t, "review PR", track.Text)
	assert.Equal(t, "inbox", track.Status)
	assert.Equal(t, "high", track.Priority)
	assert.Equal(t, "mine", track.Ownership) // default
}

func TestGetTrackByID_NotFound(t *testing.T) {
	db := openTestDB(t)

	_, err := db.GetTrackByID(999)
	assert.Error(t, err)
}
