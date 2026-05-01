package db

import (
	"fmt"
	"strings"
	"time"
)

// NotifyDueTargets surfaces every active target whose due_date has passed but
// has not yet been surfaced (notified_at is empty) into the inbox as a
// `target_due` item, then stamps notified_at so subsequent calls are no-ops.
// Returns the number of newly surfaced targets.
//
// "Active" means status ∈ {todo, in_progress, blocked} — closing the target
// (done/dismissed/snoozed) suppresses the reminder. Snoozed targets are
// re-surfaced once UnsnoozeExpiredTargets flips them back to todo.
func (db *DB) NotifyDueTargets(now time.Time) (int, error) {
	cutoff := now.UTC().Format("2006-01-02T15:04")

	rows, err := db.Query(`
		SELECT id, text, priority
		FROM targets
		WHERE due_date != ''
		  AND due_date <= ?
		  AND notified_at = ''
		  AND status IN ('todo','in_progress','blocked')`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("scanning due targets: %w", err)
	}
	type due struct {
		id       int64
		text     string
		priority string
	}
	var pending []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.text, &d.priority); err != nil {
			rows.Close()
			return 0, fmt.Errorf("reading due target: %w", err)
		}
		pending = append(pending, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterating due targets: %w", err)
	}

	if len(pending) == 0 {
		return 0, nil
	}

	stamp := now.UTC().Format("2006-01-02T15:04:05Z")
	surfaced := 0
	for _, d := range pending {
		channelID := fmt.Sprintf("target:%d", d.id)
		// ON CONFLICT keeps the operation safe if a stale row from a prior
		// surface attempt already exists at this composite key.
		_, err := db.Exec(`
			INSERT INTO inbox_items (
				channel_id, message_ts, sender_user_id, trigger_type,
				snippet, priority, target_id, item_class
			) VALUES (?, ?, '', 'target_due', ?, ?, ?, 'actionable')
			ON CONFLICT(channel_id, message_ts) DO NOTHING`,
			channelID, stamp, truncate(d.text, 200), normalizePriority(d.priority), d.id,
		)
		if err != nil {
			return surfaced, fmt.Errorf("surfacing target %d to inbox: %w", d.id, err)
		}
		if _, err := db.Exec(
			`UPDATE targets SET notified_at = ?, updated_at = ? WHERE id = ?`,
			stamp, stamp, d.id,
		); err != nil {
			return surfaced, fmt.Errorf("stamping target %d notified_at: %w", d.id, err)
		}
		surfaced++
	}
	return surfaced, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func normalizePriority(p string) string {
	switch strings.ToLower(p) {
	case "high", "medium", "low":
		return strings.ToLower(p)
	default:
		return "medium"
	}
}

