package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
)

func TestLogsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "logs" {
			found = true
			break
		}
	}
	assert.True(t, found, "logs command should be registered")
}

func TestRunLogs_NoLogFile(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	logsFlagFollow = false
	logsFlagLines = 50

	err := logsCmd.RunE(logsCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no log file found")
}

func TestRunLogs_WithLogFile(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Load config to find the log path
	cfg, err := config.Load(flagConfig)
	require.NoError(t, err)

	logPath := syncLogFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o755))
	require.NoError(t, os.WriteFile(logPath, []byte("2024-01-01 line1\n2024-01-01 line2\n2024-01-01 line3\n"), 0o600))

	logsFlagFollow = false
	logsFlagLines = 2

	buf := new(bytes.Buffer)
	logsCmd.SetOut(buf)

	err = logsCmd.RunE(logsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "line2")
	assert.Contains(t, output, "line3")
}

func TestLogsFlags(t *testing.T) {
	assert.NotNil(t, logsCmd.Flags().Lookup("follow"))
	assert.NotNil(t, logsCmd.Flags().Lookup("lines"))
}

func TestRunLogs_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	logsFlagFollow = false
	logsFlagLines = 50

	err := logsCmd.RunE(logsCmd, nil)
	assert.Error(t, err)
}
