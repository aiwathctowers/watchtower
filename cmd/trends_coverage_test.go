package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestRunTrends_WeeklyWithActionsAndDecisions(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 7*86400,
		PeriodTo:     now,
		Type:         "weekly",
		Summary:      "Productive week with multiple decisions",
		Topics:       `["api","security","testing"]`,
		Decisions:    `[{"text":"Adopt OAuth2","by":"alice"},{"text":"Switch to Go 1.25"}]`,
		ActionItems:  `[{"text":"Update CI pipeline","assignee":"devops"},{"text":"Write migration guide"}]`,
		MessageCount: 300,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err = trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	// Check for key content (through ANSI rendering)
	assert.Contains(t, output, "Productive week")
}

func TestRunTrends_WeeklyNoTopics(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 7*86400,
		PeriodTo:     now,
		Type:         "weekly",
		Summary:      "Quiet week",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 50,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	trendsCmd.SetOut(buf)

	err = trendsCmd.RunE(trendsCmd, nil)
	require.NoError(t, err)
	// ANSI escape codes may split words, so check shorter substrings
	assert.Contains(t, buf.String(), "Quiet")
	assert.Contains(t, buf.String(), "week")
}

func TestShowTopicsSummary_MultipleChannels(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	for i, topics := range []string{
		`["api","testing"]`,
		`["api","deploy"]`,
		`["deploy","monitoring"]`,
		`["api","security"]`,
	} {
		_, err = database.UpsertDigest(db.Digest{
			ChannelID:    "C001",
			PeriodFrom:   now - 3600 - float64(i*100),
			PeriodTo:     now - float64(i*100),
			Type:         "channel",
			Summary:      "Discussion",
			Topics:       topics,
			Decisions:    `[]`,
			ActionItems:  `[]`,
			MessageCount: 10,
			Model:        "haiku",
		})
		require.NoError(t, err)
	}

	buf := new(bytes.Buffer)
	err = showTopicsSummary(buf, database)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Trending Topics")
	assert.Contains(t, output, "api") // should appear 3 times
	assert.Contains(t, output, "deploy")

	database.Close()
}
