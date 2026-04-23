package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullTimeStr converts a sql.NullTime to a sql.NullString in RFC3339 format.
func nullTimeStr(nt sql.NullTime) sql.NullString {
	if !nt.Valid {
		return sql.NullString{}
	}
	return sql.NullString{Valid: true, String: nt.Time.UTC().Format(time.RFC3339)}
}

// parseTimeOrZero parses an RFC3339 string, returning zero Time on failure.
func parseTimeOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ── scan helpers ──────────────────────────────────────────────────────────────

const dayPlanSelectCols = `id, user_id, plan_date, status, has_conflicts,
	conflict_summary, generated_at, last_regenerated_at, regenerate_count,
	feedback_history, prompt_version, briefing_id, read_at, created_at, updated_at`

func scanDayPlan(row *sql.Row) (*DayPlan, error) {
	var p DayPlan
	var hasConflicts int
	var generatedAt, createdAt, updatedAt string
	var lastRegeneratedAt sql.NullString
	var readAt sql.NullString
	var feedbackHistory sql.NullString

	err := row.Scan(
		&p.ID, &p.UserID, &p.PlanDate, &p.Status, &hasConflicts,
		&p.ConflictSummary, &generatedAt, &lastRegeneratedAt, &p.RegenerateCount,
		&feedbackHistory, &p.PromptVersion, &p.BriefingID, &readAt,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning day_plan: %w", err)
	}

	p.HasConflicts = hasConflicts != 0
	p.GeneratedAt = parseTimeOrZero(generatedAt)
	p.CreatedAt = parseTimeOrZero(createdAt)
	p.UpdatedAt = parseTimeOrZero(updatedAt)

	if lastRegeneratedAt.Valid {
		p.LastRegeneratedAt = sql.NullTime{Valid: true, Time: parseTimeOrZero(lastRegeneratedAt.String)}
	}
	if readAt.Valid {
		p.ReadAt = sql.NullTime{Valid: true, Time: parseTimeOrZero(readAt.String)}
	}
	if feedbackHistory.Valid {
		p.FeedbackHistory = feedbackHistory.String
	} else {
		p.FeedbackHistory = "[]"
	}

	return &p, nil
}

func scanDayPlanRows(rows *sql.Rows) ([]DayPlan, error) {
	var plans []DayPlan
	for rows.Next() {
		var p DayPlan
		var hasConflicts int
		var generatedAt, createdAt, updatedAt string
		var lastRegeneratedAt sql.NullString
		var readAt sql.NullString
		var feedbackHistory sql.NullString

		err := rows.Scan(
			&p.ID, &p.UserID, &p.PlanDate, &p.Status, &hasConflicts,
			&p.ConflictSummary, &generatedAt, &lastRegeneratedAt, &p.RegenerateCount,
			&feedbackHistory, &p.PromptVersion, &p.BriefingID, &readAt,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning day_plan row: %w", err)
		}

		p.HasConflicts = hasConflicts != 0
		p.GeneratedAt = parseTimeOrZero(generatedAt)
		p.CreatedAt = parseTimeOrZero(createdAt)
		p.UpdatedAt = parseTimeOrZero(updatedAt)

		if lastRegeneratedAt.Valid {
			p.LastRegeneratedAt = sql.NullTime{Valid: true, Time: parseTimeOrZero(lastRegeneratedAt.String)}
		}
		if readAt.Valid {
			p.ReadAt = sql.NullTime{Valid: true, Time: parseTimeOrZero(readAt.String)}
		}
		if feedbackHistory.Valid {
			p.FeedbackHistory = feedbackHistory.String
		} else {
			p.FeedbackHistory = "[]"
		}

		plans = append(plans, p)
	}
	return plans, rows.Err()
}

const dayPlanItemSelectCols = `id, day_plan_id, kind, source_type, source_id,
	title, description, rationale, start_time, end_time, duration_min,
	priority, status, order_index, tags, created_at, updated_at`

func scanDayPlanItemRows(rows *sql.Rows) ([]DayPlanItem, error) {
	var items []DayPlanItem
	for rows.Next() {
		var it DayPlanItem
		var startTime, endTime sql.NullString
		var createdAt, updatedAt string
		var tags sql.NullString

		err := rows.Scan(
			&it.ID, &it.DayPlanID, &it.Kind, &it.SourceType, &it.SourceID,
			&it.Title, &it.Description, &it.Rationale, &startTime, &endTime, &it.DurationMin,
			&it.Priority, &it.Status, &it.OrderIndex, &tags,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning day_plan_item row: %w", err)
		}

		if startTime.Valid {
			it.StartTime = sql.NullTime{Valid: true, Time: parseTimeOrZero(startTime.String)}
		}
		if endTime.Valid {
			it.EndTime = sql.NullTime{Valid: true, Time: parseTimeOrZero(endTime.String)}
		}
		if tags.Valid {
			it.Tags = tags.String
		} else {
			it.Tags = "[]"
		}
		it.CreatedAt = parseTimeOrZero(createdAt)
		it.UpdatedAt = parseTimeOrZero(updatedAt)

		items = append(items, it)
	}
	return items, rows.Err()
}

// ── DayPlan writes ────────────────────────────────────────────────────────────

// CreateDayPlan inserts a new day plan and returns its new id.
// If p.FeedbackHistory is empty it is set to "[]".
func (db *DB) CreateDayPlan(p *DayPlan) (int64, error) {
	if p.FeedbackHistory == "" {
		p.FeedbackHistory = "[]"
	}
	generatedAt := p.GeneratedAt.UTC().Format(time.RFC3339)
	lastRegen := nullTimeStr(p.LastRegeneratedAt)
	readAt := nullTimeStr(p.ReadAt)

	var conflictSummary sql.NullString
	if p.ConflictSummary.Valid {
		conflictSummary = p.ConflictSummary
	}

	res, err := db.Exec(`INSERT INTO day_plans
		(user_id, plan_date, status, has_conflicts, conflict_summary,
		 generated_at, last_regenerated_at, regenerate_count,
		 feedback_history, prompt_version, briefing_id, read_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.PlanDate, p.Status, boolToInt(p.HasConflicts), conflictSummary,
		generatedAt, lastRegen, p.RegenerateCount,
		p.FeedbackHistory, p.PromptVersion, p.BriefingID, readAt,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting day_plan: %w", err)
	}
	return res.LastInsertId()
}

// UpsertDayPlan inserts or updates a day plan keyed on (user_id, plan_date).
// Returns the id of the row either way.
func (db *DB) UpsertDayPlan(p *DayPlan) (int64, error) {
	existing, err := db.GetDayPlan(p.UserID, p.PlanDate)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		// Update in place.
		if p.FeedbackHistory == "" {
			p.FeedbackHistory = "[]"
		}
		generatedAt := p.GeneratedAt.UTC().Format(time.RFC3339)
		lastRegen := nullTimeStr(p.LastRegeneratedAt)
		readAt := nullTimeStr(p.ReadAt)

		_, err = db.Exec(`UPDATE day_plans SET
			status = ?, has_conflicts = ?, conflict_summary = ?,
			generated_at = ?, last_regenerated_at = ?, regenerate_count = ?,
			feedback_history = ?, prompt_version = ?, briefing_id = ?, read_at = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
			WHERE user_id = ? AND plan_date = ?`,
			p.Status, boolToInt(p.HasConflicts), p.ConflictSummary,
			generatedAt, lastRegen, p.RegenerateCount,
			p.FeedbackHistory, p.PromptVersion, p.BriefingID, readAt,
			p.UserID, p.PlanDate,
		)
		if err != nil {
			return 0, fmt.Errorf("updating day_plan: %w", err)
		}
		return existing.ID, nil
	}
	return db.CreateDayPlan(p)
}

// ── DayPlan reads ─────────────────────────────────────────────────────────────

// GetDayPlan returns the day plan for (userID, date), or (nil, nil) if not found.
func (db *DB) GetDayPlan(userID, date string) (*DayPlan, error) {
	row := db.QueryRow(`SELECT `+dayPlanSelectCols+` FROM day_plans WHERE user_id = ? AND plan_date = ?`,
		userID, date)
	return scanDayPlan(row)
}

// GetDayPlanByID returns a day plan by its primary key.
func (db *DB) GetDayPlanByID(id int64) (*DayPlan, error) {
	row := db.QueryRow(`SELECT `+dayPlanSelectCols+` FROM day_plans WHERE id = ?`, id)
	p, err := scanDayPlan(row)
	if err != nil {
		return nil, fmt.Errorf("getting day_plan %d: %w", id, err)
	}
	return p, nil
}

// ListDayPlans returns day plans for a user, newest first. Defaults to 10 if limit≤0.
func (db *DB) ListDayPlans(userID string, limit int) ([]DayPlan, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.Query(`SELECT `+dayPlanSelectCols+` FROM day_plans
		WHERE user_id = ? ORDER BY plan_date DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing day_plans: %w", err)
	}
	defer rows.Close()
	return scanDayPlanRows(rows)
}

// ── DayPlan item writes ───────────────────────────────────────────────────────

// CreateDayPlanItems inserts items for a plan. Empty Tags is set to "[]".
func (db *DB) CreateDayPlanItems(planID int64, items []DayPlanItem) error {
	for _, it := range items {
		if it.Tags == "" {
			it.Tags = "[]"
		}
		startTime := nullTimeStr(it.StartTime)
		endTime := nullTimeStr(it.EndTime)

		_, err := db.Exec(`INSERT INTO day_plan_items
			(day_plan_id, kind, source_type, source_id, title, description, rationale,
			 start_time, end_time, duration_min, priority, status, order_index, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, it.Kind, it.SourceType, it.SourceID, it.Title,
			it.Description, it.Rationale,
			startTime, endTime, it.DurationMin, it.Priority,
			it.Status, it.OrderIndex, it.Tags,
		)
		if err != nil {
			return fmt.Errorf("inserting day_plan_item: %w", err)
		}
	}
	return nil
}

// ReplaceAIItems atomically removes all non-manual, non-calendar items for a
// plan and inserts newItems in their place. Manual and calendar items are preserved.
func (db *DB) ReplaceAIItems(planID int64, newItems []DayPlanItem) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.Exec(`DELETE FROM day_plan_items
		WHERE day_plan_id = ? AND source_type NOT IN ('manual','calendar')`, planID)
	if err != nil {
		return fmt.Errorf("deleting AI items: %w", err)
	}

	for _, it := range newItems {
		if it.Tags == "" {
			it.Tags = "[]"
		}
		startTime := nullTimeStr(it.StartTime)
		endTime := nullTimeStr(it.EndTime)

		_, err = tx.Exec(`INSERT INTO day_plan_items
			(day_plan_id, kind, source_type, source_id, title, description, rationale,
			 start_time, end_time, duration_min, priority, status, order_index, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, it.Kind, it.SourceType, it.SourceID, it.Title,
			it.Description, it.Rationale,
			startTime, endTime, it.DurationMin, it.Priority,
			it.Status, it.OrderIndex, it.Tags,
		)
		if err != nil {
			return fmt.Errorf("inserting replacement item: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing ReplaceAIItems: %w", err)
	}
	return nil
}

// ── DayPlan item reads ────────────────────────────────────────────────────────

// GetDayPlanItems returns items for a plan ordered by: timeblocks first,
// then start_time ASC, then order_index ASC.
func (db *DB) GetDayPlanItems(planID int64) ([]DayPlanItem, error) {
	rows, err := db.Query(`SELECT `+dayPlanItemSelectCols+` FROM day_plan_items
		WHERE day_plan_id = ?
		ORDER BY
			CASE WHEN kind = 'timeblock' THEN 0 ELSE 1 END ASC,
			start_time ASC,
			order_index ASC`,
		planID)
	if err != nil {
		return nil, fmt.Errorf("getting day_plan_items: %w", err)
	}
	defer rows.Close()
	return scanDayPlanItemRows(rows)
}

// UpdateItemStatus sets the status of a single day plan item.
func (db *DB) UpdateItemStatus(itemID int64, status string) error {
	_, err := db.Exec(`UPDATE day_plan_items SET status = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?`, status, itemID)
	if err != nil {
		return fmt.Errorf("updating item %d status: %w", itemID, err)
	}
	return nil
}

// UpdateItemOrder transactionally sets order_index for each item id.
func (db *DB) UpdateItemOrder(planID int64, orderedIDs []int64) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for idx, id := range orderedIDs {
		_, err = tx.Exec(`UPDATE day_plan_items SET order_index = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
			WHERE id = ? AND day_plan_id = ?`, idx, id, planID)
		if err != nil {
			return fmt.Errorf("updating order for item %d: %w", id, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing UpdateItemOrder: %w", err)
	}
	return nil
}

// DeleteDayPlanItem removes a single item by id.
func (db *DB) DeleteDayPlanItem(itemID int64) error {
	_, err := db.Exec(`DELETE FROM day_plan_items WHERE id = ?`, itemID)
	if err != nil {
		return fmt.Errorf("deleting day_plan_item %d: %w", itemID, err)
	}
	return nil
}

// ── DayPlan state updates ─────────────────────────────────────────────────────

// MarkDayPlanRead sets read_at to now (only if not already set).
func (db *DB) MarkDayPlanRead(planID int64) error {
	_, err := db.Exec(`UPDATE day_plans SET
		read_at = strftime('%Y-%m-%dT%H:%M:%SZ','now'),
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id = ? AND read_at IS NULL`, planID)
	if err != nil {
		return fmt.Errorf("marking day_plan %d read: %w", planID, err)
	}
	return nil
}

// SetHasConflicts updates conflict fields on a day plan.
// An empty summary string stores NULL.
func (db *DB) SetHasConflicts(planID int64, hasConflicts bool, summary string) error {
	var nullSummary sql.NullString
	if summary != "" {
		nullSummary = sql.NullString{Valid: true, String: summary}
	}
	_, err := db.Exec(`UPDATE day_plans SET
		has_conflicts = ?, conflict_summary = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id = ?`, boolToInt(hasConflicts), nullSummary, planID)
	if err != nil {
		return fmt.Errorf("setting conflicts on day_plan %d: %w", planID, err)
	}
	return nil
}

// IncrementRegenerateCount prepends feedback into the feedback_history JSON
// array (keeping last 5 entries), increments regenerate_count, and sets
// last_regenerated_at to now.
func (db *DB) IncrementRegenerateCount(planID int64, feedback string) error {
	// Load current history.
	var raw sql.NullString
	err := db.QueryRow(`SELECT feedback_history FROM day_plans WHERE id = ?`, planID).Scan(&raw)
	if err != nil {
		return fmt.Errorf("reading feedback_history for day_plan %d: %w", planID, err)
	}

	histJSON := "[]"
	if raw.Valid && raw.String != "" {
		histJSON = raw.String
	}

	var history []string
	if err = json.Unmarshal([]byte(histJSON), &history); err != nil {
		history = []string{}
	}

	// Prepend new feedback.
	if feedback != "" {
		history = append([]string{feedback}, history...)
	}
	// Keep last 5.
	if len(history) > 5 {
		history = history[:5]
	}

	updated, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("marshalling feedback_history: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`UPDATE day_plans SET
		feedback_history = ?,
		regenerate_count = regenerate_count + 1,
		last_regenerated_at = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
		WHERE id = ?`, string(updated), now, planID)
	if err != nil {
		return fmt.Errorf("incrementing regenerate_count for day_plan %d: %w", planID, err)
	}
	return nil
}
