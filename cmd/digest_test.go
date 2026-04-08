package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestDigestCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "digest" {
			found = true
			break
		}
	}
	assert.True(t, found, "digest command should be registered")
}

func TestDigestSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"generate": false, "stats": false, "summary": false}
	for _, cmd := range digestCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "digest %s subcommand should be registered", name)
	}
}

func TestRunDigest_NoDigests(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = 1

	err := digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No digests available")
}

func TestRunDigest_WithDigests(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// Insert a digest into the DB
	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "The team discussed API changes and deployment timeline",
		Topics:       `["api","deploy"]`,
		Decisions:    `[{"text":"Use REST over gRPC","by":"alice"}]`,
		ActionItems:  `[{"text":"Update docs","assignee":"bob","status":"pending"}]`,
		MessageCount: 25,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = 1

	err = digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "API changes")
}

func TestRunDigest_ChannelFilter(t *testing.T) {
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
		Summary:      "General channel discussion",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 10,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	// Filter by existing channel
	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = "general"
	digestFlagDays = 1
	defer func() { digestFlagChannel = "" }()

	err = digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
	// Output goes through ui.RenderMarkdown which adds ANSI escape codes
	assert.Contains(t, buf.String(), "General channel")
	assert.Contains(t, buf.String(), "discussion")
}

func TestRunDigest_ChannelNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestFlagChannel = "nonexistent"
	digestFlagDays = 1
	defer func() { digestFlagChannel = "" }()

	err := digestCmd.RunE(digestCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunDigestStats_Empty(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestStatsCmd.SetOut(buf)
	digestStatsFlagDays = 7

	err := digestStatsCmd.RunE(digestStatsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No digests generated")
}

func TestRunDigestStats_WithData(t *testing.T) {
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
		Summary:      "test",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 15,
		Model:        "haiku",
		InputTokens:  1000,
		OutputTokens: 200,
		CostUSD:      0,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	digestStatsCmd.SetOut(buf)
	digestStatsFlagDays = 7

	err = digestStatsCmd.RunE(digestStatsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "channel")
	assert.Contains(t, output, "1 digests")
	assert.Contains(t, output, "Total")
}

func TestRunDigestSummary_NoFlags(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = ""
	digestSummaryFlagTo = ""
	digestSummaryFlagDays = 0
	digestSummaryFlagHours = 0

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify --hours, --days, or --from")
}

func TestRunDigestSummary_InvalidFromDate(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = "not-a-date"
	digestSummaryFlagDays = 0
	digestSummaryFlagHours = 0
	defer func() { digestSummaryFlagFrom = "" }()

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --from date")
}

func TestPrintDigest_Channel(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	now := time.Now()
	d := db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   float64(now.Add(-1 * time.Hour).Unix()),
		PeriodTo:     float64(now.Unix()),
		Type:         "channel",
		Summary:      "Active discussion about API design",
		Topics:       `["api","design"]`,
		Decisions:    `[{"text":"Use REST","by":"alice"}]`,
		ActionItems:  `[{"text":"Write tests","assignee":"bob","status":"pending"}]`,
		MessageCount: 42,
		Model:        "haiku",
	}

	buf := new(bytes.Buffer)
	printDigest(buf, d, database, nil)

	output := buf.String()
	assert.Contains(t, output, "#general")
	assert.Contains(t, output, "Channel Digest")
	assert.Contains(t, output, "Active discussion")
	assert.Contains(t, output, "api, design")
	assert.Contains(t, output, "Use REST")
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "Write tests")
	assert.Contains(t, output, "bob")
}

func TestPrintDigest_Daily(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := time.Now()
	d := db.Digest{
		PeriodFrom:   float64(now.Add(-24 * time.Hour).Unix()),
		PeriodTo:     float64(now.Unix()),
		Type:         "daily",
		Summary:      "Daily rollup",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 100,
		Model:        "haiku",
	}

	buf := new(bytes.Buffer)
	printDigest(buf, d, database, nil)
	assert.Contains(t, buf.String(), "Daily Digest")
}

func TestPrintDigest_Weekly(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := time.Now()
	d := db.Digest{
		PeriodFrom:   float64(now.Add(-7 * 24 * time.Hour).Unix()),
		PeriodTo:     float64(now.Unix()),
		Type:         "weekly",
		Summary:      "Weekly trends summary",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 500,
		Model:        "haiku",
	}

	buf := new(bytes.Buffer)
	printDigest(buf, d, database, nil)
	assert.Contains(t, buf.String(), "Weekly Trends")
}

func TestDigestFlags(t *testing.T) {
	assert.NotNil(t, digestCmd.Flags().Lookup("channel"))
	assert.NotNil(t, digestCmd.Flags().Lookup("days"))
	assert.NotNil(t, digestGenerateCmd.Flags().Lookup("since"))
	assert.NotNil(t, digestGenerateCmd.Flags().Lookup("progress-json"))
	assert.NotNil(t, digestStatsCmd.Flags().Lookup("days"))
	assert.NotNil(t, digestSummaryCmd.Flags().Lookup("from"))
	assert.NotNil(t, digestSummaryCmd.Flags().Lookup("to"))
	assert.NotNil(t, digestSummaryCmd.Flags().Lookup("days"))
	assert.NotNil(t, digestSummaryCmd.Flags().Lookup("hours"))
}
