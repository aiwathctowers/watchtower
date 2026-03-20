package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertCustomEmoji(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertCustomEmoji(CustomEmoji{
		Name: "thumbsup",
		URL:  "https://example.com/thumbsup.png",
	})
	require.NoError(t, err)

	emojis, err := db.GetCustomEmojis()
	require.NoError(t, err)
	require.Len(t, emojis, 1)
	assert.Equal(t, "thumbsup", emojis[0].Name)
	assert.Equal(t, "https://example.com/thumbsup.png", emojis[0].URL)
	assert.Equal(t, "", emojis[0].AliasFor)
}

func TestUpsertCustomEmoji_Update(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertCustomEmoji(CustomEmoji{
		Name: "thumbsup",
		URL:  "https://example.com/old.png",
	})
	require.NoError(t, err)

	// Update URL
	err = db.UpsertCustomEmoji(CustomEmoji{
		Name: "thumbsup",
		URL:  "https://example.com/new.png",
	})
	require.NoError(t, err)

	emojis, err := db.GetCustomEmojis()
	require.NoError(t, err)
	require.Len(t, emojis, 1)
	assert.Equal(t, "https://example.com/new.png", emojis[0].URL)
}

func TestUpsertCustomEmoji_WithAlias(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertCustomEmoji(CustomEmoji{
		Name:     "like",
		URL:      "",
		AliasFor: "thumbsup",
	})
	require.NoError(t, err)

	emojis, err := db.GetCustomEmojis()
	require.NoError(t, err)
	require.Len(t, emojis, 1)
	assert.Equal(t, "thumbsup", emojis[0].AliasFor)
}

func TestBulkUpsertCustomEmojis(t *testing.T) {
	db := openTestDB(t)

	// Insert initial emojis
	err := db.UpsertCustomEmoji(CustomEmoji{Name: "old_emoji", URL: "https://old.png"})
	require.NoError(t, err)

	// Bulk upsert replaces everything
	err = db.BulkUpsertCustomEmojis([]CustomEmoji{
		{Name: "emoji1", URL: "https://emoji1.png"},
		{Name: "emoji2", URL: "https://emoji2.png"},
		{Name: "emoji3", URL: "https://emoji3.png"},
	})
	require.NoError(t, err)

	emojis, err := db.GetCustomEmojis()
	require.NoError(t, err)
	assert.Len(t, emojis, 3)
	// Ordered by name
	assert.Equal(t, "emoji1", emojis[0].Name)
	assert.Equal(t, "emoji2", emojis[1].Name)
	assert.Equal(t, "emoji3", emojis[2].Name)
}

func TestBulkUpsertCustomEmojis_Empty(t *testing.T) {
	db := openTestDB(t)

	// Insert an emoji first
	err := db.UpsertCustomEmoji(CustomEmoji{Name: "existing", URL: "https://existing.png"})
	require.NoError(t, err)

	// Bulk upsert with empty list clears all
	err = db.BulkUpsertCustomEmojis([]CustomEmoji{})
	require.NoError(t, err)

	emojis, err := db.GetCustomEmojis()
	require.NoError(t, err)
	assert.Empty(t, emojis)
}

func TestGetCustomEmojis_Empty(t *testing.T) {
	db := openTestDB(t)

	emojis, err := db.GetCustomEmojis()
	require.NoError(t, err)
	assert.Empty(t, emojis)
}

func TestGetCustomEmojiMap(t *testing.T) {
	db := openTestDB(t)

	err := db.BulkUpsertCustomEmojis([]CustomEmoji{
		{Name: "thumbsup", URL: "https://thumbsup.png"},
		{Name: "party", URL: "https://party.png"},
	})
	require.NoError(t, err)

	m, err := db.GetCustomEmojiMap()
	require.NoError(t, err)
	assert.Len(t, m, 2)
	assert.Equal(t, "https://thumbsup.png", m["thumbsup"])
	assert.Equal(t, "https://party.png", m["party"])
}

func TestGetCustomEmojiMap_ResolvesAliases(t *testing.T) {
	db := openTestDB(t)

	err := db.BulkUpsertCustomEmojis([]CustomEmoji{
		{Name: "thumbsup", URL: "https://thumbsup.png"},
		{Name: "like", URL: "", AliasFor: "thumbsup"},
	})
	require.NoError(t, err)

	m, err := db.GetCustomEmojiMap()
	require.NoError(t, err)
	assert.Equal(t, "https://thumbsup.png", m["thumbsup"])
	assert.Equal(t, "https://thumbsup.png", m["like"])
}

func TestGetCustomEmojiMap_TransitiveAliases(t *testing.T) {
	db := openTestDB(t)

	err := db.BulkUpsertCustomEmojis([]CustomEmoji{
		{Name: "base", URL: "https://base.png"},
		{Name: "alias1", URL: "", AliasFor: "base"},
		{Name: "alias2", URL: "", AliasFor: "alias1"},
	})
	require.NoError(t, err)

	m, err := db.GetCustomEmojiMap()
	require.NoError(t, err)
	assert.Equal(t, "https://base.png", m["base"])
	assert.Equal(t, "https://base.png", m["alias1"])
	assert.Equal(t, "https://base.png", m["alias2"])
}

func TestGetCustomEmojiMap_BrokenAlias(t *testing.T) {
	db := openTestDB(t)

	err := db.BulkUpsertCustomEmojis([]CustomEmoji{
		{Name: "broken", URL: "", AliasFor: "nonexistent"},
	})
	require.NoError(t, err)

	m, err := db.GetCustomEmojiMap()
	require.NoError(t, err)
	// Broken alias should be removed
	_, exists := m["broken"]
	assert.False(t, exists)
}

func TestGetCustomEmojiMap_Empty(t *testing.T) {
	db := openTestDB(t)

	m, err := db.GetCustomEmojiMap()
	require.NoError(t, err)
	assert.Empty(t, m)
}
