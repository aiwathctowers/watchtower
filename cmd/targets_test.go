package cmd

import (
	"bytes"
	"database/sql"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

// setupTargetsTestEnv creates a temporary workspace and returns a cleanup func.
func setupTargetsTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupWatchTestEnv(t)
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	database.Close()
	return cleanup
}

// createTestTarget inserts a target and returns its ID.
func createTestTarget(t *testing.T, text, priority, status string) int64 {
	t.Helper()
	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	id, err := database.CreateTarget(db.Target{
		Text:        text,
		Level:       "day",
		PeriodStart: "2026-04-23",
		PeriodEnd:   "2026-04-23",
		Status:      status,
		Priority:    priority,
		Ownership:   "mine",
		SourceType:  "manual",
	})
	require.NoError(t, err)
	return id
}

// --- Command registration ---

func TestTargetsCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "targets" {
			found = true
			break
		}
	}
	assert.True(t, found, "targets command should be registered")
}

func TestTargetsSubcommandsRegistered(t *testing.T) {
	expected := []string{"show", "create", "extract", "link", "unlink", "suggest-links",
		"done", "dismiss", "snooze", "update", "generate", "note", "ai-update"}
	registered := map[string]bool{}
	for _, sub := range targetsCmd.Commands() {
		registered[sub.Name()] = true
	}
	for _, name := range expected {
		assert.True(t, registered[name], "targets %s subcommand should be registered", name)
	}
}

func TestTargetsFlags(t *testing.T) {
	assert.NotNil(t, targetsCmd.Flags().Lookup("status"))
	assert.NotNil(t, targetsCmd.Flags().Lookup("priority"))
	assert.NotNil(t, targetsCmd.Flags().Lookup("ownership"))
	assert.NotNil(t, targetsCmd.Flags().Lookup("all"))
	assert.NotNil(t, targetsCmd.Flags().Lookup("json"))
	assert.NotNil(t, targetsCmd.Flags().Lookup("level"))
	// --period removed in V1; period filtering not yet wired to DB query
}

// --- List ---

func TestRunTargets_Empty(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	targetsCmd.SetOut(buf)
	resetTargetsFlags()

	err := targetsCmd.RunE(targetsCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No targets found")
}

func TestRunTargets_WithTargets(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Review the PR", "high", "todo")
	createTestTarget(t, "Deploy new version", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsCmd.SetOut(buf)
	resetTargetsFlags()

	err := targetsCmd.RunE(targetsCmd, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Review the PR")
	assert.Contains(t, out, "Deploy new version")
	assert.Contains(t, out, "HIGH")
}

func TestRunTargets_FilterByStatus(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Todo item", "medium", "todo")
	createTestTarget(t, "In progress item", "medium", "in_progress")

	buf := new(bytes.Buffer)
	targetsCmd.SetOut(buf)
	resetTargetsFlags()
	targetsFlagStatus = "todo"

	err := targetsCmd.RunE(targetsCmd, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Todo item")
	assert.NotContains(t, out, "In progress item")
}

func TestRunTargets_JSON(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "JSON target", "high", "todo")

	buf := new(bytes.Buffer)
	targetsCmd.SetOut(buf)
	resetTargetsFlags()
	targetsFlagJSON = true

	err := targetsCmd.RunE(targetsCmd, nil)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"Text": "JSON target"`)
	assert.Contains(t, out, `"Priority": "high"`)
}

// --- Create ---

func TestRunTargetsCreate(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	targetsCreateCmd.SetOut(buf)
	targetsFlagText = "New target from CLI"
	targetsFlagIntent = "test intent"
	targetsFlagPriority = "high"
	targetsFlagOwnership = "mine"
	targetsFlagDue = "2026-04-30T10:00"
	targetsFlagSourceType = "manual"
	targetsFlagSourceID = ""
	targetsFlagTags = "urgent,api"
	targetsFlagLevel = "week"
	targetsFlagPeriodStart = "2026-04-21"
	targetsFlagPeriodEnd = "2026-04-27"
	targetsFlagParent = 0

	err := targetsCreateCmd.RunE(targetsCreateCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Created target #")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	target, err := database.GetTargetByID(1)
	require.NoError(t, err)
	assert.Equal(t, "New target from CLI", target.Text)
	assert.Equal(t, "test intent", target.Intent)
	assert.Equal(t, "high", target.Priority)
	assert.Equal(t, "week", target.Level)
	assert.Equal(t, "2026-04-21", target.PeriodStart)
	assert.Equal(t, "2026-04-27", target.PeriodEnd)
	assert.Contains(t, target.Tags, "urgent")
	assert.Contains(t, target.Tags, "api")
}

func TestRunTargetsCreate_RequiresText(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	targetsFlagText = ""
	err := targetsCreateCmd.RunE(targetsCreateCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--text is required")
}

func TestRunTargetsCreate_WithParent(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	parentID := createTestTarget(t, "Parent target", "high", "todo")

	buf := new(bytes.Buffer)
	targetsCreateCmd.SetOut(buf)
	targetsFlagText = "Child target"
	targetsFlagIntent = ""
	targetsFlagPriority = "medium"
	targetsFlagOwnership = "mine"
	targetsFlagDue = ""
	targetsFlagSourceType = "manual"
	targetsFlagSourceID = ""
	targetsFlagTags = ""
	targetsFlagLevel = "day"
	targetsFlagPeriodStart = ""
	targetsFlagPeriodEnd = ""
	targetsFlagParent = int(parentID)

	err := targetsCreateCmd.RunE(targetsCreateCmd, nil)
	require.NoError(t, err)

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	child, err := database.GetTargetByID(2)
	require.NoError(t, err)
	assert.True(t, child.ParentID.Valid)
	assert.Equal(t, parentID, child.ParentID.Int64)
}

// --- Show ---

func TestRunTargetsShow(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTarget(db.Target{
		Text:        "Show this target",
		Intent:      "test intent",
		Level:       "week",
		PeriodStart: "2026-04-21",
		PeriodEnd:   "2026-04-27",
		Status:      "in_progress",
		Priority:    "high",
		Ownership:   "delegated",
		BallOn:      "alice",
		DueDate:     "2026-04-25",
		Tags:        `["review","api"]`,
		SubItems:    `[{"text":"Check tests","done":false},{"text":"Approve","done":true}]`,
		SourceType:  "track",
		SourceID:    "42",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	targetsShowCmd.SetOut(buf)

	err = targetsShowCmd.RunE(targetsShowCmd, []string{"1"})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Show this target")
	assert.Contains(t, out, "in_progress")
	assert.Contains(t, out, "high")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "2026-04-25")
	assert.Contains(t, out, "review")
	assert.Contains(t, out, "Check tests")
	assert.Contains(t, out, "[x] Approve")
	assert.Contains(t, out, "track")
	assert.Contains(t, out, "#42")
}

func TestRunTargetsShow_InvalidID(t *testing.T) {
	err := targetsShowCmd.RunE(targetsShowCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target ID")
}

// --- Done / Dismiss ---

func TestRunTargetsDone(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To finish", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsDoneCmd.SetOut(buf)

	err := targetsDoneCmd.RunE(targetsDoneCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "marked as done")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	target, err := database.GetTargetByID(1)
	require.NoError(t, err)
	assert.Equal(t, "done", target.Status)
}

func TestRunTargetsDismiss(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To dismiss", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsDismissCmd.SetOut(buf)

	err := targetsDismissCmd.RunE(targetsDismissCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dismissed")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	target, err := database.GetTargetByID(1)
	require.NoError(t, err)
	assert.Equal(t, "dismissed", target.Status)
}

// --- Snooze ---

func TestRunTargetsSnooze(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To snooze", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsSnoozeCmd.SetOut(buf)

	err := targetsSnoozeCmd.RunE(targetsSnoozeCmd, []string{"1", "2026-05-01"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "snoozed until 2026-05-01")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	target, err := database.GetTargetByID(1)
	require.NoError(t, err)
	assert.Equal(t, "snoozed", target.Status)
	assert.Equal(t, "2026-05-01", target.SnoozeUntil)
}

// --- Link / Unlink ---

func TestRunTargetsLink_Parent(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Parent", "high", "todo")
	createTestTarget(t, "Child", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsLinkCmd.SetOut(buf)
	targetsFlagLinkParent = 1
	targetsFlagLinkTo = 0
	targetsFlagLinkRelation = ""
	targetsFlagLinkExternal = ""

	err := targetsLinkCmd.RunE(targetsLinkCmd, []string{"2"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "parent set to #1")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	child, err := database.GetTargetByID(2)
	require.NoError(t, err)
	assert.True(t, child.ParentID.Valid)
	assert.Equal(t, int64(1), child.ParentID.Int64)
}

func TestRunTargetsLink_ToTarget(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Source", "high", "todo")
	createTestTarget(t, "Dest", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsLinkCmd.SetOut(buf)
	targetsFlagLinkParent = 0
	targetsFlagLinkTo = 2
	targetsFlagLinkRelation = "contributes_to"
	targetsFlagLinkExternal = ""

	err := targetsLinkCmd.RunE(targetsLinkCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Link #1 created")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	links, err := database.GetLinksForTarget(1, "outbound")
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "contributes_to", links[0].Relation)
	assert.Equal(t, sql.NullInt64{Int64: 2, Valid: true}, links[0].TargetTargetID)
}

func TestRunTargetsLink_External(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Source", "high", "todo")

	buf := new(bytes.Buffer)
	targetsLinkCmd.SetOut(buf)
	targetsFlagLinkParent = 0
	targetsFlagLinkTo = 0
	targetsFlagLinkRelation = "related"
	targetsFlagLinkExternal = "jira:PROJ-123"

	err := targetsLinkCmd.RunE(targetsLinkCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Link #1 created")
}

func TestRunTargetsLink_MissingRelation(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Source", "high", "todo")
	createTestTarget(t, "Dest", "medium", "todo")

	targetsFlagLinkParent = 0
	targetsFlagLinkTo = 2
	targetsFlagLinkRelation = ""
	targetsFlagLinkExternal = ""

	err := targetsLinkCmd.RunE(targetsLinkCmd, []string{"1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--relation is required")
}

func TestRunTargetsUnlink(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Source", "high", "todo")
	createTestTarget(t, "Dest", "medium", "todo")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	linkID, err := database.CreateTargetLink(db.TargetLink{
		SourceTargetID: 1,
		TargetTargetID: sql.NullInt64{Int64: 2, Valid: true},
		Relation:       "blocks",
		CreatedBy:      "user",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	targetsUnlinkCmd.SetOut(buf)

	err = targetsUnlinkCmd.RunE(targetsUnlinkCmd, []string{strconv.FormatInt(linkID, 10)})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "removed")
}

// --- Update ---

func TestRunTargetsUpdate(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "Original", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsUpdateCmd.SetOut(buf)

	// Reset flags then set the ones we want to change via cmd.Flags().Changed detection.
	// The cleanest way is to call ParseFlags then RunE.
	err := targetsUpdateCmd.ParseFlags([]string{"--text", "Updated", "--priority", "high"})
	require.NoError(t, err)
	targetsFlagText = "Updated"
	targetsFlagPriority = "high"

	err = targetsUpdateCmd.RunE(targetsUpdateCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "updated")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	target, err := database.GetTargetByID(1)
	require.NoError(t, err)
	assert.Equal(t, "Updated", target.Text)
	assert.Equal(t, "high", target.Priority)
}

// --- Config requirement ---

func TestRunTargets_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	resetTargetsFlags()
	err := targetsCmd.RunE(targetsCmd, nil)
	assert.Error(t, err)
}

func TestTargetsExtractCmdHasJSONFlag(t *testing.T) {
	flag := targetsExtractCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("targets extract should have --json flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--json default should be false, got %q", flag.DefValue)
	}
}

// resetTargetsFlags resets all list flags to zero values before each test.
func resetTargetsFlags() {
	targetsFlagStatus = ""
	targetsFlagPriority = ""
	targetsFlagOwnership = ""
	targetsFlagAll = false
	targetsFlagJSON = false
	targetsFlagLevel = ""
	targetsFlagSource = ""
}
