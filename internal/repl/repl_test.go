package repl

import (
	"testing"
	"time"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDeps(t *testing.T) Deps {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		ActiveWorkspace: "test-workspace",
		Workspaces: map[string]*config.WorkspaceConfig{
			"test-workspace": {SlackToken: "xoxp-test"},
		},
		AI: config.AIConfig{
			Model:         "claude-sonnet-4-20250514",
			ContextBudget: 150000,
		},
		Sync: config.SyncConfig{
			Workers: 1,
		},
	}

	return Deps{
		Config:    cfg,
		DB:        database,
		DBPath:    ":memory:",
		Domain:    "test-domain",
		Workspace: "test-workspace",
	}
}

func TestHelpText(t *testing.T) {
	text := helpText()
	assert.Contains(t, text, "/sync")
	assert.Contains(t, text, "/status")
	assert.Contains(t, text, "/catchup")
	assert.Contains(t, text, "/quit")
	assert.Contains(t, text, "/help")
}

func TestRunStatusFormat(t *testing.T) {
	deps := testDeps(t)

	output := runStatus(deps)
	assert.Contains(t, output, "Workspace:")
	assert.Contains(t, output, "Database:")
	assert.Contains(t, output, "Last sync:")
	assert.Contains(t, output, "Channels:")
}

func TestExtractSourcesSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no sources", "Just some text", ""},
		{"with sources", "Response\n\nSources:\n  [1] link1\n  [2] link2", "Sources:\n  [1] link1\n  [2] link2"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ai.ExtractSourcesSection(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetermineSinceTime(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	defer database.Close()

	// No checkpoint — should default to 24h ago
	since, err := database.DetermineSinceTime(0)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(-24*time.Hour), since, 5*time.Second)
}

func TestProcessInputSlashCommand(t *testing.T) {
	deps := testDeps(t)
	r := &REPL{deps: deps}

	// /help should not panic
	r.handleSlashCommand("/help")
	r.handleSlashCommand("/status")
	r.handleSlashCommand("/unknown")
}
