package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestChannelsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "channels" {
			found = true
			break
		}
	}
	assert.True(t, found, "channels command should be registered")
}

func TestRunChannelsEmpty(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// The setupWatchTestEnv already creates channels, so we need a fresh DB.
	// Re-use the setup but test with a filter that won't match.
	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = "dm"
	channelsFlagSort = "name"
	defer func() { channelsFlagType = ""; channelsFlagSort = "name" }()

	err := channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No channels found")
}

func TestRunChannelsList(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = ""
	channelsFlagSort = "name"

	err := channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "general")
	assert.Contains(t, output, "random")
	assert.Contains(t, output, "2 channels total")
}

func TestRunChannelsFilterByType(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = "public"
	channelsFlagSort = "name"
	defer func() { channelsFlagType = "" }()

	err := channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "general")
}

func TestRunChannelsInvalidType(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	channelsFlagType = "invalid"
	defer func() { channelsFlagType = "" }()

	err := channelsCmd.RunE(channelsCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestRunChannelsInvalidSort(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	channelsFlagSort = "invalid"
	defer func() { channelsFlagSort = "name" }()

	err := channelsCmd.RunE(channelsCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sort")
}

func TestRunChannelsShowsWatched(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Add a watch on general
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.AddWatch("channel", "C001", "general", "high"))
	database.Close()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = ""
	channelsFlagSort = "name"

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "[watched]")
}

func TestRunChannelsShowsMessageCount(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Add messages
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1700000000.000001", Text: "hello"}))
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1700000001.000001", Text: "world"}))
	database.Close()

	buf := new(bytes.Buffer)
	channelsCmd.SetOut(buf)
	channelsFlagType = ""
	channelsFlagSort = "name"

	err = channelsCmd.RunE(channelsCmd, nil)
	require.NoError(t, err)
	// Should show "2" for message count of general
	assert.Contains(t, buf.String(), "2")
}
