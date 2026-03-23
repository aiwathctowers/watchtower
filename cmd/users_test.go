package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestUsersCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "users" {
			found = true
			break
		}
	}
	assert.True(t, found, "users command should be registered")
}

func TestRunUsersList(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)
	usersFlagActive = false

	err := usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "bob")
	assert.Contains(t, output, "2 users total")
}

func TestRunUsersActiveFilter(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// setupWatchTestEnv creates alice and bob, neither is bot/deleted,
	// so --active should still show both.
	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)
	usersFlagActive = true
	defer func() { usersFlagActive = false }()

	err := usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "bob")
}

func TestRunUsersShowsBotFlag(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Add a bot user
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{ID: "U003", Name: "slackbot", IsBot: true}))
	database.Close()

	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)
	usersFlagActive = false

	err = usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "[bot]")
}

func TestRunUsersActiveExcludesBots(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Add a bot user
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{ID: "U003", Name: "slackbot", IsBot: true}))
	database.Close()

	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)
	usersFlagActive = true
	defer func() { usersFlagActive = false }()

	err = usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "slackbot")
	assert.Contains(t, output, "2 users total")
}

func TestRunUsersShowsDeletedFlag(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertUser(db.User{ID: "U004", Name: "departed", IsDeleted: true}))
	database.Close()

	buf := new(bytes.Buffer)
	usersCmd.SetOut(buf)
	usersFlagActive = false

	err = usersCmd.RunE(usersCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "[deleted]")
}

func TestRunUsersRequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := usersCmd.RunE(usersCmd, nil)
	assert.Error(t, err)
}
