package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddStarredChannel_NoProfile(t *testing.T) {
	db := openTestDB(t)

	err := db.AddStarredChannel("U_NONEXISTENT", "C1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRemoveStarredChannel_NoProfile(t *testing.T) {
	db := openTestDB(t)

	err := db.RemoveStarredChannel("U_NONEXISTENT", "C1")
	assert.Error(t, err)
}

func TestRemoveStarredChannel_NotPresent(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertUserProfile(UserProfile{
		SlackUserID:     "U123",
		StarredChannels: `["C100"]`,
	}))

	// Remove channel that's not in the list — should not error
	err := db.RemoveStarredChannel("U123", "C999")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["C100"]`, got.StarredChannels)
}

func TestAddStarredPerson_NoProfile(t *testing.T) {
	db := openTestDB(t)

	err := db.AddStarredPerson("U_NONEXISTENT", "U456")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRemoveStarredPerson_NoProfile(t *testing.T) {
	db := openTestDB(t)

	err := db.RemoveStarredPerson("U_NONEXISTENT", "U456")
	assert.Error(t, err)
}

func TestRemoveStarredPerson_NotPresent(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertUserProfile(UserProfile{
		SlackUserID:   "U123",
		StarredPeople: `["U456"]`,
	}))

	err := db.RemoveStarredPerson("U123", "U999")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["U456"]`, got.StarredPeople)
}

func TestRemoveAllStarredChannels(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertUserProfile(UserProfile{
		SlackUserID:     "U123",
		StarredChannels: `["C100"]`,
	}))

	err := db.RemoveStarredChannel("U123", "C100")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `[]`, got.StarredChannels)
}

func TestRemoveAllStarredPeople(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.UpsertUserProfile(UserProfile{
		SlackUserID:   "U123",
		StarredPeople: `["U456"]`,
	}))

	err := db.RemoveStarredPerson("U123", "U456")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `[]`, got.StarredPeople)
}
