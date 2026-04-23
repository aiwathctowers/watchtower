package dayplan

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"watchtower/internal/db"
	"watchtower/internal/prompts"
)

// promptInputs holds all context sections that are injected into the day-plan
// prompt template via fmt.Sprintf.
type promptInputs struct {
	Date, Weekday, NowLocal, UserRole  string
	WorkingHoursStart, WorkingHoursEnd string
	CalendarEvents, Tasks, Briefing    string
	Jira, People, Manual, Previous     string
	Feedback                           string
}

// buildPrompt formats the system prompt from the store (or built-in default)
// and returns it together with the version label.
func (p *Pipeline) buildPrompt(in *promptInputs) (string, string) {
	tmpl := prompts.Defaults[prompts.DayPlanGenerate]
	version := "default"
	if p.promptStore != nil {
		if stored, v, err := p.promptStore.Get(prompts.DayPlanGenerate); err == nil && stored != "" {
			tmpl = stored
			version = fmt.Sprintf("stored:%d", v)
		}
	}
	return fmt.Sprintf(tmpl,
		in.Date, in.Weekday, in.NowLocal, in.UserRole,
		in.WorkingHoursStart, in.WorkingHoursEnd,
		in.CalendarEvents, in.Tasks, in.Briefing,
		in.Jira, in.People, in.Manual, in.Previous,
		in.Feedback,
	), version
}

// ── section formatters ─────────────────────────────────────────────────────────

// formatCalendarSection renders calendar events for the prompt.
// Returns "(none)" when the slice is empty.
func formatCalendarSection(events []db.CalendarEvent) string {
	if len(events) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, ev := range events {
		start := shortTime(ev.StartTime)
		end := shortTime(ev.EndTime)
		fmt.Fprintf(&sb, "- %s–%s %s", start, end, ev.Title)
		if ev.Location != "" {
			fmt.Fprintf(&sb, " [%s]", ev.Location)
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatTasksSection renders active tasks for the prompt.
func formatTasksSection(tasks []db.Task) string {
	if len(tasks) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, t := range tasks {
		due := ""
		if t.DueDate != "" {
			due = " due:" + t.DueDate
		}
		fmt.Fprintf(&sb, "- [task_id=%d priority=%s%s] %s", t.ID, t.Priority, due, t.Text)
		if t.Intent != "" {
			fmt.Fprintf(&sb, " — %s", t.Intent)
		}
		if t.Blocking != "" {
			fmt.Fprintf(&sb, " (blocking: %s)", t.Blocking)
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatJiraSection renders active Jira issues for the prompt.
func formatJiraSection(issues []db.JiraIssue) string {
	if len(issues) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, iss := range issues {
		fmt.Fprintf(&sb, "- [%s %s] %s", iss.Key, iss.Priority, iss.Summary)
		if iss.Status != "" {
			fmt.Fprintf(&sb, " (status: %s)", iss.Status)
		}
		if iss.DueDate != "" {
			fmt.Fprintf(&sb, " due:%s", iss.DueDate)
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatPeopleSection renders active people cards (red_flags) for the prompt.
func formatPeopleSection(cards []db.PeopleCard) string {
	if len(cards) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, c := range cards {
		fmt.Fprintf(&sb, "- user:%s [%s / %s]", c.UserID, c.CommunicationStyle, c.DecisionRole)
		if c.Summary != "" {
			fmt.Fprintf(&sb, " — %s", c.Summary)
		}
		// Include red flags if any.
		var flags []string
		if c.RedFlags != "" && c.RedFlags != "[]" && c.RedFlags != "null" {
			_ = json.Unmarshal([]byte(c.RedFlags), &flags)
		}
		if len(flags) > 0 {
			fmt.Fprintf(&sb, " flags:[%s]", strings.Join(flags, "; "))
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatManualSection renders pinned manual items so the AI knows not to duplicate them.
func formatManualSection(items []db.DayPlanItem) string {
	if len(items) == 0 {
		return "(none)"
	}
	var sb strings.Builder
	for _, it := range items {
		pri := ""
		if it.Priority.Valid {
			pri = " [" + it.Priority.String + "]"
		}
		desc := ""
		if it.Description.Valid {
			desc = it.Description.String
		}
		fmt.Fprintf(&sb, "- %s%s: %s\n", it.Title, pri, desc)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// formatPreviousPlanSection summarises the previous day's plan for continuity.
func formatPreviousPlanSection(prev *db.DayPlan, items []db.DayPlanItem) string {
	if prev == nil {
		return "(none)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Date: %s\n", prev.PlanDate)
	for _, it := range items {
		marker := "[ ]"
		if it.Status == "done" {
			marker = "[x]"
		}
		fmt.Fprintf(&sb, "%s %s (%s)\n", marker, it.Title, it.Kind)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ── JSON parsing ───────────────────────────────────────────────────────────────

// parseResponse strips optional markdown fences and unmarshals the AI JSON.
func parseResponse(raw string) (*GenerateResult, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var r GenerateResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, fmt.Errorf("parse day plan JSON: %w", err)
	}
	return &r, nil
}

// ── small helpers ──────────────────────────────────────────────────────────────

func feedbackOrInitial(fb string) string {
	if fb == "" {
		return "(initial generation)"
	}
	return `"` + fb + `"`
}

func dayOfWeek(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return t.Weekday().String()
}

// userRole returns the user's configured role from their profile, or "".
func (p *Pipeline) userRole(userID string) string {
	if prof, err := p.db.GetUserProfile(userID); err == nil && prof != nil {
		return prof.Role
	}
	return ""
}

// shortTime extracts "HH:MM" from an ISO8601 string. Returns the raw input on failure.
func shortTime(iso string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, iso); err == nil {
			return t.UTC().Format("15:04")
		}
	}
	// Try bare HH:MM passthrough.
	if len(iso) >= 5 {
		return iso[:5]
	}
	return iso
}

// tasksIDSet builds a set of task IDs (as strings) from a task slice.
func tasksIDSet(tasks []db.Task) map[string]bool {
	m := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		m[fmt.Sprintf("%d", t.ID)] = true
	}
	return m
}

// jiraKeySet builds a set of Jira issue keys from an issue slice.
func jiraKeySet(issues []db.JiraIssue) map[string]bool {
	m := make(map[string]bool, len(issues))
	for _, iss := range issues {
		m[iss.Key] = true
	}
	return m
}
