package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestRunChannels_SortByMessages(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1700000000.000001", Text: "hello"}))
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1700000001.000001", Text: "world"}))
	database.Close()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = ""
	channelsFlagSort = "messages"
	defer func() { channelsFlagSort = "name" }()

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "general")
	assert.Contains(t, buf.String(), "2 channels total")
}

func TestRunChannels_SortByRecent(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1700000000.000001", Text: "recent"}))
	database.Close()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = ""
	channelsFlagSort = "recent"
	defer func() { channelsFlagSort = "name" }()

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "2 channels total")
}

func TestRunChannels_FilterPrivate(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C003", Name: "secret", Type: "private"}))
	database.Close()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = "private"
	channelsFlagSort = "name"
	defer func() { channelsFlagType = "" }()

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "secret")
	assert.Contains(t, buf.String(), "1 channels total")
}

func TestRunChannels_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	channelsFlagType = ""
	channelsFlagSort = "name"

	err := channelsCmd.RunE(channelsCmd, nil)
	assert.Error(t, err)
}

func TestRunUsers_WithDisplayNameAndEmail(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{
		ID:          "U005",
		Name:        "charlie",
		DisplayName: "Charlie Brown",
		Email:       "charlie@example.com",
	}))
	database.Close()

	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)
	usersFlagActive = false

	err = usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "charlie")
	assert.Contains(t, output, "Charlie Brown")
	assert.Contains(t, output, "charlie@example.com")
}

func TestRunUsers_EmptyDB(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	database.Close()

	// We can't easily test empty users through the command since
	// setupWatchTestEnv adds users, so test via the DB directly
	database, err = db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	filter := db.UserFilter{}
	users, err := database.GetUsers(filter)
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestRunChannels_GroupDMType(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "G001", Name: "dm-group", Type: "group_dm"}))
	database.Close()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = "group_dm"
	channelsFlagSort = "name"
	defer func() { channelsFlagType = "" }()

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dm-group")
}
