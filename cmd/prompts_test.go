package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "prompts" {
			found = true
			break
		}
	}
	assert.True(t, found, "prompts command should be registered")
}

func TestPromptsSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"list": false, "show": false, "history": false, "reset": false, "rollback": false}
	for _, cmd := range promptsCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "prompts %s subcommand should be registered", name)
	}
}

func TestTuneCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "tune" {
			found = true
			break
		}
	}
	assert.True(t, found, "tune command should be registered")
}

func TestRunPromptsList(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	promptsListCmd.SetOut(buf)

	err := promptsListCmd.RunE(promptsListCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Prompt Templates:")
	assert.Contains(t, output, "digest.channel")
	assert.Contains(t, output, "tracks.create")
	assert.Contains(t, output, "people.reduce")
}

func TestRunPromptsShow(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	promptsShowCmd.SetOut(buf)

	err := promptsShowCmd.RunE(promptsShowCmd, []string{"digest.channel"})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Prompt: digest.channel")
	assert.Contains(t, output, "Version:")
	assert.Contains(t, output, "---")
}

func TestRunPromptsHistory_WithSeed(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	promptsHistoryCmd.SetOut(buf)

	err := promptsHistoryCmd.RunE(promptsHistoryCmd, []string{"digest.channel"})
	require.NoError(t, err)

	output := buf.String()
	// Seed creates a v1 entry, so we either see the history or "No history"
	assert.Contains(t, output, "digest.channel")
}

func TestRunPromptsHistory_UnknownPrompt(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	promptsHistoryCmd.SetOut(buf)

	err := promptsHistoryCmd.RunE(promptsHistoryCmd, []string{"nonexistent.prompt"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No history")
}

func TestRunPromptsReset(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	promptsResetCmd.SetOut(buf)

	err := promptsResetCmd.RunE(promptsResetCmd, []string{"digest.channel"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "reset to built-in default")
}

func TestRunPromptsRollback_InvalidVersion(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := promptsRollbackCmd.RunE(promptsRollbackCmd, []string{"digest.channel", "abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version")
}

func TestRunPromptsRollback_NonexistentVersion(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := promptsRollbackCmd.RunE(promptsRollbackCmd, []string{"digest.channel", "999"})
	assert.Error(t, err)
}

func TestRunPromptsList_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := promptsListCmd.RunE(promptsListCmd, nil)
	assert.Error(t, err)
}

func TestTuneFlags(t *testing.T) {
	assert.NotNil(t, tuneCmd.Flags().Lookup("apply"))
	assert.NotNil(t, tuneCmd.Flags().Lookup("instructions"))
}
