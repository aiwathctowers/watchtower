package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestProfileCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "profile" {
			found = true
			break
		}
	}
	assert.True(t, found, "profile command should be registered")
}

func TestRunProfile_NoWorkspace(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := profileCmd.RunE(profileCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace found")
}

func TestRunProfile_NoProfile(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	database.Close()

	buf := new(bytes.Buffer)
	profileCmd.SetOut(buf)

	err = profileCmd.RunE(profileCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No profile configured")
}

func TestRunProfile_WithProfile(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID:         "U001",
		Role:                "Engineering Manager",
		Team:                "Platform",
		Manager:             "U099",
		Reports:             `["U002","U003"]`,
		Peers:               `["U004"]`,
		Responsibilities:    `["code review","architecture"]`,
		StarredChannels:     `["C001"]`,
		StarredPeople:       `["U002"]`,
		PainPoints:          `["too many meetings"]`,
		TrackFocus:          `["code_review","decision_needed"]`,
		OnboardingDone:      true,
		CustomPromptContext: "Focus on deployment risks",
	}))
	database.Close()

	buf := new(bytes.Buffer)
	profileCmd.SetOut(buf)

	err = profileCmd.RunE(profileCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "U001")
	assert.Contains(t, output, "Engineering Manager")
	assert.Contains(t, output, "Platform")
	assert.Contains(t, output, "Onboarding: done")
	assert.Contains(t, output, "deployment risks")
}

func TestRunProfile_PartialProfile(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID: "U001",
		Role:        "IC",
	}))
	database.Close()

	buf := new(bytes.Buffer)
	profileCmd.SetOut(buf)

	err = profileCmd.RunE(profileCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "IC")
	assert.Contains(t, output, "Onboarding: not completed")
}

func TestRunProfile_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := profileCmd.RunE(profileCmd, nil)
	assert.Error(t, err)
}
