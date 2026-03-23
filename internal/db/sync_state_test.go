package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSyncStateNotFound(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	state, err := db.GetSyncState("C999")
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestUpdateAndGetSyncState(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	state := SyncState{
		ChannelID:      "C001",
		LastSyncedTS:   "1700000000.000001",
		OldestSyncedTS: "1699000000.000001",
		Cursor:         "abc123",
		MessagesSynced: 42,
	}
	err = db.UpdateSyncState("C001", state)
	require.NoError(t, err)

	got, err := db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "C001", got.ChannelID)
	assert.Equal(t, "1700000000.000001", got.LastSyncedTS)
	assert.Equal(t, "1699000000.000001", got.OldestSyncedTS)
	assert.Equal(t, "abc123", got.Cursor)
	assert.Equal(t, 42, got.MessagesSynced)
	assert.False(t, got.IsInitialSyncComplete)
	assert.True(t, got.LastSyncAt.Valid)
}

func TestUpdateSyncStateUpsert(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Initial insert
	require.NoError(t, db.UpdateSyncState("C001", SyncState{
		LastSyncedTS:   "1700000000.000001",
		MessagesSynced: 10,
	}))

	// Update
	require.NoError(t, db.UpdateSyncState("C001", SyncState{
		LastSyncedTS:   "1700000500.000001",
		MessagesSynced: 50,
		Cursor:         "page2",
	}))

	got, err := db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "1700000500.000001", got.LastSyncedTS)
	assert.Equal(t, 50, got.MessagesSynced)
	assert.Equal(t, "page2", got.Cursor)
}

func TestUpdateSyncStateWithError(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpdateSyncState("C001", SyncState{
		Error: "channel_not_found",
	}))

	got, err := db.GetSyncState("C001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "channel_not_found", got.Error)
}
