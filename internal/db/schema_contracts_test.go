package db

import "testing"

// Behavioral contract guards that were originally tied to specific migration
// versions (V72, V73) before the goose consolidation merged all migrations
// into 00001_init.sql. The contracts still hold on the current schema; these
// tests assert the post-migration shape directly.

// TestInbox04_NoLegacyExplicitFeedbackTable guards INBOX-04: the legacy
// `explicit_feedback` table must not exist. Explicit feedback now lives as a
// `source` value in `inbox_learned_rules`.
func TestInbox04_NoLegacyExplicitFeedbackTable(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	if tableExists(t, database, "explicit_feedback") {
		t.Fatalf("explicit_feedback table must not exist; explicit feedback is recorded as inbox_learned_rules.source='explicit_feedback'")
	}
	if !tableExists(t, database, "inbox_learned_rules") {
		t.Fatalf("inbox_learned_rules table missing — explicit feedback has nowhere to land")
	}
}

// TestTracks06_TrackStatesTableExists guards TRACKS-06: the track_states
// table must exist with the columns the diffing logic depends on
// (track_id, created_at, source). Renaming or dropping these columns
// breaks the change-history audit trail.
func TestTracks06_TrackStatesTableExists(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	if !tableExists(t, database, "track_states") {
		t.Fatalf("track_states table missing — track diffing audit trail is broken")
	}
	cols := tableColumns(t, database, "track_states")
	for _, required := range []string{"track_id", "created_at", "source"} {
		if !cols[required] {
			t.Fatalf("track_states.%s column missing — required by diffing logic in tracks.go", required)
		}
	}
}
