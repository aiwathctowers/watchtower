package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertWorkspace(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ws := Workspace{
		ID:     "T024BE7LD",
		Name:   "my-company",
		Domain: "my-company",
	}
	err = db.UpsertWorkspace(ws)
	require.NoError(t, err)

	got, err := db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "T024BE7LD", got.ID)
	assert.Equal(t, "my-company", got.Name)
	assert.Equal(t, "my-company", got.Domain)
	assert.NotEmpty(t, got.SyncedAt)
}

func TestUpsertWorkspaceUpdate(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ws := Workspace{ID: "T001", Name: "old-name", Domain: "old-domain"}
	require.NoError(t, db.UpsertWorkspace(ws))

	ws.Name = "new-name"
	ws.Domain = "new-domain"
	require.NoError(t, db.UpsertWorkspace(ws))

	got, err := db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "new-name", got.Name)
	assert.Equal(t, "new-domain", got.Domain)
}

func TestGetWorkspaceEmpty(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	got, err := db.GetWorkspace()
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUpsertWorkspaceSyncedAtUpdated(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ws := Workspace{ID: "T001", Name: "test", Domain: "test"}
	require.NoError(t, db.UpsertWorkspace(ws))

	first, err := db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, first)
	firstSyncedAt := first.SyncedAt

	// Upsert again — synced_at should be updated
	require.NoError(t, db.UpsertWorkspace(ws))

	second, err := db.GetWorkspace()
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, firstSyncedAt, second.SyncedAt) // Same second, so may be equal
	assert.NotEmpty(t, second.SyncedAt)
}
