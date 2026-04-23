package dayplan

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// Pipeline orchestrates day-plan generation and persistence.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store
}

// New constructs a Pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{db: database, cfg: cfg, generator: gen, logger: logger}
}

// SetPromptStore wires an optional customisable prompt store.
func (p *Pipeline) SetPromptStore(store *prompts.Store) { p.promptStore = store }

// Run generates or regenerates the day plan for the target date.
// It is idempotent by default: if a plan already exists and neither Force nor
// Feedback is set it returns the existing plan immediately.
func (p *Pipeline) Run(ctx context.Context, opts RunOptions) (*db.DayPlan, error) {
	if !p.cfg.DayPlan.Enabled {
		return nil, nil
	}
	if opts.Date == "" {
		opts.Date = time.Now().Format("2006-01-02")
	}

	existing, err := p.db.GetDayPlan(opts.UserID, opts.Date)
	if err != nil {
		return nil, fmt.Errorf("lookup plan: %w", err)
	}

	// Short-circuit: plan exists and caller does not want regeneration.
	if existing != nil && !opts.Force && opts.Feedback == "" {
		if p.logger != nil {
			p.logger.Printf("dayplan: plan for %s already exists, skipping", opts.Date)
		}
		return existing, nil
	}

	// ── gather context ────────────────────────────────────────────────────────

	tasks, _ := p.gatherTasks()
	events, _ := p.gatherCalendarEvents(opts.Date)
	briefingData := p.gatherBriefing(opts.UserID, opts.Date)
	jiraIssues := p.gatherJira(opts.UserID)
	people := p.gatherPeople()
	prev := p.gatherPreviousPlan(opts.UserID, opts.Date)

	var prevItems []db.DayPlanItem
	if prev != nil {
		prevItems, _ = p.db.GetDayPlanItems(prev.ID)
	}

	var manual []db.DayPlanItem
	if existing != nil {
		manual, _ = p.gatherManualItems(existing.ID)
	}

	// ── build and call AI ─────────────────────────────────────────────────────

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	inputs := &promptInputs{
		Date:               opts.Date,
		Weekday:            dayOfWeek(opts.Date),
		NowLocal:           now.Format("15:04"),
		UserRole:           p.userRole(opts.UserID),
		WorkingHoursStart:  p.cfg.DayPlan.WorkingHoursStart,
		WorkingHoursEnd:    p.cfg.DayPlan.WorkingHoursEnd,
		CalendarEvents:     formatCalendarSection(events),
		Tasks:              formatTasksSection(tasks),
		Briefing:           formatBriefingContext(briefingData),
		Jira:               formatJiraSection(jiraIssues),
		People:             formatPeopleSection(people),
		Manual:             formatManualSection(manual),
		Previous:           formatPreviousPlanSection(prev, prevItems),
		Feedback:           feedbackOrInitial(opts.Feedback),
	}

	systemPrompt, promptVer := p.buildPrompt(inputs)

	resp, _, _, err := p.generator.Generate(
		digest.WithSource(ctx, "day_plan.generate"),
		systemPrompt, "Generate the day plan.", "")
	if err != nil {
		return nil, fmt.Errorf("ai generate: %w", err)
	}

	// ── parse and validate ────────────────────────────────────────────────────

	parsed, err := parseResponse(resp)
	if err != nil {
		return nil, err
	}

	newItems, dropped := buildItems(parsed, opts.Date, events, tasksIDSet(tasks), jiraKeySet(jiraIssues))
	if p.logger != nil {
		for _, d := range dropped {
			p.logger.Printf("dayplan: dropped item: %s", d)
		}
	}

	// ── persist atomically ────────────────────────────────────────────────────

	var briefingID sql.NullInt64
	if briefingData != nil {
		briefingID = sql.NullInt64{Int64: int64(briefingData.ID), Valid: true}
	}

	planRow := &db.DayPlan{
		UserID:          opts.UserID,
		PlanDate:        opts.Date,
		Status:          "active",
		GeneratedAt:     now,
		PromptVersion:   sql.NullString{String: promptVer, Valid: true},
		BriefingID:      briefingID,
		FeedbackHistory: "[]",
	}
	if existing != nil {
		planRow.ID = existing.ID
		planRow.RegenerateCount = existing.RegenerateCount
		planRow.FeedbackHistory = existing.FeedbackHistory
	}

	planID, err := p.db.UpsertDayPlan(planRow)
	if err != nil {
		return nil, fmt.Errorf("upsert plan: %w", err)
	}

	if err := p.db.ReplaceAIItems(planID, newItems); err != nil {
		return nil, fmt.Errorf("replace AI items: %w", err)
	}

	// syncCalendarItems is implemented in T10; stub here returns nil.
	if err := p.syncCalendarItems(planID, opts.Date, events); err != nil {
		return nil, fmt.Errorf("sync calendar items: %w", err)
	}

	// Increment regenerate count when this is a regeneration (with feedback or forced).
	if opts.Feedback != "" || (existing != nil && opts.Force) {
		_ = p.db.IncrementRegenerateCount(planID, opts.Feedback)
	}

	// DetectConflicts is implemented in T11; stub here is a no-op.
	_ = p.DetectConflicts(ctx, opts.UserID, opts.Date)

	return p.db.GetDayPlanByID(planID)
}

// ── buildItems (minimal impl — full validation in T9) ─────────────────────────

// buildItems converts the AI GenerateResult into DayPlanItem rows, dropping
// timeblocks that overlap calendar events or are missing times.
// Backlog items are accepted as-is.
//
// TODO(T9): add full source validation (taskIDs, jiraKeys), 15-min grid snap,
// duration clamping, and MaxTimeblocks / MinBacklog / MaxBacklog enforcement.
func buildItems(r *GenerateResult, date string, events []db.CalendarEvent,
	_ map[string]bool, _ map[string]bool) ([]db.DayPlanItem, []string) {

	var items []db.DayPlanItem
	var dropped []string

	// ── timeblocks ────────────────────────────────────────────────────────────
	for _, ai := range r.Timeblocks {
		if ai.StartTimeLocal == "" || ai.EndTimeLocal == "" {
			dropped = append(dropped, fmt.Sprintf("timeblock %q: missing start/end time", ai.Title))
			continue
		}

		startT, err1 := parseLocalHHMM(date, ai.StartTimeLocal)
		endT, err2 := parseLocalHHMM(date, ai.EndTimeLocal)
		if err1 != nil || err2 != nil {
			dropped = append(dropped, fmt.Sprintf("timeblock %q: bad time format", ai.Title))
			continue
		}

		if overlapsAny(startT, endT, events) {
			dropped = append(dropped, fmt.Sprintf("timeblock %q %s–%s overlaps calendar",
				ai.Title, ai.StartTimeLocal, ai.EndTimeLocal))
			continue
		}

		dur := int64(endT.Sub(startT).Minutes())

		sourceID := aiSourceID(ai.SourceID)
		items = append(items, db.DayPlanItem{
			Kind:        "timeblock",
			SourceType:  ai.SourceType,
			SourceID:    sourceID,
			Title:       ai.Title,
			Description: sql.NullString{Valid: ai.Description != "", String: ai.Description},
			Rationale:   sql.NullString{Valid: ai.Rationale != "", String: ai.Rationale},
			StartTime:   sql.NullTime{Valid: true, Time: startT},
			EndTime:     sql.NullTime{Valid: true, Time: endT},
			DurationMin: sql.NullInt64{Valid: true, Int64: dur},
			Priority:    sql.NullString{Valid: ai.Priority != "", String: ai.Priority},
			Status:      "pending",
			Tags:        "[]",
		})
	}

	// ── backlog ───────────────────────────────────────────────────────────────
	for _, ai := range r.Backlog {
		sourceID := aiSourceID(ai.SourceID)
		items = append(items, db.DayPlanItem{
			Kind:        "backlog",
			SourceType:  ai.SourceType,
			SourceID:    sourceID,
			Title:       ai.Title,
			Description: sql.NullString{Valid: ai.Description != "", String: ai.Description},
			Rationale:   sql.NullString{Valid: ai.Rationale != "", String: ai.Rationale},
			Priority:    sql.NullString{Valid: ai.Priority != "", String: ai.Priority},
			Status:      "pending",
			Tags:        "[]",
		})
	}

	return items, dropped
}

// aiSourceID converts the AIItem.SourceID (which can be nil, string, or number
// from JSON) into a sql.NullString.
func aiSourceID(v any) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	switch s := v.(type) {
	case string:
		if s == "" {
			return sql.NullString{}
		}
		return sql.NullString{Valid: true, String: s}
	default:
		// Numbers, booleans, etc. — stringify them.
		str := fmt.Sprintf("%v", s)
		return sql.NullString{Valid: true, String: str}
	}
}

// parseLocalHHMM parses "HH:MM" combined with the plan date (YYYY-MM-DD) into
// a UTC time.Time.  Calendar events are stored in UTC, so we treat the local
// HH:MM as UTC for comparison purposes.  Full timezone support is in T9.
func parseLocalHHMM(date, hhmm string) (time.Time, error) {
	combined := date + "T" + hhmm + ":00Z"
	return time.Parse(time.RFC3339, combined)
}

// overlapsAny returns true if the interval [start,end) overlaps any calendar event.
func overlapsAny(start, end time.Time, events []db.CalendarEvent) bool {
	for _, ev := range events {
		evStart, err1 := time.Parse(time.RFC3339, ev.StartTime)
		evEnd, err2 := time.Parse(time.RFC3339, ev.EndTime)
		if err1 != nil || err2 != nil {
			// Try fallback formats.
			evStart = parseTimeISO(ev.StartTime)
			evEnd = parseTimeISO(ev.EndTime)
			if evStart.IsZero() || evEnd.IsZero() {
				continue
			}
		}
		// Overlap: start < evEnd && end > evStart
		if start.Before(evEnd) && end.After(evStart) {
			return true
		}
	}
	return false
}

// parseTimeISO tries several common ISO8601 layouts.
func parseTimeISO(s string) time.Time {
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

// ── stubs for T10 / T11 ───────────────────────────────────────────────────────

// syncCalendarItems creates/updates calendar-sourced DayPlanItems to mirror
// today's calendar events.  Full implementation in T10.
//
// TODO(T10): implement — create 'calendar' source_type items for each event,
// upsert on event_id, remove stale calendar items.
func (p *Pipeline) syncCalendarItems(_ int64, _ string, _ []db.CalendarEvent) error {
	// Stub: no-op.
	return nil
}

// DetectConflicts scans the plan items for overlapping timeblocks or items
// clashing with calendar events and updates the plan's has_conflicts field.
// Full implementation in T11.
//
// TODO(T11): implement — find overlaps, build conflict_summary, call
// db.SetHasConflicts.
func (p *Pipeline) DetectConflicts(_ context.Context, _ string, _ string) error {
	// Stub: no-op.
	return nil
}

// ── internal helpers ───────────────────────────────────────────────────────────

// normalizePriority ensures a priority string is one of the known values.
func normalizePriority(s string) string {
	switch strings.ToLower(s) {
	case "high", "medium", "low":
		return strings.ToLower(s)
	default:
		return "medium"
	}
}
