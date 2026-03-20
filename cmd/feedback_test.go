package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "feedback" {
			found = true
			break
		}
	}
	assert.True(t, found, "feedback command should be registered")
}

func TestRunFeedback_Good(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	feedbackFlagComment = ""

	err := feedbackCmd.RunE(feedbackCmd, []string{"good", "digest", "42"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Feedback #")
	assert.Contains(t, buf.String(), "digest")
	assert.Contains(t, buf.String(), "42")
}

func TestRunFeedback_Bad(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	feedbackFlagComment = "not a real decision"

	err := feedbackCmd.RunE(feedbackCmd, []string{"bad", "track", "7"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Feedback #")
	assert.Contains(t, buf.String(), "track")
}

func TestRunFeedback_Plus1(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	feedbackFlagComment = ""

	err := feedbackCmd.RunE(feedbackCmd, []string{"+1", "decision", "42:0"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Feedback #")
}

func TestRunFeedback_Minus1(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	feedbackFlagComment = ""

	err := feedbackCmd.RunE(feedbackCmd, []string{"-1", "user_analysis", "5"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Feedback #")
}

func TestRunFeedback_InvalidRating(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	feedbackFlagComment = ""
	err := feedbackCmd.RunE(feedbackCmd, []string{"maybe", "digest", "1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid rating")
}

func TestRunFeedback_InvalidType(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	feedbackFlagComment = ""
	err := feedbackCmd.RunE(feedbackCmd, []string{"good", "invalid_type", "1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid type")
}

func TestRunFeedbackStats_Empty(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	feedbackStatsCmd.SetOut(buf)

	err := feedbackStatsCmd.RunE(feedbackStatsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No feedback recorded yet")
}

func TestRunFeedbackStats_WithData(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// First add some feedback
	feedbackFlagComment = ""
	buf := new(bytes.Buffer)
	feedbackCmd.SetOut(buf)
	require.NoError(t, feedbackCmd.RunE(feedbackCmd, []string{"good", "digest", "1"}))
	require.NoError(t, feedbackCmd.RunE(feedbackCmd, []string{"good", "digest", "2"}))
	require.NoError(t, feedbackCmd.RunE(feedbackCmd, []string{"bad", "digest", "3"}))
	require.NoError(t, feedbackCmd.RunE(feedbackCmd, []string{"good", "track", "1"}))

	// Now check stats
	buf.Reset()
	feedbackStatsCmd.SetOut(buf)
	err := feedbackStatsCmd.RunE(feedbackStatsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Feedback Statistics")
	assert.Contains(t, output, "digest")
	assert.Contains(t, output, "track")
}

func TestRunFeedback_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	feedbackFlagComment = ""
	err := feedbackCmd.RunE(feedbackCmd, []string{"good", "digest", "1"})
	assert.Error(t, err)
}

func TestFeedbackFlags(t *testing.T) {
	assert.NotNil(t, feedbackCmd.Flags().Lookup("message"))
}
