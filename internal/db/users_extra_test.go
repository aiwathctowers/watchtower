package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureUser(t *testing.T) {
	db := openTestDB(t)

	err := db.EnsureUser("U1", "alice")
	require.NoError(t, err)

	u, err := db.GetUserByID("U1")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "alice", u.Name)
	assert.True(t, u.IsStub, "EnsureUser should create stub records")

	// Re-ensure should not overwrite
	err = db.EnsureUser("U1", "different-name")
	require.NoError(t, err)

	u, err = db.GetUserByID("U1")
	require.NoError(t, err)
	assert.Equal(t, "alice", u.Name)

	// UpsertUser should clear the stub flag
	require.NoError(t, db.UpsertUser(User{ID: "U1", Name: "alice", RealName: "Alice Smith", IsBot: true}))
	u, err = db.GetUserByID("U1")
	require.NoError(t, err)
	assert.False(t, u.IsStub, "UpsertUser should clear stub flag")
	assert.True(t, u.IsBot)
}

func TestGetIncompleteUserIDs(t *testing.T) {
	db := openTestDB(t)

	// Full user — should NOT appear
	require.NoError(t, db.UpsertUser(User{ID: "U1", Name: "alice"}))

	// Messages from unknown users — should appear
	require.NoError(t, db.UpsertMessage(Message{ChannelID: "C1", TS: "1500000.000002", UserID: "U_UNKNOWN", Text: "unknown", RawJSON: "{}"}))

	// Stub user (created via EnsureUser) — should appear
	require.NoError(t, db.EnsureUser("U_STUB", "stub-bot"))

	ids, err := db.GetIncompleteUserIDs()
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "U_UNKNOWN")
	assert.Contains(t, ids, "U_STUB")
}

func TestGetIncompleteUserIDs_Empty(t *testing.T) {
	db := openTestDB(t)

	ids, err := db.GetIncompleteUserIDs()
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestGetIncompleteUserIDs_StubBackfilled(t *testing.T) {
	db := openTestDB(t)

	// Create stub
	require.NoError(t, db.EnsureUser("U1", "bot"))
	ids, err := db.GetIncompleteUserIDs()
	require.NoError(t, err)
	assert.Contains(t, ids, "U1")

	// Backfill with full profile
	require.NoError(t, db.UpsertUser(User{ID: "U1", Name: "bot", RealName: "Bot User", IsBot: true}))
	ids, err = db.GetIncompleteUserIDs()
	require.NoError(t, err)
	assert.Empty(t, ids, "backfilled user should no longer be incomplete")
}

func TestGetUsersFilter(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertUser(User{ID: "U1", Name: "alice"}))
	require.NoError(t, db.UpsertUser(User{ID: "U2", Name: "bob", IsBot: true}))
	require.NoError(t, db.UpsertUser(User{ID: "U3", Name: "charlie", IsDeleted: true}))

	// No filter
	users, err := db.GetUsers(UserFilter{})
	require.NoError(t, err)
	assert.Len(t, users, 3)

	// Exclude bots
	users, err = db.GetUsers(UserFilter{ExcludeBots: true})
	require.NoError(t, err)
	assert.Len(t, users, 2)

	// Exclude deleted
	users, err = db.GetUsers(UserFilter{ExcludeDeleted: true})
	require.NoError(t, err)
	assert.Len(t, users, 2)

	// Both
	users, err = db.GetUsers(UserFilter{ExcludeBots: true, ExcludeDeleted: true})
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Name)
}

func TestGetUserByName_Extra(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertUser(User{ID: "U1", Name: "alice"}))

	u, err := db.GetUserByName("alice")
	require.NoError(t, err)
	require.NotNil(t, u)
	assert.Equal(t, "U1", u.ID)

	u, err = db.GetUserByName("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, u)
}
