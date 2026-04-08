package jira

import (
	"fmt"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// nowFunc is overridable in tests to control time-based logic.
var nowFunc = time.Now

// BuildIssueContext formats a slice of JiraIssues into a multi-line string
// suitable for injection into AI prompts. Each issue is rendered as a single
// line with key metadata (status, priority, sprint, due/overdue, blockers).
// Returns an empty string when issues is nil or empty.
func BuildIssueContext(issues []db.JiraIssue) string {
	if len(issues) == 0 {
		return ""
	}

	var b strings.Builder
	now := nowFunc()

	for i, issue := range issues {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- [")
		b.WriteString(issue.Key)

		b.WriteString(" status=")
		b.WriteString(nonEmpty(issue.Status, "Unknown"))

		if issue.Priority != "" {
			b.WriteString(" priority=")
			b.WriteString(issue.Priority)
		}

		if issue.SprintName != "" {
			fmt.Fprintf(&b, " sprint=%q", issue.SprintName)
		}

		// Overdue check: due_date in the past AND issue not done.
		if issue.DueDate != "" && !strings.EqualFold(issue.StatusCategory, "done") {
			if due, err := time.Parse("2006-01-02", issue.DueDate); err == nil {
				if due.Before(truncateToDay(now)) {
					fmt.Fprintf(&b, " OVERDUE:%s", issue.DueDate)
				} else {
					fmt.Fprintf(&b, " due=%s", issue.DueDate)
				}
			} else {
				// Unparseable due date — include as-is.
				fmt.Fprintf(&b, " due=%s", issue.DueDate)
			}
		} else if issue.DueDate != "" {
			fmt.Fprintf(&b, " due=%s", issue.DueDate)
		}

		b.WriteString("] ")
		b.WriteString(issue.Summary)
	}

	return b.String()
}

// BuildSprintContext formats sprint statistics into a single-line summary
// for AI prompt injection. Returns an empty string for a zero-value SprintStats
// (empty sprint name).
func BuildSprintContext(stats db.SprintStats) string {
	if stats.SprintName == "" {
		return ""
	}

	daysLabel := "days left"
	if stats.DaysLeft == 1 {
		daysLabel = "day left"
	}

	return fmt.Sprintf(
		"Sprint %q: %d total (%d done, %d in progress, %d todo), %d %s",
		stats.SprintName, stats.Total,
		stats.Done, stats.InProgress, stats.Todo,
		stats.DaysLeft, daysLabel,
	)
}

// BuildDeliveryContext formats delivery metrics into a multi-line summary.
// Returns an empty string when all counts are zero and no expertise tags exist.
func BuildDeliveryContext(stats db.DeliveryStats) string {
	if stats.IssuesClosed == 0 && stats.OpenIssues == 0 && stats.OverdueIssues == 0 &&
		stats.StoryPointsCompleted == 0 && len(stats.Components) == 0 && len(stats.Labels) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Issues closed: %d, Avg cycle time: %.1f days, Story points: %.1f, Open: %d, Overdue: %d",
		stats.IssuesClosed, stats.AvgCycleTimeDays, stats.StoryPointsCompleted,
		stats.OpenIssues, stats.OverdueIssues)

	expertise := mergeUnique(stats.Components, stats.Labels)
	if len(expertise) > 0 {
		fmt.Fprintf(&b, "\nExpertise: [%s]", strings.Join(expertise, ", "))
	}

	return b.String()
}

// BuildIssueListForCLI formats issues for terminal display (plain text).
func BuildIssueListForCLI(issues []db.JiraIssue) string {
	if len(issues) == 0 {
		return ""
	}

	var b strings.Builder
	now := nowFunc()

	for i, issue := range issues {
		if i > 0 {
			b.WriteByte('\n')
		}

		b.WriteString("[")
		b.WriteString(issue.Key)
		b.WriteString(" ")
		b.WriteString(nonEmpty(issue.Status, "Unknown"))

		if issue.Priority != "" {
			b.WriteString(" ")
			b.WriteString(issue.Priority)
		}

		if issue.SprintName != "" {
			fmt.Fprintf(&b, " Sprint:%s", issue.SprintName)
		}

		if issue.DueDate != "" {
			if due, err := time.Parse("2006-01-02", issue.DueDate); err == nil {
				if due.Before(truncateToDay(now)) && !strings.EqualFold(issue.StatusCategory, "done") {
					fmt.Fprintf(&b, " OVERDUE:%s", formatShortDate(due))
				} else {
					fmt.Fprintf(&b, " Due:%s", formatShortDate(due))
				}
			}
		}

		b.WriteString("] ")
		b.WriteString(issue.Summary)
	}

	return b.String()
}

// FormatJiraBadge returns a short inline badge for a single issue: [KEY Status].
func FormatJiraBadge(issue db.JiraIssue) string {
	return fmt.Sprintf("[%s %s]", issue.Key, nonEmpty(issue.Status, "Unknown"))
}

// IsFeatureEnabled returns true if Jira is enabled globally AND the specified
// feature toggle is on. The feature parameter uses short names as defined in
// featureOrder (e.g. "my_issues", "blocker_map").
func IsFeatureEnabled(cfg *config.Config, feature string) bool {
	if cfg == nil || !cfg.Jira.Enabled {
		return false
	}
	val, known := FeatureValue(&cfg.Jira.Features, feature)
	if !known {
		return false
	}
	return val
}

// --- helpers ---

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func formatShortDate(t time.Time) string {
	return t.Format("Jan 2")
}

// mergeUnique combines two string slices, deduplicating entries (case-sensitive).
func mergeUnique(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var result []string
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}
