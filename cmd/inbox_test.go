package cmd

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestInboxCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "inbox" {
			found = true
			break
		}
	}
	assert.True(t, found, "inbox command should be registered")
}

func TestInboxSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"show": false, "resolve": false, "dismiss": false, "snooze": false, "generate": false, "task": false}
	for _, cmd := range inboxCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "inbox %s subcommand should be registered", name)
	}
}

func TestInboxFlags(t *testing.T) {
	assert.NotNil(t, inboxCmd.Flags().Lookup("priority"))
	assert.NotNil(t, inboxCmd.Flags().Lookup("type"))
	assert.NotNil(t, inboxCmd.Flags().Lookup("all"))
	assert.NotNil(t, inboxCmd.Flags().Lookup("json"))
}

func setupInboxTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupWatchTestEnv(t)
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	database.Close()
	return cleanup
}

var inboxTestSeq int

func seedInboxItem(t *testing.T, triggerType, priority, snippet string) {
	t.Helper()
	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	inboxTestSeq++
	item := db.InboxItem{
		ChannelID:    "C001",
		MessageTS:    fmt.Sprintf("1711000%03d.000100", inboxTestSeq),
		SenderUserID: "U002",
		TriggerType:  triggerType,
		Snippet:      snippet,
		Status:       "pending",
		Priority:     priority,
	}
	_, err = database.CreateInboxItem(item)
	require.NoError(t, err)
}

func TestRunInbox_WithItems(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "high", "Hey @alice review this PR")
	seedInboxItem(t, "dm", "medium", "Got a minute to chat?")

	buf := new(bytes.Buffer)
	inboxCmd.SetOut(buf)
	inboxFlagPriority = ""
	inboxFlagType = ""
	inboxFlagAll = false
	inboxFlagJSON = false

	err := inboxCmd.RunE(inboxCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Hey @alice review this PR")
	assert.Contains(t, output, "Got a minute to chat?")
	assert.Contains(t, output, "HIGH")
	assert.Contains(t, output, "DM")
}

func TestRunInbox_Empty(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	inboxCmd.SetOut(buf)
	inboxFlagPriority = ""
	inboxFlagType = ""
	inboxFlagAll = false
	inboxFlagJSON = false

	err := inboxCmd.RunE(inboxCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No inbox items found")
}

func TestRunInbox_FilterByPriority(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "high", "Urgent blocker")
	seedInboxItem(t, "mention", "low", "Minor question")

	buf := new(bytes.Buffer)
	inboxCmd.SetOut(buf)
	inboxFlagPriority = "high"
	inboxFlagType = ""
	inboxFlagAll = false
	inboxFlagJSON = false

	err := inboxCmd.RunE(inboxCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Urgent blocker")
	assert.NotContains(t, output, "Minor question")
}

func TestRunInbox_FilterByType(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "medium", "Hey @alice check")
	seedInboxItem(t, "dm", "medium", "Direct message here")

	buf := new(bytes.Buffer)
	inboxCmd.SetOut(buf)
	inboxFlagPriority = ""
	inboxFlagType = "dm"
	inboxFlagAll = false
	inboxFlagJSON = false

	err := inboxCmd.RunE(inboxCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Direct message here")
	assert.NotContains(t, output, "Hey @alice check")
}

func TestRunInbox_JSON(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "high", "JSON test item")

	buf := new(bytes.Buffer)
	inboxCmd.SetOut(buf)
	inboxFlagPriority = ""
	inboxFlagType = ""
	inboxFlagAll = false
	inboxFlagJSON = true

	err := inboxCmd.RunE(inboxCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `"Snippet": "JSON test item"`)
	assert.Contains(t, output, `"Priority": "high"`)
}

func TestRunInboxShow(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "high", "Show this inbox item")

	buf := new(bytes.Buffer)
	inboxShowCmd.SetOut(buf)

	err := inboxShowCmd.RunE(inboxShowCmd, []string{"1"})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Inbox Item #1")
	assert.Contains(t, output, "pending")
	assert.Contains(t, output, "high")
	assert.Contains(t, output, "mention")
	assert.Contains(t, output, "Show this inbox item")
}

func TestRunInboxShow_InvalidID(t *testing.T) {
	err := inboxShowCmd.RunE(inboxShowCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid inbox item ID")
}

func TestRunInboxResolve(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "medium", "To resolve")

	buf := new(bytes.Buffer)
	inboxResolveCmd.SetOut(buf)

	err := inboxResolveCmd.RunE(inboxResolveCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "resolved")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	item, err := database.GetInboxItemByID(1)
	require.NoError(t, err)
	assert.Equal(t, "resolved", item.Status)
}

func TestRunInboxDismiss(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "dm", "low", "To dismiss")

	buf := new(bytes.Buffer)
	inboxDismissCmd.SetOut(buf)

	err := inboxDismissCmd.RunE(inboxDismissCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dismissed")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	item, err := database.GetInboxItemByID(1)
	require.NoError(t, err)
	assert.Equal(t, "dismissed", item.Status)
}

func TestRunInboxSnooze(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "medium", "To snooze")

	buf := new(bytes.Buffer)
	inboxSnoozeCmd.SetOut(buf)

	err := inboxSnoozeCmd.RunE(inboxSnoozeCmd, []string{"1", "3d"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "snoozed")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	item, err := database.GetInboxItemByID(1)
	require.NoError(t, err)
	assert.Equal(t, "snoozed", item.Status)
	assert.NotEmpty(t, item.SnoozeUntil)
}

func TestRunInboxTask(t *testing.T) {
	cleanup := setupInboxTestEnv(t)
	defer cleanup()

	seedInboxItem(t, "mention", "high", "Create task from this")

	buf := new(bytes.Buffer)
	inboxTaskCmd.SetOut(buf)

	err := inboxTaskCmd.RunE(inboxTaskCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Created target #")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	target, err := database.GetTargetByID(1)
	require.NoError(t, err)
	assert.Equal(t, "Create task from this", target.Text)
	assert.Equal(t, "high", target.Priority)
	assert.Equal(t, "inbox", target.SourceType)
	assert.Equal(t, "1", target.SourceID)
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"1d", false},
		{"3d", false},
		{"1w", false},
		{"2w", false},
		{"0d", true},
		{"abc", true},
		{"", true},
		{"1x", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRunInbox_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	inboxFlagPriority = ""
	inboxFlagType = ""
	inboxFlagAll = false
	inboxFlagJSON = false

	err := inboxCmd.RunE(inboxCmd, nil)
	assert.Error(t, err)
}
