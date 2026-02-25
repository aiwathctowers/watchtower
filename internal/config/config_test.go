package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestLoad_FullConfig(t *testing.T) {
	yaml := `
active_workspace: my-company
workspaces:
  my-company:
    slack_token: "xoxp-test-token"
ai:
  api_key: "sk-ant-test"
  model: "claude-sonnet-4-20250514"
  max_tokens: 8192
  context_budget: 100000
sync:
  workers: 10
  initial_history_days: 60
  poll_interval: 30m
  sync_threads: false
  sync_on_wake: false
watch:
  channels:
    - name: "engineering"
      priority: "high"
  users:
    - name: "alice.smith"
      priority: "high"
`
	path := writeTestConfig(t, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "my-company", cfg.ActiveWorkspace)
	assert.Equal(t, "xoxp-test-token", cfg.Workspaces["my-company"].SlackToken)
	assert.Equal(t, "sk-ant-test", cfg.AI.ApiKey)
	assert.Equal(t, "claude-sonnet-4-20250514", cfg.AI.Model)
	assert.Equal(t, 8192, cfg.AI.MaxTokens)
	assert.Equal(t, 100000, cfg.AI.ContextBudget)
	assert.Equal(t, 10, cfg.Sync.Workers)
	assert.Equal(t, 60, cfg.Sync.InitialHistoryDays)
	assert.False(t, cfg.Sync.SyncThreads)
	assert.False(t, cfg.Sync.SyncOnWake)
	assert.Len(t, cfg.Watch.Channels, 1)
	assert.Equal(t, "engineering", cfg.Watch.Channels[0].Name)
	assert.Equal(t, "high", cfg.Watch.Channels[0].Priority)
	assert.Len(t, cfg.Watch.Users, 1)
	assert.Equal(t, "alice.smith", cfg.Watch.Users[0].Name)
}

func TestLoad_DefaultValues(t *testing.T) {
	path := writeTestConfig(t, "")
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, DefaultAIModel, cfg.AI.Model)
	assert.Equal(t, DefaultAIMaxTokens, cfg.AI.MaxTokens)
	assert.Equal(t, DefaultAIContextBudget, cfg.AI.ContextBudget)
	assert.Equal(t, DefaultSyncWorkers, cfg.Sync.Workers)
	assert.Equal(t, DefaultInitialHistDays, cfg.Sync.InitialHistoryDays)
	assert.Equal(t, DefaultSyncThreads, cfg.Sync.SyncThreads)
	assert.Equal(t, DefaultSyncOnWake, cfg.Sync.SyncOnWake)
}

func TestLoad_MissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	require.NoError(t, err)
	assert.Equal(t, DefaultAIModel, cfg.AI.Model)
}

func TestLoad_EnvVarOverride(t *testing.T) {
	yaml := `
active_workspace: test-ws
workspaces:
  test-ws:
    slack_token: ""
`
	path := writeTestConfig(t, yaml)

	t.Setenv("WATCHTOWER_SLACK_TOKEN", "xoxp-from-env")
	t.Setenv("ANTHROPIC_API_KEY", "sk-from-env")

	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Equal(t, "sk-from-env", cfg.AI.ApiKey)
	assert.Equal(t, "xoxp-from-env", cfg.Workspaces["test-ws"].SlackToken)
}

func TestValidate_Valid(t *testing.T) {
	cfg := &Config{
		ActiveWorkspace: "test",
		Workspaces: map[string]*WorkspaceConfig{
			"test": {SlackToken: "xoxp-token"},
		},
	}
	assert.NoError(t, cfg.Validate())
}

func TestValidate_MissingActiveWorkspace(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "active_workspace is required")
}

func TestValidate_MissingWorkspaceEntry(t *testing.T) {
	cfg := &Config{
		ActiveWorkspace: "nonexistent",
		Workspaces:      map[string]*WorkspaceConfig{},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestValidate_MissingSlackToken(t *testing.T) {
	cfg := &Config{
		ActiveWorkspace: "test",
		Workspaces: map[string]*WorkspaceConfig{
			"test": {SlackToken: ""},
		},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slack_token is required")
}

func TestGetActiveWorkspace(t *testing.T) {
	cfg := &Config{
		ActiveWorkspace: "prod",
		Workspaces: map[string]*WorkspaceConfig{
			"prod": {SlackToken: "xoxp-prod"},
		},
	}
	ws, err := cfg.GetActiveWorkspace()
	require.NoError(t, err)
	assert.Equal(t, "xoxp-prod", ws.SlackToken)
}

func TestGetActiveWorkspace_NoActive(t *testing.T) {
	cfg := &Config{}
	_, err := cfg.GetActiveWorkspace()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no active workspace")
}

func TestDBPath(t *testing.T) {
	cfg := &Config{ActiveWorkspace: "my-company"}
	path := cfg.DBPath()
	assert.Contains(t, path, filepath.Join(".local", "share", "watchtower", "my-company", "watchtower.db"))
}
