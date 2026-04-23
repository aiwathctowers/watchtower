package dayplan

import (
	"database/sql"
	"fmt"
	"time"

	"watchtower/internal/db"
)

// buildItems validates AI items and converts them into DayPlanItem records.
// Invalid items are dropped; reasons are returned in the `dropped` slice.
func buildItems(r *GenerateResult, date string, events []db.CalendarEvent,
	taskIDs, jiraKeys map[string]bool) ([]db.DayPlanItem, []string) {

	var out []db.DayPlanItem
	var dropped []string

	order := 0
	for _, ai := range r.Timeblocks {
		it, reason := aiToTimeblock(ai, date, events, taskIDs, jiraKeys)
		if reason != "" {
			dropped = append(dropped, reason)
			continue
		}
		it.OrderIndex = order
		order++
		out = append(out, *it)
	}

	order = 0
	for _, ai := range r.Backlog {
		it, reason := aiToBacklog(ai, taskIDs, jiraKeys)
		if reason != "" {
			dropped = append(dropped, reason)
			continue
		}
		it.OrderIndex = order
		order++
		out = append(out, *it)
	}

	return out, dropped
}

func aiToTimeblock(ai AIItem, date string, events []db.CalendarEvent,
	taskIDs, jiraKeys map[string]bool) (*db.DayPlanItem, string) {
	if ai.SourceType == "calendar" {
		return nil, "calendar items must not come from AI"
	}
	if ai.StartTimeLocal == "" || ai.EndTimeLocal == "" {
		return nil, fmt.Sprintf("timeblock %q missing start/end time", ai.Title)
	}
	if reason := validateSource(ai, taskIDs, jiraKeys); reason != "" {
		return nil, reason
	}

	start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+ai.StartTimeLocal, time.Local)
	if err != nil {
		return nil, fmt.Sprintf("parse start_time: %v", err)
	}
	end, err := time.ParseInLocation("2006-01-02 15:04", date+" "+ai.EndTimeLocal, time.Local)
	if err != nil {
		return nil, fmt.Sprintf("parse end_time: %v", err)
	}
	if !end.After(start) {
		return nil, fmt.Sprintf("timeblock %q end before start", ai.Title)
	}

	for _, ev := range events {
		evStart := parseEventTime(ev.StartTime)
		evEnd := parseEventTime(ev.EndTime)
		if evStart.IsZero() || evEnd.IsZero() {
			continue
		}
		if timesOverlap(start, end, evStart, evEnd) {
			return nil, fmt.Sprintf("timeblock %q overlaps calendar event %q", ai.Title, ev.Title)
		}
	}

	return &db.DayPlanItem{
		Kind:        db.DayPlanItemKindTimeblock,
		SourceType:  ai.SourceType,
		SourceID:    sourceIDToNullString(ai.SourceID),
		Title:       ai.Title,
		Description: strToNull(ai.Description),
		Rationale:   strToNull(ai.Rationale),
		StartTime:   sql.NullTime{Time: start, Valid: true},
		EndTime:     sql.NullTime{Time: end, Valid: true},
		DurationMin: sql.NullInt64{Int64: int64(end.Sub(start).Minutes()), Valid: true},
		Priority:    strToNull(ai.Priority),
		Status:      db.DayPlanItemStatusPending,
		Tags:        "[]",
	}, ""
}

func aiToBacklog(ai AIItem, taskIDs, jiraKeys map[string]bool) (*db.DayPlanItem, string) {
	if ai.SourceType == "calendar" {
		return nil, "calendar items must not come from AI"
	}
	if reason := validateSource(ai, taskIDs, jiraKeys); reason != "" {
		return nil, reason
	}
	return &db.DayPlanItem{
		Kind:        db.DayPlanItemKindBacklog,
		SourceType:  ai.SourceType,
		SourceID:    sourceIDToNullString(ai.SourceID),
		Title:       ai.Title,
		Description: strToNull(ai.Description),
		Rationale:   strToNull(ai.Rationale),
		Priority:    strToNull(ai.Priority),
		Status:      db.DayPlanItemStatusPending,
		Tags:        "[]",
	}, ""
}

func validateSource(ai AIItem, taskIDs, jiraKeys map[string]bool) string {
	switch ai.SourceType {
	case "task":
		id := sourceIDToString(ai.SourceID)
		if !taskIDs[id] {
			return fmt.Sprintf("unknown task source_id %q for %q", id, ai.Title)
		}
	case "jira":
		id := sourceIDToString(ai.SourceID)
		if !jiraKeys[id] {
			return fmt.Sprintf("unknown jira source_id %q for %q", id, ai.Title)
		}
	case "focus":
		// accept, source_id may be anything or null
	case "briefing_attention":
		if sourceIDToString(ai.SourceID) == "" {
			return fmt.Sprintf("briefing_attention item %q missing source_id", ai.Title)
		}
	default:
		return fmt.Sprintf("invalid source_type %q for %q", ai.SourceType, ai.Title)
	}
	return ""
}

func sourceIDToString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%d", int64(x))
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

func sourceIDToNullString(v any) sql.NullString {
	s := sourceIDToString(v)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func strToNull(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func timesOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}

// parseEventTime parses an ISO8601 event time string into a time.Time.
// Returns zero time on failure.
func parseEventTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
