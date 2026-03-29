package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"watchtower/internal/db"
)

func TestTasksCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "tasks" {
			found = true
			break
		}
	}
	assert.True(t, found, "tasks command should be registered")
}

func TestTasksSubcommandsRegistered(t *testing.T) {
	subs := map[string]bool{"show": false, "create": false, "done": false, "dismiss": false, "snooze": false, "update": false}
	for _, cmd := range tasksCmd.Commands() {
		if _, ok := subs[cmd.Name()]; ok {
			subs[cmd.Name()] = true
		}
	}
	for name, found := range subs {
		assert.True(t, found, "tasks %s subcommand should be registered", name)
	}
}

func TestTasksFlags(t *testing.T) {
	assert.NotNil(t, tasksCmd.Flags().Lookup("status"))
	assert.NotNil(t, tasksCmd.Flags().Lookup("priority"))
	assert.NotNil(t, tasksCmd.Flags().Lookup("ownership"))
	assert.NotNil(t, tasksCmd.Flags().Lookup("all"))
	assert.NotNil(t, tasksCmd.Flags().Lookup("json"))
}

func setupTasksTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupWatchTestEnv(t)
	database, err := openDBFromConfig()
	require.NoError(t, err)
	require.NoError(t, database.UpsertWorkspace(db.Workspace{ID: "T001", Name: "test-ws", Domain: "test-ws"}))
	require.NoError(t, database.SetCurrentUserID("U001"))
	database.Close()
	return cleanup
}

func TestRunTasks_WithTasks(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)

	_, err = database.CreateTask(db.Task{
		Text:       "Review the PR",
		Status:     "todo",
		Priority:   "high",
		Ownership:  "mine",
		DueDate:    "2026-03-31",
		SourceType: "manual",
	})
	require.NoError(t, err)

	_, err = database.CreateTask(db.Task{
		Text:       "Deploy new version",
		Status:     "todo",
		Priority:   "medium",
		Ownership:  "mine",
		SourceType: "manual",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksCmd.SetOut(buf)
	tasksFlagStatus = ""
	tasksFlagPriority = ""
	tasksFlagOwnership = ""
	tasksFlagAll = false
	tasksFlagJSON = false

	err = tasksCmd.RunE(tasksCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Review the PR")
	assert.Contains(t, output, "Deploy new version")
	assert.Contains(t, output, "HIGH")
	assert.Contains(t, output, "due: 2026-03-31")
}

func TestRunTasks_Empty(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	tasksCmd.SetOut(buf)
	tasksFlagStatus = ""
	tasksFlagPriority = ""
	tasksFlagOwnership = ""
	tasksFlagAll = false
	tasksFlagJSON = false

	err := tasksCmd.RunE(tasksCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No tasks found")
}

func TestRunTasks_FilterByStatus(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{Text: "Todo", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{Text: "In progress", Status: "in_progress", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksCmd.SetOut(buf)
	tasksFlagStatus = "todo"
	tasksFlagPriority = ""
	tasksFlagOwnership = ""
	tasksFlagAll = false
	tasksFlagJSON = false

	err = tasksCmd.RunE(tasksCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Todo")
	assert.NotContains(t, output, "In progress")
}

func TestRunTasks_JSON(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{Text: "JSON task", Status: "todo", Priority: "high", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksCmd.SetOut(buf)
	tasksFlagStatus = ""
	tasksFlagPriority = ""
	tasksFlagOwnership = ""
	tasksFlagAll = false
	tasksFlagJSON = true

	err = tasksCmd.RunE(tasksCmd, nil)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `"Text": "JSON task"`)
	assert.Contains(t, output, `"Priority": "high"`)
}

func TestRunTasksCreate(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	buf := new(bytes.Buffer)
	tasksCreateCmd.SetOut(buf)
	tasksFlagText = "New task from CLI"
	tasksFlagIntent = "test intent"
	tasksFlagPriority = "high"
	tasksFlagOwnership = "mine"
	tasksFlagDue = "2026-04-15"
	tasksFlagSourceType = "manual"
	tasksFlagSourceID = ""
	tasksFlagTags = "urgent,api"

	err := tasksCreateCmd.RunE(tasksCreateCmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Created task #")

	// Verify in DB
	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()

	task, err := database.GetTaskByID(1)
	require.NoError(t, err)
	assert.Equal(t, "New task from CLI", task.Text)
	assert.Equal(t, "test intent", task.Intent)
	assert.Equal(t, "high", task.Priority)
	assert.Equal(t, "2026-04-15", task.DueDate)
	assert.Contains(t, task.Tags, "urgent")
	assert.Contains(t, task.Tags, "api")
}

func TestRunTasksCreate_RequiresText(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	tasksFlagText = ""
	err := tasksCreateCmd.RunE(tasksCreateCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--text is required")
}

func TestRunTasksDone(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{Text: "To finish", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksDoneCmd.SetOut(buf)

	err = tasksDoneCmd.RunE(tasksDoneCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "marked as done")

	database, err = openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	task, err := database.GetTaskByID(1)
	require.NoError(t, err)
	assert.Equal(t, "done", task.Status)
}

func TestRunTasksDismiss(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{Text: "To dismiss", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksDismissCmd.SetOut(buf)

	err = tasksDismissCmd.RunE(tasksDismissCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dismissed")

	database, err = openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	task, err := database.GetTaskByID(1)
	require.NoError(t, err)
	assert.Equal(t, "dismissed", task.Status)
}

func TestRunTasksSnooze(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{Text: "To snooze", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksSnoozeCmd.SetOut(buf)

	err = tasksSnoozeCmd.RunE(tasksSnoozeCmd, []string{"1", "2026-04-01"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "snoozed until 2026-04-01")

	database, err = openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	task, err := database.GetTaskByID(1)
	require.NoError(t, err)
	assert.Equal(t, "snoozed", task.Status)
	assert.Equal(t, "2026-04-01", task.SnoozeUntil)
}

func TestRunTasksShow(t *testing.T) {
	cleanup := setupTasksTestEnv(t)
	defer cleanup()

	database, err := openDBFromConfig()
	require.NoError(t, err)
	_, err = database.CreateTask(db.Task{
		Text:       "Show this task",
		Intent:     "test intent",
		Status:     "in_progress",
		Priority:   "high",
		Ownership:  "delegated",
		BallOn:     "alice",
		DueDate:    "2026-04-01",
		Tags:       `["review","api"]`,
		SubItems:   `[{"text":"Check tests","done":false},{"text":"Approve","done":true}]`,
		SourceType: "track",
		SourceID:   "42",
	})
	require.NoError(t, err)
	database.Close()

	buf := new(bytes.Buffer)
	tasksShowCmd.SetOut(buf)

	err = tasksShowCmd.RunE(tasksShowCmd, []string{"1"})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Show this task")
	assert.Contains(t, output, "in_progress")
	assert.Contains(t, output, "high")
	assert.Contains(t, output, "delegated")
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "2026-04-01")
	assert.Contains(t, output, "review")
	assert.Contains(t, output, "Check tests")
	assert.Contains(t, output, "[x] Approve")
	assert.Contains(t, output, "track")
	assert.Contains(t, output, "#42")
}

func TestRunTasksShow_InvalidID(t *testing.T) {
	err := tasksShowCmd.RunE(tasksShowCmd, []string{"abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid task ID")
}

func TestRunTasks_RequiresConfig(t *testing.T) {
	oldFlagConfig := flagConfig
	flagConfig = "/nonexistent/config.yaml"
	defer func() { flagConfig = oldFlagConfig }()

	tasksFlagStatus = ""
	tasksFlagPriority = ""
	tasksFlagOwnership = ""
	tasksFlagAll = false
	tasksFlagJSON = false

	err := tasksCmd.RunE(tasksCmd, nil)
	assert.Error(t, err)
}
