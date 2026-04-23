package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// inboxSelectCols is the standard SELECT column list for inbox_items.
const inboxSelectCols = `id, channel_id, message_ts, thread_ts, sender_user_id,
	trigger_type, snippet, context, raw_text, permalink, status, priority,
	ai_reason, resolved_reason, snooze_until, COALESCE(waiting_user_ids,''), task_id,
	COALESCE(read_at,''), created_at, updated_at,
	COALESCE(item_class,'actionable'), COALESCE(pinned,0), COALESCE(archived_at,''), COALESCE(archive_reason,'')`

// inboxItemColumns is an alias for inboxSelectCols used by feed/pinned queries.
const inboxItemColumns = inboxSelectCols

// scanInboxItem scans an InboxItem from a row with the standard SELECT column list.
func scanInboxItem(row interface{ Scan(...any) error }) (*InboxItem, error) {
	var it InboxItem
	var pinned int
	if err := row.Scan(
		&it.ID, &it.ChannelID, &it.MessageTS, &it.ThreadTS, &it.SenderUserID,
		&it.TriggerType, &it.Snippet, &it.Context, &it.RawText, &it.Permalink, &it.Status, &it.Priority,
		&it.AIReason, &it.ResolvedReason, &it.SnoozeUntil, &it.WaitingUserIDs, &it.TaskID,
		&it.ReadAt, &it.CreatedAt, &it.UpdatedAt,
		&it.ItemClass, &pinned, &it.ArchivedAt, &it.ArchiveReason,
	); err != nil {
		return nil, err
	}
	it.Pinned = pinned != 0
	return &it, nil
}

// scanInboxItems scans all rows into a slice of InboxItem.
func scanInboxItems(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]InboxItem, error) {
	var items []InboxItem
	for rows.Next() {
		it, err := scanInboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

// CreateInboxItem inserts a new inbox item and returns its ID.
func (db *DB) CreateInboxItem(it InboxItem) (int64, error) {
	if it.Status == "" {
		it.Status = "pending"
	}
	if it.Priority == "" {
		it.Priority = "medium"
	}
	res, err := db.Exec(`INSERT INTO inbox_items (channel_id, message_ts, thread_ts, sender_user_id,
		trigger_type, snippet, context, raw_text, permalink, status, priority, ai_reason, waiting_user_ids)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ChannelID, it.MessageTS, it.ThreadTS, it.SenderUserID,
		it.TriggerType, it.Snippet, it.Context, it.RawText, it.Permalink, it.Status, it.Priority, it.AIReason, it.WaitingUserIDs,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting inbox item: %w", err)
	}
	return res.LastInsertId()
}

// FindPendingInboxByThread returns the ID of an existing pending inbox item
// for the same thread (channel_id + thread_ts). For non-threaded messages
// (threadTS=""), finds the channel-level inbox item. Returns 0 if not found.
func (db *DB) FindPendingInboxByThread(channelID, threadTS string) (int, error) {
	var id int
	err := db.QueryRow(`SELECT id FROM inbox_items
		WHERE channel_id = ? AND thread_ts = ? AND status = 'pending'
		ORDER BY created_at DESC LIMIT 1`,
		channelID, threadTS).Scan(&id)
	if err != nil {
		return 0, nil //nolint:nilerr // not found is not an error
	}
	return id, nil
}

// UpdateInboxItemSnippet updates the snippet, context, raw_text, sender, message_ts and permalink
// of an existing inbox item (used when a newer message arrives in the same thread).
func (db *DB) UpdateInboxItemSnippet(id int, messageTS, senderUserID, snippet, context, rawText, permalink string) error {
	_, err := db.Exec(`UPDATE inbox_items SET
		message_ts = ?, sender_user_id = ?, snippet = ?, context = ?, raw_text = ?, permalink = ?,
		ai_reason = '', read_at = NULL,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`,
		messageTS, senderUserID, snippet, context, rawText, permalink, id)
	if err != nil {
		return fmt.Errorf("updating inbox item %d snippet: %w", id, err)
	}
	return nil
}

// GetInboxItemByID returns a single inbox item by ID.
func (db *DB) GetInboxItemByID(id int) (*InboxItem, error) {
	row := db.QueryRow(`SELECT `+inboxSelectCols+` FROM inbox_items WHERE id = ?`, id)
	it, err := scanInboxItem(row)
	if err != nil {
		return nil, fmt.Errorf("getting inbox item %d: %w", id, err)
	}
	return it, nil
}

// GetInboxItemByMessage returns an inbox item by channel_id + message_ts.
func (db *DB) GetInboxItemByMessage(channelID, messageTS string) (*InboxItem, error) {
	row := db.QueryRow(`SELECT `+inboxSelectCols+` FROM inbox_items WHERE channel_id = ? AND message_ts = ?`,
		channelID, messageTS)
	it, err := scanInboxItem(row)
	if err != nil {
		return nil, fmt.Errorf("getting inbox item by message: %w", err)
	}
	return it, nil
}

// GetInboxItems returns inbox items matching the filter.
func (db *DB) GetInboxItems(f InboxFilter) ([]InboxItem, error) {
	query := `SELECT ` + inboxSelectCols + ` FROM inbox_items`
	var conditions []string
	var args []any

	if !f.IncludeResolved {
		conditions = append(conditions, "status NOT IN ('resolved', 'dismissed')")
	}
	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, f.Status)
	}
	if f.Priority != "" {
		conditions = append(conditions, "priority = ?")
		args = append(args, f.Priority)
	}
	if f.TriggerType != "" {
		conditions = append(conditions, "trigger_type = ?")
		args = append(args, f.TriggerType)
	}
	if f.ChannelID != "" {
		conditions = append(conditions, "channel_id = ?")
		args = append(args, f.ChannelID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY
		CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
		updated_at DESC`
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying inbox items: %w", err)
	}
	defer rows.Close()

	var items []InboxItem
	for rows.Next() {
		it, err := scanInboxItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning inbox item: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

// UpdateInboxItemStatus changes the status of an inbox item.
func (db *DB) UpdateInboxItemStatus(id int, newStatus string) error {
	_, err := db.Exec(`UPDATE inbox_items SET status = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, newStatus, id)
	if err != nil {
		return fmt.Errorf("updating inbox item %d status: %w", id, err)
	}
	return nil
}

// UpdateInboxItemPriority changes the priority of an inbox item.
func (db *DB) UpdateInboxItemPriority(id int, priority string) error {
	_, err := db.Exec(`UPDATE inbox_items SET priority = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, priority, id)
	if err != nil {
		return fmt.Errorf("updating inbox item %d priority: %w", id, err)
	}
	return nil
}

// ResolveInboxItem marks an inbox item as resolved with a reason.
func (db *DB) ResolveInboxItem(id int, reason string) error {
	_, err := db.Exec(`UPDATE inbox_items SET status = 'resolved', resolved_reason = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, reason, id)
	if err != nil {
		return fmt.Errorf("resolving inbox item %d: %w", id, err)
	}
	return nil
}

// DismissInboxItem marks an inbox item as dismissed.
func (db *DB) DismissInboxItem(id int) error {
	return db.UpdateInboxItemStatus(id, "dismissed")
}

// SnoozeInboxItem snoozes an inbox item until the given date.
func (db *DB) SnoozeInboxItem(id int, until string) error {
	_, err := db.Exec(`UPDATE inbox_items SET status = 'snoozed', snooze_until = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, until, id)
	if err != nil {
		return fmt.Errorf("snoozing inbox item %d: %w", id, err)
	}
	return nil
}

// UnsnoozeExpiredInboxItems moves snoozed inbox items past snooze_until back to pending.
func (db *DB) UnsnoozeExpiredInboxItems() (int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	res, err := db.Exec(`UPDATE inbox_items SET status = 'pending', snooze_until = '',
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE status = 'snoozed' AND snooze_until != '' AND snooze_until <= ?`, today)
	if err != nil {
		return 0, fmt.Errorf("unsnoozing inbox items: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MarkInboxRead sets read_at=now for an inbox item.
func (db *DB) MarkInboxRead(id int) error {
	_, err := db.Exec(`UPDATE inbox_items SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("marking inbox item %d read: %w", id, err)
	}
	return nil
}

// LinkInboxTask sets the task_id for an inbox item.
func (db *DB) LinkInboxTask(id int, taskID int) error {
	_, err := db.Exec(`UPDATE inbox_items SET task_id = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, taskID, id)
	if err != nil {
		return fmt.Errorf("linking inbox item %d to task %d: %w", id, taskID, err)
	}
	return nil
}

// GetInboxCounts returns (pending, unread) inbox item counts.
func (db *DB) GetInboxCounts() (int, int, error) {
	var pending, unread int
	err := db.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'pending' AND read_at IS NULL THEN 1 ELSE 0 END), 0)
		FROM inbox_items`).Scan(&pending, &unread)
	return pending, unread, err
}

// GetInboxItemsForBriefing returns pending inbox items for the daily briefing.
func (db *DB) GetInboxItemsForBriefing() ([]InboxItem, error) {
	rows, err := db.Query(`SELECT ` + inboxSelectCols + ` FROM inbox_items
		WHERE status = 'pending'
		ORDER BY
			CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
			created_at DESC
		LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("querying inbox items for briefing: %w", err)
	}
	defer rows.Close()

	var items []InboxItem
	for rows.Next() {
		it, err := scanInboxItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning inbox item for briefing: %w", err)
		}
		items = append(items, *it)
	}
	return items, rows.Err()
}

// BulkUpdateInboxPriorities updates priority and ai_reason for multiple inbox items.
func (db *DB) BulkUpdateInboxPriorities(updates map[int]struct {
	Priority string
	AIReason string
}) error {
	for id, u := range updates {
		_, err := db.Exec(`UPDATE inbox_items SET priority = ?, ai_reason = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ?`, u.Priority, u.AIReason, id)
		if err != nil {
			return fmt.Errorf("updating inbox item %d priority: %w", id, err)
		}
	}
	return nil
}

// DeduplicateThreadInboxItems merges duplicate pending inbox items for the same thread.
// Keeps the most recently updated item and resolves the rest.
func (db *DB) DeduplicateThreadInboxItems() (int, error) {
	// Find threads (and non-threaded channel groups) with multiple pending items.
	res, err := db.Exec(`UPDATE inbox_items SET status = 'resolved', resolved_reason = 'Merged duplicate'
		WHERE status = 'pending'
		AND id NOT IN (
			SELECT MAX(id) FROM inbox_items
			WHERE status = 'pending'
			GROUP BY channel_id, thread_ts
		)
		AND EXISTS (
			SELECT 1 FROM inbox_items i2
			WHERE i2.channel_id = inbox_items.channel_id
			AND i2.thread_ts = inbox_items.thread_ts
			AND i2.status = 'pending'
			AND i2.id != inbox_items.id
		)`)
	if err != nil {
		return 0, fmt.Errorf("deduplicating thread inbox items: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MergeWaitingUserIDs adds new user IDs to the waiting_user_ids JSON array of an inbox item.
func (db *DB) MergeWaitingUserIDs(id int, newUserIDs []string) error {
	var existing string
	if err := db.QueryRow(`SELECT COALESCE(waiting_user_ids,'') FROM inbox_items WHERE id = ?`, id).Scan(&existing); err != nil {
		return fmt.Errorf("reading waiting_user_ids for item %d: %w", id, err)
	}

	// Parse existing IDs.
	seen := make(map[string]bool)
	var existingIDs []string
	if existing != "" {
		_ = json.Unmarshal([]byte(existing), &existingIDs)
	}
	for _, uid := range existingIDs {
		seen[uid] = true
	}

	// Merge new IDs.
	merged := existingIDs
	for _, uid := range newUserIDs {
		if !seen[uid] {
			merged = append(merged, uid)
			seen[uid] = true
		}
	}

	data, _ := json.Marshal(merged)
	_, err := db.Exec(`UPDATE inbox_items SET waiting_user_ids = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, string(data), id)
	if err != nil {
		return fmt.Errorf("updating waiting_user_ids for item %d: %w", id, err)
	}
	return nil
}

// GetInboxLastProcessedTS returns the last processed timestamp for inbox detection.
func (db *DB) GetInboxLastProcessedTS() (float64, error) {
	var ts float64
	err := db.QueryRow(`SELECT COALESCE(inbox_last_processed_ts, 0) FROM workspace LIMIT 1`).Scan(&ts)
	if err != nil {
		return 0, fmt.Errorf("getting inbox last processed ts: %w", err)
	}
	return ts, nil
}

// SetInboxLastProcessedTS updates the last processed timestamp.
func (db *DB) SetInboxLastProcessedTS(ts float64) error {
	_, err := db.Exec(`UPDATE workspace SET inbox_last_processed_ts = ?`, ts)
	if err != nil {
		return fmt.Errorf("setting inbox last processed ts: %w", err)
	}
	return nil
}

// FindPendingMentions finds messages that mention the current user where the user hasn't replied.
func (db *DB) FindPendingMentions(currentUserID string, sinceTS float64) ([]InboxCandidate, error) {
	mentionPattern := "<@" + currentUserID + ">"
	rows, err := db.Query(`SELECT m.channel_id, m.ts, COALESCE(m.thread_ts, ''), m.user_id, m.text, m.permalink, m.ts_unix
		FROM messages m
		WHERE m.text LIKE ?
		AND m.user_id != ?
		AND m.user_id != ''
		AND m.ts_unix > ?
		AND m.is_deleted = 0
		AND NOT EXISTS (
			SELECT 1 FROM inbox_items ii
			WHERE ii.channel_id = m.channel_id AND ii.message_ts = m.ts
		)
		ORDER BY m.ts_unix DESC`,
		"%"+mentionPattern+"%", currentUserID, sinceTS)
	if err != nil {
		return nil, fmt.Errorf("finding pending mentions: %w", err)
	}
	defer rows.Close()

	var candidates []InboxCandidate
	for rows.Next() {
		var c InboxCandidate
		if err := rows.Scan(&c.ChannelID, &c.MessageTS, &c.ThreadTS, &c.SenderUserID, &c.Text, &c.Permalink, &c.TSUnix); err != nil {
			return nil, fmt.Errorf("scanning mention candidate: %w", err)
		}
		c.TriggerType = "mention"
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// FindPendingDMs finds DM messages from others where the user hasn't replied after.
func (db *DB) FindPendingDMs(currentUserID string, sinceTS float64) ([]InboxCandidate, error) {
	rows, err := db.Query(`SELECT m.channel_id, m.ts, COALESCE(m.thread_ts, ''), m.user_id, m.text, m.permalink, m.ts_unix
		FROM messages m
		JOIN channels c ON c.id = m.channel_id
		WHERE c.type = 'dm'
		AND m.user_id != ?
		AND m.user_id != ''
		AND m.ts_unix > ?
		AND m.is_deleted = 0
		AND NOT EXISTS (
			SELECT 1 FROM inbox_items ii
			WHERE ii.channel_id = m.channel_id AND ii.message_ts = m.ts
		)
		ORDER BY m.ts_unix DESC`,
		currentUserID, sinceTS)
	if err != nil {
		return nil, fmt.Errorf("finding pending DMs: %w", err)
	}
	defer rows.Close()

	var candidates []InboxCandidate
	for rows.Next() {
		var c InboxCandidate
		if err := rows.Scan(&c.ChannelID, &c.MessageTS, &c.ThreadTS, &c.SenderUserID, &c.Text, &c.Permalink, &c.TSUnix); err != nil {
			return nil, fmt.Errorf("scanning DM candidate: %w", err)
		}
		c.TriggerType = "dm"
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// FindThreadRepliesToUser finds messages in threads where currentUserID participated
// (posted the root message OR any reply), but the reply is from someone else.
func (db *DB) FindThreadRepliesToUser(currentUserID string, sinceTS float64) ([]InboxCandidate, error) {
	rows, err := db.Query(`SELECT m.channel_id, m.ts, COALESCE(m.thread_ts, ''), m.user_id, m.text, m.permalink, m.ts_unix
		FROM messages m
		WHERE m.thread_ts != ''
		  AND m.thread_ts != m.ts
		  AND m.user_id != ?
		  AND m.user_id != ''
		  AND m.ts_unix > ?
		  AND m.is_deleted = 0
		  AND EXISTS (
		      SELECT 1 FROM messages participant
		      WHERE participant.channel_id = m.channel_id
		        AND participant.thread_ts = m.thread_ts
		        AND participant.user_id = ?
		        AND participant.ts != m.ts
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM inbox_items ii
		      WHERE ii.channel_id = m.channel_id AND ii.message_ts = m.ts
		  )
		ORDER BY m.ts_unix DESC`,
		currentUserID, sinceTS, currentUserID)
	if err != nil {
		return nil, fmt.Errorf("finding thread replies to user: %w", err)
	}
	defer rows.Close()

	var candidates []InboxCandidate
	for rows.Next() {
		var c InboxCandidate
		if err := rows.Scan(&c.ChannelID, &c.MessageTS, &c.ThreadTS, &c.SenderUserID, &c.Text, &c.Permalink, &c.TSUnix); err != nil {
			return nil, fmt.Errorf("scanning thread reply candidate: %w", err)
		}
		c.TriggerType = "thread_reply"
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// FindReactionRequests finds messages posted by currentUserID where someone else added
// a question/attention reaction (question, eyes, exclamation, warning, etc.).
func (db *DB) FindReactionRequests(currentUserID string, sinceTS float64) ([]InboxCandidate, error) {
	rows, err := db.Query(`SELECT m.channel_id, m.ts, COALESCE(m.thread_ts, ''),
		   MIN(r.user_id) as reactor_id,
		   m.text, m.permalink, m.ts_unix
		FROM messages m
		JOIN reactions r ON r.channel_id = m.channel_id AND r.message_ts = m.ts
		WHERE m.user_id = ?
		  AND r.user_id != ?
		  AND r.emoji IN ('question', 'grey_question', 'eyes', 'bangbang', 'exclamation', 'heavy_exclamation_mark', 'warning', 'red_circle', 'rotating_light', 'sos')
		  AND m.ts_unix > ?
		  AND m.is_deleted = 0
		  AND NOT EXISTS (
		      SELECT 1 FROM inbox_items ii
		      WHERE ii.channel_id = m.channel_id AND ii.message_ts = m.ts
		  )
		GROUP BY m.channel_id, m.ts
		ORDER BY m.ts_unix DESC`,
		currentUserID, currentUserID, sinceTS)
	if err != nil {
		return nil, fmt.Errorf("finding reaction requests: %w", err)
	}
	defer rows.Close()

	var candidates []InboxCandidate
	for rows.Next() {
		var c InboxCandidate
		if err := rows.Scan(&c.ChannelID, &c.MessageTS, &c.ThreadTS, &c.SenderUserID, &c.Text, &c.Permalink, &c.TSUnix); err != nil {
			return nil, fmt.Errorf("scanning reaction candidate: %w", err)
		}
		c.TriggerType = "reaction"
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// CheckUserReplied checks whether the current user has acted on a message:
// replied in the thread/channel OR reacted with any emoji.
// For threaded messages, checks if user posted in the thread after message_ts.
// For non-threaded messages, checks if user posted in the channel after message_ts.
func (db *DB) CheckUserReplied(currentUserID, channelID, messageTS, threadTS string) (bool, error) {
	// Check 1: user reacted to the message (any emoji = "I saw and acknowledged").
	var reactionCount int
	err := db.QueryRow(`SELECT COUNT(*) FROM reactions
		WHERE channel_id = ? AND message_ts = ? AND user_id = ?`,
		channelID, messageTS, currentUserID).Scan(&reactionCount)
	if err != nil {
		return false, fmt.Errorf("checking reaction: %w", err)
	}
	if reactionCount > 0 {
		return true, nil
	}

	// Check 2: user replied in the thread or channel.
	var count int
	if threadTS != "" {
		err = db.QueryRow(`SELECT COUNT(*) FROM messages
			WHERE channel_id = ? AND thread_ts = ? AND user_id = ? AND ts > ?`,
			channelID, threadTS, currentUserID, messageTS).Scan(&count)
		if err != nil {
			return false, fmt.Errorf("checking thread reply: %w", err)
		}
	} else {
		// Check for any reply in the channel after the message: either a top-level
		// message or a thread reply to the original message (common in DMs).
		err = db.QueryRow(`SELECT COUNT(*) FROM messages
			WHERE channel_id = ? AND user_id = ? AND ts > ?
			AND (thread_ts IS NULL OR thread_ts = '' OR thread_ts = ?)`,
			channelID, currentUserID, messageTS, messageTS).Scan(&count)
		if err != nil {
			return false, fmt.Errorf("checking channel reply: %w", err)
		}
	}
	return count > 0, nil
}

// CheckUserRepliedBefore checks whether the current user posted in the thread/channel
// BEFORE the given messageTS. This is used to detect closing signals where the user
// already replied and the other person is just acknowledging.
func (db *DB) CheckUserRepliedBefore(currentUserID, channelID, messageTS, threadTS string) (bool, error) {
	var count int
	if threadTS != "" {
		err := db.QueryRow(`SELECT COUNT(*) FROM messages
			WHERE channel_id = ? AND thread_ts = ? AND user_id = ? AND ts < ?`,
			channelID, threadTS, currentUserID, messageTS).Scan(&count)
		if err != nil {
			return false, fmt.Errorf("checking thread reply before: %w", err)
		}
	} else {
		err := db.QueryRow(`SELECT COUNT(*) FROM messages
			WHERE channel_id = ? AND user_id = ? AND ts < ? AND (thread_ts IS NULL OR thread_ts = '')`,
			channelID, currentUserID, messageTS).Scan(&count)
		if err != nil {
			return false, fmt.Errorf("checking channel reply before: %w", err)
		}
	}
	return count > 0, nil
}

// GetThreadContext returns recent messages in a thread for context.
func (db *DB) GetThreadContext(channelID, threadTS string, limit int) ([]struct {
	UserID string
	Text   string
}, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.Query(`SELECT user_id, text FROM messages
		WHERE channel_id = ? AND (thread_ts = ? OR ts = ?)
		AND is_deleted = 0
		ORDER BY ts_unix DESC LIMIT ?`, channelID, threadTS, threadTS, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []struct {
		UserID string
		Text   string
	}
	for rows.Next() {
		var m struct {
			UserID string
			Text   string
		}
		if err := rows.Scan(&m.UserID, &m.Text); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}

// SetInboxItemClass sets the item_class ('actionable' or 'ambient') for an inbox item.
func (db *DB) SetInboxItemClass(id int64, class string) error {
	_, err := db.Exec(`UPDATE inbox_items SET item_class=?, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`,
		class, id)
	return err
}

// SetInboxPinned pins the given item IDs (pinned=1) and unpins all others in a single transaction.
func (db *DB) SetInboxPinned(ids []int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE inbox_items SET pinned=0 WHERE pinned=1`); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE inbox_items SET pinned=1 WHERE id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearPinnedAll unpins all inbox items.
func (db *DB) ClearPinnedAll() error {
	_, err := db.Exec(`UPDATE inbox_items SET pinned=0 WHERE pinned=1`)
	return err
}

// ArchiveExpiredAmbient archives ambient items older than threshold, marking reason='seen_expired'.
func (db *DB) ArchiveExpiredAmbient(threshold time.Duration) (int, error) {
	cutoff := time.Now().Add(-threshold).UTC().Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE inbox_items SET archived_at=?, archive_reason='seen_expired', updated_at=?
		WHERE item_class='ambient' AND archived_at IS NULL AND created_at < ?`,
		now, now, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ArchiveStaleActionable archives actionable items with status=pending older than threshold, marking reason='stale'.
func (db *DB) ArchiveStaleActionable(threshold time.Duration) (int, error) {
	cutoff := time.Now().Add(-threshold).UTC().Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE inbox_items SET archived_at=?, archive_reason='stale', updated_at=?
		WHERE item_class='actionable' AND archived_at IS NULL AND status='pending' AND updated_at < ?`,
		now, now, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListInboxFeed returns non-pinned, non-archived, live items newest first.
func (db *DB) ListInboxFeed(limit, offset int) ([]InboxItem, error) {
	rows, err := db.Query(`SELECT `+inboxItemColumns+` FROM inbox_items
		WHERE pinned=0 AND archived_at IS NULL AND status NOT IN ('resolved','dismissed','snoozed')
		ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInboxItems(rows)
}

// ListInboxPinned returns pinned pending items ordered by priority then newest first.
func (db *DB) ListInboxPinned() ([]InboxItem, error) {
	rows, err := db.Query(`SELECT `+inboxItemColumns+` FROM inbox_items
		WHERE pinned=1 AND status='pending' AND archived_at IS NULL
		ORDER BY
			CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
			created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInboxItems(rows)
}

// GetChannelContextBefore returns recent messages in a channel before a given timestamp.
func (db *DB) GetChannelContextBefore(channelID string, beforeTS string, limit int) ([]struct {
	UserID string
	Text   string
}, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := db.Query(`SELECT user_id, text FROM messages
		WHERE channel_id = ? AND ts < ?
		AND (thread_ts IS NULL OR thread_ts = '' OR thread_ts = ts)
		AND is_deleted = 0
		ORDER BY ts_unix DESC LIMIT ?`, channelID, beforeTS, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []struct {
		UserID string
		Text   string
	}
	for rows.Next() {
		var m struct {
			UserID string
			Text   string
		}
		if err := rows.Scan(&m.UserID, &m.Text); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}
