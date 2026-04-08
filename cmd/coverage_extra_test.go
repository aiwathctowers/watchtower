package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// --- Profile edge cases ---

func TestRunProfile_OnboardingDone(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID:         "U001",
		Role:                "Engineering Manager",
		Team:                "Platform",
		Manager:             "VP Engineering",
		Reports:             `["alice","bob"]`,
		Peers:               `["carol"]`,
		Responsibilities:    `["code review","architecture"]`,
		StarredChannels:     `["general","engineering"]`,
		StarredPeople:       `["alice"]`,
		PainPoints:          `["too many meetings"]`,
		TrackFocus:          `["deployment","security"]`,
		OnboardingDone:      true,
		CustomPromptContext: "Focus on security-related items",
	}))
	database.Close()

	buf := new(bytes.Buffer)
	profileCmd.SetOut(buf)

	err = profileCmd.RunE(profileCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Engineering Manager")
	assert.Contains(t, output, "Platform")
	assert.Contains(t, output, "VP Engineering")
	assert.Contains(t, output, "alice, bob")
	assert.Contains(t, output, "carol")
	assert.Contains(t, output, "code review, architecture")
	assert.Contains(t, output, "general, engineering")
	assert.Contains(t, output, "too many meetings")
	assert.Contains(t, output, "deployment, security")
	assert.Contains(t, output, "Onboarding: done")
	assert.Contains(t, output, "Focus on security-related items")
}

func TestRunProfile_OnboardingNotDone(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	require.NoError(t, database.UpsertUserProfile(db.UserProfile{
		SlackUserID:    "U001",
		Role:           "Developer",
		OnboardingDone: false,
	}))
	database.Close()

	buf := new(bytes.Buffer)
	profileCmd.SetOut(buf)

	err = profileCmd.RunE(profileCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Developer")
	assert.Contains(t, output, "Onboarding: not completed")
}

// --- Status with last sync time ---
func TestStatusCommand_WithLastSync(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configYAML := `active_workspace: test-ws
workspaces:
  test-ws:
    slack_token: "xoxp-test-token"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

	homeDir := tmpDir
	wsDBDir := filepath.Join(homeDir, ".local", "share", "watchtower", "test-ws")
	require.NoError(t, os.MkdirAll(wsDBDir, 0o755))
	wsDBPath := filepath.Join(wsDBDir, "watchtower.db")

	database, err := db.Open(wsDBPath)
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))
	require.NoError(t, database.UpsertMessage(db.Message{ChannelID: "C001", TS: "1700000000.000001", Text: "hello"}))
	database.Close()

	t.Setenv("HOME", homeDir)
	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	statusCmd.SetOut(buf)

	err = statusCmd.RunE(statusCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Workspace: test-ws")
	assert.Contains(t, output, "Database:")
	assert.Contains(t, output, "Daemon:")
}

// --- Watch remove for user ---
func TestWatchRemoveUser(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// First add a user watch
	watchFlagPriority = "normal"
	buf := new(bytes.Buffer)
	watchAddCmd.SetOut(buf)
	require.NoError(t, watchAddCmd.RunE(watchAddCmd, []string{"@bob"}))

	// Now remove it
	buf.Reset()
	watchRemoveCmd.SetOut(buf)
	err := watchRemoveCmd.RunE(watchRemoveCmd, []string{"@bob"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Removed @bob from watch list")
}

// --- Tracks with priority filter ---
func TestRunTracks_PriorityFilter(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	_, err = database.UpsertTrack(db.Track{
		Text:       "High priority task",
		Priority:   "high",
		ChannelIDs: `["C001"]`,
	})
	require.NoError(t, err)

	_, err = database.UpsertTrack(db.Track{
		Text:       "Low priority task",
		Priority:   "low",
		ChannelIDs: `["C001"]`,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagPriority = "high"
	tracksFlagOwnership = ""
	tracksFlagChannel = ""
	tracksFlagUpdates = false
	defer func() { tracksFlagPriority = "" }()

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "High priority task")
}

// --- Tracks channel filter success ---
func TestRunTracks_ChannelFilterSuccess(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	_, err = database.UpsertTrack(db.Track{
		Text:       "General task",
		Priority:   "medium",
		ChannelIDs: `["C001"]`,
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagPriority = ""
	tracksFlagChannel = "general"
	tracksFlagUpdates = false
	defer func() { tracksFlagChannel = "" }()

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "General task")
}

// --- printTracks with channel lookup from DB ---
func TestPrintTracks_ChannelLookup(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	tracks := []db.Track{
		{
			ID:         1,
			Text:       "Task without channel name",
			Priority:   "medium",
			Ownership:  "mine",
			Category:   "task",
			ChannelIDs: `["C001"]`,
		},
	}

	buf := new(bytes.Buffer)
	printTracks(buf, tracks, database, false)
	assert.Contains(t, buf.String(), "#general")
}

// --- ShowDigestCatchup with channel digest and unknown channel ---
func TestShowDigestCatchup_ChannelUnknownID(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	now := float64(time.Now().Unix())
	_, err = database.UpsertDigest(db.Digest{
		ChannelID:    "CUNKNOWN",
		PeriodFrom:   now - 3600,
		PeriodTo:     now,
		Type:         "channel",
		Summary:      "Discussion in unknown channel",
		Topics:       `[]`,
		Decisions:    `[]`,
		ActionItems:  `[]`,
		MessageCount: 5,
		Model:        "haiku",
	})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	shown := showDigestCatchup(buf, database, now-100000)
	assert.True(t, shown)
	// Should show the raw channel ID since it can't be resolved
	assert.NotEmpty(t, buf.String())
}

// --- ConfigShow with all fields ---
func TestConfigShow_AllFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `active_workspace: myteam
workspaces:
  myteam:
    slack_token: "xoxp-long-secret-token-value"
ai:
  model: "claude-sonnet-4-6"
  context_budget: 50000
sync:
  workers: 4
  initial_history_days: 30
  poll_interval: "15m"
  sync_threads: true
  sync_on_wake: true
digest:
  enabled: true
  model: "haiku"
  min_messages: 10
`
	require.NoError(t, os.WriteFile(configPath, []byte(yaml), 0o600))

	oldFlagConfig := flagConfig
	flagConfig = configPath
	defer func() { flagConfig = oldFlagConfig }()

	buf := new(bytes.Buffer)
	configShowCmd.SetOut(buf)

	err := configShowCmd.RunE(configShowCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "active_workspace: myteam")
	assert.Contains(t, output, "ai.model: claude-sonnet-4-6")
	assert.Contains(t, output, "sync.workers: 4")
	assert.Contains(t, output, "sync.sync_threads: true")
	assert.Contains(t, output, "sync.sync_on_wake: true")
	assert.Contains(t, output, "digest.enabled: true")
	assert.Contains(t, output, "digest.min_messages: 10")
	assert.Contains(t, output, "xoxp-****")
}

// --- Decisions with decision that has no "by" field ---
func TestRunDecisions_DecisionWithoutBy(t *testing.T) {
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
		Summary:      "Discussion",
		Topics:       `[]`,
		Decisions:    `[{"text":"Team consensus: no breaking changes"}]`,
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

	output := buf.String()
	assert.Contains(t, output, "Team consensus")
	assert.NotContains(t, output, "(by")
}

// --- Digest generate flags ---
func TestDigestGenerate_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	err := digestGenerateCmd.RunE(digestGenerateCmd, nil)
	assert.Error(t, err)
}

// --- DB migrate ---
func TestDbMigrate_Success(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	// dbMigrateCmd uses fmt.Println, not cmd.OutOrStdout()
	// so we just verify it doesn't error
	err := dbMigrateCmd.RunE(dbMigrateCmd, nil)
	require.NoError(t, err)
}
