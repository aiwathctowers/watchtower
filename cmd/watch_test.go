package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// setupWatchTestEnv creates a temp dir with config, DB, and test data.
// It returns a cleanup function that restores env vars.
func setupWatchTestEnv(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "config.yaml")
	configYAML := `active_workspace: test-ws
workspaces:
  test-ws:
    slack_token: "xoxp-test-token"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	homeDir := tmpDir
	wsDBDir := filepath.Join(homeDir, ".local", "share", "watchtower", "test-ws")
	require.NoError(t, os.MkdirAll(wsDBDir, 0o755))
	wsDBPath := filepath.Join(wsDBDir, "watchtower.db")

	database, err := db.Open(wsDBPath)
	require.NoError(t, err)
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C002", Name: "random", Type: "public"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U001", Name: "alice"}))
	require.NoError(t, database.UpsertUser(db.User{ID: "U002", Name: "bob"}))
	database.Close()

	t.Setenv("HOME", homeDir)
	oldFlagConfig := flagConfig
	flagConfig = configPath

	return func() {
		flagConfig = oldFlagConfig
	}
}

func TestWatchCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "watch" {
			found = true
			break
		}
	}
	assert.True(t, found, "watch command should be registered")
}

func TestWatchSubcommandsRegistered(t *testing.T) {
	subcommands := map[string]bool{"add": false, "remove": false, "list": false}
	for _, cmd := range watchCmd.Commands() {
		if _, ok := subcommands[cmd.Name()]; ok {
			subcommands[cmd.Name()] = true
		}
	}
	for name, found := range subcommands {
		assert.True(t, found, "watch %s subcommand should be registered", name)
	}
}

func TestWatchAddChannel(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	watchFlagPriority = "high"
	defer func() { watchFlagPriority = "normal" }()

	err := watchAddCmd.RunE(watchAddCmd, []string{"#general"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Watching #general (priority: high)")
}

func TestWatchAddUser(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	watchFlagPriority = "normal"

	err := watchAddCmd.RunE(watchAddCmd, []string{"@alice"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Watching @alice (priority: normal)")
}

func TestWatchAddWithoutPrefix(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	watchFlagPriority = "low"
	defer func() { watchFlagPriority = "normal" }()

	// "general" without prefix should resolve as channel
	err := watchAddCmd.RunE(watchAddCmd, []string{"general"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Watching #general (priority: low)")
}

func TestWatchAddUserWithoutPrefix(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	watchFlagPriority = "normal"

	// "alice" without prefix - not a channel name, should resolve as user
	err := watchAddCmd.RunE(watchAddCmd, []string{"alice"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Watching @alice (priority: normal)")
}

func TestWatchAddNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	watchFlagPriority = "normal"
	err := watchAddCmd.RunE(watchAddCmd, []string{"#nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWatchAddInvalidPriority(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	watchFlagPriority = "extreme"
	defer func() { watchFlagPriority = "normal" }()

	err := watchAddCmd.RunE(watchAddCmd, []string{"#general"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid priority")
}

func TestWatchRemove(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// First add a watch
	watchFlagPriority = "normal"
	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"#general"}))

	// Now remove it
	buf.Reset()
	watchRemoveCmd.SetOut(buf)
	err := watchRemoveCmd.RunE(watchRemoveCmd, []string{"#general"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Removed #general from watch list")
}

func TestWatchRemoveNotWatched(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Try to remove something that's not watched
	err := watchRemoveCmd.RunE(watchRemoveCmd, []string{"#general"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no watch entry found")
}

func TestWatchListEmpty(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	watchListCmd.SetOut(buf)
	err := watchListCmd.RunE(watchListCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No watched channels or users.")
}

func TestWatchListWithItems(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Add some watches
	watchFlagPriority = "high"
	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"#general"}))

	watchFlagPriority = "normal"
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"@alice"}))
	watchFlagPriority = "normal" // reset

	// List them
	buf.Reset()
	watchListCmd.SetOut(buf)
	err := watchListCmd.RunE(watchListCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "#general")
	assert.Contains(t, output, "high")
	assert.Contains(t, output, "@alice")
	assert.Contains(t, output, "normal")
}

func TestWatchAddRequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	watchFlagPriority = "normal"
	err := watchAddCmd.RunE(watchAddCmd, []string{"#general"})
	assert.Error(t, err)
}

func TestResolveTargetChannelByPrefix(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, "#general")
	require.NoError(t, err)
	assert.Equal(t, "channel", entityType)
	assert.Equal(t, "C001", entityID)
	assert.Equal(t, "general", entityName)
}

func TestResolveTargetUserByPrefix(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, "@bob")
	require.NoError(t, err)
	assert.Equal(t, "user", entityType)
	assert.Equal(t, "U002", entityID)
	assert.Equal(t, "bob", entityName)
}

func TestResolveTargetFallbackOrder(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	// "random" is a channel name, should resolve as channel first
	entityType, _, entityName, err := resolveTarget(database, "random")
	require.NoError(t, err)
	assert.Equal(t, "channel", entityType)
	assert.Equal(t, "random", entityName)

	// "bob" is only a user name, should resolve as user
	entityType, _, entityName, err = resolveTarget(database, "bob")
	require.NoError(t, err)
	assert.Equal(t, "user", entityType)
	assert.Equal(t, "bob", entityName)
}
