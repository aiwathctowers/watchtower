package dayplan

import (
	"context"
	"fmt"
	"strings"
)

// DetectConflicts compares plan timeblocks (source_type != 'calendar') against
// current calendar events for date, then updates has_conflicts + conflict_summary
// on the plan row. No-op if plan doesn't exist.
func (p *Pipeline) DetectConflicts(ctx context.Context, userID, date string) error {
	plan, err := p.db.GetDayPlan(userID, date)
	if err != nil {
		return fmt.Errorf("lookup plan: %w", err)
	}
	if plan == nil {
		return nil
	}

	items, err := p.db.GetDayPlanItems(plan.ID)
	if err != nil {
		return fmt.Errorf("load items: %w", err)
	}
	events, err := p.gatherCalendarEvents(date)
	if err != nil {
		return fmt.Errorf("gather events: %w", err)
	}

	var conflicts []string
	for _, it := range items {
		if it.Kind != "timeblock" {
			continue
		}
		if it.SourceType == "calendar" {
			continue
		}
		if !it.StartTime.Valid || !it.EndTime.Valid {
			continue
		}
		for _, ev := range events {
			evStart := parseEventTime(ev.StartTime)
			evEnd := parseEventTime(ev.EndTime)
			if evStart.IsZero() || evEnd.IsZero() {
				continue
			}
			if timesOverlap(it.StartTime.Time, it.EndTime.Time, evStart, evEnd) {
				conflicts = append(conflicts, fmt.Sprintf(
					"%s (%s–%s) overlaps %q (%s–%s)",
					it.Title,
					it.StartTime.Time.Local().Format("15:04"),
					it.EndTime.Time.Local().Format("15:04"),
					ev.Title,
					evStart.Local().Format("15:04"),
					evEnd.Local().Format("15:04"),
				))
			}
		}
	}

	summary := ""
	if len(conflicts) > 0 {
		summary = strings.Join(conflicts, "; ")
	}
	return p.db.SetHasConflicts(plan.ID, len(conflicts) > 0, summary)
}
