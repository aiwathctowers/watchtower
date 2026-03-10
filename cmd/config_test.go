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

func TestConfigSubcommands(t *testing.T) {
	assert.NotNil(t, configCmd)
	names := make([]string, 0)
	for _, sub := range configCmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "init")
	assert.Contains(t, names, "set")
	assert.Contains(t, names, "show")
}

func TestConfigInit(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Simulate user input: choose manual auth (2), then workspace name + slack token
	input := "2\ntest-workspace\nxoxp-test-token\n"

	buf := new(bytes.Buffer)
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Config written to:")
	assert.Contains(t, output, "Database directory:")

	// Verify config file was created
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "test-workspace")
	assert.Contains(t, content, "xoxp-test-token")
}

func TestConfigSet(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create an initial config file
	initial := "active_workspace: test\n"
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0o644))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"ai.model", "claude-opus-4-6"})
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "Set ai.model = claude-opus-4-6")

	// Verify the value was written
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "claude-opus-4-6")
}

func TestConfigShow(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `active_workspace: demo
workspaces:
  demo:
    slack_token: "xoxp-secret-token-here"
ai:
  model: "claude-sonnet-4-6"
`
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o644))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configShowCmd.SetOut(buf)

	err := configShowCmd.RunE(configShowCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	// Tokens should be masked
	assert.NotContains(t, output, "xoxp-secret-token-here")
	assert.Contains(t, output, "****")
	// Non-sensitive values should appear
	assert.Contains(t, output, "claude-sonnet-4-6")
	assert.Contains(t, output, "demo")
	// Defaults should be shown
	assert.Contains(t, output, "sync.workers:")
	assert.Contains(t, output, "sync.poll_interval:")
}

func TestConfigShow_NoFile(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configShowCmd.SetOut(buf)

	err := configShowCmd.RunE(configShowCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No config file found")
}

func TestMaskValue(t *testing.T) {
	assert.Equal(t, "****", maskValue("short"))
	assert.Equal(t, "twelv****", maskValue("twelve-char"))
	assert.Equal(t, "xoxp-****", maskValue("xoxp-secret-token-here"))
}
