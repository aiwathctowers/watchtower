package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFeedbackForPrompt(t *testing.T) {
	db := openTestDB(t)

	_, _ = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "1", Rating: 1})
	_, _ = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "2", Rating: -1})
	_, _ = db.AddFeedback(Feedback{EntityType: "track", EntityID: "1", Rating: 1})
	_, _ = db.AddFeedback(Feedback{EntityType: "user_analysis", EntityID: "1", Rating: -1})

	// digest.channel maps to "digest"
	fbs, err := db.GetFeedbackForPrompt("digest.channel", 10)
	require.NoError(t, err)
	assert.Len(t, fbs, 2)

	// digest.daily maps to "digest"
	fbs, err = db.GetFeedbackForPrompt("digest.daily", 10)
	require.NoError(t, err)
	assert.Len(t, fbs, 2)

	// tracks.extract maps to "track"
	fbs, err = db.GetFeedbackForPrompt("tracks.extract", 10)
	require.NoError(t, err)
	assert.Len(t, fbs, 1)

	// analysis.user maps to "user_analysis"
	fbs, err = db.GetFeedbackForPrompt("analysis.user", 10)
	require.NoError(t, err)
	assert.Len(t, fbs, 1)

	// Unknown prompt ID
	_, err = db.GetFeedbackForPrompt("unknown.prompt", 10)
	assert.Error(t, err)
}

func TestGetFeedbackWithLimit(t *testing.T) {
	db := openTestDB(t)

	for i := 0; i < 10; i++ {
		_, _ = db.AddFeedback(Feedback{EntityType: "digest", EntityID: "1", Rating: 1})
	}

	fbs, err := db.GetFeedbackWithLimit(FeedbackFilter{}, 5)
	require.NoError(t, err)
	assert.Len(t, fbs, 5)
}
