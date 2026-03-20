package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
)

func TestPidFilePath(t *testing.T) {
	cfg := &config.Config{ActiveWorkspace: "test"}
	t.Setenv("HOME", "/tmp/test-home")
	path := pidFilePath(cfg)
	assert.Contains(t, path, "test")
	assert.Contains(t, path, "daemon.pid")
}

func TestLogFilePath(t *testing.T) {
	cfg := &config.Config{ActiveWorkspace: "test"}
	t.Setenv("HOME", "/tmp/test-home")
	path := logFilePath(cfg)
	assert.Contains(t, path, "test")
	assert.Contains(t, path, "daemon.log")
}

func TestSyncLogFilePath(t *testing.T) {
	cfg := &config.Config{ActiveWorkspace: "test"}
	t.Setenv("HOME", "/tmp/test-home")
	path := syncLogFilePath(cfg)
	assert.Contains(t, path, "test")
	assert.Contains(t, path, "watchtower.log")
}

func TestSyncResultPath(t *testing.T) {
	cfg := &config.Config{ActiveWorkspace: "test"}
	t.Setenv("HOME", "/tmp/test-home")
	path := syncResultPath(cfg)
	assert.Contains(t, path, "test")
	assert.Contains(t, path, "last_sync.json")
}

func TestSyncAdditionalFlags(t *testing.T) {
	assert.NotNil(t, syncCmd.Flags().Lookup("detach"))
	assert.NotNil(t, syncCmd.Flags().Lookup("stop"))
	assert.NotNil(t, syncCmd.Flags().Lookup("skip-dms"))
	assert.NotNil(t, syncCmd.Flags().Lookup("progress-json"))
	assert.NotNil(t, syncCmd.Flags().Lookup("days"))
}

func TestSyncStopSubcommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range syncCmd.Commands() {
		if cmd.Name() == "stop" {
			found = true
			break
		}
	}
	assert.True(t, found, "sync stop subcommand should be registered")
}

func TestSyncDetachRequiresDaemon(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Create a minimal config with a real token to pass Validate()
	tmpDir := os.Getenv("HOME")
	configPath := filepath.Join(tmpDir, "config-detach.yaml")
	configYAML := `active_workspace: test-ws
workspaces:
  test-ws:
    slack_token: "xoxp-test-token"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	syncFlagFull = false
	syncFlagDaemon = false
	syncFlagDetach = true
	syncFlagStop = false
	defer func() { syncFlagDetach = false }()

	// Ensure we're not the detached child
	t.Setenv("WATCHTOWER_DETACH", "")

	err := syncCmd.RunE(syncCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--detach requires --daemon")
}

func TestOpenDBFromConfig(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NotNil(t, database)
	database.Close()
}

func TestOpenDBFromConfig_InvalidConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	database, err := openDBFromConfig()
	assert.Error(t, err)
	assert.Nil(t, database)
}

func TestOpenTracksDB(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openTracksDB()
	require.NoError(t, err)
	require.NotNil(t, database)
	database.Close()
}

func TestOpenTracksDB_InvalidConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	database, err := openTracksDB()
	assert.Error(t, err)
	assert.Nil(t, database)
}

func TestOpenPromptStore(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	cfg, store, closer, err := openPromptStore()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, store)
	require.NotNil(t, closer)
	closer()
}

func TestOpenPromptStore_InvalidConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	_, _, _, err := openPromptStore()
	assert.Error(t, err)
}
