package dayplan

import (
	"context"
	"database/sql"
	"fmt"

	"watchtower/internal/db"
)

// syncCalendarItems diffs current calendar_events for date against existing
// day_plan_items with source_type='calendar' on planID: adds new, updates
// modified, removes orphans. Matches by event.ID == item.source_id.
func (p *Pipeline) syncCalendarItems(planID int64, date string, events []db.CalendarEvent) error {
	existing, err := p.db.GetDayPlanItems(planID)
	if err != nil {
		return fmt.Errorf("load existing items: %w", err)
	}

	existingByID := map[string]db.DayPlanItem{}
	for _, it := range existing {
		if it.SourceType == db.DayPlanItemSourceCalendar && it.SourceID.Valid {
			existingByID[it.SourceID.String] = it
		}
	}

	eventByID := map[string]db.CalendarEvent{}
	for _, e := range events {
		eventByID[e.ID] = e
	}

	// Delete orphans (existing calendar items whose event no longer exists in current list).
	for id, it := range existingByID {
		if _, ok := eventByID[id]; !ok {
			if err := p.db.DeleteDayPlanItem(it.ID); err != nil {
				return fmt.Errorf("delete orphan calendar item %d: %w", it.ID, err)
			}
		}
	}

	// Upsert each current event.
	for id, ev := range eventByID {
		start := parseEventTime(ev.StartTime)
		end := parseEventTime(ev.EndTime)
		if start.IsZero() || end.IsZero() {
			if p.logger != nil {
				p.logger.Printf("dayplan: skip event %q: unparseable start=%q end=%q", ev.ID, ev.StartTime, ev.EndTime)
			}
			continue
		}
		durationMin := int64(end.Sub(start).Minutes())

		if cur, ok := existingByID[id]; ok {
			// Update in-place.
			_, err := p.db.Exec(`
				UPDATE day_plan_items
				SET title = ?, description = ?, start_time = ?, end_time = ?, duration_min = ?,
				    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
				WHERE id = ?`,
				ev.Title,
				sql.NullString{String: ev.Description, Valid: ev.Description != ""},
				start.UTC().Format("2006-01-02T15:04:05Z"),
				end.UTC().Format("2006-01-02T15:04:05Z"),
				durationMin,
				cur.ID,
			)
			if err != nil {
				return fmt.Errorf("update calendar item %d: %w", cur.ID, err)
			}
		} else {
			// Insert new.
			it := db.DayPlanItem{
				DayPlanID:   planID,
				Kind:        db.DayPlanItemKindTimeblock,
				SourceType:  db.DayPlanItemSourceCalendar,
				SourceID:    sql.NullString{String: ev.ID, Valid: true},
				Title:       ev.Title,
				Description: sql.NullString{String: ev.Description, Valid: ev.Description != ""},
				StartTime:   sql.NullTime{Time: start, Valid: true},
				EndTime:     sql.NullTime{Time: end, Valid: true},
				DurationMin: sql.NullInt64{Int64: durationMin, Valid: true},
				Status:      db.DayPlanItemStatusPending,
				Tags:        "[]",
			}
			if err := p.db.CreateDayPlanItems(planID, []db.DayPlanItem{it}); err != nil {
				return fmt.Errorf("insert calendar item for %s: %w", ev.ID, err)
			}
		}
	}
	return nil
}

// SyncCalendarItemsForDate is the daemon-facing wrapper: looks up plan, gathers
// current events for date, then calls syncCalendarItems. No-ops if no plan.
func (p *Pipeline) SyncCalendarItemsForDate(ctx context.Context, userID, date string) error {
	plan, err := p.db.GetDayPlan(userID, date)
	if err != nil {
		return fmt.Errorf("lookup plan: %w", err)
	}
	if plan == nil {
		return nil
	}
	events, err := p.gatherCalendarEvents(date)
	if err != nil {
		return fmt.Errorf("gather calendar events: %w", err)
	}
	return p.syncCalendarItems(plan.ID, date, events)
}
