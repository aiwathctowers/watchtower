package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"watchtower/internal/db"
)

// calAttendee is the JSON shape of each element in calendar_events.attendees.
type calAttendee struct {
	Email      string `json:"email"`
	RSVPStatus string `json:"rsvp_status"`
}

// calEventRow holds a single calendar_events row we care about.
type calEventRow struct {
	id          string
	title       string
	attendees   string
	eventStatus string
	syncedAt    string
	updatedAt   string
}

// DetectCalendar scans calendar_events for events that involve myEmail and were
// synced or updated after sinceTS. It creates inbox items for three trigger types:
//
//   - calendar_invite: newly synced event where the attendee RSVP status is "needsAction"
//   - calendar_time_change: event updated after it was first synced (rescheduled)
//   - calendar_cancelled: event whose event_status is "cancelled"
//
// Each event+trigger pair is deduplicated so repeated calls are idempotent.
func DetectCalendar(ctx context.Context, database *db.DB, myEmail string, sinceTS time.Time) (int, error) {
	if myEmail == "" {
		return 0, nil
	}

	sinceISO := sinceTS.UTC().Format(time.RFC3339)

	// Collect all candidate rows first, then close rows before issuing further queries.
	// This avoids a connection deadlock on SQLite with MaxOpenConns(1).
	rows, err := database.Query(`
		SELECT id, title, attendees, event_status, synced_at, updated_at
		FROM calendar_events
		WHERE synced_at > ? OR updated_at > ?`,
		sinceISO, sinceISO)
	if err != nil {
		return 0, fmt.Errorf("calendar_detector: query calendar_events: %w", err)
	}

	var events []calEventRow
	for rows.Next() {
		var e calEventRow
		if err := rows.Scan(&e.id, &e.title, &e.attendees, &e.eventStatus, &e.syncedAt, &e.updatedAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("calendar_detector: scan: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("calendar_detector: rows error: %w", err)
	}
	rows.Close()

	created := 0
	for _, e := range events {
		// Check attendee list for this user.
		var attendees []calAttendee
		_ = json.Unmarshal([]byte(e.attendees), &attendees)
		amIAttendee := false
		myRSVP := ""
		for _, a := range attendees {
			if a.Email == myEmail {
				amIAttendee = true
				myRSVP = a.RSVPStatus
				break
			}
		}
		if !amIAttendee {
			continue
		}

		// Determine trigger type. Cancelled takes priority.
		trig := ""
		switch {
		case e.eventStatus == "cancelled":
			trig = "calendar_cancelled"
		case e.syncedAt > sinceISO && myRSVP == "needsAction":
			// Newly arrived event that needs an RSVP response.
			trig = "calendar_invite"
		case e.updatedAt > e.syncedAt:
			// Event was modified after it was first synced — treat as a time/detail change.
			trig = "calendar_time_change"
		}
		if trig == "" {
			continue
		}

		// Dedup key: event ID as channel_id, updatedAt (or syncedAt) as message_ts.
		dedupeTS := e.updatedAt
		if dedupeTS == "" {
			dedupeTS = e.syncedAt
		}
		exists, err := calendarInboxItemExists(database, e.id, dedupeTS, trig)
		if err != nil {
			return created, fmt.Errorf("calendar_detector: dedup check: %w", err)
		}
		if exists {
			continue
		}

		item := db.InboxItem{
			ChannelID:    e.id,
			MessageTS:    dedupeTS,
			SenderUserID: e.id,
			TriggerType:  trig,
			Snippet:      e.title,
			ItemClass:    DefaultItemClass(trig),
			Status:       "pending",
			Priority:     "medium",
		}
		if _, err := database.CreateInboxItem(item); err != nil {
			return created, fmt.Errorf("calendar_detector: create inbox item for event %s: %w", e.id, err)
		}
		created++
	}
	return created, nil
}

// calendarInboxItemExists returns true if an inbox item already exists for the
// given event ID (stored as channel_id), timestamp (message_ts), and trigger type.
// This is inlined to avoid symbol collisions with other detector packages.
func calendarInboxItemExists(database *db.DB, eventID, messageTS, triggerType string) (bool, error) {
	var count int
	err := database.QueryRow(`
		SELECT COUNT(*) FROM inbox_items
		WHERE channel_id = ? AND message_ts = ? AND trigger_type = ?`,
		eventID, messageTS, triggerType).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
