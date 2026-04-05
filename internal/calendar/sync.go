package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// Syncer fetches calendar events and stores them in the database.
type Syncer struct {
	client *Client
	db     *db.DB
	cfg    *config.Config
	logger *log.Logger
}

// NewSyncer creates a calendar syncer.
// If logger is nil, a no-op logger is used.
func NewSyncer(client *Client, database *db.DB, cfg *config.Config, logger *log.Logger) *Syncer {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Syncer{
		client: client,
		db:     database,
		cfg:    cfg,
		logger: logger,
	}
}

// Sync fetches calendars and events, upserts them to DB, and cleans up stale data.
// Returns the count of new/updated events.
func (s *Syncer) Sync(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	syncedAt := now.Format(time.RFC3339)
	timeMin := now.Add(-24 * time.Hour) // past 1 day

	daysAhead := s.cfg.Calendar.SyncDaysAhead
	if daysAhead <= 0 {
		daysAhead = config.DefaultCalendarSyncDaysAhead
	}
	timeMax := now.Add(time.Duration(daysAhead) * 24 * time.Hour)

	// Sync calendar list first.
	calInfos, err := s.client.FetchCalendars(ctx)
	if err != nil {
		s.logger.Printf("calendar: failed to fetch calendar list: %v", err)
		// Continue with selected calendars from config if available.
	} else {
		for _, ci := range calInfos {
			cal := db.CalendarCalendar{
				ID:         ci.ID,
				Name:       ci.Summary,
				IsPrimary:  ci.Primary,
				IsSelected: true,
				Color:      ci.Color,
				SyncedAt:   syncedAt,
			}
			if err := s.db.UpsertCalendar(cal); err != nil {
				s.logger.Printf("calendar: failed to upsert calendar %s: %v", ci.ID, err)
			}
		}
	}

	// Determine which calendars to sync.
	calendarIDs := s.cfg.Calendar.SelectedCalendars
	if len(calendarIDs) == 0 {
		// Use selected calendars from DB.
		dbIDs, err := s.db.GetSelectedCalendarIDs()
		if err != nil {
			s.logger.Printf("calendar: failed to get selected calendars from DB, falling back to primary: %v", err)
			calendarIDs = []string{"primary"}
		} else if len(dbIDs) == 0 {
			calendarIDs = []string{"primary"}
		} else {
			calendarIDs = dbIDs
		}
	}

	events, err := s.client.FetchEvents(ctx, calendarIDs, timeMin, timeMax)
	if err != nil {
		return 0, fmt.Errorf("fetching calendar events: %w", err)
	}

	// Resolve attendee emails to Slack user IDs.
	events = s.ResolveAttendees(events)

	count := 0
	for _, e := range events {
		attendeesJSON, err := json.Marshal(e.Attendees)
		if err != nil {
			attendeesJSON = []byte("[]")
		}

		rawJSON := e.RawJSON
		if rawJSON == "" {
			rawJSON = "{}"
		}

		dbEvent := db.CalendarEvent{
			ID:             e.ID,
			CalendarID:     e.CalendarID,
			Title:          e.Title,
			Description:    e.Description,
			Location:       e.Location,
			StartTime:      e.StartTime.Format(time.RFC3339),
			EndTime:        e.EndTime.Format(time.RFC3339),
			OrganizerEmail: e.Organizer,
			Attendees:      string(attendeesJSON),
			IsRecurring:    e.Recurring,
			IsAllDay:       e.IsAllDay,
			EventStatus:    e.EventStatus,
			EventType:      e.EventType,
			HTMLLink:       e.HTMLLink,
			RawJSON:        rawJSON,
			UpdatedAt:      e.UpdatedAt,
		}

		if err := s.db.UpsertCalendarEvent(dbEvent, syncedAt); err != nil {
			s.logger.Printf("calendar: failed to upsert event %s: %v", e.ID, err)
			continue
		}
		count++
	}

	// Cleanup stale events per calendar (synced before this run).
	for _, calID := range calendarIDs {
		if n, err := s.db.DeleteStaleCalendarEvents(calID, syncedAt); err != nil {
			s.logger.Printf("calendar: failed to cleanup stale events for %s: %v", calID, err)
		} else if n > 0 {
			s.logger.Printf("calendar: removed %d stale events from %s", n, calID)
		}
	}

	return count, nil
}

// ResolveAttendees matches attendee emails to Slack user_ids via the users table
// and caches the mapping in calendar_attendee_map.
func (s *Syncer) ResolveAttendees(events []CalendarEvent) []CalendarEvent {
	for i, e := range events {
		for j, a := range e.Attendees {
			if a.Email == "" {
				continue
			}
			// Check cache first.
			if uid, err := s.db.GetSlackUserIDByEmail(a.Email); err == nil && uid != "" {
				events[i].Attendees[j].SlackUserID = uid
				continue
			}
			// Look up in users table.
			user, err := s.db.GetUserByEmail(a.Email)
			if err == nil && user != nil {
				events[i].Attendees[j].SlackUserID = user.ID
				// Cache the mapping.
				_ = s.db.UpsertAttendeeMap(a.Email, user.ID)
			}
		}
	}
	return events
}
