package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDigestStats(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "test", Model: "haiku",
		MessageCount: 10, InputTokens: 100, OutputTokens: 50, CostUSD: 0,
	})
	require.NoError(t, err)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "test2", Model: "haiku",
		MessageCount: 20, InputTokens: 200, OutputTokens: 100, CostUSD: 0,
	})
	require.NoError(t, err)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "", Type: "daily",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "daily", Model: "haiku",
		MessageCount: 30, InputTokens: 300, OutputTokens: 150, CostUSD: 0,
	})
	require.NoError(t, err)

	// All digests
	stats, err := db.GetDigestStats(DigestFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, stats.TotalDigests)
	assert.Equal(t, 60, stats.TotalMessages)
	assert.Equal(t, 600, stats.InputTokens)
	assert.Equal(t, 300, stats.OutputTokens)
	assert.InDelta(t, 0.0, stats.CostUSD, 0.001)

	// Filter by type
	stats, err = db.GetDigestStats(DigestFilter{Type: "channel"})
	require.NoError(t, err)
	assert.Equal(t, 2, stats.TotalDigests)
	assert.Equal(t, 30, stats.TotalMessages)

	// Filter by channel
	stats, err = db.GetDigestStats(DigestFilter{ChannelID: "C1"})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.TotalDigests)
}

func TestGetDigestStats_Empty(t *testing.T) {
	db := openTestDB(t)

	stats, err := db.GetDigestStats(DigestFilter{})
	require.NoError(t, err)
	assert.Equal(t, 0, stats.TotalDigests)
	assert.Equal(t, 0, stats.TotalMessages)
}

func TestDeduplicateDailyDigests(t *testing.T) {
	db := openTestDB(t)

	// Insert two daily digests for the same day (different period_to)
	_, err := db.UpsertDigest(Digest{
		ChannelID: "", Type: "daily",
		PeriodFrom: 1000000, PeriodTo: 1086399,
		Summary: "first", Model: "haiku",
	})
	require.NoError(t, err)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "", Type: "daily",
		PeriodFrom: 1000000, PeriodTo: 1086400,
		Summary: "second (newer)", Model: "haiku",
	})
	require.NoError(t, err)

	// Channel digest — should not be affected
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "channel", Model: "haiku",
	})
	require.NoError(t, err)

	deleted, err := db.DeduplicateDailyDigests()
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted) // first daily should be removed

	digests, err := db.GetDigests(DigestFilter{Type: "daily"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "second (newer)", digests[0].Summary)

	// Channel digest untouched
	digests, err = db.GetDigests(DigestFilter{Type: "channel"})
	require.NoError(t, err)
	assert.Len(t, digests, 1)
}

func TestGetDigestDecisionsForChannel(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertChannel(Channel{ID: "C1", Name: "general", Type: "public"}))

	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary:   "test",
		Decisions: `[{"text":"use gRPC","by":"@alice","importance":"high"},{"text":"deploy Friday","by":"@bob","importance":"medium"}]`,
		Model:     "haiku",
	})
	require.NoError(t, err)

	decisions, err := db.GetDigestDecisionsForChannel("C1", 1000000, 2000000)
	require.NoError(t, err)
	assert.Len(t, decisions, 2)
	assert.Equal(t, "general", decisions[0].ChannelName)
	assert.Contains(t, decisions[0].Decision, "use gRPC")
	assert.Contains(t, decisions[0].Decision, "@alice")
	assert.Contains(t, decisions[0].Decision, "high")
}

func TestGetDigestDecisionsForChannel_NoDecisions(t *testing.T) {
	db := openTestDB(t)

	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 2000000,
		Summary: "test", Decisions: "[]", Model: "haiku",
	})
	require.NoError(t, err)

	decisions, err := db.GetDigestDecisionsForChannel("C1", 1000000, 2000000)
	require.NoError(t, err)
	assert.Empty(t, decisions)
}

func TestGetDigestStatsFilterByUnixRange(t *testing.T) {
	db := openTestDB(t)

	// Insert digests with different period ranges
	_, err := db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000, PeriodTo: 1500000,
		Summary: "early", Model: "haiku",
		MessageCount: 10, InputTokens: 100, OutputTokens: 50, CostUSD: 0,
	})
	require.NoError(t, err)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C2", Type: "channel",
		PeriodFrom: 2000000, PeriodTo: 2500000,
		Summary: "late", Model: "haiku",
		MessageCount: 20, InputTokens: 200, OutputTokens: 100, CostUSD: 0,
	})
	require.NoError(t, err)

	// Filter by FromUnix
	stats, err := db.GetDigestStats(DigestFilter{FromUnix: 1500000})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.TotalDigests)
	assert.Equal(t, 20, stats.TotalMessages)

	// Filter by ToUnix
	stats, err = db.GetDigestStats(DigestFilter{ToUnix: 1500000})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.TotalDigests)
	assert.Equal(t, 10, stats.TotalMessages)

	// Both FromUnix and ToUnix
	stats, err = db.GetDigestStats(DigestFilter{FromUnix: 900000, ToUnix: 2600000})
	require.NoError(t, err)
	assert.Equal(t, 2, stats.TotalDigests)
}

func TestGetDigestByID_NotFound(t *testing.T) {
	db := openTestDB(t)

	d, err := db.GetDigestByID(999)
	require.NoError(t, err)
	assert.Nil(t, d)
}
