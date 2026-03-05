package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertDigest(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	d := Digest{
		ChannelID:    "C123",
		Type:         "channel",
		PeriodFrom:   1000000.0,
		PeriodTo:     2000000.0,
		Summary:      "Team discussed deployment plans",
		Topics:       `["deployment","testing"]`,
		Decisions:    `[{"text":"deploy Friday","by":"@alice"}]`,
		ActionItems:  `[{"text":"write tests","assignee":"@bob"}]`,
		MessageCount: 42,
		Model:        "haiku",
	}

	id, err := db.UpsertDigest(d)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	// Verify stored
	got, err := db.GetDigestByID(int(id))
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "C123", got.ChannelID)
	assert.Equal(t, "channel", got.Type)
	assert.Equal(t, 1000000.0, got.PeriodFrom)
	assert.Equal(t, 2000000.0, got.PeriodTo)
	assert.Equal(t, "Team discussed deployment plans", got.Summary)
	assert.Equal(t, `["deployment","testing"]`, got.Topics)
	assert.Equal(t, 42, got.MessageCount)
	assert.Equal(t, "haiku", got.Model)
}

func TestUpsertDigestReplacesExisting(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	d := Digest{
		ChannelID:  "C123",
		Type:       "channel",
		PeriodFrom: 1000000.0,
		PeriodTo:   2000000.0,
		Summary:    "v1",
		Model:      "haiku",
	}
	_, err = db.UpsertDigest(d)
	require.NoError(t, err)

	d.Summary = "v2"
	d.MessageCount = 10
	_, err = db.UpsertDigest(d)
	require.NoError(t, err)

	// Should only be one digest
	digests, err := db.GetDigests(DigestFilter{ChannelID: "C123"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "v2", digests[0].Summary)
	assert.Equal(t, 10, digests[0].MessageCount)
}

func TestGetDigestsFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert channel digests
	for _, ch := range []string{"C1", "C2"} {
		_, err := db.UpsertDigest(Digest{
			ChannelID:  ch,
			Type:       "channel",
			PeriodFrom: 1000000.0,
			PeriodTo:   2000000.0,
			Summary:    "channel digest " + ch,
			Model:      "haiku",
		})
		require.NoError(t, err)
	}

	// Insert daily digest
	_, err = db.UpsertDigest(Digest{
		ChannelID:  "",
		Type:       "daily",
		PeriodFrom: 1000000.0,
		PeriodTo:   2000000.0,
		Summary:    "daily digest",
		Model:      "haiku",
	})
	require.NoError(t, err)

	// Filter by channel
	digests, err := db.GetDigests(DigestFilter{ChannelID: "C1"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "C1", digests[0].ChannelID)

	// Filter by type
	digests, err = db.GetDigests(DigestFilter{Type: "channel"})
	require.NoError(t, err)
	assert.Len(t, digests, 2)

	digests, err = db.GetDigests(DigestFilter{Type: "daily"})
	require.NoError(t, err)
	assert.Len(t, digests, 1)

	// All digests
	digests, err = db.GetDigests(DigestFilter{})
	require.NoError(t, err)
	assert.Len(t, digests, 3)

	// With limit
	digests, err = db.GetDigests(DigestFilter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, digests, 2)
}

func TestGetDigestsTimeFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000.0, PeriodTo: 2000000.0,
		Summary: "early", Model: "haiku",
	})
	require.NoError(t, err)

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 3000000.0, PeriodTo: 4000000.0,
		Summary: "late", Model: "haiku",
	})
	require.NoError(t, err)

	// Filter from >= 2500000
	digests, err := db.GetDigests(DigestFilter{FromUnix: 2500000.0})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "late", digests[0].Summary)

	// Filter to <= 3000000
	digests, err = db.GetDigests(DigestFilter{ToUnix: 3000000.0})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "early", digests[0].Summary)
}

func TestGetLatestDigest(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// No digests yet
	d, err := db.GetLatestDigest("C1", "channel")
	require.NoError(t, err)
	assert.Nil(t, d)

	// Insert two digests for same channel
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000.0, PeriodTo: 2000000.0,
		Summary: "older", Model: "haiku",
	})
	require.NoError(t, err)

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 3000000.0, PeriodTo: 4000000.0,
		Summary: "newer", Model: "haiku",
	})
	require.NoError(t, err)

	d, err = db.GetLatestDigest("C1", "channel")
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, "newer", d.Summary)
	assert.Equal(t, 4000000.0, d.PeriodTo)
}

func TestDeleteDigestsOlderThan(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 1000000.0, PeriodTo: 2000000.0,
		Summary: "old", Model: "haiku",
	})
	require.NoError(t, err)

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C1", Type: "channel",
		PeriodFrom: 5000000.0, PeriodTo: 6000000.0,
		Summary: "new", Model: "haiku",
	})
	require.NoError(t, err)

	deleted, err := db.DeleteDigestsOlderThan(3000000.0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	digests, err := db.GetDigests(DigestFilter{})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.Equal(t, "new", digests[0].Summary)
}

func TestChannelsWithNewMessages(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Insert messages in two channels
	_, err = db.Exec("INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '1000000.000001', 'U1', 'old')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C1', '2000000.000001', 'U1', 'new')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO messages (channel_id, ts, user_id, text) VALUES ('C2', '3000000.000001', 'U1', 'newest')")
	require.NoError(t, err)

	// Since 1500000 -> C1 (has msg at 2000000) and C2 (has msg at 3000000)
	channels, err := db.ChannelsWithNewMessages(1500000.0)
	require.NoError(t, err)
	assert.Equal(t, []string{"C1", "C2"}, channels)

	// Since 2500000 -> only C2
	channels, err = db.ChannelsWithNewMessages(2500000.0)
	require.NoError(t, err)
	assert.Equal(t, []string{"C2"}, channels)

	// Since 4000000 -> none
	channels, err = db.ChannelsWithNewMessages(4000000.0)
	require.NoError(t, err)
	assert.Nil(t, channels)
}

func TestDigestTypeConstraint(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Valid types
	for _, typ := range []string{"channel", "daily", "weekly"} {
		_, err := db.UpsertDigest(Digest{
			ChannelID: "C_" + typ, Type: typ,
			PeriodFrom: 1000000.0, PeriodTo: 2000000.0,
			Summary: "test", Model: "haiku",
		})
		require.NoError(t, err, "type %q should be valid", typ)
	}

	// Invalid type
	_, err = db.Exec(`INSERT INTO digests (channel_id, type, period_from, period_to, summary)
		VALUES ('C1', 'invalid', 1000000.0, 2000000.0, 'test')`)
	assert.Error(t, err)
}
