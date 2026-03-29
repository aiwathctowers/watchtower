package db

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertChannel(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ch := Channel{
		ID:         "C001",
		Name:       "general",
		Type:       "public",
		Topic:      "General discussion",
		Purpose:    "Company-wide announcements",
		IsArchived: false,
		IsMember:   true,
		NumMembers: 150,
	}
	err = db.UpsertChannel(ch)
	require.NoError(t, err)

	got, err := db.GetChannelByID("C001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "general", got.Name)
	assert.Equal(t, "public", got.Type)
	assert.Equal(t, "General discussion", got.Topic)
	assert.True(t, got.IsMember)
	assert.Equal(t, 150, got.NumMembers)
	assert.NotEmpty(t, got.UpdatedAt)
}

func TestUpsertChannelUpdate(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ch := Channel{ID: "C001", Name: "general", Type: "public", NumMembers: 100}
	require.NoError(t, db.UpsertChannel(ch))

	// Update
	ch.Topic = "Updated topic"
	ch.NumMembers = 200
	require.NoError(t, db.UpsertChannel(ch))

	got, err := db.GetChannelByID("C001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated topic", got.Topic)
	assert.Equal(t, 200, got.NumMembers)
}

func TestUpsertChannelWithDMUserID(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ch := Channel{
		ID:       "D001",
		Name:     "dm-alice",
		Type:     "dm",
		DMUserID: sql.NullString{String: "U001", Valid: true},
	}
	require.NoError(t, db.UpsertChannel(ch))

	got, err := db.GetChannelByID("D001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.DMUserID.Valid)
	assert.Equal(t, "U001", got.DMUserID.String)
}

func TestGetChannelByNameNotFound(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	got, err := db.GetChannelByName("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetChannelByIDNotFound(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	got, err := db.GetChannelByID("C999")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetChannelByName(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "random", Type: "public"}))

	got, err := db.GetChannelByName("random")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "C002", got.ID)
}

func TestGetChannelsNoFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "random", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C003", Name: "secret", Type: "private"}))

	channels, err := db.GetChannels(ChannelFilter{})
	require.NoError(t, err)
	assert.Len(t, channels, 3)
	// Should be sorted by name
	assert.Equal(t, "general", channels[0].Name)
	assert.Equal(t, "random", channels[1].Name)
	assert.Equal(t, "secret", channels[2].Name)
}

func TestGetChannelsFilterByType(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "secret", Type: "private"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "D001", Name: "dm-alice", Type: "dm"}))

	channels, err := db.GetChannels(ChannelFilter{Type: "public"})
	require.NoError(t, err)
	assert.Len(t, channels, 1)
	assert.Equal(t, "general", channels[0].Name)
}

func TestGetChannelsFilterByArchived(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "active", Type: "public", IsArchived: false}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "old", Type: "public", IsArchived: true}))

	f := false
	channels, err := db.GetChannels(ChannelFilter{IsArchived: &f})
	require.NoError(t, err)
	assert.Len(t, channels, 1)
	assert.Equal(t, "active", channels[0].Name)
}

func TestGetChannelsFilterByMember(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "mine", Type: "public", IsMember: true}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "notmine", Type: "public", IsMember: false}))

	tr := true
	channels, err := db.GetChannels(ChannelFilter{IsMember: &tr})
	require.NoError(t, err)
	assert.Len(t, channels, 1)
	assert.Equal(t, "mine", channels[0].Name)
}

func TestGetChannelsCombinedFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "pub-member", Type: "public", IsMember: true}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "pub-not", Type: "public", IsMember: false}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C003", Name: "priv-member", Type: "private", IsMember: true}))

	tr := true
	channels, err := db.GetChannels(ChannelFilter{Type: "public", IsMember: &tr})
	require.NoError(t, err)
	assert.Len(t, channels, 1)
	assert.Equal(t, "pub-member", channels[0].Name)
}

func TestGetChannelListBasic(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public", NumMembers: 50}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "random", Type: "public", NumMembers: 30}))

	items, err := db.GetChannelList(ChannelFilter{}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "general", items[0].Name)
	assert.Equal(t, "random", items[1].Name)
	// No messages, so counts should be 0
	assert.Equal(t, 0, items[0].MessageCount)
	assert.False(t, items[0].LastActivity.Valid)
	assert.False(t, items[0].IsWatched)
}

func TestGetChannelListWithMessages(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "random", Type: "public"}))

	// Add messages to general
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000000.000001", Text: "hello"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000100.000001", Text: "world"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000200.000001", Text: "!"}))

	// Add 1 message to random
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000050.000001", Text: "hi"}))

	items, err := db.GetChannelList(ChannelFilter{}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, 3, items[0].MessageCount) // general
	assert.Equal(t, 1, items[1].MessageCount) // random
	assert.True(t, items[0].LastActivity.Valid)
	assert.InDelta(t, 1700000200.0, items[0].LastActivity.Float64, 1.0)
}

func TestGetChannelListSortByMessages(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "few-msgs", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "many-msgs", Type: "public"}))

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000000.000001", Text: "one"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000000.000001", Text: "one"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000001.000001", Text: "two"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700000002.000001", Text: "three"}))

	items, err := db.GetChannelList(ChannelFilter{}, ChannelSortMessages)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "many-msgs", items[0].Name)
	assert.Equal(t, 3, items[0].MessageCount)
}

func TestGetChannelListSortByRecent(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "old-activity", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "new-activity", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C003", Name: "no-activity", Type: "public"}))

	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C001", TS: "1700000000.000001", Text: "old"}))
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C002", TS: "1700099999.000001", Text: "new"}))

	items, err := db.GetChannelList(ChannelFilter{}, ChannelSortRecent)
	require.NoError(t, err)
	assert.Len(t, items, 3)
	assert.Equal(t, "new-activity", items[0].Name)
	assert.Equal(t, "old-activity", items[1].Name)
	assert.Equal(t, "no-activity", items[2].Name) // null last, sorted by name
}

func TestGetChannelListWithFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "secret", Type: "private"}))

	items, err := db.GetChannelList(ChannelFilter{Type: "private"}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "secret", items[0].Name)
}

func TestGetChannelListWithArchivedFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "active-ch", Type: "public", IsArchived: false}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "archived-ch", Type: "public", IsArchived: true}))

	// Filter for non-archived
	f := false
	items, err := db.GetChannelList(ChannelFilter{IsArchived: &f}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "active-ch", items[0].Name)

	// Filter for archived
	tr := true
	items, err = db.GetChannelList(ChannelFilter{IsArchived: &tr}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "archived-ch", items[0].Name)
}

func TestGetChannelListWithMemberFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "joined", Type: "public", IsMember: true}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "not-joined", Type: "public", IsMember: false}))

	// Members only
	tr := true
	items, err := db.GetChannelList(ChannelFilter{IsMember: &tr}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "joined", items[0].Name)

	// Non-members only
	f := false
	items, err = db.GetChannelList(ChannelFilter{IsMember: &f}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, "not-joined", items[0].Name)
}

func TestUpdateChannelLastRead(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))

	// Set last_read
	require.NoError(t, db.UpdateChannelLastRead("C001", "1700000100.000001"))
	got, err := db.GetChannelByID("C001")
	require.NoError(t, err)
	assert.Equal(t, "1700000100.000001", got.LastRead)

	// Only advances forward
	require.NoError(t, db.UpdateChannelLastRead("C001", "1700000050.000001"))
	got, err = db.GetChannelByID("C001")
	require.NoError(t, err)
	assert.Equal(t, "1700000100.000001", got.LastRead)

	// Advances to newer
	require.NoError(t, db.UpdateChannelLastRead("C001", "1700000200.000001"))
	got, err = db.GetChannelByID("C001")
	require.NoError(t, err)
	assert.Equal(t, "1700000200.000001", got.LastRead)
}

func TestUpsertChannelLastReadPreserved(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Upsert with last_read
	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public", LastRead: "1700000100.000001"}))
	got, err := db.GetChannelByID("C001")
	require.NoError(t, err)
	assert.Equal(t, "1700000100.000001", got.LastRead)

	// Upsert without last_read should preserve existing
	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	got, err = db.GetChannelByID("C001")
	require.NoError(t, err)
	assert.Equal(t, "1700000100.000001", got.LastRead)
}

func TestAutoMarkReadFromSlack(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Setup: channel with last_read at ts 1700000300
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C001", Name: "general", Type: "public",
		LastRead: "1700000300.000000",
	}))
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C002", Name: "random", Type: "public",
		LastRead: "1700000100.000000",
	}))

	// Channel digest for C001 with period_to = 1700000200 — should be marked read
	// (last_read 1700000300 >= period_to 1700000200)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C001", Type: "channel",
		PeriodFrom: 1700000000, PeriodTo: 1700000200,
		Summary: "digest 1",
	})
	require.NoError(t, err)

	// Channel digest for C001 with period_to = 1700000400 — should NOT be marked read
	// (last_read 1700000300 < period_to 1700000400)
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C001", Type: "channel",
		PeriodFrom: 1700000200, PeriodTo: 1700000400,
		Summary: "digest 2",
	})
	require.NoError(t, err)

	// Channel digest for C002 with period_to = 1700000050 — should be marked read
	_, err = db.UpsertDigest(Digest{
		ChannelID: "C002", Type: "channel",
		PeriodFrom: 1700000000, PeriodTo: 1700000050,
		Summary: "digest 3",
	})
	require.NoError(t, err)

	// Daily rollup covering both channel digests' period
	_, err = db.UpsertDigest(Digest{
		ChannelID: "", Type: "daily",
		PeriodFrom: 1700000000, PeriodTo: 1700000400,
		Summary: "daily rollup",
	})
	require.NoError(t, err)

	// Run auto-mark
	digestsMarked, _, err := db.AutoMarkReadFromSlack()
	require.NoError(t, err)
	assert.Equal(t, int64(2), digestsMarked) // 2 channel digests marked

	// Verify: digest 1 (C001, period_to=200) should be read
	digests, err := db.GetDigests(DigestFilter{ChannelID: "C001", Type: "channel"})
	require.NoError(t, err)
	require.Len(t, digests, 2)
	// Newest first
	assert.False(t, digests[0].ReadAt.Valid, "digest with period_to=400 should be unread")
	assert.True(t, digests[1].ReadAt.Valid, "digest with period_to=200 should be read")

	// Verify: digest 3 (C002, period_to=50) should be read
	digests, err = db.GetDigests(DigestFilter{ChannelID: "C002", Type: "channel"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.True(t, digests[0].ReadAt.Valid, "digest for C002 should be read")

	// Verify: daily rollup should NOT be read (C001 digest 2 is still unread)
	digests, err = db.GetDigests(DigestFilter{Type: "daily"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.False(t, digests[0].ReadAt.Valid, "daily rollup should be unread since not all channel digests are read")
}

func TestAutoMarkReadFromSlack_DailyMarkedWhenAllChannelDigestsRead(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// One channel, all read in Slack
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C001", Name: "general", Type: "public",
		LastRead: "1700000500.000000",
	}))

	_, err = db.UpsertDigest(Digest{
		ChannelID: "C001", Type: "channel",
		PeriodFrom: 1700000000, PeriodTo: 1700000200,
		Summary: "ch digest",
	})
	require.NoError(t, err)

	_, err = db.UpsertDigest(Digest{
		ChannelID: "", Type: "daily",
		PeriodFrom: 1700000000, PeriodTo: 1700000200,
		Summary: "daily",
	})
	require.NoError(t, err)

	digestsMarked, _, err := db.AutoMarkReadFromSlack()
	require.NoError(t, err)
	// 1 channel digest + 1 daily = 2
	assert.Equal(t, int64(2), digestsMarked)

	digests, err := db.GetDigests(DigestFilter{Type: "daily"})
	require.NoError(t, err)
	require.Len(t, digests, 1)
	assert.True(t, digests[0].ReadAt.Valid, "daily should be marked read when all channel digests are read")
}

func TestAutoMarkReadFromSlack_TracksMarkedWhenAllDigestsRead(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Channel with last_read covering everything.
	require.NoError(t, db.UpsertChannel(Channel{
		ID: "C001", Name: "general", Type: "public",
		LastRead: "1700000500.000000",
	}))

	// Two channel digests — both will be auto-marked read (last_read=500 >= period_to).
	d1ID, err := db.UpsertDigest(Digest{
		ChannelID: "C001", Type: "channel",
		PeriodFrom: 1700000000, PeriodTo: 1700000200, Summary: "d1",
	})
	require.NoError(t, err)
	d2ID, err := db.UpsertDigest(Digest{
		ChannelID: "C001", Type: "channel",
		PeriodFrom: 1700000200, PeriodTo: 1700000400, Summary: "d2",
	})
	require.NoError(t, err)

	// Track linked to both digests — should be auto-marked read.
	allReadTrackID, err := db.UpsertTrack(Track{
		Text: "all digests read", Priority: "medium",
		RelatedDigestIDs: fmt.Sprintf("[%d,%d]", d1ID, d2ID),
	})
	require.NoError(t, err)
	require.NoError(t, db.SetTrackHasUpdates(int(allReadTrackID)))

	// Third digest with period_to beyond last_read — will NOT be marked read.
	d3ID, err := db.UpsertDigest(Digest{
		ChannelID: "C001", Type: "channel",
		PeriodFrom: 1700000400, PeriodTo: 1700000600, Summary: "d3",
	})
	require.NoError(t, err)

	// Track linked to d1 (read) and d3 (unread) — should NOT be auto-marked.
	partialTrackID, err := db.UpsertTrack(Track{
		Text: "partial digests read", Priority: "medium",
		RelatedDigestIDs: fmt.Sprintf("[%d,%d]", d1ID, d3ID),
	})
	require.NoError(t, err)
	require.NoError(t, db.SetTrackHasUpdates(int(partialTrackID)))

	// Track with no updates (has_updates=0) — should be left alone.
	noUpdatesTrackID, err := db.UpsertTrack(Track{
		Text: "no updates", Priority: "medium",
		RelatedDigestIDs: fmt.Sprintf("[%d]", d1ID),
	})
	require.NoError(t, err)
	// has_updates defaults to 0, don't set it

	digestsMarked, tracksMarked, err := db.AutoMarkReadFromSlack()
	require.NoError(t, err)
	assert.Equal(t, int64(2), digestsMarked) // d1 and d2
	assert.Equal(t, int64(1), tracksMarked)  // only allReadTrack

	// Verify: allReadTrack should be read (has_updates=0, read_at set).
	track, err := db.GetTrackByID(int(allReadTrackID))
	require.NoError(t, err)
	assert.False(t, track.HasUpdates, "track with all digests read should have has_updates=0")
	assert.NotEmpty(t, track.ReadAt, "track with all digests read should have read_at set")

	// Verify: partialTrack should still have updates.
	track, err = db.GetTrackByID(int(partialTrackID))
	require.NoError(t, err)
	assert.True(t, track.HasUpdates, "track with unread digest should still have has_updates=1")

	// Verify: noUpdatesTrack should be unchanged (has_updates was already 0).
	track, err = db.GetTrackByID(int(noUpdatesTrackID))
	require.NoError(t, err)
	assert.False(t, track.HasUpdates)
}

func TestGetChannelListWatchedStatus(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertChannel(Channel{ID: "C001", Name: "watched-ch", Type: "public"}))
	require.NoError(t, db.UpsertChannel(Channel{ID: "C002", Name: "unwatched-ch", Type: "public"}))
	require.NoError(t, db.AddWatch("channel", "C001", "watched-ch", "high"))

	items, err := db.GetChannelList(ChannelFilter{}, ChannelSortName)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	// Alphabetical: unwatched-ch before watched-ch
	assert.Equal(t, "unwatched-ch", items[0].Name)
	assert.False(t, items[0].IsWatched)
	assert.Equal(t, "watched-ch", items[1].Name)
	assert.True(t, items[1].IsWatched)
}
