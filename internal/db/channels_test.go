package db

import (
	"database/sql"
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
