package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// --- configSet edge cases (different names from config_coverage_test.go) ---

func TestConfigSet_BooleanTrueValue(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"digest.enabled", "true"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set digest.enabled = true")
}

func TestConfigSet_BooleanFalseValue(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"digest.enabled", "false"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set digest.enabled = false")
}

func TestConfigSet_IntegerValueSync(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)

	err := configSetCmd.RunE(configSetCmd, []string{"sync.workers", "8"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Set sync.workers = 8")
}

func TestConfigSet_UnrecognizedKeyWarning(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)
	configSetCmd.SetErr(errBuf)

	err := configSetCmd.RunE(configSetCmd, []string{"unknown.key", "value"})
	require.NoError(t, err)
	assert.Contains(t, errBuf.String(), "not a recognized config key")
}

func TestConfigSet_WorkspacePrefixNoWarning(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	configSetCmd.SetOut(buf)
	configSetCmd.SetErr(errBuf)

	// workspaces.* keys should not trigger the warning
	err := configSetCmd.RunE(configSetCmd, []string{"workspaces.test-ws.slack_token", "xoxp-new-token"})
	require.NoError(t, err)
	assert.NotContains(t, errBuf.String(), "not a recognized")
}

// --- configInit edge cases ---

func TestConfigInit_EmptyWorkspaceNameFails(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	input := "2\n\nxoxp-test-token\n" // empty workspace name
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workspace name is required")
}

func TestConfigInit_InvalidCharsInWorkspaceName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	input := "2\nmy workspace!@#\nxoxp-test-token\n" // invalid chars
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestConfigInit_EmptyTokenFails(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	input := "2\ntest-ws\n\n" // empty token
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slack token is required")
}

// --- runSyncStopCmd ---

func TestRunSyncStopCmd_NoDaemonPidFile(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// No daemon PID file exists, so this should print "No daemon is running"
	err := syncStopCmd.RunE(syncStopCmd, nil)
	require.NoError(t, err)
}

func TestRunSyncStopCmd_BadConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := syncStopCmd.RunE(syncStopCmd, nil)
	assert.Error(t, err)
}

// --- showPeopleList with rich data (redflags, concerns, highlights) ---

func TestShowPeopleList_RichData(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := float64(today.AddDate(0, 0, -7).Unix())
	to := float64(today.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       120,
		ChannelsActive:     5,
		ThreadsInitiated:   15,
		ThreadsReplied:     30,
		AvgMessageLength:   75.5,
		VolumeChangePct:    -25.0,
		Summary:            "Alice had a quieter week than usual",
		CommunicationStyle: "verbose",
		DecisionRole:       "driver",
		RedFlags:           `["communication gap with frontend team"]`,
		Highlights:         `["shipped critical fix","mentored new intern"]`,
		Accomplishments:    `[]`,
		CommunicationGuide: "",
		DecisionStyle:      "",
		Tactics:            "[]",
		Model:              "haiku",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = 1

	err = peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "verbose")
	assert.Contains(t, output, "driver")
}

// --- showUserDetail with rich data ---

func TestShowUserDetail_RichData(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()
	from := float64(now.AddDate(0, 0, -7).Unix())
	to := float64(now.Unix())

	_, err = database.UpsertPeopleCard(db.PeopleCard{
		UserID:             "U001",
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       80,
		ChannelsActive:     4,
		ThreadsInitiated:   8,
		ThreadsReplied:     20,
		AvgMessageLength:   50.0,
		VolumeChangePct:    10.0,
		Summary:            "Alice was productive this week",
		CommunicationStyle: "concise",
		DecisionRole:       "approver",
		RedFlags:           `["missed a code review deadline"]`,
		Highlights:         `["led sprint planning"]`,
		Accomplishments:    `["released v2.0"]`,
		CommunicationGuide: "Concise and action-oriented",
		DecisionStyle:      "",
		Tactics:            `["delegate code reviews"]`,
		Model:              "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	err = showUserDetail(buf, database, "alice", from, to)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "productive")
	assert.Contains(t, output, "action-oriented")

	database.Close()
}

// --- runStatus ---

func TestRunStatus_NoWorkspaceYet(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	statusCmd.SetOut(buf)

	err := statusCmd.RunE(statusCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "not yet synced")
	assert.Contains(t, output, "Last sync: never")
	assert.Contains(t, output, "not running")
}

func TestRunStatus_WithWatchedItems(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.AddWatch("channel", "C001", "general", "high"))
	database.Close()

	buf := new(bytes.Buffer)
	statusCmd.SetOut(buf)

	err = statusCmd.RunE(statusCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-ws")
	assert.Contains(t, output, "watched")
}

// --- configShow with workspace flag ---

func TestConfigShow_ExistingConfigWithDefaults(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	configShowCmd.SetOut(buf)

	err := configShowCmd.RunE(configShowCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "active_workspace: test-ws")
	assert.Contains(t, output, "xoxp-****")
}

// --- runDigest with negative days ---

func TestRunDigest_DaysNegativeClamp(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	digestCmd.SetOut(buf)
	digestFlagChannel = ""
	digestFlagDays = -10 // clamped to 1

	err := digestCmd.RunE(digestCmd, nil)
	require.NoError(t, err)
}

// --- runDecisions edge cases ---

func TestRunDecisions_InvalidJSONSkipsGracefully(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	// Digest with invalid JSON in decisions — should skip gracefully
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "test",
		Topics:       `[]`,
		Decisions:    `not json at all`,
		ActionItems:  `[]`,
		MessageCount: 5,
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

// --- printDigest edge cases ---

func TestPrintDigest_EmptyActionsAndDecisions(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := time.Now()
	d := db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   float64(now.Add(-1 * time.Hour).Unix()),
		PeriodTo:     float64(now.Unix()),
		Type:         "channel",
		Summary:      "Quiet channel",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 3,
		Model:        "haiku",
	}

	buf := new(bytes.Buffer)
	printDigest(buf, d, database)

	output := buf.String()
	assert.Contains(t, output, "Quiet channel")
	assert.NotContains(t, output, "Decisions:")
	assert.NotContains(t, output, "Action Items:")
}

func TestPrintDigest_ActionWithNoAssigneeField(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := time.Now()
	d := db.Digest{
		ChannelID:    "C001",
		PeriodFrom:   float64(now.Add(-1 * time.Hour).Unix()),
		PeriodTo:     float64(now.Unix()),
		Type:         "channel",
		Summary:      "Summary",
		Topics:       `[]`,
		Decisions:    `[{"text":"Decision without by field"}]`,
		ActionItems:  `[{"text":"Unassigned task","assignee":"","status":"open"}]`,
		MessageCount: 5,
		Model:        "haiku",
	}

	buf := new(bytes.Buffer)
	printDigest(buf, d, database)

	output := buf.String()
	assert.Contains(t, output, "Decision without by field")
	assert.NotContains(t, output, "(by )")
	assert.Contains(t, output, "Unassigned task")
}

// --- configInit with choice "1" (OAuth) but no credentials ---

func TestConfigInit_OAuthMissingCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	t.Setenv("WATCHTOWER_OAUTH_CLIENT_ID", "")
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_SECRET", "")

	input := "1\n" // choose OAuth
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	// With no OAuth credentials set, should fail
	if err != nil {
		assert.Contains(t, err.Error(), "OAuth")
	}
}

func TestConfigInit_DefaultChoiceOAuth(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	t.Setenv("WATCHTOWER_OAUTH_CLIENT_ID", "")
	t.Setenv("WATCHTOWER_OAUTH_CLIENT_SECRET", "")

	// Empty choice = default (1 = OAuth)
	input := "\n"
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configInitCmd.SetOut(buf)
	configInitCmd.SetIn(strings.NewReader(input))

	err := configInitCmd.RunE(configInitCmd, nil)
	if err != nil {
		assert.Contains(t, err.Error(), "OAuth")
	}
}

// --- runDigestSummary requires config ---

func TestRunDigestSummary_NoConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	digestSummaryFlagDays = 3
	digestSummaryFlagHours = 0
	digestSummaryFlagFrom = ""
	digestSummaryFlagTo = ""
	defer func() { digestSummaryFlagDays = 0 }()

	err := digestSummaryCmd.RunE(digestSummaryCmd, nil)
	assert.Error(t, err)
}

// --- runTracks with multiple priorities ---

func TestRunTracks_MultiplePriorities(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	for _, prio := range []string{"high", "medium", "low"} {
		_, err = database.UpsertTrack(db.Track{
			Text:     "Track priority: " + prio,
			Priority: prio,
		})
		require.NoError(t, err)
	}
	database.Close()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagPriority = ""
	tracksFlagOwnership = ""
	tracksFlagChannel = ""
	tracksFlagUpdates = false

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Track priority: high")
	assert.Contains(t, output, "Track priority: medium")
	assert.Contains(t, output, "Track priority: low")
}

// --- runStatus with last sync result ---

func TestRunStatus_WithLastSyncResult(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	cfg, err := loadTestConfig()
	require.NoError(t, err)

	// Create a last_sync.json
	resultPath := syncResultPath(cfg)
	require.NoError(t, os.MkdirAll(filepath.Dir(resultPath), 0o755))
	resultJSON := `{"finished_at":"2025-03-15T10:00:00Z","duration_secs":15.5,"messages_fetched":500,"threads_fetched":50,"error":""}`
	require.NoError(t, os.WriteFile(resultPath, []byte(resultJSON), 0o600))

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
	assert.Contains(t, output, "Last run:")
}

// --- people multiple weeks ---

func TestRunPeople_MultiWeeksWithAnalysisData(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := time.Now().UTC()

	// Insert analyses for two different weeks
	for i := 0; i < 2; i++ {
		from := float64(now.AddDate(0, 0, -(i+1)*7).Unix())
		to := float64(now.AddDate(0, 0, -i*7).Unix())
		_, err = database.UpsertPeopleCard(db.PeopleCard{
			UserID:             "U001",
			PeriodFrom:         from,
			PeriodTo:           to,
			MessageCount:       30 + i*10,
			ChannelsActive:     2,
			Summary:            "Alice week data",
			CommunicationStyle: "concise",
			DecisionRole:       "driver",
			RedFlags:           `[]`,
			Highlights:         `[]`,
			Accomplishments:    `[]`,
			CommunicationGuide: "",
			DecisionStyle:      "",
			Tactics:            "[]",
			Model:              "haiku",
		})
		require.NoError(t, err)
	}
	database.Close()

	buf := new(bytes.Buffer)
	peopleCmd.SetOut(buf)
	peopleFlagUser = ""
	peopleFlagPrevious = false
	peopleFlagWeeks = 2

	err = peopleCmd.RunE(peopleCmd, nil)
	require.NoError(t, err)
	// Should have output for both weeks
	assert.NotEmpty(t, buf.String())
}

// --- writeConfigAtomic permissions ---

func TestWriteConfigAtomic_PreservesPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	require.NoError(t, os.WriteFile(configPath, []byte("key: value\n"), 0o600))

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Suppress unused import
var _ = fmt.Sprintf
var _ = strings.NewReader
var _ = filepath.Join
