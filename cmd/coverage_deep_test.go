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

// --- parseTrackID ---

func TestParseTrackID_Valid(t *testing.T) {
	id, err := parseTrackID("42")
	require.NoError(t, err)
	assert.Equal(t, 42, id)
}

func TestParseTrackID_NotANumber(t *testing.T) {
	_, err := parseTrackID("abc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid track ID")
}

func TestParseTrackID_Zero(t *testing.T) {
	_, err := parseTrackID("0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")
}

func TestParseTrackID_Negative(t *testing.T) {
	_, err := parseTrackID("-5")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")
}

// --- parseSnoozeUntil ---

func TestParseSnoozeUntil_Tomorrow(t *testing.T) {
	result, err := parseSnoozeUntil("tomorrow", 0)
	require.NoError(t, err)
	assert.True(t, result.After(time.Now()))
	assert.Equal(t, 9, result.Hour())
}

func TestParseSnoozeUntil_NextWeek(t *testing.T) {
	result, err := parseSnoozeUntil("next-week", 0)
	require.NoError(t, err)
	assert.True(t, result.After(time.Now()))
	assert.Equal(t, time.Monday, result.Weekday())
}

func TestParseSnoozeUntil_WeekdayNames(t *testing.T) {
	for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		result, err := parseSnoozeUntil(day, 0)
		require.NoError(t, err, "day=%s", day)
		assert.True(t, result.After(time.Now()), "day=%s should be in the future", day)
		assert.Equal(t, 9, result.Hour(), "day=%s", day)
	}
}

func TestParseSnoozeUntil_FutureDate(t *testing.T) {
	futureDate := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	result, err := parseSnoozeUntil(futureDate, 0)
	require.NoError(t, err)
	assert.True(t, result.After(time.Now()))
}

func TestParseSnoozeUntil_PastDate(t *testing.T) {
	pastDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	_, err := parseSnoozeUntil(pastDate, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "in the past")
}

func TestParseSnoozeUntil_InvalidDate(t *testing.T) {
	_, err := parseSnoozeUntil("not-a-date", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --until value")
}

func TestParseSnoozeUntil_BothFlags(t *testing.T) {
	_, err := parseSnoozeUntil("tomorrow", 4)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not both")
}

func TestParseSnoozeUntil_NoFlags(t *testing.T) {
	_, err := parseSnoozeUntil("", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify --until")
}

func TestParseSnoozeUntil_HoursOnly(t *testing.T) {
	result, err := parseSnoozeUntil("", 4)
	require.NoError(t, err)
	expected := time.Now().Add(4 * time.Hour)
	assert.InDelta(t, expected.Unix(), result.Unix(), 2)
}

// --- dayOfWeek ---

func TestDayOfWeek_AllDays(t *testing.T) {
	days := map[string]time.Weekday{
		"sunday":    time.Sunday,
		"monday":    time.Monday,
		"tuesday":   time.Tuesday,
		"wednesday": time.Wednesday,
		"thursday":  time.Thursday,
		"friday":    time.Friday,
		"saturday":  time.Saturday,
	}
	for name, expected := range days {
		assert.Equal(t, expected, dayOfWeek(name), "day=%s", name)
	}
}

func TestDayOfWeek_UnknownFallback(t *testing.T) {
	assert.Equal(t, time.Monday, dayOfWeek("bogus"))
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

func TestRunTracks_InvalidStatus(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagStatus = "invalid_status"
	defer func() { tracksFlagStatus = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --status")
}

func TestRunTracks_InvalidPriority(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagPriority = "invalid_priority"
	defer func() { tracksFlagPriority = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --priority")
}

func TestRunTracks_InvalidOwnership(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagOwnership = "invalid_ownership"
	defer func() { tracksFlagOwnership = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --ownership")
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

func TestRunTracks_StatusAll(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagStatus = "all"
	defer func() { tracksFlagStatus = "" }()

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
			ID:              1,
			Text:            "Review PR #123",
			Status:          "active",
			Priority:        "high",
			Category:        "code_review",
			ChannelID:       "C001",
			RequesterName:   "alice",
			Tags:            `["urgent","frontend"]`,
			Context:         "PR is blocking deployment",
			Blocking:        "production deploy",
			DecisionSummary: "Need approval for new API",
			DueDate:         float64(time.Now().Add(24 * time.Hour).Unix()),
			HasUpdates:      true,
			Ownership:       "delegated",
			CreatedAt:       time.Now().Add(-2 * time.Hour).Format("2006-01-02T15:04:05Z"),
		},
		{
			ID:                2,
			Text:              "Investigate memory leak",
			Status:            "snoozed",
			Priority:          "low",
			Category:          "bug_fix",
			SourceChannelName: "backend",
			SnoozeUntil:       float64(time.Now().Add(48 * time.Hour).Unix()),
			Ownership:         "watching",
			CreatedAt:         time.Now().Add(-5 * time.Hour).Format("2006-01-02T15:04:05Z"),
		},
		{
			ID:       3,
			Text:     "Unknown priority item",
			Status:   "done",
			Priority: "unknown",
			Category: "discussion",
		},
	}

	var buf bytes.Buffer
	printTracks(&buf, items, database)
	output := buf.String()

	// Item 1 fields
	assert.Contains(t, output, "#1")
	assert.Contains(t, output, "Review PR #123")
	assert.Contains(t, output, "#general")
	assert.Contains(t, output, "from: alice")
	assert.Contains(t, output, "blocking")
	assert.Contains(t, output, "Blocking: production deploy")
	assert.Contains(t, output, "Decision: Need approval")
	assert.Contains(t, output, "due:")

	// Item 2 fields
	assert.Contains(t, output, "#2")
	assert.Contains(t, output, "memory leak")
	assert.Contains(t, output, "[snoozed]")
	assert.Contains(t, output, "snoozed until")
	assert.Contains(t, output, "#backend")

	// Item 3 fields
	assert.Contains(t, output, "#3")
	assert.Contains(t, output, "[done]")
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

// --- tracksAccept ---

func TestRunTracksAccept_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := tracksAcceptCmd.RunE(tracksAcceptCmd, []string{"1"})
	assert.Error(t, err)
}

// --- tracksDone / tracksDismiss ---

func TestRunTracksDismiss_InvalidID(t *testing.T) {
	err := tracksDismissCmd.RunE(tracksDismissCmd, []string{"xyz"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid track ID")
}

func TestRunTracksSnooze_InvalidID(t *testing.T) {
	err := tracksSnoozeCmd.RunE(tracksSnoozeCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid track ID")
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
		CostUSD:      0.01,
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
	assert.Contains(t, output, "0.0100")
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
	printDigest(&buf, d, database)
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
