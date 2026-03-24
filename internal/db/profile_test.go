package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserProfile_NotFound(t *testing.T) {
	db := openTestDB(t)

	profile, err := db.GetUserProfile("U_NONEXISTENT")
	require.NoError(t, err)
	assert.Nil(t, profile)
}

func TestUpsertAndGetUserProfile(t *testing.T) {
	db := openTestDB(t)

	p := UserProfile{
		SlackUserID:         "U123",
		Role:                "Engineering Manager",
		Team:                "Platform",
		Responsibilities:    `["API reliability","infrastructure"]`,
		Reports:             `["U456","U789"]`,
		Peers:               `["U111"]`,
		Manager:             "U000",
		StarredChannels:     `["C100","C200"]`,
		StarredPeople:       `["U999"]`,
		PainPoints:          `["missing messages","lost decisions"]`,
		TrackFocus:          `["blockers","deadlines"]`,
		OnboardingDone:      true,
		CustomPromptContext: "You are helping an EM responsible for Platform team.",
	}

	err := db.UpsertUserProfile(p)
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "U123", got.SlackUserID)
	assert.Equal(t, "Engineering Manager", got.Role)
	assert.Equal(t, "Platform", got.Team)
	assert.Equal(t, `["API reliability","infrastructure"]`, got.Responsibilities)
	assert.Equal(t, `["U456","U789"]`, got.Reports)
	assert.Equal(t, `["U111"]`, got.Peers)
	assert.Equal(t, "U000", got.Manager)
	assert.Equal(t, `["C100","C200"]`, got.StarredChannels)
	assert.Equal(t, `["U999"]`, got.StarredPeople)
	assert.Equal(t, `["missing messages","lost decisions"]`, got.PainPoints)
	assert.Equal(t, `["blockers","deadlines"]`, got.TrackFocus)
	assert.True(t, got.OnboardingDone)
	assert.Equal(t, "You are helping an EM responsible for Platform team.", got.CustomPromptContext)
	assert.NotEmpty(t, got.CreatedAt)
	assert.NotEmpty(t, got.UpdatedAt)
}

func TestUpsertUserProfile_Update(t *testing.T) {
	db := openTestDB(t)

	// Create initial profile.
	err := db.UpsertUserProfile(UserProfile{
		SlackUserID: "U123",
		Role:        "IC",
		Team:        "Backend",
	})
	require.NoError(t, err)

	// Update with new data.
	err = db.UpsertUserProfile(UserProfile{
		SlackUserID: "U123",
		Role:        "Tech Lead",
		Team:        "Platform",
		Reports:     `["U456"]`,
	})
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "Tech Lead", got.Role)
	assert.Equal(t, "Platform", got.Team)
	assert.Equal(t, `["U456"]`, got.Reports)
}

func TestUpsertUserProfile_Defaults(t *testing.T) {
	db := openTestDB(t)

	// Insert with only slack_user_id.
	err := db.UpsertUserProfile(UserProfile{SlackUserID: "U_MINIMAL"})
	require.NoError(t, err)

	got, err := db.GetUserProfile("U_MINIMAL")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "", got.Role)
	assert.Equal(t, "", got.Team)
	assert.Equal(t, "", got.Responsibilities)
	assert.Equal(t, "", got.Reports)
	assert.Equal(t, "", got.Manager)
	assert.False(t, got.OnboardingDone)
}

func TestAddStarredChannel(t *testing.T) {
	db := openTestDB(t)

	// Create profile with no starred channels
	err := db.UpsertUserProfile(UserProfile{SlackUserID: "U123"})
	require.NoError(t, err)

	// Add first starred channel
	err = db.AddStarredChannel("U123", "C100")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["C100"]`, got.StarredChannels)

	// Add second starred channel
	err = db.AddStarredChannel("U123", "C200")
	require.NoError(t, err)

	got, err = db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["C100","C200"]`, got.StarredChannels)
}

func TestAddStarredChannel_Idempotent(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertUserProfile(UserProfile{
		SlackUserID:     "U123",
		StarredChannels: `["C100"]`,
	})
	require.NoError(t, err)

	// Add the same channel again — should be idempotent
	err = db.AddStarredChannel("U123", "C100")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["C100"]`, got.StarredChannels)
}

func TestRemoveStarredChannel(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertUserProfile(UserProfile{
		SlackUserID:     "U123",
		StarredChannels: `["C100","C200","C300"]`,
	})
	require.NoError(t, err)

	// Remove middle channel
	err = db.RemoveStarredChannel("U123", "C200")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["C100","C300"]`, got.StarredChannels)

	// Remove another channel
	err = db.RemoveStarredChannel("U123", "C100")
	require.NoError(t, err)

	got, err = db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["C300"]`, got.StarredChannels)
}

func TestAddStarredPerson(t *testing.T) {
	db := openTestDB(t)

	// Create profile with no starred people
	err := db.UpsertUserProfile(UserProfile{SlackUserID: "U123"})
	require.NoError(t, err)

	// Add first starred person
	err = db.AddStarredPerson("U123", "U456")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["U456"]`, got.StarredPeople)

	// Add second starred person
	err = db.AddStarredPerson("U123", "U789")
	require.NoError(t, err)

	got, err = db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["U456","U789"]`, got.StarredPeople)
}

func TestAddStarredPerson_Idempotent(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertUserProfile(UserProfile{
		SlackUserID:   "U123",
		StarredPeople: `["U456"]`,
	})
	require.NoError(t, err)

	// Add the same person again — should be idempotent
	err = db.AddStarredPerson("U123", "U456")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["U456"]`, got.StarredPeople)
}

func TestRemoveStarredPerson(t *testing.T) {
	db := openTestDB(t)

	err := db.UpsertUserProfile(UserProfile{
		SlackUserID:   "U123",
		StarredPeople: `["U456","U789","U999"]`,
	})
	require.NoError(t, err)

	// Remove middle person
	err = db.RemoveStarredPerson("U123", "U789")
	require.NoError(t, err)

	got, err := db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["U456","U999"]`, got.StarredPeople)

	// Remove another person
	err = db.RemoveStarredPerson("U123", "U456")
	require.NoError(t, err)

	got, err = db.GetUserProfile("U123")
	require.NoError(t, err)
	assert.Equal(t, `["U999"]`, got.StarredPeople)
}
