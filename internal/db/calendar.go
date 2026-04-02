package db

import (
	"fmt"
	"time"
)

// UpsertCalendar inserts or updates a Google Calendar.
func (db *DB) UpsertCalendar(cal CalendarCalendar) error {
	_, err := db.Exec(`INSERT INTO calendar_calendars (id, name, is_primary, is_selected, color, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, is_primary=excluded.is_primary, color=excluded.color, synced_at=excluded.synced_at`,
		cal.ID, cal.Name, cal.IsPrimary, cal.IsSelected, cal.Color, cal.SyncedAt)
	if err != nil {
		return fmt.Errorf("upserting calendar %s: %w", cal.ID, err)
	}
	return nil
}

// GetCalendars returns all synced calendars.
func (db *DB) GetCalendars() ([]CalendarCalendar, error) {
	rows, err := db.Query(`SELECT id, name, is_primary, is_selected, color, synced_at FROM calendar_calendars ORDER BY is_primary DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("querying calendars: %w", err)
	}
	defer rows.Close()

	var cals []CalendarCalendar
	for rows.Next() {
		var c CalendarCalendar
		if err := rows.Scan(&c.ID, &c.Name, &c.IsPrimary, &c.IsSelected, &c.Color, &c.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning calendar: %w", err)
		}
		cals = append(cals, c)
	}
	return cals, rows.Err()
}

// GetSelectedCalendarIDs returns IDs of calendars marked as selected.
func (db *DB) GetSelectedCalendarIDs() ([]string, error) {
	rows, err := db.Query(`SELECT id FROM calendar_calendars WHERE is_selected = 1`)
	if err != nil {
		return nil, fmt.Errorf("querying selected calendars: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning calendar id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetCalendarSelected updates the is_selected flag for a calendar.
func (db *DB) SetCalendarSelected(id string, selected bool) error {
	_, err := db.Exec(`UPDATE calendar_calendars SET is_selected = ? WHERE id = ?`, selected, id)
	if err != nil {
		return fmt.Errorf("setting calendar %s selected=%v: %w", id, selected, err)
	}
	return nil
}

// UpsertCalendarEvent inserts or replaces a calendar event.
func (db *DB) UpsertCalendarEvent(ev CalendarEvent) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO calendar_events
		(id, calendar_id, title, description, location, start_time, end_time,
		 organizer_email, attendees, is_recurring, is_all_day, event_status,
		 event_type, html_link, raw_json, synced_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ','now'), ?)`,
		ev.ID, ev.CalendarID, ev.Title, ev.Description, ev.Location,
		ev.StartTime, ev.EndTime, ev.OrganizerEmail, ev.Attendees,
		ev.IsRecurring, ev.IsAllDay, ev.EventStatus, ev.EventType,
		ev.HTMLLink, ev.RawJSON, ev.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting calendar event %s: %w", ev.ID, err)
	}
	return nil
}

// UpsertCalendarEvents inserts or replaces multiple calendar events.
func (db *DB) UpsertCalendarEvents(events []CalendarEvent) error {
	for _, ev := range events {
		if err := db.UpsertCalendarEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

// GetCalendarEvents returns events matching the filter.
func (db *DB) GetCalendarEvents(filter CalendarEventFilter) ([]CalendarEvent, error) {
	query := `SELECT id, calendar_id, title, description, location, start_time, end_time,
		organizer_email, attendees, is_recurring, is_all_day, event_status,
		event_type, html_link, raw_json, synced_at, updated_at
		FROM calendar_events WHERE 1=1`
	var args []any

	if filter.CalendarID != "" {
		query += ` AND calendar_id = ?`
		args = append(args, filter.CalendarID)
	}
	if filter.FromTime != "" {
		query += ` AND end_time >= ?`
		args = append(args, filter.FromTime)
	}
	if filter.ToTime != "" {
		query += ` AND start_time <= ?`
		args = append(args, filter.ToTime)
	}
	query += ` ORDER BY start_time`
	if filter.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, filter.Limit)
	}

	return db.queryCalendarEvents(query, args...)
}

// GetCalendarEventsForDate returns all events on a given date (YYYY-MM-DD).
func (db *DB) GetCalendarEventsForDate(date string) ([]CalendarEvent, error) {
	from := date + "T00:00:00Z"
	to := date + "T23:59:59Z"
	return db.GetCalendarEvents(CalendarEventFilter{FromTime: from, ToTime: to})
}

// GetCalendarEventByID returns a single event by its Google ID.
func (db *DB) GetCalendarEventByID(id string) (*CalendarEvent, error) {
	query := `SELECT id, calendar_id, title, description, location, start_time, end_time,
		organizer_email, attendees, is_recurring, is_all_day, event_status,
		event_type, html_link, raw_json, synced_at, updated_at
		FROM calendar_events WHERE id = ?`
	events, err := db.queryCalendarEvents(query, id)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	return &events[0], nil
}

// GetNextEvent returns the next upcoming event from now.
func (db *DB) GetNextEvent() (*CalendarEvent, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	events, err := db.GetCalendarEvents(CalendarEventFilter{FromTime: now, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	return &events[0], nil
}

// DeleteStaleCalendarEvents removes events for a calendar synced before the given timestamp.
func (db *DB) DeleteStaleCalendarEvents(calendarID string, beforeSyncedAt string) (int, error) {
	result, err := db.Exec(`DELETE FROM calendar_events WHERE calendar_id = ? AND synced_at < ?`, calendarID, beforeSyncedAt)
	if err != nil {
		return 0, fmt.Errorf("deleting stale calendar events: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ClearCalendarEvents removes all calendar data (used on disconnect).
func (db *DB) ClearCalendarEvents() error {
	if _, err := db.Exec(`DELETE FROM calendar_events`); err != nil {
		return fmt.Errorf("clearing calendar events: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM calendar_calendars`); err != nil {
		return fmt.Errorf("clearing calendars: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM calendar_attendee_map`); err != nil {
		return fmt.Errorf("clearing attendee map: %w", err)
	}
	return nil
}

// UpsertAttendeeMap caches an email to slack_user_id mapping.
func (db *DB) UpsertAttendeeMap(email, slackUserID string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO calendar_attendee_map (email, slack_user_id, resolved_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%SZ','now'))`, email, slackUserID)
	if err != nil {
		return fmt.Errorf("upserting attendee map for %s: %w", email, err)
	}
	return nil
}

// GetAttendeeMap returns the full email to slack_user_id cache.
func (db *DB) GetAttendeeMap() (map[string]string, error) {
	rows, err := db.Query(`SELECT email, slack_user_id FROM calendar_attendee_map WHERE slack_user_id != ''`)
	if err != nil {
		return nil, fmt.Errorf("querying attendee map: %w", err)
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var email, uid string
		if err := rows.Scan(&email, &uid); err != nil {
			return nil, fmt.Errorf("scanning attendee map: %w", err)
		}
		m[email] = uid
	}
	return m, rows.Err()
}

// GetSlackUserIDByEmail looks up a cached Slack user ID for an email.
func (db *DB) GetSlackUserIDByEmail(email string) (string, error) {
	var uid string
	err := db.QueryRow(`SELECT slack_user_id FROM calendar_attendee_map WHERE email = ?`, email).Scan(&uid)
	if err != nil {
		return "", err
	}
	return uid, nil
}

func (db *DB) queryCalendarEvents(query string, args ...any) ([]CalendarEvent, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying calendar events: %w", err)
	}
	defer rows.Close()

	var events []CalendarEvent
	for rows.Next() {
		var e CalendarEvent
		if err := rows.Scan(&e.ID, &e.CalendarID, &e.Title, &e.Description, &e.Location,
			&e.StartTime, &e.EndTime, &e.OrganizerEmail, &e.Attendees,
			&e.IsRecurring, &e.IsAllDay, &e.EventStatus, &e.EventType,
			&e.HTMLLink, &e.RawJSON, &e.SyncedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning calendar event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
