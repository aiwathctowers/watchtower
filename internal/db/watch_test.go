package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddWatch(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = db.AddWatch("channel", "C001", "general", "high")
	require.NoError(t, err)

	items, err := db.GetWatchList()
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "channel", items[0].EntityType)
	assert.Equal(t, "C001", items[0].EntityID)
	assert.Equal(t, "general", items[0].EntityName)
	assert.Equal(t, "high", items[0].Priority)
	assert.NotEmpty(t, items[0].CreatedAt)
}

func TestAddWatchUpdate(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.AddWatch("channel", "C001", "general", "normal"))

	// Update priority
	require.NoError(t, db.AddWatch("channel", "C001", "general", "high"))

	items, err := db.GetWatchList()
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "high", items[0].Priority)
}

func TestAddWatchInvalidType(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = db.AddWatch("invalid", "X001", "foo", "normal")
	assert.Error(t, err)
}

func TestAddWatchInvalidPriority(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = db.AddWatch("channel", "C001", "general", "urgent")
	assert.Error(t, err)
}

func TestRemoveWatch(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.AddWatch("channel", "C001", "general", "high"))

	err = db.RemoveWatch("channel", "C001")
	require.NoError(t, err)

	items, err := db.GetWatchList()
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestRemoveWatchNotFound(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = db.RemoveWatch("channel", "C999")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no watch entry found")
}

func TestGetWatchListOrdering(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.AddWatch("channel", "C003", "low-chan", "low"))
	require.NoError(t, db.AddWatch("user", "U001", "alice", "high"))
	require.NoError(t, db.AddWatch("channel", "C001", "general", "high"))
	require.NoError(t, db.AddWatch("user", "U002", "bob", "normal"))

	items, err := db.GetWatchList()
	require.NoError(t, err)
	require.Len(t, items, 4)

	// High priority first, then by type and name
	assert.Equal(t, "high", items[0].Priority)
	assert.Equal(t, "high", items[1].Priority)
	assert.Equal(t, "normal", items[2].Priority)
	assert.Equal(t, "low", items[3].Priority)
}

func TestGetWatchListEmpty(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	items, err := db.GetWatchList()
	require.NoError(t, err)
	assert.Empty(t, items)
}
