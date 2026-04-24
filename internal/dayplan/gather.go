package dayplan

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"watchtower/internal/briefing"
	"watchtower/internal/db"
)

// gatherTargets returns active targets (todo, in_progress, blocked), ordered by priority.
func (p *Pipeline) gatherTargets() ([]db.Target, error) {
	rows, err := p.db.Query(`SELECT id, text, intent, level, custom_label, period_start, period_end,
		parent_id, status, priority, ownership,
		ball_on, due_date, snooze_until, blocking, tags, sub_items, notes,
		progress, source_type, source_id, ai_level_confidence, created_at, updated_at
		FROM targets
		WHERE status IN ('todo', 'in_progress', 'blocked')
		ORDER BY
			CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 END,
			CASE WHEN due_date = '' THEN 1 ELSE 0 END,
			due_date ASC,
			created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying active targets: %w", err)
	}
	defer rows.Close()

	var targets []db.Target
	for rows.Next() {
		var t db.Target
		if err := rows.Scan(
			&t.ID, &t.Text, &t.Intent, &t.Level, &t.CustomLabel, &t.PeriodStart, &t.PeriodEnd,
			&t.ParentID, &t.Status, &t.Priority, &t.Ownership,
			&t.BallOn, &t.DueDate, &t.SnoozeUntil, &t.Blocking, &t.Tags, &t.SubItems, &t.Notes,
			&t.Progress, &t.SourceType, &t.SourceID, &t.AILevelConfidence, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning target: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// gatherCalendarEvents returns all calendar events occurring on the given date (YYYY-MM-DD).
func (p *Pipeline) gatherCalendarEvents(date string) ([]db.CalendarEvent, error) {
	events, err := p.db.GetCalendarEventsForDate(date)
	if err != nil {
		return nil, fmt.Errorf("querying calendar events for %s: %w", date, err)
	}
	return events, nil
}

// gatherBriefing returns today's briefing for the user, falling back to yesterday's.
// Returns nil if neither exists (not an error).
func (p *Pipeline) gatherBriefing(userID, date string) *db.Briefing {
	b, err := p.db.GetBriefing(userID, date)
	if err == nil && b != nil {
		return b
	}

	// Try yesterday.
	t, err2 := time.Parse("2006-01-02", date)
	if err2 != nil {
		return nil
	}
	yesterday := t.AddDate(0, 0, -1).Format("2006-01-02")
	b, err = p.db.GetBriefing(userID, yesterday)
	if err == nil && b != nil {
		return b
	}
	return nil
}

// gatherJira returns active Jira issues assigned to the user.
// Errors are logged and an empty slice is returned (graceful degradation).
func (p *Pipeline) gatherJira(userID string) []db.JiraIssue {
	issues, err := p.db.GetJiraIssuesByAssigneeSlackID(userID)
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("dayplan: gatherJira: %v", err)
		}
		return nil
	}
	return issues
}

// gatherPeople returns the latest active people cards.
// Errors are logged and an empty slice is returned (graceful degradation).
func (p *Pipeline) gatherPeople() []db.PeopleCard {
	cards, err := p.db.GetPeopleCards(db.PeopleCardFilter{Limit: 50})
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("dayplan: gatherPeople: %v", err)
		}
		return nil
	}
	// Filter to active / ready cards only.
	var active []db.PeopleCard
	for _, c := range cards {
		if c.Status == "active" || c.Status == "ready" {
			active = append(active, c)
		}
	}
	return active
}

// gatherManualItems returns only manually added items for an existing plan.
func (p *Pipeline) gatherManualItems(planID int64) ([]db.DayPlanItem, error) {
	all, err := p.db.GetDayPlanItems(planID)
	if err != nil {
		return nil, fmt.Errorf("getting plan items for %d: %w", planID, err)
	}
	var manual []db.DayPlanItem
	for _, it := range all {
		if it.SourceType == "manual" {
			manual = append(manual, it)
		}
	}
	return manual, nil
}

// gatherPreviousPlan returns the most recent day plan for the user before the given date.
// Returns nil if none found.
func (p *Pipeline) gatherPreviousPlan(userID, date string) *db.DayPlan {
	plans, err := p.db.ListDayPlans(userID, 10)
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("dayplan: gatherPreviousPlan: %v", err)
		}
		return nil
	}
	for i := range plans {
		if plans[i].PlanDate < date {
			return &plans[i]
		}
	}
	return nil
}

// formatBriefingContext formats the attention and coaching sections of a briefing
// into a human-readable string for injection into the day plan prompt.
// Returns "(none)" if the briefing is nil or both sections are empty.
func formatBriefingContext(b *db.Briefing) string {
	if b == nil {
		return "(none)"
	}

	var attn []briefing.AttentionItem
	if b.Attention != "" && b.Attention != "null" {
		_ = json.Unmarshal([]byte(b.Attention), &attn)
	}

	var coaching []briefing.CoachingItem
	if b.Coaching != "" && b.Coaching != "null" {
		_ = json.Unmarshal([]byte(b.Coaching), &coaching)
	}

	if len(attn) == 0 && len(coaching) == 0 {
		return "(none)"
	}

	var sb strings.Builder
	sb.WriteString("Attention items:\n")
	for _, a := range attn {
		fmt.Fprintf(&sb, "- [%s:%s] %s (priority: %s; reason: %s)\n",
			a.SourceType, a.SourceID, a.Text, a.Priority, a.Reason)
	}
	sb.WriteString("Coaching hints:\n")
	for _, c := range coaching {
		fmt.Fprintf(&sb, "- %s\n", c.Text)
	}
	return strings.TrimRight(sb.String(), "\n")
}
