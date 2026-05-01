package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotifyDueTargets_OverdueSurfaces verifies that a target whose due_date
// is in the past produces exactly one inbox_items row with trigger_type
// 'target_due', and that the target's notified_at is stamped so subsequent
// calls do not duplicate the surface.
func TestNotifyDueTargets_OverdueSurfaces(t *testing.T) {
	db := openTestDB(t)

	overdue := makeTarget("Send Q1 review", "todo", "high")
	overdue.DueDate = "2020-01-01T12:00"
	id, err := db.CreateTarget(overdue)
	require.NoError(t, err)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n, err := db.NotifyDueTargets(now)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var trigger string
	var targetID sql.NullInt64
	var snippet, priority string
	err = db.QueryRow(`
		SELECT trigger_type, target_id, snippet, priority
		FROM inbox_items WHERE target_id = ?`, id).
		Scan(&trigger, &targetID, &snippet, &priority)
	require.NoError(t, err)
	assert.Equal(t, "target_due", trigger)
	assert.True(t, targetID.Valid)
	assert.Equal(t, id, targetID.Int64)
	assert.Equal(t, "Send Q1 review", snippet)
	assert.Equal(t, "high", priority)

	// notified_at is stamped on the target.
	var notifiedAt string
	err = db.QueryRow(`SELECT notified_at FROM targets WHERE id = ?`, id).Scan(&notifiedAt)
	require.NoError(t, err)
	assert.NotEmpty(t, notifiedAt)

	// Second call is a no-op (idempotent).
	n2, err := db.NotifyDueTargets(now)
	require.NoError(t, err)
	assert.Equal(t, 0, n2)
}

// TestNotifyDueTargets_FutureDueIgnored verifies that targets whose due_date
// has not yet passed are not surfaced.
func TestNotifyDueTargets_FutureDueIgnored(t *testing.T) {
	db := openTestDB(t)

	future := makeTarget("Plan 2099", "todo", "medium")
	future.DueDate = "2099-12-31T12:00"
	_, err := db.CreateTarget(future)
	require.NoError(t, err)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n, err := db.NotifyDueTargets(now)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM inbox_items WHERE trigger_type='target_due'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// TestNotifyDueTargets_ClosedStatusesSkipped verifies done/dismissed targets
// are not surfaced even if overdue — closing the target ends the reminder.
func TestNotifyDueTargets_ClosedStatusesSkipped(t *testing.T) {
	db := openTestDB(t)

	for _, status := range []string{"done", "dismissed"} {
		tgt := makeTarget("Closed "+status, status, "medium")
		tgt.DueDate = "2020-01-01T12:00"
		_, err := db.CreateTarget(tgt)
		require.NoError(t, err)
	}

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n, err := db.NotifyDueTargets(now)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestInbox02_AutoResolveTargetOnClose extends INBOX-02 to the target_due
// trigger family — closing the underlying target (status → done/dismissed)
// resolves any pending target_due inbox item that points to it, so the user
// never closes the same thing twice. See docs/inventory/inbox-pulse.md.
func TestInbox02_AutoResolveTargetOnClose(t *testing.T) {
	for _, closeStatus := range []string{"done", "dismissed"} {
		t.Run(closeStatus, func(t *testing.T) {
			db := openTestDB(t)

			tgt := makeTarget("Auto-close me", "todo", "high")
			tgt.DueDate = "2020-01-01T12:00"
			id, err := db.CreateTarget(tgt)
			require.NoError(t, err)

			now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
			_, err = db.NotifyDueTargets(now)
			require.NoError(t, err)

			// Pre-condition: inbox item is pending.
			var status string
			require.NoError(t,
				db.QueryRow(`SELECT status FROM inbox_items WHERE target_id = ?`, id).Scan(&status))
			assert.Equal(t, "pending", status)

			// Close the target.
			require.NoError(t, db.UpdateTargetStatus(int(id), closeStatus))

			// Post-condition: inbox item auto-resolved.
			require.NoError(t,
				db.QueryRow(`SELECT status FROM inbox_items WHERE target_id = ?`, id).Scan(&status))
			assert.Equal(t, "resolved", status)
		})
	}
}

// TestNotifyDueTargets_NoDueDate verifies targets without a due_date are
// never surfaced.
func TestNotifyDueTargets_NoDueDate(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateTarget(makeTarget("No deadline", "todo", "high"))
	require.NoError(t, err)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	n, err := db.NotifyDueTargets(now)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
