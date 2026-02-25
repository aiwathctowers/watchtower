package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAskCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "ask" {
			found = true
			break
		}
	}
	assert.True(t, found, "ask command should be registered")
}

func TestAskCommandRequiresArgs(t *testing.T) {
	err := askCmd.Args(askCmd, nil)
	assert.Error(t, err)
}

func TestAskCommandAcceptsArgs(t *testing.T) {
	err := askCmd.Args(askCmd, []string{"what happened yesterday"})
	assert.NoError(t, err)
}

func TestAskCommandRequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/path/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := askCmd.RunE(askCmd, []string{"test question"})
	assert.Error(t, err)
}

func TestAskCommandFlags(t *testing.T) {
	f := askCmd.Flags()

	assert.NotNil(t, f.Lookup("model"))
	assert.NotNil(t, f.Lookup("no-stream"))
	assert.NotNil(t, f.Lookup("channel"))
	assert.NotNil(t, f.Lookup("since"))
}

func TestExtractSourcesSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no sources",
			input:    "Just some regular text response.",
			expected: "",
		},
		{
			name:     "with sources",
			input:    "Some response text\n\nSources:\n  [1] #general 2025-02-24 14:30 — https://example.slack.com/archives/C001/p1234\n",
			expected: "Sources:\n  [1] #general 2025-02-24 14:30 — https://example.slack.com/archives/C001/p1234",
		},
		{
			name:     "empty sources",
			input:    "Response\nSources:\n",
			expected: "Sources:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSourcesSection(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
