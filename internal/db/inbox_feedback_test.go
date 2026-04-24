package db

import (
	"testing"
	"time"
)

// seedInboxItem inserts a minimal inbox_items row and returns its ID.
func seedInboxItem(t *testing.T, d *DB, sender, channel, trigger string) int64 {
	t.Helper()
	res, err := d.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, created_at, updated_at)
		VALUES (?,?,?,?,'pending','medium',?,?)`,
		channel, "1.0", sender, trigger,
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestInboxFeedback_Record(t *testing.T) {
	database := openTestDB(t)

	itemID := seedInboxItem(t, database, "U123", "C1", "mention")

	if err := database.RecordInboxFeedback(itemID, -1, "never_show"); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`SELECT rating, reason FROM inbox_feedback WHERE inbox_item_id=?`, itemID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected a feedback row")
	}
	var r int
	var reason string
	_ = rows.Scan(&r, &reason)
	if r != -1 || reason != "never_show" {
		t.Errorf("got r=%d reason=%s", r, reason)
	}
}

func TestInboxFeedback_ListForItem(t *testing.T) {
	database := openTestDB(t)

	itemID := seedInboxItem(t, database, "U1", "C1", "mention")
	_ = database.RecordInboxFeedback(itemID, 1, "")
	_ = database.RecordInboxFeedback(itemID, -1, "source_noise")

	got, err := database.GetFeedbackForItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 feedback, got %d", len(got))
	}
}
