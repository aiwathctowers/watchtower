package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertReactionBatch(t *testing.T) {
	db := openTestDB(t)

	tx, err := db.Begin()
	require.NoError(t, err)

	reactions := []Reaction{
		{ChannelID: "C1", MessageTS: "1500000.000001", UserID: "U1", Emoji: "thumbsup"},
		{ChannelID: "C1", MessageTS: "1500000.000001", UserID: "U2", Emoji: "thumbsup"},
		{ChannelID: "C1", MessageTS: "1500000.000001", UserID: "U3", Emoji: "fire"},
	}
	require.NoError(t, db.UpsertReactionBatch(tx, reactions))
	require.NoError(t, tx.Commit())

	// Verify rows exist.
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM reactions`).Scan(&count))
	assert.Equal(t, 3, count)
}

func TestUpsertReactionBatch_Dedup(t *testing.T) {
	db := openTestDB(t)

	r := Reaction{ChannelID: "C1", MessageTS: "1500000.000001", UserID: "U1", Emoji: "thumbsup"}

	tx1, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, db.UpsertReactionBatch(tx1, []Reaction{r}))
	require.NoError(t, tx1.Commit())

	// Insert same reaction again — should be ignored.
	tx2, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, db.UpsertReactionBatch(tx2, []Reaction{r}))
	require.NoError(t, tx2.Commit())

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM reactions`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestUpsertReactionBatch_Empty(t *testing.T) {
	db := openTestDB(t)

	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, db.UpsertReactionBatch(tx, nil))
	require.NoError(t, tx.Commit())
}

func TestGetReactionsForMessages(t *testing.T) {
	db := openTestDB(t)

	tx, err := db.Begin()
	require.NoError(t, err)
	reactions := []Reaction{
		{ChannelID: "C1", MessageTS: "100.001", UserID: "U1", Emoji: "thumbsup"},
		{ChannelID: "C1", MessageTS: "100.001", UserID: "U2", Emoji: "thumbsup"},
		{ChannelID: "C1", MessageTS: "100.001", UserID: "U3", Emoji: "fire"},
		{ChannelID: "C1", MessageTS: "200.001", UserID: "U1", Emoji: "heart"},
		// Different channel — should not appear.
		{ChannelID: "C2", MessageTS: "100.001", UserID: "U1", Emoji: "eyes"},
	}
	require.NoError(t, db.UpsertReactionBatch(tx, reactions))
	require.NoError(t, tx.Commit())

	result, err := db.GetReactionsForMessages("C1", []string{"100.001", "200.001", "999.999"})
	require.NoError(t, err)

	// Message 100.001: thumbsup(2), fire(1)
	require.Len(t, result["100.001"], 2)
	assert.Equal(t, "thumbsup", result["100.001"][0].Emoji)
	assert.Equal(t, 2, result["100.001"][0].Count)
	assert.Equal(t, "fire", result["100.001"][1].Emoji)
	assert.Equal(t, 1, result["100.001"][1].Count)

	// Message 200.001: heart(1)
	require.Len(t, result["200.001"], 1)
	assert.Equal(t, "heart", result["200.001"][0].Emoji)

	// Message 999.999: no reactions
	assert.Empty(t, result["999.999"])
}

func TestGetReactionsForMessages_Empty(t *testing.T) {
	db := openTestDB(t)

	result, err := db.GetReactionsForMessages("C1", nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetReactionsForMessages_Top5Limit(t *testing.T) {
	db := openTestDB(t)

	tx, err := db.Begin()
	require.NoError(t, err)
	emojis := []string{"a", "b", "c", "d", "e", "f", "g"}
	for _, emoji := range emojis {
		require.NoError(t, db.UpsertReactionBatch(tx, []Reaction{
			{ChannelID: "C1", MessageTS: "100.001", UserID: "U1", Emoji: emoji},
		}))
	}
	require.NoError(t, tx.Commit())

	result, err := db.GetReactionsForMessages("C1", []string{"100.001"})
	require.NoError(t, err)
	assert.Len(t, result["100.001"], 5) // capped at 5
}

func TestFormatReactions(t *testing.T) {
	assert.Equal(t, "", FormatReactions(nil))
	assert.Equal(t, "", FormatReactions([]ReactionSummary{}))
	assert.Equal(t, " [thumbsup:3]", FormatReactions([]ReactionSummary{{Emoji: "thumbsup", Count: 3}}))
	assert.Equal(t, " [thumbsup:3 fire:1]", FormatReactions([]ReactionSummary{
		{Emoji: "thumbsup", Count: 3},
		{Emoji: "fire", Count: 1},
	}))
}
