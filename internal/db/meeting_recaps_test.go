package db

import (
	"testing"
)

func TestMeetingRecapUpsertAndGet(t *testing.T) {
	database := openTestDB(t)

	// Seed calendar_calendars (required by FK chain: meeting_recaps -> calendar_events -> calendar_calendars)
	if _, err := database.Exec(`INSERT INTO calendar_calendars (id, name) VALUES ('cal-1', 'Test Calendar')`); err != nil {
		t.Fatalf("seeding calendar: %v", err)
	}

	// Need a calendar event to satisfy FK
	if _, err := database.Exec(`INSERT INTO calendar_events (id, calendar_id, title, start_time, end_time)
		VALUES ('evt-1', 'cal-1', 'Test event', '2026-04-27T10:00:00Z', '2026-04-27T11:00:00Z')`); err != nil {
		t.Fatalf("seeding event: %v", err)
	}

	if err := database.UpsertMeetingRecap("evt-1", "raw notes here", `{"summary":"x"}`); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	got, err := database.GetMeetingRecap("evt-1")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got == nil {
		t.Fatal("expected recap, got nil")
	}
	if got.SourceText != "raw notes here" {
		t.Errorf("source_text = %q, want %q", got.SourceText, "raw notes here")
	}
	if got.RecapJSON != `{"summary":"x"}` {
		t.Errorf("recap_json = %q, want %q", got.RecapJSON, `{"summary":"x"}`)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Error("timestamps must be set")
	}

	// Idempotent re-upsert overrides
	if err := database.UpsertMeetingRecap("evt-1", "edited", `{"summary":"y"}`); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got2, _ := database.GetMeetingRecap("evt-1")
	if got2.SourceText != "edited" || got2.RecapJSON != `{"summary":"y"}` {
		t.Errorf("re-upsert failed: %+v", got2)
	}
}

func TestMeetingRecapGetMissing(t *testing.T) {
	database := openTestDB(t)

	got, err := database.GetMeetingRecap("nope")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing event, got %+v", got)
	}
}

func TestMeetingRecapCascadeDelete(t *testing.T) {
	database := openTestDB(t)

	if _, err := database.Exec(`INSERT INTO calendar_calendars (id, name) VALUES ('cal-1', 'Test Calendar')`); err != nil {
		t.Fatalf("seeding calendar: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO calendar_events (id, calendar_id, title, start_time, end_time)
		VALUES ('evt-2', 'cal-1', 't', '2026-04-27T10:00:00Z', '2026-04-27T11:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMeetingRecap("evt-2", "x", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM calendar_events WHERE id='evt-2'`); err != nil {
		t.Fatal(err)
	}

	got, _ := database.GetMeetingRecap("evt-2")
	if got != nil {
		t.Errorf("expected recap to be cascade-deleted, got %+v", got)
	}
}

func TestGetMeetingNotesForEvent(t *testing.T) {
	database := openTestDB(t)

	// meeting_notes has no FK constraint on event_id, so no calendar seeding needed
	inserts := []struct {
		typ  string
		text string
		ord  int
	}{
		{"question", "topic A", 0},
		{"note", "freeform B", 0},
		{"question", "topic C", 1},
	}
	for _, ins := range inserts {
		if _, err := database.Exec(`INSERT INTO meeting_notes (event_id, type, text, sort_order)
			VALUES ('evt-3', ?, ?, ?)`, ins.typ, ins.text, ins.ord); err != nil {
			t.Fatal(err)
		}
	}

	notes, err := database.GetMeetingNotesForEvent("evt-3")
	if err != nil {
		t.Fatalf("get notes: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(notes))
	}
	// Assert all texts present
	seen := map[string]bool{}
	for _, n := range notes {
		seen[n.Text] = true
	}
	for _, want := range []string{"topic A", "topic C", "freeform B"} {
		if !seen[want] {
			t.Errorf("missing note %q", want)
		}
	}
}
