package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddFeedback(t *testing.T) {
	db := openTestDB(t)
	id, err := db.AddFeedback(Feedback{
		EntityType: "digest",
		EntityID:   "42",
		Rating:     1,
		Comment:    "great summary",
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestAddFeedbackNegative(t *testing.T) {
	db := openTestDB(t)
	id, err := db.AddFeedback(Feedback{
		EntityType: "track",
		EntityID:   "7",
		Rating:     -1,
		Comment:    "not relevant to me",
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestGetFeedback(t *testing.T) {
	db := openTestDB(t)
	_, err := db.AddFeedback(Feedback{EntityType: "digest", EntityID: "1", Rating: 1})
	require.NoError(t, err)
	_, err = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "2", Rating: -1})
	require.NoError(t, err)
	_, err = db.AddFeedback(Feedback{EntityType: "track", EntityID: "3", Rating: 1})
	require.NoError(t, err)

	// All feedback
	all, err := db.GetFeedback(FeedbackFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Filter by type
	digestOnly, err := db.GetFeedback(FeedbackFilter{EntityType: "digest"})
	require.NoError(t, err)
	assert.Len(t, digestOnly, 2)

	// Filter by rating
	positive, err := db.GetFeedback(FeedbackFilter{Rating: 1})
	require.NoError(t, err)
	assert.Len(t, positive, 2)

	// Filter by entity
	specific, err := db.GetFeedback(FeedbackFilter{EntityType: "digest", EntityID: "1"})
	require.NoError(t, err)
	assert.Len(t, specific, 1)
	assert.Equal(t, 1, specific[0].Rating)
}

func TestGetFeedbackStats(t *testing.T) {
	db := openTestDB(t)
	_, _ = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "1", Rating: 1})
	_, _ = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "2", Rating: 1})
	_, _ = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "3", Rating: -1})
	_, _ = db.AddFeedback(Feedback{EntityType: "track", EntityID: "1", Rating: -1})

	stats, err := db.GetFeedbackStats()
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// Stats are sorted by entity_type alphabetically
	assert.Equal(t, "digest", stats[0].EntityType)
	assert.Equal(t, 2, stats[0].Positive)
	assert.Equal(t, 1, stats[0].Negative)
	assert.Equal(t, 3, stats[0].Total)

	assert.Equal(t, "track", stats[1].EntityType)
	assert.Equal(t, 0, stats[1].Positive)
	assert.Equal(t, 1, stats[1].Negative)
	assert.Equal(t, 1, stats[1].Total)
}
