package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigSet_BoolTrue(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"digest.enabled", "true"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set digest.enabled = true")

	data, _ := os.ReadFile(configPath)
	assert.Contains(t, string(data), "true")
}

func TestConfigSet_BoolFalse(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"sync.sync_threads", "false"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set sync.sync_threads = false")
}

func TestConfigSet_Integer(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"sync.workers", "8"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set sync.workers = 8")
}

func TestConfigSet_Duration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"sync.poll_interval", "15m"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set sync.poll_interval = 15m")
}

func TestConfigSet_UnknownKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)
	configSetCmd.SetErr(errBuf)

	err := configSetCmd.RunE(configSetCmd, []string{"unknown.key", "value"})
	require.NoError(t, err)
	assert.Contains(t, errBuf.String(), "not a recognized config key")
}

func TestConfigSet_WorkspaceKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)
	configSetCmd.SetErr(errBuf)

	// workspace-level keys should not trigger warning
	err := configSetCmd.RunE(configSetCmd, []string{"workspaces.test.slack_token", "xoxp-new"})
	require.NoError(t, err)
	assert.Empty(t, errBuf.String())
	assert.Contains(t, buf.String(), "Set workspaces.test.slack_token = xoxp-new")
}

func TestConfigSet_NoConfigFile(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := configSetCmd.RunE(configSetCmd, []string{"ai.model", "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading config")
}

func TestConfigInit_ManualEmptyWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	input := "2\n\nxoxp-test-token\n"
	buf := new(bytes.Buffer)

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workspace name is required")
}

func TestConfigInit_ManualEmptyToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	input := "2\ntest-workspace\n\n"
	buf := new(bytes.Buffer)

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slack token is required")
}

func TestConfigInit_ManualInvalidWorkspaceName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	input := "2\nhas spaces!\nxoxp-test-token\n"
	buf := new(bytes.Buffer)

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestWriteConfigAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Write a config atomically
	require.NoError(t, os.WriteFile(configPath, []byte("active_workspace: test\n"), 0o600))

	// Verify permissions
	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestKnownConfigKeys(t *testing.T) {
	// Verify all expected keys are in the map
	expectedKeys := []string{
		"active_workspace",
		"ai.model",
		"ai.context_budget",
		"sync.workers",
		"sync.initial_history_days",
		"sync.poll_interval",
		"sync.sync_threads",
		"sync.sync_on_wake",
		"digest.enabled",
		"digest.model",
		"digest.min_messages",
		"digest.language",
		"digest.workers",
		"claude_path",
	}

	for _, key := range expectedKeys {
		assert.True(t, knownConfigKeys[key], "key %q should be in knownConfigKeys", key)
	}

	// Verify unknown key is NOT in the map
	assert.False(t, knownConfigKeys["nonexistent.key"])
}
