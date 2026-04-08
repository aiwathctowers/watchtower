package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// --- buildDigestContext ---

func TestBuildDigestContext_DailyAvailable(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "daily",
		Summary:      "Productive day across all teams",
		Topics:       `["deploy","security"]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 100,
		Model:        "haiku",
	})
	require.NoError(t, err)

	result := buildDigestContext(database)
	assert.Contains(t, result, "Daily summary: Productive day")
	assert.Contains(t, result, "Topics: deploy, security")
}

func TestBuildDigestContext_ChannelFallback(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "API discussion in general",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 15,
		Model:        "haiku",
	})
	require.NoError(t, err)

	result := buildDigestContext(database)
	assert.Contains(t, result, "#general")
	assert.Contains(t, result, "15 msgs")
	assert.Contains(t, result, "API discussion")
}

func TestBuildDigestContext_NoDigests(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	result := buildDigestContext(database)
	assert.Empty(t, result)
}

// --- appendTopics ---

func TestAppendTopics_Empty(t *testing.T) {
	var sb strings.Builder
	appendTopics(&sb, `[]`)
	assert.Empty(t, sb.String())
}

func TestAppendTopics_InvalidJSON(t *testing.T) {
	var sb strings.Builder
	appendTopics(&sb, `not json`)
	assert.Empty(t, sb.String())
}

func TestAppendTopics_WithTopics(t *testing.T) {
	var sb strings.Builder
	appendTopics(&sb, `["api","deploy","testing"]`)
	assert.Contains(t, sb.String(), "Topics: api, deploy, testing")
}

// --- printJSONList ---

func TestPrintJSONList_EmptyArray(t *testing.T) {
	buf := new(bytes.Buffer)
	printJSONList(buf, "  Items: ", "[]")
	assert.Empty(t, buf.String())
}

func TestPrintJSONList_EmptyString(t *testing.T) {
	buf := new(bytes.Buffer)
	printJSONList(buf, "  Items: ", "")
	assert.Empty(t, buf.String())
}

func TestPrintJSONList_InvalidJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	printJSONList(buf, "  Items: ", "not json at all")
	assert.Contains(t, buf.String(), "(invalid JSON)")
}

func TestPrintJSONList_ValidItems(t *testing.T) {
	buf := new(bytes.Buffer)
	printJSONList(buf, "  Reports: ", `["U001","U002","U003"]`)
	assert.Contains(t, buf.String(), "Reports: U001, U002, U003")
}

func TestPrintJSONList_EmptyParsedArray(t *testing.T) {
	buf := new(bytes.Buffer)
	// JSON is valid but parses to empty array
	printJSONList(buf, "  Items: ", `  []  `)
	// This won't match the simple "[]" check, but will parse to empty
	assert.Empty(t, buf.String())
}

// --- printLastLines ---

func TestPrintLastLines_FileNotFound(t *testing.T) {
	buf := new(bytes.Buffer)
	err := printLastLines(buf, "/nonexistent/file.log", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "opening log file")
}

func TestPrintLastLines_FewerLinesThanRequested(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.log")
	require.NoError(t, os.WriteFile(tmpFile, []byte("line1\nline2\n"), 0o600))

	buf := new(bytes.Buffer)
	err := printLastLines(buf, tmpFile, 100)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "line1")
	assert.Contains(t, buf.String(), "line2")
}

func TestPrintLastLines_ExactN(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.log")
	require.NoError(t, os.WriteFile(tmpFile, []byte("a\nb\nc\nd\ne\n"), 0o600))

	buf := new(bytes.Buffer)
	err := printLastLines(buf, tmpFile, 2)
	require.NoError(t, err)
	output := buf.String()
	assert.NotContains(t, output, "a\n")
	assert.NotContains(t, output, "b\n")
	assert.NotContains(t, output, "c\n")
	assert.Contains(t, output, "d")
	assert.Contains(t, output, "e")
}

// --- printDigestDetails ---

func TestPrintDigestDetails_WithDecisionsAndActions(t *testing.T) {
	d := db.Digest{
		Decisions:   `[{"text":"Use REST","by":"alice"},{"text":"Freeze code"}]`,
		ActionItems: `[{"text":"Update docs","assignee":"bob"},{"text":"Deploy","assignee":""}]`,
	}
	var buf bytes.Buffer
	printDigestDetails(&buf, d)
	output := buf.String()
	assert.Contains(t, output, "Use REST")
	assert.Contains(t, output, "by alice")
	assert.Contains(t, output, "Freeze code")
	assert.Contains(t, output, "Update docs")
	assert.Contains(t, output, "-> bob")
	assert.Contains(t, output, "Deploy")
}

func TestPrintDigestDetails_EmptyDecisionsAndActions(t *testing.T) {
	d := db.Digest{
		Decisions:   `[]`,
		ActionItems: `[]`,
	}
	var buf bytes.Buffer
	printDigestDetails(&buf, d)
	assert.Empty(t, buf.String())
}

func TestPrintDigestDetails_InvalidJSON(t *testing.T) {
	d := db.Digest{
		Decisions:   `not json`,
		ActionItems: `also not json`,
	}
	var buf bytes.Buffer
	printDigestDetails(&buf, d)
	assert.Empty(t, buf.String())
}

// --- maskValue ---

func TestMaskValue_Long(t *testing.T) {
	assert.Equal(t, "xoxp-****", maskValue("xoxp-12345"))
}

func TestMaskValue_Short(t *testing.T) {
	assert.Equal(t, "****", maskValue("abc"))
}

func TestMaskValue_ExactlyFive(t *testing.T) {
	assert.Equal(t, "****", maskValue("abcde"))
}

func TestMaskValue_SixChars(t *testing.T) {
	assert.Equal(t, "abcde****", maskValue("abcdef"))
}

// --- runDigest edge cases ---

func TestRunDigest_DaysClampedHigh(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = 999999 // should be clamped to 3650

	err := digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
	// Should run without overflow
	assert.Contains(t, buf.String(), "No digests available")
}

func TestRunDigest_DaysClampedZero(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = -5 // should be clamped to 1

	err := digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
}

// --- runDigestSummary time flag parsing ---

func TestRunDigestSummary_InvalidToDate(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	digestSummaryFlagFrom = "2025-01-01"
	digestSummaryFlagTo = "not-a-date"
	digestSummaryFlagDays = 0
	digestSummaryFlagHours = 0
	defer func() {
		digestSummaryFlagFrom = ""
		digestSummaryFlagTo = ""
	}()

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --to date")
}

// --- runDecisions edge cases ---

func TestRunDecisions_DaysClampedToSeven(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	decisionsCmd.SetOut(buf)
	decisionsFlagDays = -3 // should be clamped to 7

	err := decisionsCmd.RunE(decisionsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No decisions found")
}

// --- runTracks validation ---

func TestRunTracks_InvalidPriority(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagPriority = "invalid_priority"
	defer func() { tracksFlagPriority = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --priority")
}

func TestRunTracks_ChannelNotFound(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagChannel = "nonexistent-channel"
	defer func() { tracksFlagChannel = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunTracks_Empty(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)

	err := tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No tracks found")
}

// --- printTracks comprehensive ---

func TestPrintTracks_AllFields(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	items := []db.Track{
		{
			ID:         1,
			Text:       "API Redesign Discussion",
			Context:    "Under review",
			Priority:   "high",
			Ownership:  "mine",
			Category:   "code_review",
			ChannelIDs: `["C001"]`,
			Tags:       `["urgent","frontend"]`,
			HasUpdates: true,
			UpdatedAt:  time.Now().Add(-2 * time.Hour).Format("2006-01-02T15:04:05Z"),
		},
		{
			ID:        2,
			Text:      "Memory Leak Investigation",
			Priority:  "low",
			Ownership: "watching",
			Category:  "bug_fix",
			Tags:      `[]`,
		},
		{
			ID:        3,
			Text:      "Unknown priority item",
			Priority:  "medium",
			Ownership: "mine",
			Category:  "task",
		},
	}

	var buf bytes.Buffer
	printTracks(&buf, items, database, false)
	output := buf.String()

	assert.Contains(t, output, "#1")
	assert.Contains(t, output, "API Redesign Discussion")
	assert.Contains(t, output, "#2")
	assert.Contains(t, output, "Memory Leak Investigation")
	assert.Contains(t, output, "#3")
}

// --- configShow edge cases ---

func TestConfigShow_NoConfigFile(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = filepath.Join(t.TempDir(), "nonexistent.yaml")
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configShowCmd.SetOut(buf)

	err := configShowCmd.RunE(configShowCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No config file found")
}

// --- configSet edge cases ---

func TestConfigSet_DurationValue(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"sync.poll_interval", "15m"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set sync.poll_interval = 15m")
}

// --- runLogs edge cases ---

func TestRunLogs_AllLines(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	cfg, err := loadTestConfig()
	require.NoError(t, err)

	logPath := syncLogFilePath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(logPath), 0o755))
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0o600))

	logsFlagFollow = false
	logsFlagLines = 100 // more than file has

	buf := new(bytes.Buffer)
	logsCmd.SetOut(buf)

	err = logsCmd.RunE(logsCmd, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "line1")
	assert.Contains(t, output, "line10")
}

// --- runStatus with last sync time ---

func TestRunStatus_WithDaemonNotRunning(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	database.Close()

	buf := new(bytes.Buffer)
	statusCmd.SetOut(buf)

	err = statusCmd.RunE(statusCmd, nil)
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "test-ws")
	assert.Contains(t, output, "not running")
}

// --- prompts rollback valid version ---

func TestRunPromptsRollback_ValidVersionNotFound(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := promptsRollbackCmd.RunE(promptsRollbackCmd, []string{"digest.channel", "0"})
	assert.Error(t, err)
}

// --- digestStats all-time cost ---

func TestRunDigestStats_AllTimeCost(t *testing.T) {
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
		MessageCount: 20,
		Model:        "haiku",
		InputTokens:  2000,
		OutputTokens: 500,
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
	assert.Contains(t, output, "All time")
	assert.Contains(t, output, "1 digests")
}

// --- printDigest with channel lookup miss ---

func TestPrintDigest_ChannelNotInDB(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := time.Now()
	d := db.Digest{
		ChannelID:    "C999",
		PeriodFrom:   float64(now.Add(-1 * time.Hour).Unix()),
		PeriodTo:     float64(now.Unix()),
		Type:         "channel",
		Summary:      "Discussion in unknown channel",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 5,
		Model:        "haiku",
	}

	var buf bytes.Buffer
	printDigest(&buf, d, database, nil)
	output := buf.String()
	// Should fallback to channel ID since channel not in DB
	assert.Contains(t, output, "C999")
	assert.Contains(t, output, "Channel Digest")
}

// --- openDBFromConfig ---

func TestOpenDBFromConfig_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	_, err := openDBFromConfig()
	assert.Error(t, err)
}

// --- runDigestStats clamp zero ---

func TestRunDigestStats_DaysClampedToSeven(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestStatsCmd.SetOut(buf)
	digestStatsFlagDays = -5 // should be clamped to 7

	err := digestStatsCmd.RunE(digestStatsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No digests generated")
}
