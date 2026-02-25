package db

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatsEmpty(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	stats, err := database.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 0, stats.ChannelCount)
	assert.Equal(t, 0, stats.WatchedCount)
	assert.Equal(t, 0, stats.UserCount)
	assert.Equal(t, 0, stats.MessageCount)
	assert.Equal(t, 0, stats.ThreadCount)
}

func TestGetStatsWithData(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// Add users
	require.NoError(t, database.UpsertUser(User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(User{ID: "U002", Name: "bob"}))

	// Add channels
	require.NoError(t, database.UpsertChannel(Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertChannel(Channel{ID: "C002", Name: "random", Type: "public"}))
	require.NoError(t, database.UpsertChannel(Channel{ID: "C003", Name: "secret", Type: "private"}))

	// Add watch items
	require.NoError(t, database.AddWatch("channel", "C001", "general", "high"))
	require.NoError(t, database.AddWatch("user", "U001", "alice", "normal"))

	// Add messages (some with threads)
	require.NoError(t, database.UpsertMessage(Message{ChannelID: "C001", TS: "1000000000.000001", UserID: "U001", Text: "hello"}))
	require.NoError(t, database.UpsertMessage(Message{ChannelID: "C001", TS: "1000000000.000002", UserID: "U002", Text: "thread start", ReplyCount: 3}))
	require.NoError(t, database.UpsertMessage(Message{
		ChannelID: "C001", TS: "1000000000.000003", UserID: "U001", Text: "reply",
		ThreadTS: sql.NullString{String: "1000000000.000002", Valid: true},
	}))
	require.NoError(t, database.UpsertMessage(Message{ChannelID: "C002", TS: "1000000000.000004", UserID: "U001", Text: "in random", ReplyCount: 1}))

	stats, err := database.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats.ChannelCount)
	assert.Equal(t, 2, stats.WatchedCount)
	assert.Equal(t, 2, stats.UserCount)
	assert.Equal(t, 4, stats.MessageCount)
	assert.Equal(t, 2, stats.ThreadCount) // messages with reply_count > 0
}

func TestLastSyncTimeEmpty(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	lastSync, err := database.LastSyncTime()
	require.NoError(t, err)
	assert.Equal(t, "", lastSync)
}

func TestLastSyncTimeWithData(t *testing.T) {
	database, err := Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpdateSyncState("C001", SyncState{
		ChannelID:    "C001",
		LastSyncedTS: "1000000000.000001",
	}))
	require.NoError(t, database.UpdateSyncState("C002", SyncState{
		ChannelID:    "C002",
		LastSyncedTS: "1000000001.000001",
	}))

	lastSync, err := database.LastSyncTime()
	require.NoError(t, err)
	assert.NotEmpty(t, lastSync)
}
