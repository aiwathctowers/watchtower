package cmd

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestTracksCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "tracks" {
			found = true
			break
		}
	}
	assert.True(t, found, "tracks command should be registered")
}

func TestTracksSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"generate": false, "accept": false, "done": false, "dismiss": false, "snooze": false}
	for _, cmd := range tracksCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "tracks %s subcommand should be registered", name)
	}
}

func TestActionsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "actions" {
			found = true
			break
		}
	}
	assert.True(t, found, "actions (deprecated) command should be registered")
}

func TestRunTracks_NoCurrentUser(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""

	err := tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Current user not set")
}

func TestRunTracks_Empty(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""

	err := tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No tracks found")
}

func TestRunTracks_WithTracks(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertTrack(db.Track{
		ChannelID:         "C001",
		AssigneeUserID:    "U001",
		Text:              "Review the PR for auth changes",
		Context:           "Alice asked in #general",
		Status:            "inbox",
		Priority:          "high",
		PeriodFrom:        now - 86400,
		PeriodTo:          now,
		Model:             "haiku",
		Category:          "code_review",
		RequesterName:     "alice",
		SourceChannelName: "general",
		Ownership:         "mine",
	})
	require.NoError(t, err)

	_, err = database.UpsertTrack(db.Track{
		ChannelID:         "C001",
		AssigneeUserID:    "U001",
		Text:              "Deploy the new version",
		Context:           "Scheduled for Friday",
		Status:            "active",
		Priority:          "medium",
		PeriodFrom:        now - 86400,
		PeriodTo:          now,
		Model:             "haiku",
		Category:          "task",
		RequesterName:     "bob",
		SourceChannelName: "general",
		Blocking:          "QA team",
		Ownership:         "mine",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Review the PR")
	assert.Contains(t, output, "Deploy the new version")
	assert.Contains(t, output, "Inbox")
	assert.Contains(t, output, "Active")
}

func TestRunTracks_StatusFilter(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	_, err = database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "Done task",
		Status:         "done",
		Priority:       "low",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)
	database.Close()

	// Default filter (inbox + active) - should not see done items
	buf := new(bytes.Buffer)
	tracksCmd.SetOut(buf)
	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No tracks found")

	// Filter by "done" status
	buf.Reset()
	tracksCmd.SetOut(buf)
	tracksFlagStatus = "done"
	defer func() { tracksFlagStatus = "" }()

	err = tracksCmd.RunE(tracksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Done task")
}

func setupTracksTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupWatchTestEnv(t)
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	database.Close()
	return cleanup
}

func TestRunTracks_InvalidStatusFlag(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagStatus = "invalid"
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""
	defer func() { tracksFlagStatus = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --status")
}

func TestRunTracks_InvalidPriorityFlag(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagStatus = ""
	tracksFlagPriority = "extreme"
	tracksFlagChannel = ""
	tracksFlagOwnership = ""
	defer func() { tracksFlagPriority = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --priority")
}

func TestRunTracks_InvalidOwnershipFlag(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = "invalid"
	defer func() { tracksFlagOwnership = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --ownership")
}

func TestRunTracks_ChannelFilter(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = "nonexistent"
	tracksFlagOwnership = ""
	defer func() { tracksFlagChannel = "" }()

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunTracksAccept(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	id, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "Accept this",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tracksAcceptCmd.SetOut(buf)

	err = tracksAcceptCmd.RunE(tracksAcceptCmd, []string{fmt.Sprintf("%d", id)})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "accepted")
}

func TestRunTracksDone(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	id, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "Finish this",
		Status:         "active",
		Priority:       "medium",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tracksDoneCmd.SetOut(buf)

	err = tracksDoneCmd.RunE(tracksDoneCmd, []string{fmt.Sprintf("%d", id)})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "marked as done")
}

func TestRunTracksDismiss(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	id, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "Dismiss this",
		Status:         "inbox",
		Priority:       "low",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tracksDismissCmd.SetOut(buf)

	err = tracksDismissCmd.RunE(tracksDismissCmd, []string{fmt.Sprintf("%d", id)})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "marked as dismissed")
}

func TestRunTracksSnooze(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())
	id, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "Snooze this",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)
	database.Close()

	tracksSnoozeFlagUntil = "tomorrow"
	tracksSnoozeFlagHours = 0
	defer func() { tracksSnoozeFlagUntil = ""; tracksSnoozeFlagHours = 0 }()

	buf := new(bytes.Buffer)
	tracksSnoozeCmd.SetOut(buf)

	err = tracksSnoozeCmd.RunE(tracksSnoozeCmd, []string{fmt.Sprintf("%d", id)})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "snoozed until")
}

func TestRunTracksSnooze_NoFlags(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	tracksSnoozeFlagUntil = ""
	tracksSnoozeFlagHours = 0

	err := tracksSnoozeCmd.RunE(tracksSnoozeCmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify")
}

func TestRunTracksAccept_InvalidID(t *testing.T) {
	cleanup := setupWatchTestEnv(t)
	defer cleanup()

	err := tracksAcceptCmd.RunE(tracksAcceptCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid track ID")
}

func TestRunTracks_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	tracksFlagStatus = ""
	tracksFlagPriority = ""
	tracksFlagChannel = ""
	tracksFlagOwnership = ""

	err := tracksCmd.RunE(tracksCmd, nil)
	assert.Error(t, err)
}

func TestTracksFlags(t *testing.T) {
	assert.NotNil(t, tracksCmd.Flags().Lookup("status"))
	assert.NotNil(t, tracksCmd.Flags().Lookup("priority"))
	assert.NotNil(t, tracksCmd.Flags().Lookup("channel"))
	assert.NotNil(t, tracksCmd.Flags().Lookup("ownership"))
	assert.NotNil(t, tracksGenerateCmd.Flags().Lookup("since"))
	assert.NotNil(t, tracksGenerateCmd.Flags().Lookup("progress-json"))
	assert.NotNil(t, tracksSnoozeCmd.Flags().Lookup("until"))
	assert.NotNil(t, tracksSnoozeCmd.Flags().Lookup("hours"))
}

func TestVerifyTrackOwnership(t *testing.T) {
	cleanup := setupTracksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	now := float64(time.Now().Unix())

	// Track owned by U001 (current user)
	id1, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U001",
		Text:           "My track",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)

	// Track owned by U002 (different user)
	id2, err := database.UpsertTrack(db.Track{
		ChannelID:      "C001",
		AssigneeUserID: "U002",
		Text:           "Other's track",
		Status:         "inbox",
		Priority:       "medium",
		PeriodFrom:     now - 86400,
		PeriodTo:       now,
		Model:          "haiku",
		Ownership:      "mine",
	})
	require.NoError(t, err)

	// Own track should pass
	err = verifyTrackOwnership(database, int(id1))
	require.NoError(t, err)

	// Other's track should fail
	err = verifyTrackOwnership(database, int(id2))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "different user")

	database.Close()
}

func TestPrintTracks(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(tmpDir + "/test.db")
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertChannel(db.Channel{ID: "C001", Name: "general", Type: "public"}))

	tracks := []db.Track{
		{
			ID:                1,
			ChannelID:         "C001",
			Text:              "Review PR #42",
			Status:            "inbox",
			Priority:          "high",
			Category:          "code_review",
			RequesterName:     "alice",
			SourceChannelName: "general",
			HasUpdates:        true,
			CreatedAt:         time.Now().Add(-1 * time.Hour).Format("2006-01-02T15:04:05Z"),
			Ownership:         "mine",
		},
		{
			ID:              2,
			ChannelID:       "C001",
			Text:            "Decide on framework",
			Status:          "active",
			Priority:        "medium",
			Category:        "decision_needed",
			Context:         "Team needs to choose between React and Vue",
			Blocking:        "Frontend team",
			DecisionSummary: "Leaning towards React",
			Tags:            `["frontend","framework"]`,
			DueDate:         float64(time.Now().Add(48 * time.Hour).Unix()),
			CreatedAt:       time.Now().Add(-2 * time.Hour).Format("2006-01-02T15:04:05Z"),
			Ownership:       "delegated",
		},
		{
			ID:          3,
			ChannelID:   "C001",
			Text:        "Snoozed task",
			Status:      "snoozed",
			Priority:    "low",
			SnoozeUntil: float64(time.Now().Add(24 * time.Hour).Unix()),
			CreatedAt:   time.Now().Add(-3 * time.Hour).Format("2006-01-02T15:04:05Z"),
			Ownership:   "watching",
		},
	}

	buf := new(bytes.Buffer)
	printTracks(buf, tracks, database)

	output := buf.String()
	assert.Contains(t, output, "Review PR #42")
	assert.Contains(t, output, "review")
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "Decide on framework")
	assert.Contains(t, output, "Blocking:")
	assert.Contains(t, output, "Decision:")
	assert.Contains(t, output, "due:")
	assert.Contains(t, output, "snoozed until")
}
