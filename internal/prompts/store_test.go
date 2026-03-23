package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

func TestSeed(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	err := store.Seed()
	require.NoError(t, err)

	// All defaults should be in DB
	all, err := database.GetAllPrompts()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))

	// Calling Seed again should be idempotent
	err = store.Seed()
	require.NoError(t, err)
}

func TestGetFromDB(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
	assert.Contains(t, tmpl, "analyzing Slack messages")
}

func TestGetFallbackToDefault(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	// Don't seed — should fall back to built-in default

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, 0, version) // 0 = from default, not DB
	assert.Contains(t, tmpl, "analyzing Slack messages")
}

func TestGetUnknown(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	_, _, err := store.Get("nonexistent.prompt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown prompt")
}

func TestUpdate(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()

	err := store.Update(DigestChannel, "custom prompt text", "manual edit")
	require.NoError(t, err)

	tmpl, version, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Equal(t, "custom prompt text", tmpl)
	assert.Equal(t, 2, version)
}

func TestReset(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "custom text", "test")

	err := store.Reset(DigestChannel)
	require.NoError(t, err)

	tmpl, _, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Contains(t, tmpl, "analyzing Slack messages") // back to default
}

func TestResetUnknown(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)

	err := store.Reset("nonexistent")
	assert.Error(t, err)
}

func TestRollback(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "v2 text", "tune")
	_ = store.Update(DigestChannel, "v3 text", "tune")

	err := store.Rollback(DigestChannel, 1)
	require.NoError(t, err)

	tmpl, _, err := store.Get(DigestChannel)
	require.NoError(t, err)
	assert.Contains(t, tmpl, "analyzing Slack messages") // original v1
}

func TestHistory(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	_ = store.Seed()
	_ = store.Update(DigestChannel, "v2", "manual")

	history, err := store.History(DigestChannel)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, 2, history[0].Version)
	assert.Equal(t, "manual", history[0].Reason)
	assert.Equal(t, 1, history[1].Version)
}

func TestGetAll(t *testing.T) {
	database := openTestDB(t)
	store := New(database, nil)
	// Don't seed — GetAll should still return defaults

	all, err := store.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))

	// Seed and verify
	_ = store.Seed()
	all, err = store.GetAll()
	require.NoError(t, err)
	assert.Len(t, all, len(Defaults))
}

func TestDefaultsMatchPromptIDs(t *testing.T) {
	// Verify all AllIDs have defaults
	for _, id := range AllIDs {
		_, ok := Defaults[id]
		assert.True(t, ok, "prompt %q should have a default", id)
	}
	// Verify all AllIDs have descriptions
	for _, id := range AllIDs {
		_, ok := Descriptions[id]
		assert.True(t, ok, "prompt %q should have a description", id)
	}
}
