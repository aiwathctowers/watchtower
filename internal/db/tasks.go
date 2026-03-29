package db

import (
	"fmt"
	"strings"
	"time"
)

// taskSelectCols is the standard SELECT column list for tasks.
const taskSelectCols = `id, text, intent, status, priority, ownership,
	ball_on, due_date, snooze_until, blocking, tags, sub_items,
	source_type, source_id, created_at, updated_at`

// scanTask scans a Task from a row with the standard SELECT column list.
func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	if err := row.Scan(
		&t.ID, &t.Text, &t.Intent, &t.Status, &t.Priority, &t.Ownership,
		&t.BallOn, &t.DueDate, &t.SnoozeUntil, &t.Blocking, &t.Tags, &t.SubItems,
		&t.SourceType, &t.SourceID, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTask inserts a new task and returns its ID.
func (db *DB) CreateTask(t Task) (int64, error) {
	if t.Tags == "" {
		t.Tags = "[]"
	}
	if t.SubItems == "" {
		t.SubItems = "[]"
	}
	res, err := db.Exec(`INSERT INTO tasks (text, intent, status, priority, ownership,
		ball_on, due_date, snooze_until, blocking, tags, sub_items,
		source_type, source_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Text, t.Intent, t.Status, t.Priority, t.Ownership,
		t.BallOn, t.DueDate, t.SnoozeUntil, t.Blocking, t.Tags, t.SubItems,
		t.SourceType, t.SourceID,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting task: %w", err)
	}
	return res.LastInsertId()
}

// UpdateTask updates all fields of an existing task by ID.
func (db *DB) UpdateTask(t Task) error {
	_, err := db.Exec(`UPDATE tasks SET
		text = ?, intent = ?, status = ?, priority = ?, ownership = ?,
		ball_on = ?, due_date = ?, snooze_until = ?, blocking = ?, tags = ?, sub_items = ?,
		source_type = ?, source_id = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`,
		t.Text, t.Intent, t.Status, t.Priority, t.Ownership,
		t.BallOn, t.DueDate, t.SnoozeUntil, t.Blocking, t.Tags, t.SubItems,
		t.SourceType, t.SourceID,
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("updating task %d: %w", t.ID, err)
	}
	return nil
}

// GetTaskByID returns a single task by ID.
func (db *DB) GetTaskByID(id int) (*Task, error) {
	row := db.QueryRow(`SELECT `+taskSelectCols+` FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if err != nil {
		return nil, fmt.Errorf("getting task %d: %w", id, err)
	}
	return t, nil
}

// GetTasks returns tasks matching the filter.
// By default excludes done/dismissed tasks unless IncludeDone is true.
func (db *DB) GetTasks(f TaskFilter) ([]Task, error) {
	query := `SELECT ` + taskSelectCols + ` FROM tasks`
	var conditions []string
	var args []any

	if !f.IncludeDone {
		conditions = append(conditions, "status NOT IN ('done', 'dismissed')")
	}
	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, f.Status)
	}
	if f.Priority != "" {
		conditions = append(conditions, "priority = ?")
		args = append(args, f.Priority)
	}
	if f.Ownership != "" {
		conditions = append(conditions, "ownership = ?")
		args = append(args, f.Ownership)
	}
	if f.SourceType != "" {
		conditions = append(conditions, "source_type = ?")
		args = append(args, f.SourceType)
	}
	if f.SourceID != "" {
		conditions = append(conditions, "source_id = ?")
		args = append(args, f.SourceID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY
		CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
		CASE WHEN due_date = '' THEN 1 ELSE 0 END,
		due_date ASC,
		created_at DESC`
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

// UpdateTaskStatus changes the status of a task and updates updated_at.
func (db *DB) UpdateTaskStatus(id int, newStatus string) error {
	_, err := db.Exec(`UPDATE tasks SET status = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, newStatus, id)
	if err != nil {
		return fmt.Errorf("updating task %d status: %w", id, err)
	}
	return nil
}

// DeleteTask removes a task by ID.
func (db *DB) DeleteTask(id int) error {
	_, err := db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting task %d: %w", id, err)
	}
	return nil
}

// GetTaskCounts returns (active, overdue) task counts.
// Active = not done/dismissed. Overdue = active with due_date < today.
func (db *DB) GetTaskCounts() (int, int, error) {
	var active, overdue int
	today := time.Now().UTC().Format("2006-01-02")
	err := db.QueryRow(`SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN due_date != '' AND due_date < ? THEN 1 ELSE 0 END), 0)
		FROM tasks WHERE status NOT IN ('done', 'dismissed')`, today).Scan(&active, &overdue)
	return active, overdue, err
}

// UnsnoozeExpiredTasks moves snoozed tasks with expired snooze_until back to todo.
// Returns the number of tasks unsnoozed.
func (db *DB) UnsnoozeExpiredTasks() (int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	res, err := db.Exec(`UPDATE tasks SET status = 'todo', snooze_until = '',
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE status = 'snoozed' AND snooze_until != '' AND snooze_until <= ?`, today)
	if err != nil {
		return 0, fmt.Errorf("unsnoozing tasks: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GetTasksForBriefing returns active tasks relevant for the daily briefing.
// This includes todo, in_progress, and blocked tasks, ordered by priority.
func (db *DB) GetTasksForBriefing() ([]Task, error) {
	rows, err := db.Query(`SELECT ` + taskSelectCols + ` FROM tasks
		WHERE status IN ('todo', 'in_progress', 'blocked')
		ORDER BY
			CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
			CASE WHEN due_date = '' THEN 1 ELSE 0 END,
			due_date ASC,
			created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying tasks for briefing: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task for briefing: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}
