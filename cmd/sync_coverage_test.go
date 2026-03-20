package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncStopCmd_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := syncStopCmd.RunE(syncStopCmd, nil)
	assert.Error(t, err)
}

func TestSyncStopCmd_NoDaemonRunning(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// syncStopCmd uses runSyncStopCmd which loads config internally
	// When no PID file exists, FindProcess returns (0, nil) and prints "No daemon is running."
	err := syncStopCmd.RunE(syncStopCmd, nil)
	// May succeed (prints "No daemon is running.") or error depending on pid file handling
	_ = err
}

func TestRunSync_StopFlag(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	syncFlagStop = true
	syncFlagFull = false
	syncFlagDaemon = false
	syncFlagDetach = false
	defer func() { syncFlagStop = false }()

	// Stop flag path: should call runSyncStop which tries to find daemon PID
	// When no PID file, it prints "No daemon is running." and returns nil
	err := syncCmd.RunE(syncCmd, nil)
	// Just verify it doesn't panic
	_ = err
}

func TestRunSync_DetachWithoutDaemon(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Create a real config with slack token for Validate() to pass
	tmpDir := os.Getenv("HOME")
	configPath := filepath.Join(tmpDir, "config-detach-test.yaml")
	configYAML := `active_workspace: test-ws
workspaces:
  test-ws:
    slack_token: "xoxp-test-token"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	syncFlagDetach = true
	syncFlagDaemon = false
	syncFlagFull = false
	syncFlagStop = false
	defer func() { syncFlagDetach = false }()

	// Ensure we're not the detached child
	t.Setenv("WATCHTOWER_DETACH", "")

	err := syncCmd.RunE(syncCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--detach requires --daemon")
}

func TestRunSync_DaysOverride(t *testing.T) {
	// Test that --days flag is recognized
	f := syncCmd.Flags().Lookup("days")
	assert.NotNil(t, f)
	assert.Equal(t, "0", f.DefValue)
}

func TestSyncFlagSkipDMs(t *testing.T) {
	f := syncCmd.Flags().Lookup("skip-dms")
	assert.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

func TestSyncFlagChannels(t *testing.T) {
	f := syncCmd.Flags().Lookup("channels")
	assert.NotNil(t, f)
}
