package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddImportanceCorrection(t *testing.T) {
	db := openTestDB(t)

	id, err := db.AddImportanceCorrection(ImportanceCorrection{
		DigestID:           1,
		DecisionIdx:        0,
		DecisionText:       "deploy on Friday",
		OriginalImportance: "low",
		NewImportance:      "high",
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestAddImportanceCorrection_Replace(t *testing.T) {
	db := openTestDB(t)

	_, err := db.AddImportanceCorrection(ImportanceCorrection{
		DigestID:           1,
		DecisionIdx:        0,
		DecisionText:       "deploy on Friday",
		OriginalImportance: "low",
		NewImportance:      "medium",
	})
	require.NoError(t, err)

	// Replace with new importance
	_, err = db.AddImportanceCorrection(ImportanceCorrection{
		DigestID:           1,
		DecisionIdx:        0,
		DecisionText:       "deploy on Friday",
		OriginalImportance: "low",
		NewImportance:      "high",
	})
	require.NoError(t, err)

	corrections, err := db.GetImportanceCorrections()
	require.NoError(t, err)
	// Should have one row (replaced)
	require.Len(t, corrections, 1)
	assert.Equal(t, "high", corrections[0].NewImportance)
}

func TestGetImportanceCorrections(t *testing.T) {
	db := openTestDB(t)

	_, err := db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 1, DecisionIdx: 0,
		DecisionText: "decision A", OriginalImportance: "low", NewImportance: "high",
	})
	require.NoError(t, err)

	_, err = db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 2, DecisionIdx: 1,
		DecisionText: "decision B", OriginalImportance: "medium", NewImportance: "low",
	})
	require.NoError(t, err)

	corrections, err := db.GetImportanceCorrections()
	require.NoError(t, err)
	assert.Len(t, corrections, 2)

	// Newest first
	assert.Equal(t, "decision B", corrections[0].DecisionText)
	assert.Equal(t, "decision A", corrections[1].DecisionText)

	// Verify fields
	assert.Equal(t, 2, corrections[0].DigestID)
	assert.Equal(t, 1, corrections[0].DecisionIdx)
	assert.Equal(t, "medium", corrections[0].OriginalImportance)
	assert.Equal(t, "low", corrections[0].NewImportance)
	assert.NotEmpty(t, corrections[0].CreatedAt)
}

func TestGetImportanceCorrections_Empty(t *testing.T) {
	db := openTestDB(t)

	corrections, err := db.GetImportanceCorrections()
	require.NoError(t, err)
	assert.Empty(t, corrections)
}

func TestHasImportanceCorrections(t *testing.T) {
	db := openTestDB(t)

	// No corrections
	has, err := db.HasImportanceCorrections()
	require.NoError(t, err)
	assert.False(t, has)

	// Add one
	_, err = db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 1, DecisionIdx: 0,
		DecisionText: "test", OriginalImportance: "low", NewImportance: "high",
	})
	require.NoError(t, err)

	has, err = db.HasImportanceCorrections()
	require.NoError(t, err)
	assert.True(t, has)
}

func TestGetImportanceCorrectionFor(t *testing.T) {
	db := openTestDB(t)

	// No correction exists
	c, err := db.GetImportanceCorrectionFor(1, 0)
	require.NoError(t, err)
	assert.Nil(t, c)

	// Add correction
	_, err = db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 1, DecisionIdx: 0,
		DecisionText: "test", OriginalImportance: "low", NewImportance: "high",
	})
	require.NoError(t, err)

	// Find it
	c, err = db.GetImportanceCorrectionFor(1, 0)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, 1, c.DigestID)
	assert.Equal(t, 0, c.DecisionIdx)
	assert.Equal(t, "test", c.DecisionText)
	assert.Equal(t, "low", c.OriginalImportance)
	assert.Equal(t, "high", c.NewImportance)

	// Wrong digest_id
	c, err = db.GetImportanceCorrectionFor(2, 0)
	require.NoError(t, err)
	assert.Nil(t, c)

	// Wrong decision_idx
	c, err = db.GetImportanceCorrectionFor(1, 1)
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestClearImportanceCorrections(t *testing.T) {
	db := openTestDB(t)

	_, err := db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 1, DecisionIdx: 0,
		DecisionText: "test1", OriginalImportance: "low", NewImportance: "high",
	})
	require.NoError(t, err)
	_, err = db.AddImportanceCorrection(ImportanceCorrection{
		DigestID: 2, DecisionIdx: 0,
		DecisionText: "test2", OriginalImportance: "medium", NewImportance: "low",
	})
	require.NoError(t, err)

	err = db.ClearImportanceCorrections()
	require.NoError(t, err)

	has, err := db.HasImportanceCorrections()
	require.NoError(t, err)
	assert.False(t, has)

	corrections, err := db.GetImportanceCorrections()
	require.NoError(t, err)
	assert.Empty(t, corrections)
}

func TestClearImportanceCorrections_NoOp(t *testing.T) {
	db := openTestDB(t)

	// Clearing when empty should not error
	err := db.ClearImportanceCorrections()
	require.NoError(t, err)
}
