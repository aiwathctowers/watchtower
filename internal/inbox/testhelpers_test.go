package inbox

import (
	"testing"

	"watchtower/internal/db"
)

// queryInboxByTrigger returns all inbox_items with the given trigger_type.
func queryInboxByTrigger(t *testing.T, d *db.DB, triggerType string) []db.InboxItem {
	t.Helper()
	rows, err := d.Query(`SELECT id, channel_id, message_ts, thread_ts, sender_user_id,
		trigger_type, snippet, context, raw_text, permalink, status, priority,
		ai_reason, resolved_reason, snooze_until, COALESCE(waiting_user_ids,''), target_id,
		COALESCE(read_at,''), created_at, updated_at,
		COALESCE(item_class,'actionable'), COALESCE(pinned,0), COALESCE(archived_at,''), COALESCE(archive_reason,'')
		FROM inbox_items WHERE trigger_type = ?`, triggerType)
	if err != nil {
		t.Fatalf("queryInboxByTrigger: %v", err)
	}
	defer rows.Close()

	var items []db.InboxItem
	for rows.Next() {
		var it db.InboxItem
		var pinned int
		if err := rows.Scan(
			&it.ID, &it.ChannelID, &it.MessageTS, &it.ThreadTS, &it.SenderUserID,
			&it.TriggerType, &it.Snippet, &it.Context, &it.RawText, &it.Permalink,
			&it.Status, &it.Priority, &it.AIReason, &it.ResolvedReason, &it.SnoozeUntil,
			&it.WaitingUserIDs, &it.TargetID, &it.ReadAt, &it.CreatedAt, &it.UpdatedAt,
			&it.ItemClass, &pinned, &it.ArchivedAt, &it.ArchiveReason,
		); err != nil {
			t.Fatalf("queryInboxByTrigger scan: %v", err)
		}
		it.Pinned = pinned != 0
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("queryInboxByTrigger rows: %v", err)
	}
	return items
}
