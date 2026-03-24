package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCheckpointEmpty(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	cp, err := db.GetCheckpoint()
	require.NoError(t, err)
	assert.Nil(t, cp)
}

func TestUpdateAndGetCheckpoint(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	require.NoError(t, db.UpdateCheckpoint(ts))

	cp, err := db.GetCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, 1, cp.ID)
	assert.Equal(t, "2025-06-15T10:30:00Z", cp.LastCheckedAt)
}

func TestUpdateCheckpointOverwrite(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ts1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.UpdateCheckpoint(ts1))

	ts2 := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.UpdateCheckpoint(ts2))

	cp, err := db.GetCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, "2025-06-15T12:00:00Z", cp.LastCheckedAt)
}

func TestUpdateCheckpointConvertsToUTC(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	loc, _ := time.LoadLocation("America/New_York")
	ts := time.Date(2025, 6, 15, 10, 0, 0, 0, loc)
	require.NoError(t, db.UpdateCheckpoint(ts))

	cp, err := db.GetCheckpoint()
	require.NoError(t, err)
	require.NotNil(t, cp)
	// Should be stored in UTC
	assert.Equal(t, "2025-06-15T14:00:00Z", cp.LastCheckedAt)
}

func TestDetermineSinceTime_ExplicitDuration(t *testing.T) {
	db := openTestDB(t)

	before := time.Now()
	since, err := db.DetermineSinceTime(2 * time.Hour)
	require.NoError(t, err)

	expected := before.Add(-2 * time.Hour)
	assert.WithinDuration(t, expected, since, 2*time.Second)
}

func TestDetermineSinceTime_FromCheckpoint(t *testing.T) {
	db := openTestDB(t)

	ts := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	require.NoError(t, db.UpdateCheckpoint(ts))

	since, err := db.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.Equal(t, ts, since)
}

func TestDetermineSinceTime_Default24Hours(t *testing.T) {
	db := openTestDB(t)

	before := time.Now()
	since, err := db.DetermineSinceTime(0)
	require.NoError(t, err)

	expected := before.Add(-24 * time.Hour)
	assert.WithinDuration(t, expected, since, 2*time.Second)
}

func TestDetermineSinceTime_DurationOverridesCheckpoint(t *testing.T) {
	db := openTestDB(t)

	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.UpdateCheckpoint(ts))

	before := time.Now()
	since, err := db.DetermineSinceTime(1 * time.Hour)
	require.NoError(t, err)

	expected := before.Add(-1 * time.Hour)
	assert.WithinDuration(t, expected, since, 2*time.Second)
}
