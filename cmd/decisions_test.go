package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestDecisionsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "decisions" {
			found = true
			break
		}
	}
	assert.True(t, found, "decisions command should be registered")
}

func TestRunDecisions_NoDecisions(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	decisionsCmd.SetOut(buf)
	decisionsFlagDays = 7

	err := decisionsCmd.RunE(decisionsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No decisions found")
}

func TestRunDecisions_WithDecisions(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "Discussion about architecture",
		Topics:       `[]`,
		Decisions:    `[{"text":"Use microservices","by":"alice"},{"text":"Deploy to k8s","by":"bob"}]`,
		ActionItems:  `[]`,
		MessageCount: 20,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	decisionsCmd.SetOut(buf)
	decisionsFlagDays = 7

	err = decisionsCmd.RunE(decisionsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Use microservices")
	assert.Contains(t, output, "(by alice)")
	assert.Contains(t, output, "Deploy to k8s")
	assert.Contains(t, output, "(by bob)")
	assert.Contains(t, output, "#general")
}

func TestRunDecisions_CrossChannel(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "",
		PeriodFrom:   now - 86400,
		PeriodTo:     now,
		Type:         "daily",
		Summary:      "Daily summary",
		Topics:       `[]`,
		Decisions:    `[{"text":"Freeze features for release"}]`,
		ActionItems:  `[]`,
		MessageCount: 50,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	decisionsCmd.SetOut(buf)
	decisionsFlagDays = 7

	err = decisionsCmd.RunE(decisionsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Freeze features for release")
	assert.Contains(t, output, "(cross-channel)")
}

func TestRunDecisions_EmptyDecisionsJSON(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "No decisions here",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 10,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	decisionsCmd.SetOut(buf)
	decisionsFlagDays = 7

	err = decisionsCmd.RunE(decisionsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No decisions found")
}

func TestRunDecisions_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	decisionsFlagDays = 7
	err := decisionsCmd.RunE(decisionsCmd, nil)
	assert.Error(t, err)
}

func TestDecisionsFlags(t *testing.T) {
	assert.NotNil(t, decisionsCmd.Flags().Lookup("days"))
}
