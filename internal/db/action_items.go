package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// UpsertActionItem inserts or updates an action item with change history logging.
// Deduplicates on (channel_id, assignee_user_id, source_message_ts, text).
// On conflict: updates context, priority, due_date but preserves user-set status.
func (db *DB) UpsertActionItem(item ActionItem) (int64, error) {
	var dueDate any
	if item.DueDate > 0 {
		dueDate = item.DueDate
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning action item upsert tx: %w", err)
	}
	defer tx.Rollback()

	// Check if item already exists (within the same transaction to avoid TOCTOU race)
	var existing struct {
		id       int64
		context  string
		priority string
		dueDate  sql.NullFloat64
		status   string
	}
	err = tx.QueryRow(`SELECT id, context, priority, due_date, status FROM action_items
		WHERE channel_id = ? AND assignee_user_id = ? AND source_message_ts = ? AND text = ?`,
		item.ChannelID, item.AssigneeUserID, item.SourceMessageTS, item.Text,
	).Scan(&existing.id, &existing.context, &existing.priority, &existing.dueDate, &existing.status)

	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("checking existing action item: %w", err)
	}
	isNew := err == sql.ErrNoRows

	// Perform upsert
	res, err := tx.Exec(`INSERT INTO action_items
		(channel_id, assignee_user_id, assignee_raw, text, context,
		 source_message_ts, source_channel_name, status, priority, due_date,
		 period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		 participants, source_refs,
		 requester_name, requester_user_id, category, blocking, tags,
		 decision_summary, decision_options, related_digest_ids, sub_items)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(channel_id, assignee_user_id, source_message_ts, text) DO UPDATE SET
			context = excluded.context,
			priority = excluded.priority,
			due_date = excluded.due_date,
			period_from = excluded.period_from,
			period_to = excluded.period_to,
			model = excluded.model,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			cost_usd = excluded.cost_usd,
			participants = excluded.participants,
			source_refs = excluded.source_refs,
			requester_name = excluded.requester_name,
			requester_user_id = excluded.requester_user_id,
			category = excluded.category,
			blocking = excluded.blocking,
			tags = excluded.tags,
			decision_summary = excluded.decision_summary,
			decision_options = excluded.decision_options,
			related_digest_ids = excluded.related_digest_ids,
			sub_items = excluded.sub_items`,
		item.ChannelID, item.AssigneeUserID, item.AssigneeRaw,
		item.Text, item.Context,
		item.SourceMessageTS, item.SourceChannelName,
		item.Status, item.Priority, dueDate,
		item.PeriodFrom, item.PeriodTo,
		item.Model, item.InputTokens, item.OutputTokens, item.CostUSD,
		item.Participants, item.SourceRefs,
		item.RequesterName, item.RequesterUserID, item.Category, item.Blocking, item.Tags,
		item.DecisionSummary, item.DecisionOptions, item.RelatedDigestIDs, item.SubItems)
	if err != nil {
		return 0, fmt.Errorf("upserting action item: %w", err)
	}

	// Resolve the item ID and log history within the transaction
	var itemID int64
	if isNew {
		itemID, _ = res.LastInsertId()
		logActionItemEventTx(tx, itemID, "created", "", "", "")
	} else {
		itemID = existing.id
		// Log field changes
		if existing.priority != item.Priority {
			logActionItemEventTx(tx, itemID, "priority_changed", "priority", existing.priority, item.Priority)
		}
		if existing.context != item.Context && item.Context != "" {
			logActionItemEventTx(tx, itemID, "context_updated", "context", truncate(existing.context, 100), truncate(item.Context, 100))
		}
		oldDue := ""
		if existing.dueDate.Valid {
			oldDue = fmt.Sprintf("%.0f", existing.dueDate.Float64)
		}
		newDue := ""
		if item.DueDate > 0 {
			newDue = fmt.Sprintf("%.0f", item.DueDate)
		}
		if oldDue != newDue {
			logActionItemEventTx(tx, itemID, "due_date_changed", "due_date", oldDue, newDue)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing action item upsert: %w", err)
	}
	return itemID, nil
}

// ActionItemFilter specifies criteria for querying action items.
type ActionItemFilter struct {
	AssigneeUserID string  // filter by assignee (empty = any)
	Status         string  // filter by status (empty = any)
	ChannelID      string  // filter by channel (empty = any)
	Priority       string  // filter by priority (empty = any)
	FromUnix       float64 // period_from >= this (0 = no filter)
	ToUnix         float64 // period_to <= this (0 = no filter)
	Limit          int     // max results (0 = no limit)
	HasUpdates     *bool   // filter by has_updates (nil = no filter)
}

// GetActionItems returns action items matching the filter, newest first.
func (db *DB) GetActionItems(f ActionItemFilter) ([]ActionItem, error) {
	query := `SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items
		FROM action_items`
	var conditions []string
	var args []any

	if f.AssigneeUserID != "" {
		conditions = append(conditions, "assignee_user_id = ?")
		args = append(args, f.AssigneeUserID)
	}
	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, f.Status)
	}
	if f.ChannelID != "" {
		conditions = append(conditions, "channel_id = ?")
		args = append(args, f.ChannelID)
	}
	if f.Priority != "" {
		conditions = append(conditions, "priority = ?")
		args = append(args, f.Priority)
	}
	if f.FromUnix > 0 {
		conditions = append(conditions, "period_from >= ?")
		args = append(args, f.FromUnix)
	}
	if f.ToUnix > 0 {
		conditions = append(conditions, "period_to <= ?")
		args = append(args, f.ToUnix)
	}
	if f.HasUpdates != nil {
		if *f.HasUpdates {
			conditions = append(conditions, "has_updates = 1")
		} else {
			conditions = append(conditions, "has_updates = 0")
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY created_at DESC`

	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying action items: %w", err)
	}
	defer rows.Close()

	return scanActionItems(rows)
}

// UpdateActionItemStatus changes the status of an action item and logs the change.
// If the new status is "done" or "dismissed", completed_at is set to now.
func (db *DB) UpdateActionItemStatus(id int, status string) error {
	switch status {
	case "inbox", "active", "done", "dismissed", "snoozed":
	default:
		return fmt.Errorf("invalid action item status: %q", status)
	}

	// Get old status for history
	var oldStatus string
	if err := db.QueryRow(`SELECT status FROM action_items WHERE id = ?`, id).Scan(&oldStatus); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("action item %d not found", id)
		}
		return fmt.Errorf("getting action item status: %w", err)
	}

	var err error
	if status == "done" || status == "dismissed" {
		_, err = db.Exec(`UPDATE action_items SET status = ?, completed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, status, id)
	} else {
		_, err = db.Exec(`UPDATE action_items SET status = ?, completed_at = NULL WHERE id = ?`, status, id)
	}
	if err != nil {
		return fmt.Errorf("updating action item status: %w", err)
	}

	if oldStatus != status {
		event := "status_changed"
		if oldStatus != "inbox" && oldStatus != "active" && (status == "inbox" || status == "active") {
			event = "reopened"
		}
		db.logActionItemEvent(int64(id), event, "status", oldStatus, status)
	}

	return nil
}

// GetActionItemAssignee returns the assignee_user_id for an action item.
func (db *DB) GetActionItemAssignee(id int) (string, error) {
	var assignee string
	err := db.QueryRow(`SELECT assignee_user_id FROM action_items WHERE id = ?`, id).Scan(&assignee)
	if err != nil {
		return "", fmt.Errorf("getting action item assignee: %w", err)
	}
	return assignee, nil
}

// CountOpenActionItems returns the number of active action items (inbox + active) for a user.
func (db *DB) CountOpenActionItems(assigneeUserID string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM action_items WHERE assignee_user_id = ? AND status IN ('inbox', 'active')`, assigneeUserID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting open action items: %w", err)
	}
	return count, nil
}

// DeleteActionItemsForWindow removes inbox action items whose period falls within
// or overlaps the given window. Uses range overlap instead of exact match so that
// day-aligned windows correctly clean up items from prior runs.
// Active/done/dismissed items are preserved since the user has interacted with them.
func (db *DB) DeleteActionItemsForWindow(assigneeUserID string, periodFrom, periodTo float64) (int64, error) {
	res, err := db.Exec(`DELETE FROM action_items WHERE assignee_user_id = ? AND period_from >= ? AND period_to <= ? AND status = 'inbox'`,
		assigneeUserID, periodFrom, periodTo)
	if err != nil {
		return 0, fmt.Errorf("deleting action items for window: %w", err)
	}
	return res.RowsAffected()
}

// ActionItemHistoryEntry represents a single change log entry.
type ActionItemHistoryEntry struct {
	ID           int
	ActionItemID int
	Event        string
	Field        string
	OldValue     string
	NewValue     string
	CreatedAt    string
}

// GetActionItemHistory returns the change history for an action item.
func (db *DB) GetActionItemHistory(actionItemID int) ([]ActionItemHistoryEntry, error) {
	rows, err := db.Query(`SELECT id, action_item_id, event, field, old_value, new_value, created_at
		FROM action_item_history WHERE action_item_id = ? ORDER BY created_at ASC`, actionItemID)
	if err != nil {
		return nil, fmt.Errorf("querying action item history: %w", err)
	}
	defer rows.Close()

	var entries []ActionItemHistoryEntry
	for rows.Next() {
		var e ActionItemHistoryEntry
		if err := rows.Scan(&e.ID, &e.ActionItemID, &e.Event, &e.Field, &e.OldValue, &e.NewValue, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AcceptActionItem moves an action item from 'inbox' to 'active'.
// Returns an error if the item is not found or not in inbox status.
func (db *DB) AcceptActionItem(id int) error {
	var status string
	if err := db.QueryRow(`SELECT status FROM action_items WHERE id = ?`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("action item %d not found", id)
		}
		return fmt.Errorf("getting action item status: %w", err)
	}
	if status != "inbox" {
		return fmt.Errorf("action item %d is not in inbox status (current: %s)", id, status)
	}

	if _, err := db.Exec(`UPDATE action_items SET status = 'active', completed_at = NULL WHERE id = ?`, id); err != nil {
		return fmt.Errorf("accepting action item: %w", err)
	}
	db.logActionItemEvent(int64(id), "accepted", "status", "inbox", "active")
	return nil
}

// SnoozeActionItem snoozes an action item until a given Unix timestamp.
// Works from both 'inbox' and 'active' statuses.
func (db *DB) SnoozeActionItem(id int, until float64) error {
	var status string
	if err := db.QueryRow(`SELECT status FROM action_items WHERE id = ?`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("action item %d not found", id)
		}
		return fmt.Errorf("getting action item status: %w", err)
	}
	if status != "inbox" && status != "active" {
		return fmt.Errorf("action item %d cannot be snoozed from status %q", id, status)
	}

	if _, err := db.Exec(`UPDATE action_items SET status = 'snoozed', snooze_until = ?, pre_snooze_status = ? WHERE id = ?`,
		until, status, id); err != nil {
		return fmt.Errorf("snoozing action item: %w", err)
	}
	snoozeLabel := formatSnoozeUntil(until)
	if snoozeLabel != "" {
		snoozeLabel = "until " + snoozeLabel
	}
	db.logActionItemEvent(int64(id), "snoozed", "status", status, snoozeLabel)
	return nil
}

// ReactivateSnoozedItems reactivates all snoozed items whose snooze_until has passed.
// Returns the number of reactivated items.
func (db *DB) ReactivateSnoozedItems() (int, error) {
	// First, find all items that need reactivation so we can log history for each.
	rows, err := db.Query(`SELECT id, pre_snooze_status FROM action_items
		WHERE status = 'snoozed' AND snooze_until IS NOT NULL AND snooze_until <= unixepoch('now')`)
	if err != nil {
		return 0, fmt.Errorf("querying snoozed items for reactivation: %w", err)
	}
	defer rows.Close()

	type snoozedItem struct {
		id              int
		preSnoozeStatus string
	}
	var items []snoozedItem
	for rows.Next() {
		var si snoozedItem
		if err := rows.Scan(&si.id, &si.preSnoozeStatus); err != nil {
			return 0, fmt.Errorf("scanning snoozed item: %w", err)
		}
		items = append(items, si)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterating snoozed items: %w", err)
	}

	if len(items) == 0 {
		return 0, nil
	}

	// Batch update: restore pre_snooze_status (default to 'inbox' if empty).
	if _, err := db.Exec(`UPDATE action_items
		SET status = CASE WHEN pre_snooze_status = '' THEN 'inbox' ELSE pre_snooze_status END,
		    snooze_until = NULL,
		    pre_snooze_status = ''
		WHERE status = 'snoozed' AND snooze_until IS NOT NULL AND snooze_until <= unixepoch('now')`); err != nil {
		return 0, fmt.Errorf("reactivating snoozed items: %w", err)
	}

	// Log history for each item.
	for _, si := range items {
		target := si.preSnoozeStatus
		if target == "" {
			target = "inbox"
		}
		db.logActionItemEvent(int64(si.id), "reactivated", "status", "snoozed", target)
	}

	return len(items), nil
}

// SetActionItemHasUpdates sets the has_updates flag on an action item.
// If setting to true, logs an 'update_detected' event.
func (db *DB) SetActionItemHasUpdates(id int, hasUpdates bool) error {
	val := 0
	if hasUpdates {
		val = 1
	}
	res, err := db.Exec(`UPDATE action_items SET has_updates = ? WHERE id = ?`, val, id)
	if err != nil {
		return fmt.Errorf("setting has_updates: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("action item %d not found", id)
	}
	if hasUpdates {
		db.logActionItemEvent(int64(id), "update_detected", "", "", "")
	}
	return nil
}

// MarkActionItemUpdateRead clears the has_updates flag and logs an 'update_read' event.
func (db *DB) MarkActionItemUpdateRead(id int) error {
	res, err := db.Exec(`UPDATE action_items SET has_updates = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("marking update read: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("action item %d not found", id)
	}
	db.logActionItemEvent(int64(id), "update_read", "", "", "")
	return nil
}

// GetActionItemsForUpdateCheck returns active/inbox items that have a source_message_ts.
// Used by the update tracking pipeline to check for new thread activity.
func (db *DB) GetActionItemsForUpdateCheck() ([]ActionItem, error) {
	rows, err := db.Query(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items
		FROM action_items
		WHERE status IN ('inbox', 'active') AND source_message_ts != ''`)
	if err != nil {
		return nil, fmt.Errorf("querying action items for update check: %w", err)
	}
	defer rows.Close()
	return scanActionItems(rows)
}

// UpdateLastCheckedTS sets the last_checked_ts for an action item,
// recording the latest thread reply TS that has been examined.
func (db *DB) UpdateLastCheckedTS(id int, ts string) error {
	res, err := db.Exec(`UPDATE action_items SET last_checked_ts = ? WHERE id = ?`, ts, id)
	if err != nil {
		return fmt.Errorf("updating last_checked_ts: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("action item %d not found", id)
	}
	return nil
}

// UpdateActionItemContext updates the context field of an action item
// and logs the change in history.
func (db *DB) UpdateActionItemContext(id int, newContext string) error {
	var oldContext string
	if err := db.QueryRow(`SELECT context FROM action_items WHERE id = ?`, id).Scan(&oldContext); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("action item %d not found", id)
		}
		return fmt.Errorf("getting action item context: %w", err)
	}

	if _, err := db.Exec(`UPDATE action_items SET context = ? WHERE id = ?`, newContext, id); err != nil {
		return fmt.Errorf("updating action item context: %w", err)
	}

	if oldContext != newContext {
		db.logActionItemEvent(int64(id), "context_updated", "context", truncate(oldContext, 100), truncate(newContext, 100))
	}
	return nil
}

func logActionItemEventTx(tx *sql.Tx, actionItemID int64, event, field, oldValue, newValue string) {
	if actionItemID <= 0 {
		return
	}
	_, err := tx.Exec(`INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)`, actionItemID, event, field, oldValue, newValue)
	if err != nil {
		// Log but don't fail the caller — history is best-effort.
		log.Printf("warning: logging action item event: %v", err)
	}
}

func (db *DB) logActionItemEvent(actionItemID int64, event, field, oldValue, newValue string) {
	if actionItemID <= 0 {
		return
	}
	_, err := db.Exec(`INSERT INTO action_item_history (action_item_id, event, field, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)`, actionItemID, event, field, oldValue, newValue)
	if err != nil {
		// Log but don't fail the caller — history is best-effort.
		log.Printf("warning: logging action item event: %v", err)
	}
}

// FindRelatedDigestIDs returns digest IDs that overlap with the given channel and time window.
func (db *DB) FindRelatedDigestIDs(channelID string, periodFrom, periodTo float64) ([]int, error) {
	rows, err := db.Query(`SELECT id FROM digests
		WHERE (channel_id = ? OR channel_id = '')
		  AND period_from <= ? AND period_to >= ?
		ORDER BY period_to DESC LIMIT 10`,
		channelID, periodTo, periodFrom)
	if err != nil {
		return nil, fmt.Errorf("finding related digests: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning digest id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpdateActionItemFromExtraction updates an existing action item with new data from
// a re-extraction pass. Only updates fields that changed, logging history for each change.
// Preserves user-set status. Returns true if any field was actually changed.
func (db *DB) UpdateActionItemFromExtraction(id int, update ActionItem) (bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("beginning update tx: %w", err)
	}
	defer tx.Rollback()

	// Load current state.
	var existing struct {
		context         string
		priority        string
		dueDate         sql.NullFloat64
		decisionSummary string
		decisionOptions string
		relatedDigests  string
		subItems        string
		tags            string
		blocking        string
		category        string
	}
	err = tx.QueryRow(`SELECT context, priority, due_date, decision_summary, decision_options,
		related_digest_ids, sub_items, tags, blocking, category FROM action_items WHERE id = ?`, id).
		Scan(&existing.context, &existing.priority, &existing.dueDate, &existing.decisionSummary,
			&existing.decisionOptions, &existing.relatedDigests, &existing.subItems,
			&existing.tags, &existing.blocking, &existing.category)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("action item %d not found", id)
		}
		return false, fmt.Errorf("loading existing action item: %w", err)
	}

	changed := false

	// Update context if changed.
	if update.Context != "" && update.Context != existing.context {
		logActionItemEventTx(tx, int64(id), "re_extracted", "context",
			"", truncate(update.Context, 200))
		changed = true
	}

	// Update priority if changed.
	if update.Priority != "" && update.Priority != existing.priority {
		logActionItemEventTx(tx, int64(id), "priority_changed", "priority", existing.priority, update.Priority)
		changed = true
	}

	// Update due_date if changed.
	oldDue := ""
	if existing.dueDate.Valid {
		oldDue = fmt.Sprintf("%.0f", existing.dueDate.Float64)
	}
	newDue := ""
	if update.DueDate > 0 {
		newDue = fmt.Sprintf("%.0f", update.DueDate)
	}
	if oldDue != newDue && newDue != "" {
		logActionItemEventTx(tx, int64(id), "due_date_changed", "due_date", oldDue, newDue)
		changed = true
	}

	// Update decision_summary if changed.
	if update.DecisionSummary != "" && update.DecisionSummary != existing.decisionSummary {
		logActionItemEventTx(tx, int64(id), "decision_evolved", "decision_summary",
			"", truncate(update.DecisionSummary, 200))
		changed = true
	}

	// Update related_digest_ids if changed.
	if update.RelatedDigestIDs != "" && update.RelatedDigestIDs != existing.relatedDigests {
		summary := summarizeDigestLinked(existing.relatedDigests, update.RelatedDigestIDs)
		logActionItemEventTx(tx, int64(id), "digest_linked", "related_digest_ids", "", summary)
		changed = true
	}

	// Update sub_items if changed.
	if update.SubItems != "" && update.SubItems != existing.subItems {
		summary := summarizeSubItemsChange(existing.subItems, update.SubItems)
		logActionItemEventTx(tx, int64(id), "sub_items_updated", "sub_items", "", summary)
		changed = true
	}

	if !changed {
		return false, nil
	}

	// Apply all updates.
	var dueDate any
	if update.DueDate > 0 {
		dueDate = update.DueDate
	}

	_, err = tx.Exec(`UPDATE action_items SET
		context = CASE WHEN ? != '' THEN ? ELSE context END,
		priority = CASE WHEN ? != '' THEN ? ELSE priority END,
		due_date = CASE WHEN ? IS NOT NULL THEN ? ELSE due_date END,
		decision_summary = CASE WHEN ? != '' THEN ? ELSE decision_summary END,
		decision_options = CASE WHEN ? != '' THEN ? ELSE decision_options END,
		related_digest_ids = CASE WHEN ? != '' THEN ? ELSE related_digest_ids END,
		sub_items = CASE WHEN ? != '' THEN ? ELSE sub_items END,
		tags = CASE WHEN ? != '' THEN ? ELSE tags END,
		blocking = CASE WHEN ? != '' THEN ? ELSE blocking END,
		category = CASE WHEN ? != '' THEN ? ELSE category END,
		participants = CASE WHEN ? != '' THEN ? ELSE participants END,
		source_refs = CASE WHEN ? != '' THEN ? ELSE source_refs END
		WHERE id = ?`,
		update.Context, update.Context,
		update.Priority, update.Priority,
		dueDate, dueDate,
		update.DecisionSummary, update.DecisionSummary,
		update.DecisionOptions, update.DecisionOptions,
		update.RelatedDigestIDs, update.RelatedDigestIDs,
		update.SubItems, update.SubItems,
		update.Tags, update.Tags,
		update.Blocking, update.Blocking,
		update.Category, update.Category,
		update.Participants, update.Participants,
		update.SourceRefs, update.SourceRefs,
		id)
	if err != nil {
		return false, fmt.Errorf("updating action item %d: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("committing action item update: %w", err)
	}
	return true, nil
}

// GetExistingActionItemsForChannel returns active/inbox items for a specific channel and user.
// Used by the extraction pipeline to pass existing items into the AI prompt for deduplication.
func (db *DB) GetExistingActionItemsForChannel(channelID, assigneeUserID string) ([]ActionItem, error) {
	rows, err := db.Query(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items
		FROM action_items
		WHERE channel_id = ? AND assignee_user_id = ? AND status IN ('inbox', 'active')
		ORDER BY created_at DESC`, channelID, assigneeUserID)
	if err != nil {
		return nil, fmt.Errorf("querying existing action items for channel: %w", err)
	}
	defer rows.Close()
	return scanActionItems(rows)
}

// GetExistingActionItemsExcludingChannel returns active/inbox items for a user
// from all channels EXCEPT the specified one. Used for cross-channel completion detection.
func (db *DB) GetExistingActionItemsExcludingChannel(excludeChannelID, assigneeUserID string) ([]ActionItem, error) {
	rows, err := db.Query(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items
		FROM action_items
		WHERE channel_id != ? AND assignee_user_id = ? AND status IN ('inbox', 'active')
		ORDER BY created_at DESC
		LIMIT 20`, excludeChannelID, assigneeUserID)
	if err != nil {
		return nil, fmt.Errorf("querying cross-channel action items: %w", err)
	}
	defer rows.Close()
	return scanActionItems(rows)
}

// UpdateActionItemSubItems updates the sub_items JSON for an action item.
func (db *DB) UpdateActionItemSubItems(id int, subItems string) error {
	res, err := db.Exec(`UPDATE action_items SET sub_items = ? WHERE id = ?`, subItems, id)
	if err != nil {
		return fmt.Errorf("updating sub_items: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("action item %d not found", id)
	}
	return nil
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// summarizeSubItemsChange computes a human-readable diff between old and new sub-items JSON.
func summarizeSubItemsChange(oldJSON, newJSON string) string {
	type subItem struct {
		Text   string `json:"text"`
		IsDone bool   `json:"isDone"`
	}
	var oldItems, newItems []subItem
	_ = json.Unmarshal([]byte(oldJSON), &oldItems)
	_ = json.Unmarshal([]byte(newJSON), &newItems)

	oldMap := make(map[string]bool, len(oldItems))
	for _, it := range oldItems {
		oldMap[it.Text] = it.IsDone
	}
	newMap := make(map[string]bool, len(newItems))
	for _, it := range newItems {
		newMap[it.Text] = it.IsDone
	}

	var parts []string

	// Items completed
	var completed []string
	for _, it := range newItems {
		if it.IsDone {
			if wasDone, exists := oldMap[it.Text]; exists && !wasDone {
				completed = append(completed, it.Text)
			}
		}
	}
	if len(completed) > 0 {
		parts = append(parts, fmt.Sprintf("completed: %s", truncate(strings.Join(completed, ", "), 80)))
	}

	// Items added
	var added []string
	for _, it := range newItems {
		if _, exists := oldMap[it.Text]; !exists {
			added = append(added, it.Text)
		}
	}
	if len(added) > 0 {
		parts = append(parts, fmt.Sprintf("added: %s", truncate(strings.Join(added, ", "), 80)))
	}

	// Items removed
	var removed []string
	for _, it := range oldItems {
		if _, exists := newMap[it.Text]; !exists {
			removed = append(removed, it.Text)
		}
	}
	if len(removed) > 0 {
		parts = append(parts, fmt.Sprintf("removed: %s", truncate(strings.Join(removed, ", "), 80)))
	}

	// Items uncompleted
	var uncompleted []string
	for _, it := range newItems {
		if !it.IsDone {
			if wasDone, exists := oldMap[it.Text]; exists && wasDone {
				uncompleted = append(uncompleted, it.Text)
			}
		}
	}
	if len(uncompleted) > 0 {
		parts = append(parts, fmt.Sprintf("reopened: %s", truncate(strings.Join(uncompleted, ", "), 80)))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%d items → %d items", len(oldItems), len(newItems))
	}
	return strings.Join(parts, "; ")
}

// summarizeDigestLinked computes which digest IDs were added.
func summarizeDigestLinked(oldJSON, newJSON string) string {
	var oldIDs, newIDs []int
	_ = json.Unmarshal([]byte(oldJSON), &oldIDs)
	_ = json.Unmarshal([]byte(newJSON), &newIDs)

	oldSet := make(map[int]bool, len(oldIDs))
	for _, id := range oldIDs {
		oldSet[id] = true
	}

	var added []string
	for _, id := range newIDs {
		if !oldSet[id] {
			added = append(added, fmt.Sprintf("#%d", id))
		}
	}

	if len(added) > 0 {
		return "linked " + strings.Join(added, ", ")
	}
	return fmt.Sprintf("%d digests", len(newIDs))
}

// formatSnoozeUntil converts a Unix timestamp to a human-readable date string.
func formatSnoozeUntil(until float64) string {
	if until <= 0 {
		return ""
	}
	t := time.Unix(int64(until), 0)
	now := time.Now()
	if t.Year() == now.Year() {
		return t.Format("Jan 2, 15:04")
	}
	return t.Format("Jan 2 2006, 15:04")
}

func scanActionItems(rows *sql.Rows) ([]ActionItem, error) {
	var items []ActionItem
	for rows.Next() {
		var item ActionItem
		var dueDate sql.NullFloat64
		var snoozeUntil sql.NullFloat64
		err := rows.Scan(
			&item.ID, &item.ChannelID, &item.AssigneeUserID, &item.AssigneeRaw,
			&item.Text, &item.Context,
			&item.SourceMessageTS, &item.SourceChannelName,
			&item.Status, &item.Priority, &dueDate,
			&item.PeriodFrom, &item.PeriodTo,
			&item.Model, &item.InputTokens, &item.OutputTokens, &item.CostUSD,
			&item.CreatedAt, &item.CompletedAt,
			&item.HasUpdates, &item.LastCheckedTS, &snoozeUntil, &item.PreSnoozeStatus,
			&item.Participants, &item.SourceRefs,
			&item.RequesterName, &item.RequesterUserID, &item.Category, &item.Blocking, &item.Tags,
			&item.DecisionSummary, &item.DecisionOptions, &item.RelatedDigestIDs, &item.SubItems,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning action item row: %w", err)
		}
		if dueDate.Valid {
			item.DueDate = dueDate.Float64
		}
		if snoozeUntil.Valid {
			item.SnoozeUntil = snoozeUntil.Float64
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
