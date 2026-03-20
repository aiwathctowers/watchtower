package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAndGetCurrentUserID(t *testing.T) {
	db := openTestDB(t)

	// Must have a workspace first
	require.NoError(t, db.UpsertWorkspace(Workspace{ID: "T1", Name: "test", Domain: "test.slack.com"}))

	err := db.SetCurrentUserID("U123")
	require.NoError(t, err)

	uid, err := db.GetCurrentUserID()
	require.NoError(t, err)
	assert.Equal(t, "U123", uid)
}

func TestSetCurrentUserID_NoWorkspace(t *testing.T) {
	db := openTestDB(t)

	err := db.SetCurrentUserID("U123")
	assert.Error(t, err)
}

func TestGetCurrentUserID_NoWorkspace(t *testing.T) {
	db := openTestDB(t)

	uid, err := db.GetCurrentUserID()
	require.NoError(t, err)
	assert.Equal(t, "", uid)
}

func TestSetAndGetSearchLastDate(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertWorkspace(Workspace{ID: "T1", Name: "test", Domain: "test.slack.com"}))

	err := db.SetSearchLastDate("2025-01-15")
	require.NoError(t, err)

	date, err := db.GetSearchLastDate()
	require.NoError(t, err)
	assert.Equal(t, "2025-01-15", date)
}

func TestSetSearchLastDate_NoWorkspace(t *testing.T) {
	db := openTestDB(t)

	err := db.SetSearchLastDate("2025-01-15")
	assert.Error(t, err)
}

func TestTouchSyncedAt(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertWorkspace(Workspace{ID: "T1", Name: "test", Domain: "test.slack.com"}))

	err := db.TouchSyncedAt()
	require.NoError(t, err)

	ws, err := db.GetWorkspace()
	require.NoError(t, err)
	assert.True(t, ws.SyncedAt.Valid)
}
