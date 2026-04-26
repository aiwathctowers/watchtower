package cmd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/spf13/pflag"
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
		"done", "dismiss", "delete", "snooze", "update", "generate", "note", "ai-update",
		"promote-subitem"}
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

// --- Delete ---

func TestRunTargetsDelete(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To delete", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsDeleteCmd.SetOut(buf)

	err := targetsDeleteCmd.RunE(targetsDeleteCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Target #1 removed")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	_, err = database.GetTargetByID(1)
	require.Error(t, err, "target should be gone after delete")
}

func TestRunTargetsDelete_NotFound(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	err := targetsDeleteCmd.RunE(targetsDeleteCmd, []string{"999999"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunTargetsDelete_JSON(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To delete json", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsDeleteCmd.SetOut(buf)

	require.NoError(t, targetsDeleteCmd.Flags().Set("json", "true"))
	defer func() { _ = targetsDeleteCmd.Flags().Set("json", "false") }()

	err := targetsDeleteCmd.RunE(targetsDeleteCmd, []string{"1"})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"id":1`)
	assert.Contains(t, out, `"removed":true`)
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

func TestTargetsSuggestLinksCmdHasJSONFlag(t *testing.T) {
	flag := targetsSuggestLinksCmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("targets suggest-links should have --json flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--json default should be false, got %q", flag.DefValue)
	}
}

// --- promote-subitem ---

// resetPromoteSubItemFlags clears Changed state on every flag of the
// promote-subitem command so tests stay isolated.
func resetPromoteSubItemFlags() {
	targetsPromoteSubItemCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}

// createTestTargetWithSubItems inserts a parent target with the given
// inheritance-relevant fields and an arbitrary sub-items JSON. Returns the ID.
func createTestTargetWithSubItems(t *testing.T, text, level, priority, subItemsJSON string) int64 {
	t.Helper()
	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	id, err := database.CreateTarget(db.Target{
		Text:        text,
		Intent:      "the why",
		Level:       level,
		PeriodStart: "2026-04-20",
		PeriodEnd:   "2026-04-26",
		Status:      "in_progress",
		Priority:    priority,
		Ownership:   "mine",
		BallOn:      "alice",
		Tags:        `["a","b"]`,
		SubItems:    subItemsJSON,
		SourceType:  "manual",
	})
	require.NoError(t, err)
	return id
}

func TestPromoteSubItemCmd_HasExpectedFlags(t *testing.T) {
	for _, name := range []string{
		"text", "intent", "level", "priority", "ownership",
		"due", "period-start", "period-end", "tags", "json",
	} {
		assert.NotNil(t, targetsPromoteSubItemCmd.Flags().Lookup(name),
			"promote-subitem must have --%s flag", name)
	}
}

func TestPromoteSubItem_Success(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	parentID := createTestTargetWithSubItems(t, "Parent", "week", "high",
		`[{"text":"first","done":false},{"text":"second","done":false}]`)

	buf := new(bytes.Buffer)
	targetsPromoteSubItemCmd.SetOut(buf)
	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd,
		[]string{strconv.FormatInt(parentID, 10), "0"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Promoted sub-item #0")

	// Verify side effects on parent and child.
	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	parent, err := database.GetTargetByID(int(parentID))
	require.NoError(t, err)
	assert.Contains(t, parent.SubItems, "second", "remaining sub-item should still be there")
	assert.NotContains(t, parent.SubItems, "first", "promoted sub-item should be removed")

	children, err := database.GetTargets(db.TargetFilter{ParentID: &parentID, IncludeDone: true})
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, "first", children[0].Text)
	assert.Equal(t, "week", children[0].Level, "level inherited from parent")
	assert.Equal(t, "high", children[0].Priority, "priority inherited from parent")
	assert.Equal(t, "promoted_subitem", children[0].SourceType)
}

func TestPromoteSubItem_JSONOutput(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	parentID := createTestTargetWithSubItems(t, "P", "day", "medium",
		`[{"text":"sole","done":false}]`)

	buf := new(bytes.Buffer)
	targetsPromoteSubItemCmd.SetOut(buf)
	require.NoError(t, targetsPromoteSubItemCmd.Flags().Set("json", "true"))
	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd,
		[]string{strconv.FormatInt(parentID, 10), "0"})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &payload))
	assert.Equal(t, "sole", payload["text"])
	assert.Equal(t, "day", payload["level"])
	assert.Equal(t, "promoted_subitem", payload["source_type"])
	assert.EqualValues(t, parentID, payload["parent_id"])
	// id is a number > 0
	idVal, ok := payload["id"].(float64)
	require.True(t, ok, "id field should be numeric in JSON")
	assert.Greater(t, idVal, float64(0))
}

func TestPromoteSubItem_OverridesApplied(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	parentID := createTestTargetWithSubItems(t, "Parent", "week", "high",
		`[{"text":"rough","done":false}]`)

	require.NoError(t, targetsPromoteSubItemCmd.ParseFlags([]string{
		"--text", "polished",
		"--level", "day",
		"--priority", "low",
		"--due", "2026-05-01T10:00",
		"--tags", "x,y",
	}))

	buf := new(bytes.Buffer)
	targetsPromoteSubItemCmd.SetOut(buf)
	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd,
		[]string{strconv.FormatInt(parentID, 10), "0"})
	require.NoError(t, err)

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	children, err := database.GetTargets(db.TargetFilter{ParentID: &parentID, IncludeDone: true})
	require.NoError(t, err)
	require.Len(t, children, 1)
	c := children[0]
	assert.Equal(t, "polished", c.Text)
	assert.Equal(t, "day", c.Level)
	assert.Equal(t, "low", c.Priority)
	assert.Equal(t, "2026-05-01T10:00", c.DueDate)
	assert.Equal(t, `["x","y"]`, c.Tags)
}

func TestPromoteSubItem_InvalidTargetID(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd, []string{"abc", "0"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target ID")
}

func TestPromoteSubItem_InvalidIndex(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd, []string{"1", "-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid sub-item index")
}

func TestPromoteSubItem_OutOfRange(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	parentID := createTestTargetWithSubItems(t, "P", "day", "medium",
		`[{"text":"only","done":false}]`)

	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd,
		[]string{strconv.FormatInt(parentID, 10), "5"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestPromoteSubItem_ParentNotFound(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()
	resetPromoteSubItemFlags()

	err := targetsPromoteSubItemCmd.RunE(targetsPromoteSubItemCmd, []string{"99999", "0"})
	require.Error(t, err)
}
