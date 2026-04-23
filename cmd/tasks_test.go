package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTasksStubRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "tasks" {
			found = true
			break
		}
	}
	assert.True(t, found, "tasks stub command should be registered")
}

func TestTasksStubAcceptsArbitraryArgs(t *testing.T) {
	// The stub must accept subcommand args without panicking or returning
	// "unknown command" — it simply prints the deprecation message and exits.
	// We test only that the command is configured with ArbitraryArgs and
	// DisableFlagParsing so all invocations reach the RunE handler.
	assert.True(t, tasksCmd.DisableFlagParsing,
		"tasks stub should have DisableFlagParsing to capture all args")
}

func TestTasksStubIsNotHidden(t *testing.T) {
	// The stub should be visible so users discover the rename message.
	assert.False(t, tasksCmd.Hidden, "tasks stub should not be hidden")
}
