package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSearchMessages(t *testing.T, db *DB) {
	t.Helper()
	msgs := []Message{
		{ChannelID: "C001", TS: "1700000001.000001", UserID: "U001", Text: "deployment to production successful", RawJSON: "{}"},
		{ChannelID: "C001", TS: "1700000002.000001", UserID: "U002", Text: "the bug in login flow is fixed", RawJSON: "{}"},
		{ChannelID: "C002", TS: "1700000003.000001", UserID: "U001", Text: "deploying new feature to staging", RawJSON: "{}"},
		{ChannelID: "C002", TS: "1700000500.000001", UserID: "U003", Text: "database migration completed", RawJSON: "{}"},
		{ChannelID: "C003", TS: "1700001000.000001", UserID: "U002", Text: "production deployment rollback needed", RawJSON: "{}"},
	}
	for _, msg := range msgs {
		require.NoError(t, db.UpsertMessage(msg))
	}
}

func TestSearchMessagesBasic(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("deployment", SearchOpts{})
	require.NoError(t, err)
	assert.Len(t, results, 2) // "deployment to production" and "production deployment rollback"
}

func TestSearchMessagesStemming(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	// "deploying" should match via porter stemmer
	results, err := db.SearchMessages("deploying", SearchOpts{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "stemming should match deploying")

	// "deployment" should match both deployment messages
	results, err = db.SearchMessages("deployment", SearchOpts{})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results), "should match 'deployment' in two messages")
}

func TestSearchMessagesChannelFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("deployment", SearchOpts{ChannelIDs: []string{"C001"}})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "C001", results[0].ChannelID)
}

func TestSearchMessagesUserFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("deployment", SearchOpts{UserIDs: []string{"U001"}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "U001", results[0].UserID)
}

func TestSearchMessagesTimeFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	// Only the late deployment message
	results, err := db.SearchMessages("deployment", SearchOpts{FromUnix: 1700000900})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Text, "rollback")
}

func TestSearchMessagesTimeRangeFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	// Only early deployment message
	results, err := db.SearchMessages("deployment", SearchOpts{ToUnix: 1700000100})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Text, "production successful")
}

func TestSearchMessagesCombinedFilters(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("deployment", SearchOpts{
		ChannelIDs: []string{"C001", "C003"},
		UserIDs:    []string{"U001"},
		ToUnix:     1700000100,
	})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "C001", results[0].ChannelID)
	assert.Equal(t, "U001", results[0].UserID)
}

func TestSearchMessagesEmptyQuery(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	results, err := db.SearchMessages("", SearchOpts{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestSearchMessagesNoResults(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("nonexistenttermxyz", SearchOpts{})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSearchMessagesLimit(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("deploy", SearchOpts{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestSearchMessagesMultipleChannelFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	seedSearchMessages(t, db)

	results, err := db.SearchMessages("deployment", SearchOpts{ChannelIDs: []string{"C001", "C003"}})
	require.NoError(t, err)
	assert.Len(t, results, 2)
	for _, r := range results {
		assert.Contains(t, []string{"C001", "C003"}, r.ChannelID)
	}
}
