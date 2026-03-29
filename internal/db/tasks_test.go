package db

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTask_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateTask(Task{
		Text:       "Review PR #42",
		Intent:     "Check the API changes",
		Status:     "todo",
		Priority:   "high",
		Ownership:  "mine",
		BallOn:     "alice",
		DueDate:    "2026-03-25",
		Tags:       `["review","api"]`,
		SubItems:   `[{"text":"Check tests","done":false}]`,
		SourceType: "track",
		SourceID:   "7",
	})
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))

	task, err := db.GetTaskByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Review PR #42", task.Text)
	assert.Equal(t, "Check the API changes", task.Intent)
	assert.Equal(t, "todo", task.Status)
	assert.Equal(t, "high", task.Priority)
	assert.Equal(t, "mine", task.Ownership)
	assert.Equal(t, "alice", task.BallOn)
	assert.Equal(t, "2026-03-25", task.DueDate)
	assert.Equal(t, `["review","api"]`, task.Tags)
	assert.Equal(t, `[{"text":"Check tests","done":false}]`, task.SubItems)
	assert.Equal(t, "track", task.SourceType)
	assert.Equal(t, "7", task.SourceID)
	assert.NotEmpty(t, task.CreatedAt)
	assert.NotEmpty(t, task.UpdatedAt)
}

func TestCreateTask_Defaults(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateTask(Task{
		Text:       "Simple task",
		Status:     "todo",
		Priority:   "medium",
		Ownership:  "mine",
		SourceType: "manual",
	})
	require.NoError(t, err)

	task, err := db.GetTaskByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "", task.Intent)
	assert.Equal(t, "", task.BallOn)
	assert.Equal(t, "", task.DueDate)
	assert.Equal(t, "", task.SnoozeUntil)
	assert.Equal(t, "", task.Blocking)
	assert.Equal(t, "[]", task.Tags)
	assert.Equal(t, "[]", task.SubItems)
	assert.Equal(t, "", task.SourceID)
}

func TestGetTasks_FilterByStatus(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTask(Task{Text: "Todo task", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "In progress task", Status: "in_progress", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Done task", Status: "done", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	tasks, err := db.GetTasks(TaskFilter{Status: "todo"})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "Todo task", tasks[0].Text)
}

func TestGetTasks_FilterByPriority(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTask(Task{Text: "High task", Status: "todo", Priority: "high", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Low task", Status: "todo", Priority: "low", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	tasks, err := db.GetTasks(TaskFilter{Priority: "high"})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "High task", tasks[0].Text)
}

func TestGetTasks_FilterByOwnership(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTask(Task{Text: "My task", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Delegated task", Status: "todo", Priority: "medium", Ownership: "delegated", SourceType: "manual"})
	require.NoError(t, err)

	tasks, err := db.GetTasks(TaskFilter{Ownership: "delegated"})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "Delegated task", tasks[0].Text)
}

func TestGetTasks_FilterBySource(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTask(Task{Text: "From track", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "track", SourceID: "5"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Manual", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	tasks, err := db.GetTasks(TaskFilter{SourceType: "track", SourceID: "5"})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "From track", tasks[0].Text)
}

func TestGetTasks_DefaultExcludesDone(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTask(Task{Text: "Active", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Done", Status: "done", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Dismissed", Status: "dismissed", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	// Default: excludes done/dismissed
	tasks, err := db.GetTasks(TaskFilter{})
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "Active", tasks[0].Text)

	// IncludeDone: shows all
	tasks, err = db.GetTasks(TaskFilter{IncludeDone: true})
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
}

func TestGetTasks_Ordering(t *testing.T) {
	db := openTestDB(t)

	// High priority, no due date
	_, err := db.CreateTask(Task{Text: "High no date", Status: "todo", Priority: "high", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	// Medium priority, due soon
	_, err = db.CreateTask(Task{Text: "Medium due soon", Status: "todo", Priority: "medium", Ownership: "mine", DueDate: "2026-03-20", SourceType: "manual"})
	require.NoError(t, err)
	// High priority, due later
	_, err = db.CreateTask(Task{Text: "High due later", Status: "todo", Priority: "high", Ownership: "mine", DueDate: "2026-03-30", SourceType: "manual"})
	require.NoError(t, err)
	// Low priority, due soon
	_, err = db.CreateTask(Task{Text: "Low due soon", Status: "todo", Priority: "low", Ownership: "mine", DueDate: "2026-03-15", SourceType: "manual"})
	require.NoError(t, err)

	tasks, err := db.GetTasks(TaskFilter{})
	require.NoError(t, err)
	require.Len(t, tasks, 4)
	// Order: high with due_date first, high without due_date, medium with due_date, low with due_date
	assert.Equal(t, "High due later", tasks[0].Text)
	assert.Equal(t, "High no date", tasks[1].Text)
	assert.Equal(t, "Medium due soon", tasks[2].Text)
	assert.Equal(t, "Low due soon", tasks[3].Text)
}

func TestUpdateTask(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateTask(Task{Text: "Original", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	// Shift created_at into the past so updated_at will differ after update.
	_, err = db.Exec(`UPDATE tasks SET created_at = '2020-01-01T00:00:00Z', updated_at = '2020-01-01T00:00:00Z' WHERE id = ?`, id)
	require.NoError(t, err)

	err = db.UpdateTask(Task{
		ID:         int(id),
		Text:       "Updated",
		Intent:     "new intent",
		Status:     "in_progress",
		Priority:   "high",
		Ownership:  "delegated",
		BallOn:     "bob",
		DueDate:    "2026-04-01",
		Tags:       `["urgent"]`,
		SubItems:   `[]`,
		SourceType: "manual",
	})
	require.NoError(t, err)

	updated, err := db.GetTaskByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.Text)
	assert.Equal(t, "new intent", updated.Intent)
	assert.Equal(t, "in_progress", updated.Status)
	assert.Equal(t, "high", updated.Priority)
	assert.Equal(t, "delegated", updated.Ownership)
	assert.Equal(t, "bob", updated.BallOn)
	assert.Equal(t, "2026-04-01", updated.DueDate)
	assert.NotEqual(t, "2020-01-01T00:00:00Z", updated.UpdatedAt)
}

func TestUpdateTaskStatus(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateTask(Task{Text: "Task", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	err = db.UpdateTaskStatus(int(id), "in_progress")
	require.NoError(t, err)

	task, err := db.GetTaskByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "in_progress", task.Status)

	err = db.UpdateTaskStatus(int(id), "done")
	require.NoError(t, err)

	task, err = db.GetTaskByID(int(id))
	require.NoError(t, err)
	assert.Equal(t, "done", task.Status)
}

func TestDeleteTask(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateTask(Task{Text: "To delete", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	err = db.DeleteTask(int(id))
	require.NoError(t, err)

	_, err = db.GetTaskByID(int(id))
	assert.Error(t, err)
}

func TestGetTaskCounts(t *testing.T) {
	db := openTestDB(t)

	// Active tasks
	_, err := db.CreateTask(Task{Text: "Active 1", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Active 2", Status: "in_progress", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	// Done task (not counted)
	_, err = db.CreateTask(Task{Text: "Done", Status: "done", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	// Overdue task
	_, err = db.CreateTask(Task{Text: "Overdue", Status: "todo", Priority: "high", Ownership: "mine", DueDate: "2020-01-01", SourceType: "manual"})
	require.NoError(t, err)

	active, overdue, err := db.GetTaskCounts()
	require.NoError(t, err)
	assert.Equal(t, 3, active)
	assert.Equal(t, 1, overdue)
}

func TestUnsnoozeExpiredTasks(t *testing.T) {
	db := openTestDB(t)

	// Snoozed until yesterday — should be unsnoozed
	_, err := db.CreateTask(Task{Text: "Expired snooze", Status: "snoozed", Priority: "medium", Ownership: "mine", SnoozeUntil: "2020-01-01", SourceType: "manual"})
	require.NoError(t, err)
	// Snoozed until today — should be unsnoozed (snooze_until <= today)
	_, err = db.CreateTask(Task{Text: "Today snooze", Status: "snoozed", Priority: "medium", Ownership: "mine", SnoozeUntil: "2026-03-25", SourceType: "manual"})
	require.NoError(t, err)
	// Snoozed until far future — should remain snoozed
	_, err = db.CreateTask(Task{Text: "Future snooze", Status: "snoozed", Priority: "medium", Ownership: "mine", SnoozeUntil: "2099-12-31", SourceType: "manual"})
	require.NoError(t, err)
	// Not snoozed — should not be affected
	_, err = db.CreateTask(Task{Text: "Normal task", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	n, err := db.UnsnoozeExpiredTasks()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, 1) // At least the 2020 one

	// Check all tasks
	tasks, err := db.GetTasks(TaskFilter{IncludeDone: true})
	require.NoError(t, err)

	statusByText := make(map[string]string)
	for _, task := range tasks {
		statusByText[task.Text] = task.Status
	}
	assert.Equal(t, "todo", statusByText["Expired snooze"])
	assert.Equal(t, "todo", statusByText["Normal task"])
	assert.Equal(t, "snoozed", statusByText["Future snooze"])
}

func TestGetTasksForBriefing(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTask(Task{Text: "Todo", Status: "todo", Priority: "high", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "In progress", Status: "in_progress", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Blocked", Status: "blocked", Priority: "low", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Done", Status: "done", Priority: "high", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)
	_, err = db.CreateTask(Task{Text: "Snoozed", Status: "snoozed", Priority: "high", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	tasks, err := db.GetTasksForBriefing()
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
	// Should be ordered by priority
	assert.Equal(t, "Todo", tasks[0].Text)
	assert.Equal(t, "In progress", tasks[1].Text)
	assert.Equal(t, "Blocked", tasks[2].Text)
}

func TestFeedback_TaskEntityType(t *testing.T) {
	db := openTestDB(t)

	id, err := db.CreateTask(Task{Text: "Feedback test", Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
	require.NoError(t, err)

	fID, err := db.AddFeedback(Feedback{
		EntityType: "task",
		EntityID:   fmt.Sprintf("%d", id),
		Rating:     1,
		Comment:    "helpful task",
	})
	require.NoError(t, err)
	assert.Greater(t, fID, int64(0))

	feedback, err := db.GetFeedback(FeedbackFilter{EntityType: "task"})
	require.NoError(t, err)
	require.Len(t, feedback, 1)
	assert.Equal(t, "task", feedback[0].EntityType)
	assert.Equal(t, 1, feedback[0].Rating)
}

func TestGetTasks_Limit(t *testing.T) {
	db := openTestDB(t)

	for i := 0; i < 5; i++ {
		_, err := db.CreateTask(Task{Text: fmt.Sprintf("Task %d", i), Status: "todo", Priority: "medium", Ownership: "mine", SourceType: "manual"})
		require.NoError(t, err)
	}

	tasks, err := db.GetTasks(TaskFilter{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, tasks, 3)
}
