package db

import (
	"testing"
	"time"
)

func TestInbox_SetItemClass(t *testing.T) {
	database := openTestDB(t)
	id := seedInboxItem(t, database, "U1", "C1", "mention")
	if err := database.SetInboxItemClass(id, "ambient"); err != nil {
		t.Fatal(err)
	}
	var cls string
	database.QueryRow(`SELECT item_class FROM inbox_items WHERE id=?`, id).Scan(&cls)
	if cls != "ambient" {
		t.Errorf("got %s", cls)
	}
}

func TestInbox_SetPinned(t *testing.T) {
	database := openTestDB(t)
	id := seedInboxItem(t, database, "U1", "C1", "mention")
	database.SetInboxPinned([]int64{id})
	var p int
	database.QueryRow(`SELECT pinned FROM inbox_items WHERE id=?`, id).Scan(&p)
	if p != 1 {
		t.Errorf("not pinned: %d", p)
	}

	// ClearPinned resets pinned
	database.ClearPinnedAll()
	database.QueryRow(`SELECT pinned FROM inbox_items WHERE id=?`, id).Scan(&p)
	if p != 0 {
		t.Error("still pinned")
	}
}

func TestInbox_ArchiveExpired(t *testing.T) {
	database := openTestDB(t)

	// Ambient item 8 days old
	oldT := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	database.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, created_at, updated_at)
		VALUES ('C1','1.0','U1','decision_made','pending','low','ambient',?,?)`, oldT, oldT)

	n, err := database.ArchiveExpiredAmbient(7 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("want 1 archived, got %d", n)
	}

	// Verify archived_at set + reason
	var reason string
	var arch string
	database.QueryRow(`SELECT archive_reason, archived_at FROM inbox_items WHERE item_class='ambient'`).Scan(&reason, &arch)
	if reason != "seen_expired" {
		t.Errorf("reason=%q", reason)
	}
	if arch == "" {
		t.Error("archived_at empty")
	}
}

func TestInbox_ArchiveStale(t *testing.T) {
	database := openTestDB(t)
	oldT := time.Now().Add(-15 * 24 * time.Hour).UTC().Format(time.RFC3339)
	database.Exec(`INSERT INTO inbox_items (channel_id, message_ts, sender_user_id, trigger_type, status, priority, item_class, created_at, updated_at)
		VALUES ('C1','1.0','U1','mention','pending','medium','actionable',?,?)`, oldT, oldT)
	n, _ := database.ArchiveStaleActionable(14 * 24 * time.Hour)
	if n != 1 {
		t.Errorf("want 1, got %d", n)
	}
}

func TestInbox_FeedQuery_ExcludesArchivedAndTerminated(t *testing.T) {
	database := openTestDB(t)
	alive := seedInboxItem(t, database, "U1", "C1", "mention")
	archived := seedInboxItem(t, database, "U2", "C2", "mention")
	database.Exec(`UPDATE inbox_items SET archived_at=? WHERE id=?`, time.Now().Format(time.RFC3339), archived)
	resolved := seedInboxItem(t, database, "U3", "C3", "mention")
	database.Exec(`UPDATE inbox_items SET status='resolved' WHERE id=?`, resolved)

	got, err := database.ListInboxFeed(50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || int64(got[0].ID) != alive {
		t.Errorf("expected only alive item, got %+v", got)
	}
}

func TestInbox_PinnedList(t *testing.T) {
	database := openTestDB(t)
	a := seedInboxItem(t, database, "U1", "C1", "mention")
	_ = seedInboxItem(t, database, "U2", "C2", "mention")
	database.SetInboxPinned([]int64{a})
	got, _ := database.ListInboxPinned()
	if len(got) != 1 || int64(got[0].ID) != a {
		t.Errorf("bad pinned list: %+v", got)
	}
}
