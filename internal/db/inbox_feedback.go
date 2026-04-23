package db

import "time"

// InboxFeedback represents a single feedback record for an inbox item.
type InboxFeedback struct {
	ID          int64
	InboxItemID int64
	Rating      int
	Reason      string
	CreatedAt   string
}

// RecordInboxFeedback inserts a feedback row for the given inbox item.
// rating must be -1 or 1; reason is one of "", "source_noise", "wrong_priority",
// "wrong_class", "never_show".
func (db *DB) RecordInboxFeedback(itemID int64, rating int, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at) VALUES (?,?,?,?)`,
		itemID, rating, reason, now,
	)
	return err
}

// GetFeedbackForItem returns all feedback rows for the given inbox item.
func (db *DB) GetFeedbackForItem(itemID int64) ([]InboxFeedback, error) {
	rows, err := db.Query(
		`SELECT id, inbox_item_id, rating, reason, created_at FROM inbox_feedback WHERE inbox_item_id=?`,
		itemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InboxFeedback
	for rows.Next() {
		var f InboxFeedback
		rows.Scan(&f.ID, &f.InboxItemID, &f.Rating, &f.Reason, &f.CreatedAt)
		out = append(out, f)
	}
	return out, rows.Err()
}
