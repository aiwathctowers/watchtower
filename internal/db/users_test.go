package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertUser(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	u := User{
		ID:          "U001",
		Name:        "alice",
		DisplayName: "Alice Smith",
		RealName:    "Alice J. Smith",
		Email:       "alice@example.com",
		IsBot:       false,
		IsDeleted:   false,
		ProfileJSON: `{"title":"Engineer"}`,
	}
	err = db.UpsertUser(u)
	require.NoError(t, err)

	got, err := db.GetUserByID("U001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.Name)
	assert.Equal(t, "Alice Smith", got.DisplayName)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.False(t, got.IsBot)
	assert.NotEmpty(t, got.UpdatedAt)
}

func TestUpsertUserUpdate(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	u := User{ID: "U001", Name: "alice", Email: "alice@old.com"}
	require.NoError(t, db.UpsertUser(u))

	u.Email = "alice@new.com"
	u.DisplayName = "Alice New"
	require.NoError(t, db.UpsertUser(u))

	got, err := db.GetUserByID("U001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "alice@new.com", got.Email)
	assert.Equal(t, "Alice New", got.DisplayName)
}

func TestGetUserByName(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertUser(User{ID: "U001", Name: "alice"}))
	require.NoError(t, db.UpsertUser(User{ID: "U002", Name: "bob"}))

	got, err := db.GetUserByName("bob")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "U002", got.ID)
}

func TestGetUserByIDNotFound(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	got, err := db.GetUserByID("U999")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetUserByNameNotFound(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	got, err := db.GetUserByName("nobody")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetUsersNoFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertUser(User{ID: "U001", Name: "alice"}))
	require.NoError(t, db.UpsertUser(User{ID: "U002", Name: "bob", IsBot: true}))
	require.NoError(t, db.UpsertUser(User{ID: "U003", Name: "charlie", IsDeleted: true}))

	users, err := db.GetUsers(UserFilter{})
	require.NoError(t, err)
	assert.Len(t, users, 3)
	// Sorted by name
	assert.Equal(t, "alice", users[0].Name)
	assert.Equal(t, "bob", users[1].Name)
	assert.Equal(t, "charlie", users[2].Name)
}

func TestGetUsersExcludeBots(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertUser(User{ID: "U001", Name: "alice"}))
	require.NoError(t, db.UpsertUser(User{ID: "U002", Name: "slackbot", IsBot: true}))

	users, err := db.GetUsers(UserFilter{ExcludeBots: true})
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Name)
}

func TestGetUsersExcludeDeleted(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertUser(User{ID: "U001", Name: "alice"}))
	require.NoError(t, db.UpsertUser(User{ID: "U002", Name: "gone", IsDeleted: true}))

	users, err := db.GetUsers(UserFilter{ExcludeDeleted: true})
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Name)
}

func TestGetUsersCombinedFilter(t *testing.T) {
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, db.UpsertUser(User{ID: "U001", Name: "alice"}))
	require.NoError(t, db.UpsertUser(User{ID: "U002", Name: "slackbot", IsBot: true}))
	require.NoError(t, db.UpsertUser(User{ID: "U003", Name: "gone", IsDeleted: true}))
	require.NoError(t, db.UpsertUser(User{ID: "U004", Name: "deadbot", IsBot: true, IsDeleted: true}))

	users, err := db.GetUsers(UserFilter{ExcludeBots: true, ExcludeDeleted: true})
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Name)
}
