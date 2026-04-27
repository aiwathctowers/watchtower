package db

import (
	"database/sql"
	"fmt"
)

// MeetingRecap is an AI-generated post-meeting summary attached to a calendar
// event. One row per event_id; re-running the recap CLI overwrites the row.
type MeetingRecap struct {
	EventID    string
	SourceText string
	RecapJSON  string
	CreatedAt  string
	UpdatedAt  string
}

// UpsertMeetingRecap inserts a new recap or updates an existing row for the
// given event_id. updated_at is bumped to "now" on every call.
func (db *DB) UpsertMeetingRecap(eventID, sourceText, recapJSON string) error {
	_, err := db.Exec(`
		INSERT INTO meeting_recaps (event_id, source_text, recap_json)
		VALUES (?, ?, ?)
		ON CONFLICT(event_id) DO UPDATE SET
			source_text = excluded.source_text,
			recap_json  = excluded.recap_json,
			updated_at  = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
	`, eventID, sourceText, recapJSON)
	if err != nil {
		return fmt.Errorf("upserting meeting recap for %s: %w", eventID, err)
	}
	return nil
}

// GetMeetingRecap returns the recap for the given event, or (nil, nil) if none.
func (db *DB) GetMeetingRecap(eventID string) (*MeetingRecap, error) {
	var r MeetingRecap
	err := db.QueryRow(`
		SELECT event_id, source_text, recap_json, created_at, updated_at
		FROM meeting_recaps WHERE event_id = ?
	`, eventID).Scan(&r.EventID, &r.SourceText, &r.RecapJSON, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading meeting recap for %s: %w", eventID, err)
	}
	return &r, nil
}

// MeetingNote is a single row in meeting_notes (questions or freeform notes
// attached to a calendar event).
type MeetingNote struct {
	ID        int64
	EventID   string
	Type      string // 'question' | 'note'
	Text      string
	IsChecked bool
	SortOrder int
	TaskID    sql.NullInt64
	CreatedAt string
	UpdatedAt string
}

// GetMeetingNotesForEvent returns all meeting_notes for the event, ordered
// first by type (questions before notes) then by sort_order.
func (db *DB) GetMeetingNotesForEvent(eventID string) ([]MeetingNote, error) {
	rows, err := db.Query(`
		SELECT id, event_id, type, text, is_checked, sort_order, task_id, created_at, updated_at
		FROM meeting_notes WHERE event_id = ?
		ORDER BY type DESC, sort_order ASC
	`, eventID)
	if err != nil {
		return nil, fmt.Errorf("loading meeting notes for %s: %w", eventID, err)
	}
	defer rows.Close()

	var out []MeetingNote
	for rows.Next() {
		var n MeetingNote
		var checked int
		if err := rows.Scan(&n.ID, &n.EventID, &n.Type, &n.Text, &checked, &n.SortOrder, &n.TaskID, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning meeting note: %w", err)
		}
		n.IsChecked = checked != 0
		out = append(out, n)
	}
	return out, rows.Err()
}
