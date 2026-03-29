package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ensureChannels creates minimal channel records for FK compliance.
func ensureChannels(t *testing.T, db *DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		require.NoError(t, db.UpsertChannel(Channel{ID: id, Name: id, Type: "public"}))
	}
}

func TestToggleMuteForLLM(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1")

	// First toggle: sets to true (creates row)
	val, err := db.ToggleMuteForLLM("C1")
	require.NoError(t, err)
	assert.True(t, val)

	// Second toggle: sets to false
	val, err = db.ToggleMuteForLLM("C1")
	require.NoError(t, err)
	assert.False(t, val)

	// Third toggle: back to true
	val, err = db.ToggleMuteForLLM("C1")
	require.NoError(t, err)
	assert.True(t, val)
}

func TestToggleFavorite(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1")

	val, err := db.ToggleFavorite("C1")
	require.NoError(t, err)
	assert.True(t, val)

	val, err = db.ToggleFavorite("C1")
	require.NoError(t, err)
	assert.False(t, val)
}

func TestSetMuteForLLM(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1")

	require.NoError(t, db.SetMuteForLLM("C1", true))
	cs, err := db.GetChannelSettings("C1")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.True(t, cs.IsMutedForLLM)

	require.NoError(t, db.SetMuteForLLM("C1", false))
	cs, err = db.GetChannelSettings("C1")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.False(t, cs.IsMutedForLLM)
}

func TestSetFavorite(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1")

	require.NoError(t, db.SetFavorite("C1", true))
	cs, err := db.GetChannelSettings("C1")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.True(t, cs.IsFavorite)

	require.NoError(t, db.SetFavorite("C1", false))
	cs, err = db.GetChannelSettings("C1")
	require.NoError(t, err)
	assert.False(t, cs.IsFavorite)
}

func TestGetChannelSettings_NotFound(t *testing.T) {
	db := openTestDB(t)

	cs, err := db.GetChannelSettings("NONEXISTENT")
	require.NoError(t, err)
	assert.Nil(t, cs)
}

func TestGetAllChannelSettings(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1", "C2")

	require.NoError(t, db.SetMuteForLLM("C1", true))
	require.NoError(t, db.SetFavorite("C2", true))

	all, err := db.GetAllChannelSettings()
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestGetMutedChannelIDs(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1", "C2", "C3")

	// No muted channels
	ids, err := db.GetMutedChannelIDs()
	require.NoError(t, err)
	assert.Empty(t, ids)

	// Mute some channels
	require.NoError(t, db.SetMuteForLLM("C1", true))
	require.NoError(t, db.SetMuteForLLM("C2", true))
	require.NoError(t, db.SetFavorite("C3", true)) // not muted, just favorite

	ids, err = db.GetMutedChannelIDs()
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "C1")
	assert.Contains(t, ids, "C2")
}

func TestToggleMutePreservesFavorite(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1")

	// Set favorite first
	require.NoError(t, db.SetFavorite("C1", true))

	// Toggle mute should not affect favorite
	_, err := db.ToggleMuteForLLM("C1")
	require.NoError(t, err)

	cs, err := db.GetChannelSettings("C1")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.True(t, cs.IsMutedForLLM)
	assert.True(t, cs.IsFavorite) // preserved
}

func TestToggleFavoritePreservesMute(t *testing.T) {
	db := openTestDB(t)
	ensureChannels(t, db, "C1")

	require.NoError(t, db.SetMuteForLLM("C1", true))

	_, err := db.ToggleFavorite("C1")
	require.NoError(t, err)

	cs, err := db.GetChannelSettings("C1")
	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.True(t, cs.IsFavorite)
	assert.True(t, cs.IsMutedForLLM) // preserved
}
