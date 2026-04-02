// Package calendar provides Google Calendar integration for Watchtower.
package calendar

import "time"

// CalendarEvent represents a Google Calendar event (internal model).
type CalendarEvent struct {
	ID             string    // Google event ID
	Title          string    // event summary (sanitized, no body)
	Description    string    // event description (trimmed)
	StartTime      time.Time // UTC
	EndTime        time.Time // UTC
	IsAllDay       bool
	Location       string // optional
	Organizer      string // email
	Attendees      []Attendee
	ResponseStatus string // user's response: "accepted", "tentative", "declined", "needsAction"
	EventStatus    string // event lifecycle: "confirmed", "tentative", "cancelled"
	Recurring      bool
	CalendarID     string
	HTMLLink       string
	EventType      string // e.g. "default", "focusTime", "outOfOffice"
	UpdatedAt      string // ISO8601 from Google API
	RawJSON        string // original JSON (populated during sync)
}

// Attendee represents a calendar event attendee.
type Attendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"display_name"`
	ResponseStatus string `json:"response_status"` // accepted/declined/tentative/needsAction
	SlackUserID    string `json:"slack_user_id"`   // resolved via email→users.email lookup
}

// CalendarInfo represents a visible Google Calendar.
type CalendarInfo struct {
	ID       string
	Summary  string
	Primary  bool
	Color    string // hex color from Google
	Selected bool   // user-configured in settings
}
