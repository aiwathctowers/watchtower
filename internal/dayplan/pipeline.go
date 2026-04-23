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

// ── stubs for T11 ────────────────────────────────────────────────────────────

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
