package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

const targetSelectCols = `id, text, intent, level, custom_label, period_start, period_end,
	parent_id, status, priority, ownership,
	ball_on, due_date, snooze_until, blocking, tags, sub_items, notes,
	progress, source_type, source_id, ai_level_confidence, created_at, updated_at`

func scanTarget(row interface{ Scan(...any) error }) (*Target, error) {
	var t Target
	if err := row.Scan(
		&t.ID, &t.Text, &t.Intent, &t.Level, &t.CustomLabel, &t.PeriodStart, &t.PeriodEnd,
		&t.ParentID, &t.Status, &t.Priority, &t.Ownership,
		&t.BallOn, &t.DueDate, &t.SnoozeUntil, &t.Blocking, &t.Tags, &t.SubItems, &t.Notes,
		&t.Progress, &t.SourceType, &t.SourceID, &t.AILevelConfidence, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTarget inserts a new target and returns its ID.
func (db *DB) CreateTarget(t Target) (int64, error) {
	if t.Tags == "" {
		t.Tags = "[]"
	}
	if t.SubItems == "" {
		t.SubItems = "[]"
	}
	if t.Notes == "" {
		t.Notes = "[]"
	}
	if t.Level == "" {
		t.Level = "day"
	}
	if t.PeriodStart == "" {
		t.PeriodStart = time.Now().UTC().Format("2006-01-02")
	}
	if t.PeriodEnd == "" {
		t.PeriodEnd = t.PeriodStart
	}

	// Derive initial progress from status (no children yet).
	progress := statusToProgress(t.Status)

	res, err := db.Exec(`INSERT INTO targets
		(text, intent, level, custom_label, period_start, period_end, parent_id,
		 status, priority, ownership, ball_on, due_date, snooze_until, blocking,
		 tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Text, t.Intent, t.Level, t.CustomLabel, t.PeriodStart, t.PeriodEnd, t.ParentID,
		t.Status, t.Priority, t.Ownership, t.BallOn, t.DueDate, t.SnoozeUntil, t.Blocking,
		t.Tags, t.SubItems, t.Notes, progress, t.SourceType, t.SourceID, t.AILevelConfidence,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Propagate progress to parent if one is set.
	if t.ParentID.Valid {
		if rerr := db.RecomputeParentProgress(t.ParentID.Int64); rerr != nil {
			// non-fatal
			_ = rerr
		}
	}
	return id, nil
}

// UpdateTarget updates all mutable fields of an existing target.
// It captures the old parent_id before the update, recomputes progress
// (mirroring UpdateTargetStatus semantics for leaf targets), and propagates
// progress to both old and new parents when parent_id changes.
func (db *DB) UpdateTarget(t Target) error {
	// Capture old parent before mutating.
	var oldParentID sql.NullInt64
	_ = db.QueryRow(`SELECT parent_id FROM targets WHERE id = ?`, t.ID).Scan(&oldParentID)

	// Derive progress from status (applied when target has no non-dismissed children).
	progress := statusToProgress(t.Status)

	_, err := db.Exec(`UPDATE targets SET
		text = ?, intent = ?, level = ?, custom_label = ?, period_start = ?, period_end = ?,
		parent_id = ?, status = ?, priority = ?, ownership = ?,
		ball_on = ?, due_date = ?, snooze_until = ?, blocking = ?,
		tags = ?, sub_items = ?, notes = ?, source_type = ?, source_id = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id = ?`,
		t.Text, t.Intent, t.Level, t.CustomLabel, t.PeriodStart, t.PeriodEnd,
		t.ParentID, t.Status, t.Priority, t.Ownership,
		t.BallOn, t.DueDate, t.SnoozeUntil, t.Blocking,
		t.Tags, t.SubItems, t.Notes, t.SourceType, t.SourceID,
		t.ID,
	)
	if err != nil {
		return fmt.Errorf("updating target %d: %w", t.ID, err)
	}

	// Update own progress when it has no non-dismissed children (leaf semantics).
	_, _ = db.Exec(`UPDATE targets SET progress = ? WHERE id = ? AND
		NOT EXISTS (SELECT 1 FROM targets c WHERE c.parent_id = targets.id AND c.status != 'dismissed')`,
		progress, t.ID)

	// Propagate progress to parent(s). Always recompute new parent.
	if t.ParentID.Valid {
		_ = db.RecomputeParentProgress(t.ParentID.Int64)
	}
	// If parent changed, also recompute old parent (so its average no longer includes this target).
	if oldParentID.Valid && oldParentID.Int64 != t.ParentID.Int64 {
		_ = db.RecomputeParentProgress(oldParentID.Int64)
	}
	return nil
}

// GetTargetByID returns a single target by ID.
func (db *DB) GetTargetByID(id int) (*Target, error) {
	row := db.QueryRow(`SELECT `+targetSelectCols+` FROM targets WHERE id = ?`, id)
	t, err := scanTarget(row)
	if err != nil {
		return nil, fmt.Errorf("getting target %d: %w", id, err)
	}
	return t, nil
}

// GetTargets returns targets matching the filter.
func (db *DB) GetTargets(f TargetFilter) ([]Target, error) {
	query := `SELECT ` + targetSelectCols + ` FROM targets`
	var conditions []string
	var args []any

	if !f.IncludeDone {
		conditions = append(conditions, "status NOT IN ('done','dismissed')")
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
	if f.Level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, f.Level)
	}
	if f.ParentID != nil {
		conditions = append(conditions, "parent_id = ?")
		args = append(args, *f.ParentID)
	}
	if f.SourceType != "" {
		conditions = append(conditions, "source_type = ?")
		args = append(args, f.SourceType)
	}
	if f.SourceID != "" {
		conditions = append(conditions, "source_id = ?")
		args = append(args, f.SourceID)
	}
	if f.Search != "" {
		conditions = append(conditions, `(text LIKE ? ESCAPE '\' OR intent LIKE ? ESCAPE '\')`+``)
		like := "%" + escapeLike(f.Search) + "%"
		args = append(args, like, like)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY
		CASE level WHEN 'quarter' THEN 0 WHEN 'month' THEN 1 WHEN 'week' THEN 2 WHEN 'day' THEN 3 ELSE 4 END,
		period_start ASC,
		CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
		CASE WHEN due_date = '' THEN 1 ELSE 0 END,
		due_date ASC,
		created_at DESC`
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying targets: %w", err)
	}
	defer rows.Close()

	var targets []Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning target: %w", err)
		}
		targets = append(targets, *t)
	}
	return targets, rows.Err()
}

// UpdateTargetStatus changes the status of a target and recomputes parent progress.
func (db *DB) UpdateTargetStatus(id int, newStatus string) error {
	// Fetch parent_id before updating.
	var parentID sql.NullInt64
	_ = db.QueryRow(`SELECT parent_id FROM targets WHERE id = ?`, id).Scan(&parentID)

	_, err := db.Exec(`UPDATE targets SET status = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id = ?`, newStatus, id)
	if err != nil {
		return fmt.Errorf("updating target %d status: %w", id, err)
	}

	// Recompute own progress from status (leaf target with no children).
	progress := statusToProgress(newStatus)
	_, _ = db.Exec(`UPDATE targets SET progress = ? WHERE id = ? AND
		NOT EXISTS (SELECT 1 FROM targets c WHERE c.parent_id = targets.id AND c.status != 'dismissed')`,
		progress, id)

	// BEHAVIOR INBOX-02 — closing a target resolves its target_due inbox item
	// so the user never has to close the same thing twice (mirrors how
	// auto-resolve works for slack/jira/calendar). See
	// docs/inventory/inbox-pulse.md.
	if newStatus == "done" || newStatus == "dismissed" {
		_, _ = db.Exec(`UPDATE inbox_items
			SET status = 'resolved',
			    resolved_reason = 'target_closed',
			    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
			WHERE target_id = ? AND trigger_type = 'target_due' AND status = 'pending'`, id)
	}

	if parentID.Valid {
		_ = db.RecomputeParentProgress(parentID.Int64)
	}
	return nil
}

// DeleteTarget removes a target by ID.
func (db *DB) DeleteTarget(id int) error {
	var parentID sql.NullInt64
	_ = db.QueryRow(`SELECT parent_id FROM targets WHERE id = ?`, id).Scan(&parentID)

	_, err := db.Exec(`DELETE FROM targets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting target %d: %w", id, err)
	}

	if parentID.Valid {
		_ = db.RecomputeParentProgress(parentID.Int64)
	}
	return nil
}

// GetTargetCounts returns (active, overdue) target counts.
func (db *DB) GetTargetCounts() (int, int, error) {
	var active, overdue int
	now := time.Now().UTC().Format("2006-01-02T15:04")
	err := db.QueryRow(`SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN due_date != '' AND due_date < ? THEN 1 ELSE 0 END), 0)
		FROM targets WHERE status NOT IN ('done','dismissed')`, now).Scan(&active, &overdue)
	return active, overdue, err
}

// UnsnoozeExpiredTargets moves snoozed targets with expired snooze_until back to todo.
func (db *DB) UnsnoozeExpiredTargets() (int, error) {
	now := time.Now().UTC().Format("2006-01-02T15:04")
	res, err := db.Exec(`UPDATE targets SET status = 'todo', snooze_until = '',
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE status = 'snoozed' AND snooze_until != '' AND snooze_until <= ?`, now)
	if err != nil {
		return 0, fmt.Errorf("unsnoozing targets: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GetTargetsForBriefing returns active targets relevant for the daily briefing.
func (db *DB) GetTargetsForBriefing() ([]Target, error) {
	rows, err := db.Query(`SELECT ` + targetSelectCols + ` FROM targets
		WHERE status IN ('todo','in_progress','blocked')
		ORDER BY
			CASE level WHEN 'quarter' THEN 0 WHEN 'month' THEN 1 WHEN 'week' THEN 2 WHEN 'day' THEN 3 ELSE 4 END,
			CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
			CASE WHEN due_date = '' THEN 1 ELSE 0 END,
			due_date ASC,
			created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying targets for briefing: %w", err)
	}
	defer rows.Close()

	var targets []Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning target for briefing: %w", err)
		}
		targets = append(targets, *t)
	}
	return targets, rows.Err()
}

// recomputeParentProgressMaxDepth caps the ancestor walk to prevent infinite
// loops from cycles or unexpectedly deep hierarchies.
const recomputeParentProgressMaxDepth = 20

// targetsQuerier is the subset of *sql.DB / *sql.Tx used by the parent-progress
// walker, so the same logic can run inside a transaction or against the pool.
type targetsQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// RecomputeParentProgress updates parent.progress to AVG of non-dismissed children's progress,
// then walks up the ancestor chain iteratively (max 20 levels). Cycles are detected via a
// visited set; exceeding max depth logs a warning and stops without returning an error.
func (db *DB) RecomputeParentProgress(parentID int64) error {
	return recomputeParentProgressOn(db, parentID)
}

// recomputeParentProgressOn is the shared implementation used by both the
// auto-commit `RecomputeParentProgress` (taking *sql.DB) and the in-tx variant
// invoked from `PromoteSubItemToChild` (taking *sql.Tx) so the recompute is
// part of the same atomic unit as the mutations that triggered it.
func recomputeParentProgressOn(q targetsQuerier, parentID int64) error {
	visited := make(map[int64]bool)
	current := parentID

	for depth := 0; depth < recomputeParentProgressMaxDepth; depth++ {
		if visited[current] {
			log.Printf("db: RecomputeParentProgress detected cycle at target %d — stopping", current)
			return nil
		}
		visited[current] = true

		var avg sql.NullFloat64
		err := q.QueryRow(`SELECT AVG(progress) FROM targets
			WHERE parent_id = ? AND status != 'dismissed'`, current).Scan(&avg)
		if err != nil {
			return fmt.Errorf("averaging children progress for target %d: %w", current, err)
		}

		var newProgress float64
		if avg.Valid {
			newProgress = avg.Float64
		} else {
			// No non-dismissed children: derive from own status.
			var status string
			if serr := q.QueryRow(`SELECT status FROM targets WHERE id = ?`, current).Scan(&status); serr == nil {
				newProgress = statusToProgress(status)
			}
		}

		_, err = q.Exec(`UPDATE targets SET progress = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
			WHERE id = ?`, newProgress, current)
		if err != nil {
			return fmt.Errorf("updating parent %d progress: %w", current, err)
		}

		// Walk up to the next ancestor.
		var nextParent sql.NullInt64
		if gerr := q.QueryRow(`SELECT parent_id FROM targets WHERE id = ?`, current).Scan(&nextParent); gerr != nil || !nextParent.Valid {
			break // reached the root or target not found
		}
		current = nextParent.Int64
	}

	if visited[current] {
		// Already handled the cycle case above; nothing more to do.
		return nil
	}
	// If we exhausted max depth without a cycle, log a warning.
	if len(visited) >= recomputeParentProgressMaxDepth {
		log.Printf("db: RecomputeParentProgress reached max depth (%d) at target %d — stopping", recomputeParentProgressMaxDepth, current)
	}
	return nil
}

// escapeLike escapes backslash, percent, and underscore in s so it is safe
// to embed in a SQLite LIKE pattern with ESCAPE '\'.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// statusToProgress maps a target status to a leaf-level progress value.
func statusToProgress(status string) float64 {
	switch status {
	case "done":
		return 1.0
	case "in_progress":
		return 0.5
	case "blocked":
		return 0.2
	default: // todo, snoozed, dismissed
		return 0.0
	}
}
