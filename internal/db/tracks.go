package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

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

// UpsertTrack inserts or updates a track with change history logging.
// Deduplicates on (channel_id, assignee_user_id, source_message_ts, text).
// On conflict: updates context, priority, due_date but preserves user-set status.
func (db *DB) UpsertTrack(item Track) (int64, error) {
	// Default ownership to "mine" if empty.
	if item.Ownership == "" {
		item.Ownership = "mine"
	}

	var dueDate any
	if item.DueDate > 0 {
		dueDate = item.DueDate
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning track upsert tx: %w", err)
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
	err = tx.QueryRow(`SELECT id, context, priority, due_date, status FROM tracks
		WHERE channel_id = ? AND assignee_user_id = ? AND source_message_ts = ? AND text = ?`,
		item.ChannelID, item.AssigneeUserID, item.SourceMessageTS, item.Text,
	).Scan(&existing.id, &existing.context, &existing.priority, &existing.dueDate, &existing.status)

	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("checking existing track: %w", err)
	}
	isNew := err == sql.ErrNoRows

	// For existing active/done/dismissed items, only update metadata (not context/priority/due_date)
	// to avoid overwriting user-curated data during re-extraction.
	if !isNew && existing.status != "inbox" {
		logTrackEventTx(tx, existing.id, "re_extracted", "", "", "metadata only (status="+existing.status+")")
		fp := item.Fingerprint
		if fp == "" {
			fp = "[]"
		}
		_, err = tx.Exec(`UPDATE tracks SET
			related_digest_ids = ?, sub_items = ?, tags = ?, participants = ?, source_refs = ?, fingerprint = ?
			WHERE id = ?`,
			item.RelatedDigestIDs, item.SubItems, item.Tags, item.Participants, item.SourceRefs, fp,
			existing.id)
		if err != nil {
			return 0, fmt.Errorf("updating track metadata: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("committing track metadata update: %w", err)
		}
		return existing.id, nil
	}

	// Perform upsert
	fingerprint := item.Fingerprint
	if fingerprint == "" {
		fingerprint = "[]"
	}

	res, err := tx.Exec(`INSERT INTO tracks
		(channel_id, assignee_user_id, assignee_raw, text, context,
		 source_message_ts, source_channel_name, status, priority, due_date,
		 period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		 participants, source_refs,
		 requester_name, requester_user_id, category, blocking, tags,
		 decision_summary, decision_options, related_digest_ids, sub_items, prompt_version,
		 ownership, ball_on, owner_user_id, fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			sub_items = excluded.sub_items,
			prompt_version = excluded.prompt_version,
			ownership = excluded.ownership,
			ball_on = excluded.ball_on,
			owner_user_id = excluded.owner_user_id,
			fingerprint = excluded.fingerprint`,
		item.ChannelID, item.AssigneeUserID, item.AssigneeRaw,
		item.Text, item.Context,
		item.SourceMessageTS, item.SourceChannelName,
		item.Status, item.Priority, dueDate,
		item.PeriodFrom, item.PeriodTo,
		item.Model, item.InputTokens, item.OutputTokens, item.CostUSD,
		item.Participants, item.SourceRefs,
		item.RequesterName, item.RequesterUserID, item.Category, item.Blocking, item.Tags,
		item.DecisionSummary, item.DecisionOptions, item.RelatedDigestIDs, item.SubItems,
		item.PromptVersion,
		item.Ownership, item.BallOn, item.OwnerUserID, fingerprint)
	if err != nil {
		return 0, fmt.Errorf("upserting track: %w", err)
	}

	// Resolve the item ID and log history within the transaction
	var itemID int64
	if isNew {
		itemID, _ = res.LastInsertId()
		logTrackEventTx(tx, itemID, "created", "", "", "")
	} else {
		itemID = existing.id
		// Log field changes
		if existing.priority != item.Priority {
			logTrackEventTx(tx, itemID, "priority_changed", "priority", existing.priority, item.Priority)
		}
		if existing.context != item.Context && item.Context != "" {
			logTrackEventTx(tx, itemID, "context_updated", "context", truncate(existing.context, 100), truncate(item.Context, 100))
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
			logTrackEventTx(tx, itemID, "due_date_changed", "due_date", oldDue, newDue)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing track upsert: %w", err)
	}
	return itemID, nil
}

// TrackFilter specifies criteria for querying tracks.
type TrackFilter struct {
	AssigneeUserID string  // filter by assignee (empty = any)
	Status         string  // filter by status (empty = any)
	ChannelID      string  // filter by channel (empty = any)
	Priority       string  // filter by priority (empty = any)
	Ownership      string  // filter by ownership: "mine", "delegated", "watching" (empty = any)
	FromUnix       float64 // period_from >= this (0 = no filter)
	ToUnix         float64 // period_to <= this (0 = no filter)
	Limit          int     // max results (0 = no limit)
	HasUpdates     *bool   // filter by has_updates (nil = no filter)
}

// GetTracks returns tracks matching the filter, newest first.
func (db *DB) GetTracks(f TrackFilter) ([]Track, error) {
	query := `SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items, prompt_version,
		ownership, ball_on, owner_user_id
		FROM tracks`
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
	if f.Ownership != "" {
		conditions = append(conditions, "ownership = ?")
		args = append(args, f.Ownership)
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
	query += ` ORDER BY CASE WHEN source_message_ts != '' THEN source_message_ts ELSE created_at END DESC`

	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tracks: %w", err)
	}
	defer rows.Close()

	return scanTracks(rows)
}

// UpdateTrackStatus changes the status of a track and logs the change.
// If the new status is "done" or "dismissed", completed_at is set to now.
// Uses a conditional UPDATE to avoid race conditions between SELECT and UPDATE.
func (db *DB) UpdateTrackStatus(id int, status string) error {
	switch status {
	case "inbox", "active", "done", "dismissed", "snoozed":
	default:
		return fmt.Errorf("invalid track status: %q", status)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning status update tx: %w", err)
	}
	defer tx.Rollback()

	// Get old status for history (within transaction to avoid TOCTOU race)
	var oldStatus string
	if err := tx.QueryRow(`SELECT status FROM tracks WHERE id = ?`, id).Scan(&oldStatus); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track status: %w", err)
	}

	if status == "done" || status == "dismissed" {
		_, err = tx.Exec(`UPDATE tracks SET status = ?, completed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, status, id)
	} else {
		_, err = tx.Exec(`UPDATE tracks SET status = ?, completed_at = NULL WHERE id = ?`, status, id)
	}
	if err != nil {
		return fmt.Errorf("updating track status: %w", err)
	}

	if oldStatus != status {
		event := "status_changed"
		if oldStatus != "inbox" && oldStatus != "active" && (status == "inbox" || status == "active") {
			event = "reopened"
		}
		logTrackEventTx(tx, int64(id), event, "status", oldStatus, status)

		// Auto-record feedback: dismiss = negative, done = positive.
		if status == "dismissed" {
			_, _ = tx.Exec(`INSERT INTO feedback (entity_type, entity_id, rating, comment) VALUES ('track', ?, -1, 'auto: dismissed')`,
				fmt.Sprintf("%d", id))
		} else if status == "done" {
			_, _ = tx.Exec(`INSERT INTO feedback (entity_type, entity_id, rating, comment) VALUES ('track', ?, 1, 'auto: completed')`,
				fmt.Sprintf("%d", id))
		}
	}

	return tx.Commit()
}

// GetTrackAssignee returns the assignee_user_id for a track.
func (db *DB) GetTrackAssignee(id int) (string, error) {
	var assignee string
	err := db.QueryRow(`SELECT assignee_user_id FROM tracks WHERE id = ?`, id).Scan(&assignee)
	if err != nil {
		return "", fmt.Errorf("getting track assignee: %w", err)
	}
	return assignee, nil
}

// CountOpenTracks returns the number of active tracks (inbox + active) for a user.
func (db *DB) CountOpenTracks(assigneeUserID string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM tracks WHERE assignee_user_id = ? AND status IN ('inbox', 'active')`, assigneeUserID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting open tracks: %w", err)
	}
	return count, nil
}

// DeleteTracksForWindow removes inbox tracks whose period falls within
// or overlaps the given window. Uses range overlap instead of exact match so that
// day-aligned windows correctly clean up items from prior runs.
// Active/done/dismissed items are preserved since the user has interacted with them.
func (db *DB) DeleteTracksForWindow(assigneeUserID string, periodFrom, periodTo float64) (int64, error) {
	res, err := db.Exec(`DELETE FROM tracks WHERE assignee_user_id = ? AND period_from >= ? AND period_to <= ? AND status = 'inbox'`,
		assigneeUserID, periodFrom, periodTo)
	if err != nil {
		return 0, fmt.Errorf("deleting tracks for window: %w", err)
	}
	return res.RowsAffected()
}

// TrackHistoryEntry represents a single change log entry.
type TrackHistoryEntry struct {
	ID        int
	TrackID   int
	Event     string
	Field     string
	OldValue  string
	NewValue  string
	CreatedAt string
}

// GetTrackHistory returns the change history for a track.
func (db *DB) GetTrackHistory(trackID int) ([]TrackHistoryEntry, error) {
	rows, err := db.Query(`SELECT id, track_id, event, field, old_value, new_value, created_at
		FROM track_history WHERE track_id = ? ORDER BY created_at ASC`, trackID)
	if err != nil {
		return nil, fmt.Errorf("querying track history: %w", err)
	}
	defer rows.Close()

	var entries []TrackHistoryEntry
	for rows.Next() {
		var e TrackHistoryEntry
		if err := rows.Scan(&e.ID, &e.TrackID, &e.Event, &e.Field, &e.OldValue, &e.NewValue, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning history row: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AcceptTrack moves a track from 'inbox' to 'active'.
// Returns an error if the item is not found or not in inbox status.
// Uses a transaction to avoid race conditions between status check and update.
func (db *DB) AcceptTrack(id int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning accept tx: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRow(`SELECT status FROM tracks WHERE id = ?`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track status: %w", err)
	}
	if status != "inbox" {
		return fmt.Errorf("track %d is not in inbox status (current: %s)", id, status)
	}

	if _, err := tx.Exec(`UPDATE tracks SET status = 'active', completed_at = NULL WHERE id = ?`, id); err != nil {
		return fmt.Errorf("accepting track: %w", err)
	}
	logTrackEventTx(tx, int64(id), "accepted", "status", "inbox", "active")
	return tx.Commit()
}

// SnoozeTrack snoozes a track until a given Unix timestamp.
// Works from both 'inbox' and 'active' statuses.
// Uses a transaction to avoid race conditions between status check and update.
func (db *DB) SnoozeTrack(id int, until float64) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning snooze tx: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRow(`SELECT status FROM tracks WHERE id = ?`, id).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track status: %w", err)
	}
	if status != "inbox" && status != "active" {
		return fmt.Errorf("track %d cannot be snoozed from status %q", id, status)
	}

	if _, err := tx.Exec(`UPDATE tracks SET status = 'snoozed', snooze_until = ?, pre_snooze_status = ? WHERE id = ?`,
		until, status, id); err != nil {
		return fmt.Errorf("snoozing track: %w", err)
	}
	snoozeLabel := formatSnoozeUntil(until)
	if snoozeLabel != "" {
		snoozeLabel = "until " + snoozeLabel
	}
	logTrackEventTx(tx, int64(id), "snoozed", "status", status, snoozeLabel)
	return tx.Commit()
}

// ReactivateSnoozedTracks reactivates all snoozed tracks whose snooze_until has passed.
// Returns the number of reactivated tracks. Uses a transaction for atomicity.
func (db *DB) ReactivateSnoozedTracks() (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning reactivation tx: %w", err)
	}
	defer tx.Rollback()

	// Find all items that need reactivation within the transaction.
	rows, err := tx.Query(`SELECT id, pre_snooze_status FROM tracks
		WHERE status = 'snoozed' AND snooze_until IS NOT NULL AND snooze_until <= unixepoch('now')`)
	if err != nil {
		return 0, fmt.Errorf("querying snoozed tracks for reactivation: %w", err)
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
			return 0, fmt.Errorf("scanning snoozed track: %w", err)
		}
		items = append(items, si)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterating snoozed tracks: %w", err)
	}

	if len(items) == 0 {
		return 0, nil
	}

	// Batch update: restore pre_snooze_status (default to 'inbox' if empty).
	if _, err := tx.Exec(`UPDATE tracks
		SET status = CASE WHEN pre_snooze_status = '' THEN 'inbox' ELSE pre_snooze_status END,
		    snooze_until = NULL,
		    pre_snooze_status = ''
		WHERE status = 'snoozed' AND snooze_until IS NOT NULL AND snooze_until <= unixepoch('now')`); err != nil {
		return 0, fmt.Errorf("reactivating snoozed tracks: %w", err)
	}

	// Log history for each item within the transaction.
	for _, si := range items {
		target := si.preSnoozeStatus
		if target == "" {
			target = "inbox"
		}
		logTrackEventTx(tx, int64(si.id), "reactivated", "status", "snoozed", target)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing reactivation: %w", err)
	}
	return len(items), nil
}

// SetTrackHasUpdates sets the has_updates flag on a track.
// If setting to true, logs an 'update_detected' event.
// Uses a transaction to ensure atomicity of the flag update and history log.
func (db *DB) SetTrackHasUpdates(id int, hasUpdates bool) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning set has_updates tx: %w", err)
	}
	defer tx.Rollback()

	val := 0
	if hasUpdates {
		val = 1
	}
	res, err := tx.Exec(`UPDATE tracks SET has_updates = ? WHERE id = ?`, val, id)
	if err != nil {
		return fmt.Errorf("setting has_updates: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("track %d not found", id)
	}
	if hasUpdates {
		logTrackEventTx(tx, int64(id), "update_detected", "", "", "")
	}
	return tx.Commit()
}

// MarkTrackUpdateRead clears the has_updates flag and logs an 'update_read' event.
// Uses a transaction to ensure atomicity of the flag update and history log.
func (db *DB) MarkTrackUpdateRead(id int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning mark update read tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE tracks SET has_updates = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("marking update read: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("track %d not found", id)
	}
	logTrackEventTx(tx, int64(id), "update_read", "", "", "")
	return tx.Commit()
}

// HasTracksForUser returns true if at least one track exists for the given user.
func (db *DB) HasTracksForUser(userID string) (bool, error) {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM tracks WHERE assignee_user_id = ? LIMIT 1`, userID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetTracksForUpdateCheck returns active/inbox tracks that have a source_message_ts.
// Used by the update tracking pipeline to check for new thread activity.
func (db *DB) GetTracksForUpdateCheck() ([]Track, error) {
	rows, err := db.Query(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items, prompt_version,
		ownership, ball_on, owner_user_id
		FROM tracks
		WHERE status IN ('inbox', 'active') AND source_message_ts != ''`)
	if err != nil {
		return nil, fmt.Errorf("querying tracks for update check: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// UpdateLastCheckedTS sets the last_checked_ts for a track,
// recording the latest thread reply TS that has been examined.
func (db *DB) UpdateLastCheckedTS(id int, ts string) error {
	res, err := db.Exec(`UPDATE tracks SET last_checked_ts = ? WHERE id = ?`, ts, id)
	if err != nil {
		return fmt.Errorf("updating last_checked_ts: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("track %d not found", id)
	}
	return nil
}

// UpdateTrackContext updates the context field of a track
// and logs the change in history. Uses a transaction to avoid TOCTOU races.
func (db *DB) UpdateTrackContext(id int, newContext string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning context update tx: %w", err)
	}
	defer tx.Rollback()

	var oldContext string
	if err := tx.QueryRow(`SELECT context FROM tracks WHERE id = ?`, id).Scan(&oldContext); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track context: %w", err)
	}

	if _, err := tx.Exec(`UPDATE tracks SET context = ? WHERE id = ?`, newContext, id); err != nil {
		return fmt.Errorf("updating track context: %w", err)
	}

	if oldContext != newContext {
		logTrackEventTx(tx, int64(id), "context_updated", "context", truncate(oldContext, 100), truncate(newContext, 100))
	}
	return tx.Commit()
}

func logTrackEventTx(tx *sql.Tx, trackID int64, event, field, oldValue, newValue string) {
	if trackID <= 0 {
		return
	}
	_, err := tx.Exec(`INSERT INTO track_history (track_id, event, field, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)`, trackID, event, field, oldValue, newValue)
	if err != nil {
		// Log but don't fail the caller — history is best-effort.
		log.Printf("warning: logging track event: %v", err)
	}
}

// FindRelatedDigestIDs returns digest IDs that overlap with the given channel and time window.
func (db *DB) FindRelatedDigestIDs(channelID string, periodFrom, periodTo float64) ([]int, error) {
	const relatedDigestLimit = 10
	rows, err := db.Query(`SELECT id FROM digests
		WHERE (channel_id = ? OR channel_id = '')
		  AND period_from <= ? AND period_to >= ?
		ORDER BY period_to DESC LIMIT ?`,
		channelID, periodTo, periodFrom, relatedDigestLimit)
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

// UpdateTrackFromExtraction updates an existing track with new data from
// a re-extraction pass. Only updates fields that changed, logging history for each change.
// Preserves user-set status. Returns true if any field was actually changed.
func (db *DB) UpdateTrackFromExtraction(id int, update Track) (bool, error) {
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
		ownership       string
		ballOn          string
		ownerUserID     string
	}
	err = tx.QueryRow(`SELECT context, priority, due_date, decision_summary, decision_options,
		related_digest_ids, sub_items, tags, blocking, category,
		ownership, ball_on, owner_user_id FROM tracks WHERE id = ?`, id).
		Scan(&existing.context, &existing.priority, &existing.dueDate, &existing.decisionSummary,
			&existing.decisionOptions, &existing.relatedDigests, &existing.subItems,
			&existing.tags, &existing.blocking, &existing.category,
			&existing.ownership, &existing.ballOn, &existing.ownerUserID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, fmt.Errorf("track %d not found", id)
		}
		return false, fmt.Errorf("loading existing track: %w", err)
	}

	changed := false

	// Update context if changed.
	if update.Context != "" && update.Context != existing.context {
		logTrackEventTx(tx, int64(id), "re_extracted", "context",
			truncate(existing.context, 200), truncate(update.Context, 200))
		changed = true
	}

	// Update priority if changed.
	if update.Priority != "" && update.Priority != existing.priority {
		logTrackEventTx(tx, int64(id), "priority_changed", "priority", existing.priority, update.Priority)
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
		logTrackEventTx(tx, int64(id), "due_date_changed", "due_date", oldDue, newDue)
		changed = true
	}

	// Update decision_summary if changed.
	if update.DecisionSummary != "" && update.DecisionSummary != existing.decisionSummary {
		logTrackEventTx(tx, int64(id), "decision_evolved", "decision_summary",
			"", truncate(update.DecisionSummary, 200))
		changed = true
	}

	// Update related_digest_ids if changed.
	if update.RelatedDigestIDs != "" && update.RelatedDigestIDs != existing.relatedDigests {
		summary := summarizeDigestLinked(existing.relatedDigests, update.RelatedDigestIDs)
		logTrackEventTx(tx, int64(id), "digest_linked", "related_digest_ids", "", summary)
		changed = true
	}

	// Update sub_items if changed.
	if update.SubItems != "" && update.SubItems != existing.subItems {
		summary := summarizeSubItemsChange(existing.subItems, update.SubItems)
		logTrackEventTx(tx, int64(id), "sub_items_updated", "sub_items", "", summary)
		changed = true
	}

	// Update ownership if changed.
	if update.Ownership != "" && update.Ownership != existing.ownership {
		logTrackEventTx(tx, int64(id), "ownership_changed", "ownership", existing.ownership, update.Ownership)
		changed = true
	}

	// Update ball_on if changed.
	if update.BallOn != "" && update.BallOn != existing.ballOn {
		logTrackEventTx(tx, int64(id), "ball_on_changed", "ball_on", existing.ballOn, update.BallOn)
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

	_, err = tx.Exec(`UPDATE tracks SET
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
		source_refs = CASE WHEN ? != '' THEN ? ELSE source_refs END,
		ownership = CASE WHEN ? != '' THEN ? ELSE ownership END,
		ball_on = CASE WHEN ? != '' THEN ? ELSE ball_on END,
		owner_user_id = CASE WHEN ? != '' THEN ? ELSE owner_user_id END
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
		update.Ownership, update.Ownership,
		update.BallOn, update.BallOn,
		update.OwnerUserID, update.OwnerUserID,
		id)
	if err != nil {
		return false, fmt.Errorf("updating track %d: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("committing track update: %w", err)
	}
	return true, nil
}

// HasResolvedTrackForMessage checks if a done/dismissed track already exists for this
// (channel_id, assignee_user_id, source_message_ts) combination.
// Used to prevent re-extraction of tracks the user has already resolved.
func (db *DB) HasResolvedTrackForMessage(channelID, assigneeUserID, sourceMessageTS string) (bool, error) {
	if sourceMessageTS == "" {
		return false, nil
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM tracks
		WHERE channel_id = ? AND assignee_user_id = ? AND source_message_ts = ?
		AND status IN ('done', 'dismissed')`,
		channelID, assigneeUserID, sourceMessageTS).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking resolved track: %w", err)
	}
	return count > 0, nil
}

// FindResolvedTrackByFingerprint finds a done/dismissed track (within last 7 days) that
// shares at least one fingerprint entity with the given set.
// Returns (trackID, status, found).
func (db *DB) FindResolvedTrackByFingerprint(channelID, assigneeUserID string, fingerprint []string) (int64, string, bool, error) {
	if len(fingerprint) == 0 {
		return 0, "", false, nil
	}
	rows, err := db.Query(`SELECT id, status, fingerprint FROM tracks
		WHERE assignee_user_id = ? AND status IN ('done', 'dismissed')
		AND created_at > strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-7 days')`,
		assigneeUserID)
	if err != nil {
		return 0, "", false, fmt.Errorf("querying resolved tracks for fingerprint: %w", err)
	}
	defer rows.Close()

	fpSet := make(map[string]struct{}, len(fingerprint))
	for _, f := range fingerprint {
		fpSet[f] = struct{}{}
	}

	for rows.Next() {
		var id int64
		var status, fpJSON string
		if err := rows.Scan(&id, &status, &fpJSON); err != nil {
			continue
		}
		var existing []string
		if err := json.Unmarshal([]byte(fpJSON), &existing); err != nil || len(existing) == 0 {
			continue
		}
		for _, e := range existing {
			if _, ok := fpSet[e]; ok {
				return id, status, true, nil
			}
		}
	}
	return 0, "", false, rows.Err()
}

// ReopenTrack moves a done track back to inbox with has_updates=true and logs the event.
// Updates context if newContext is non-empty.
func (db *DB) ReopenTrack(id int64, newContext string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning reopen tx: %w", err)
	}
	defer tx.Rollback()

	var oldStatus, oldContext string
	err = tx.QueryRow(`SELECT status, context FROM tracks WHERE id = ?`, id).Scan(&oldStatus, &oldContext)
	if err != nil {
		return fmt.Errorf("loading track for reopen: %w", err)
	}

	setClauses := `status = 'inbox', has_updates = 1, completed_at = NULL`
	args := []any{}
	if newContext != "" {
		setClauses += `, context = ?`
		args = append(args, newContext)
	}
	args = append(args, id)
	if _, err := tx.Exec(`UPDATE tracks SET `+setClauses+` WHERE id = ?`, args...); err != nil { //nolint:gosec // setClauses built from constants, not user input
		return fmt.Errorf("reopening track: %w", err)
	}

	logTrackEventTx(tx, id, "reopened", "status", oldStatus, "inbox")
	if newContext != "" && newContext != oldContext {
		logTrackEventTx(tx, id, "context_updated", "context",
			truncate(oldContext, 100), truncate(newContext, 100))
	}

	return tx.Commit()
}

// AppendTrackActivity records new activity on a resolved track without reopening it.
// Sets has_updates=true and logs the event with the new context.
func (db *DB) AppendTrackActivity(id int64, newContext string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning append activity tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE tracks SET has_updates = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("setting has_updates: %w", err)
	}

	detail := "new activity detected"
	if newContext != "" {
		detail = truncate(newContext, 200)
	}
	logTrackEventTx(tx, id, "new_activity", "", "", detail)

	return tx.Commit()
}

// GetResolvedTracksSummary returns a compact one-line summary of recently resolved tracks
// for injection into the AI prompt as context. Max 500 chars.
func (db *DB) GetResolvedTracksSummary(assigneeUserID string, since time.Time) (string, error) {
	rows, err := db.Query(`SELECT substr(text, 1, 80), fingerprint FROM tracks
		WHERE assignee_user_id = ? AND status IN ('done', 'dismissed')
		AND created_at > ?
		ORDER BY created_at DESC
		LIMIT 20`,
		assigneeUserID, since.UTC().Format("2006-01-02T15:04:05Z"))
	if err != nil {
		return "", fmt.Errorf("querying resolved tracks summary: %w", err)
	}
	defer rows.Close()

	var parts []string
	totalLen := 0
	for rows.Next() {
		var text, fpJSON string
		if err := rows.Scan(&text, &fpJSON); err != nil {
			continue
		}
		// Use fingerprint entities as compact reference if available.
		var fp []string
		if err := json.Unmarshal([]byte(fpJSON), &fp); err == nil && len(fp) > 0 {
			text += " (" + strings.Join(fp[:min(len(fp), 3)], ",") + ")"
		}
		if totalLen+len(text) > 500 {
			break
		}
		parts = append(parts, text)
		totalLen += len(text) + 2 // account for "; "
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "; "), nil
}

// GetExistingTracksForChannel returns active/inbox tracks for a specific channel and user.
// Used by the extraction pipeline to pass existing items into the AI prompt for deduplication.
func (db *DB) GetExistingTracksForChannel(channelID, assigneeUserID string) ([]Track, error) {
	rows, err := db.Query(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items, prompt_version,
		ownership, ball_on, owner_user_id
		FROM tracks
		WHERE channel_id = ? AND assignee_user_id = ? AND status IN ('inbox', 'active')
		ORDER BY created_at DESC
		LIMIT ?`, channelID, assigneeUserID, 50)
	if err != nil {
		return nil, fmt.Errorf("querying existing tracks for channel: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// GetExistingTracksExcludingChannel returns active/inbox tracks for a user
// from all channels EXCEPT the specified one. Used for cross-channel completion detection.
func (db *DB) GetExistingTracksExcludingChannel(excludeChannelID, assigneeUserID string) ([]Track, error) {
	rows, err := db.Query(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items, prompt_version,
		ownership, ball_on, owner_user_id
		FROM tracks
		WHERE channel_id != ? AND assignee_user_id = ? AND status IN ('inbox', 'active')
		ORDER BY created_at DESC
		LIMIT ?`, excludeChannelID, assigneeUserID, 20)
	if err != nil {
		return nil, fmt.Errorf("querying cross-channel tracks: %w", err)
	}
	defer rows.Close()
	return scanTracks(rows)
}

// UpdateTrackBallOn updates the ball_on field of a track and logs the change.
func (db *DB) UpdateTrackBallOn(id int, newBallOn string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning ball_on update tx: %w", err)
	}
	defer tx.Rollback()

	var oldBallOn string
	if err := tx.QueryRow(`SELECT ball_on FROM tracks WHERE id = ?`, id).Scan(&oldBallOn); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track ball_on: %w", err)
	}

	if oldBallOn == newBallOn {
		return nil
	}

	if _, err := tx.Exec(`UPDATE tracks SET ball_on = ? WHERE id = ?`, newBallOn, id); err != nil {
		return fmt.Errorf("updating ball_on: %w", err)
	}
	logTrackEventTx(tx, int64(id), "ball_on_changed", "ball_on", oldBallOn, newBallOn)
	return tx.Commit()
}

// UpdateTrackSubItems updates the sub_items JSON for a track and logs the change.
func (db *DB) UpdateTrackSubItems(id int, subItems string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning sub_items update tx: %w", err)
	}
	defer tx.Rollback()

	var oldSubItems string
	if err := tx.QueryRow(`SELECT sub_items FROM tracks WHERE id = ?`, id).Scan(&oldSubItems); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track sub_items: %w", err)
	}

	if _, err := tx.Exec(`UPDATE tracks SET sub_items = ? WHERE id = ?`, subItems, id); err != nil {
		return fmt.Errorf("updating sub_items: %w", err)
	}

	if oldSubItems != subItems {
		summary := summarizeSubItemsChange(oldSubItems, subItems)
		logTrackEventTx(tx, int64(id), "sub_items_updated", "sub_items", "", summary)
	}

	return tx.Commit()
}

// UpdateTrackOwnership updates the ownership field of a track and logs the change.
func (db *DB) UpdateTrackOwnership(id int, newOwnership string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning ownership update tx: %w", err)
	}
	defer tx.Rollback()

	var oldOwnership string
	if err := tx.QueryRow(`SELECT ownership FROM tracks WHERE id = ?`, id).Scan(&oldOwnership); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("track %d not found", id)
		}
		return fmt.Errorf("getting track ownership: %w", err)
	}

	if oldOwnership == newOwnership {
		return nil
	}

	if _, err := tx.Exec(`UPDATE tracks SET ownership = ? WHERE id = ?`, newOwnership, id); err != nil {
		return fmt.Errorf("updating ownership: %w", err)
	}
	logTrackEventTx(tx, int64(id), "ownership_changed", "ownership", oldOwnership, newOwnership)
	return tx.Commit()
}

func scanTracks(rows *sql.Rows) ([]Track, error) {
	var items []Track
	for rows.Next() {
		var item Track
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
			&item.PromptVersion,
			&item.Ownership, &item.BallOn, &item.OwnerUserID,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning track row: %w", err)
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
