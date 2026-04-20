package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/sync"
)

// --- printProgress edge cases ---

func TestPrintProgress_WithVerboseFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	p := sync.NewProgress()

	oldVerbose := flagVerbose
	flagVerbose = true
	defer func() { flagVerbose = oldVerbose }()

	// Should not attempt terminal cursor movement when verbose
	printProgress(buf, p, "test-ws")
	assert.NotEmpty(t, buf.String())
}

func TestPrintProgressJSON_AllFields(t *testing.T) {
	buf := new(bytes.Buffer)
	snap := sync.Snapshot{
		Phase:               sync.PhaseMessages,
		StartTime:           time.Now().Add(-60 * time.Second),
		UsersTotal:          200,
		UsersDone:           100,
		ChannelsTotal:       50,
		ChannelsDone:        25,
		DiscoveryPages:      5,
		DiscoveryTotalPages: 10,
		DiscoveryChannels:   100,
		DiscoveryUsers:      200,
		UserProfilesTotal:   200,
		UserProfilesDone:    100,
		MsgChannelsTotal:    50,
		MsgChannelsDone:     25,
		MessagesFetched:     1000,
	}

	printProgressJSON(buf, snap, nil)
	output := buf.String()
	assert.Contains(t, output, "Messages")
	assert.Contains(t, output, `"users_total":200`)
	assert.Contains(t, output, `"messages_fetched":1000`)
	assert.Contains(t, output, `"discovery_pages":5`)
}

// --- Catchup with channel fallback ---

func TestShowDigestCatchup_ChannelFallbackMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C002", Name: "random", Type: "public"}))

	now := float64(time.Now().Unix())
	for _, ch := range []string{"C001", "C002"} {
		_, err = database.UpsertDigest(db.Digest{
			ChannelID:    ch,
			PeriodFrom:   now - 3600,
			PeriodTo:     now,
			Type:         "channel",
			Summary:      "Discussion in " + ch,
			Topics:       `[]`,
			Decisions:    `[{"text":"Some decision","by":"someone"}]`,
			ActionItems:  `[{"text":"Do something","assignee":"person"}]`,
			MessageCount: 10,
			Model:        "haiku",
		})
		require.NoError(t, err)
	}

	buf := new(bytes.Buffer)
	shown := showDigestCatchup(buf, database, now-100000)
	assert.True(t, shown)
	assert.NotEmpty(t, buf.String())
}

// --- Digest with multiple types ---

func TestRunDigest_DailyDigest(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 43200, // 12h ago — safely within 1-day filter window
		PeriodTo:     now,
		Type:         "daily",
		Summary:      "Daily rollup: busy day across the org",
		Topics:       `["deploy","security"]`,
		Decisions:    `[{"text":"Ship it","by":"CTO"}]`,
		ActionItems:  `[{"text":"Fix security bug","assignee":"security team"}]`,
		MessageCount: 200,
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
	// Should show the daily digest
	output := buf.String()
	assert.Contains(t, output, "Daily")
}

func TestRunDigest_WeeklyDigest(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		// Use 6 days ago to stay safely inside the 7-day filter window even if
		// the wall clock ticks over a second between setup and command execution.
		PeriodFrom:   now - 6*86400,
		PeriodTo:     now,
		Type:         "weekly",
		Summary:      "Weekly summary: productive sprint",
		Topics:       `["planning"]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 500,
		Model:        "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = 7

	err = digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Weekly")
}

// --- Config set with string value ---

func TestConfigSet_StringValue(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	// Using the config from setupWatchTestEnv
	err := configSetCmd.RunE(configSetCmd, []string{"digest.language", "russian"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set digest.language = russian")
}

// --- Watch list with items of different types ---

func TestWatchListWithMixedItems(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	watchFlagPriority = "high"
	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"#general"}))

	watchFlagPriority = "low"
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"@alice"}))

	watchFlagPriority = "normal"
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"@bob"}))

	buf.Reset()
	watchListCmd.SetOut(buf)
	err := watchListCmd.RunE(watchListCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "#general")
	assert.Contains(t, output, "@alice")
	assert.Contains(t, output, "@bob")
	assert.Contains(t, output, "high")
	assert.Contains(t, output, "low")
}

// --- Resolve target edge cases ---

func TestResolveTarget_UserNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	_, _, _, err = resolveTarget(database, "@nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveTarget_NoPrefixNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	_, _, _, err = resolveTarget(database, "doesnotexist")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- cliGenerator ---

func TestCliGenerator(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	cfg, err := loadTestConfig()
	require.NoError(t, err)

	gen := cliGenerator(cfg)
	assert.NotNil(t, gen)
}

// loadTestConfig is a helper to load config for testing
func loadTestConfig() (*config.Config, error) {
	return config.Load(flagConfig)
}

// --- Digest stats with multiple types ---

func TestRunDigestStats_MultipleTypes(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	for _, typ := range []string{"channel", "daily", "channel"} {
		_, err = database.UpsertDigest(db.Digest{
			ChannelID:    "C001",
			PeriodFrom:   now - 3600,
			PeriodTo:     now,
			Type:         typ,
			Summary:      "test",
			Topics:       `[]`,
			Decisions:    `[]`,
			ActionItems:  `[]`,
			MessageCount: 15,
			Model:        "haiku",
			InputTokens:  500,
			OutputTokens: 100,
			CostUSD:      0,
		})
		require.NoError(t, err)
		now += 10 // slightly different period_from to avoid dedup
	}
	database.Close()

	buf := new(bytes.Buffer)
	digestStatsCmd.SetOut(buf)
	digestStatsFlagDays = 7

	err = digestStatsCmd.RunE(digestStatsCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "channel")
	assert.Contains(t, output, "Total")
}
