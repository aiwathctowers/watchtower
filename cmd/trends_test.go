package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestTrendsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "trends" {
			found = true
			break
		}
	}
	assert.True(t, found, "trends command should be registered")
}

func TestRunTrends_NoData(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err := trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No trends data available")
}

func TestRunTrends_WithWeeklyDigest(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 7*86400,
		PeriodTo:     now,
		Type:         "weekly",
		Summary:      "Busy week with many API changes and deployments",
		Topics:       `["api","deploy","testing"]`,
		Decisions:    `[{"text":"Adopt Go modules","by":"team"}]`,
		ActionItems:  `[{"text":"Update CI pipeline","assignee":"devops"}]`,
		MessageCount: 500,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err = trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "API changes")
}

func TestRunTrends_FallbackToTopics(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())

	// Insert only channel digests (no weekly)
	for _, ch := range []string{"C001", "C002"} {
		_, err = database.UpsertDigest(db.Digest{
			ChannelID:    ch,
			PeriodFrom:   now - 3600,
			PeriodTo:     now,
			Type:         "channel",
			Summary:      "Discussion",
			Topics:       `["api","testing"]`,
			Decisions:    `[]`,
			ActionItems:  `[]`,
			MessageCount: 10,
			Model:        "haiku",
		})
		require.NoError(t, err)
	}
	database.Close()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err = trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Trending Topics")
	assert.Contains(t, output, "api")
	assert.Contains(t, output, "testing")
}

func TestRunTrends_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := trendsCmd.RunE(trendsCmd, nil)
	assert.Error(t, err)
}

func TestShowTopicsSummary_NoTopics(t *testing.T) {
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
		Summary:      "No topics",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 5,
		Model:        "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showTopicsSummary(buf, database)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No topics extracted")

	database.Close()
}
