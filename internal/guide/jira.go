package guide

import (
	"fmt"
	"strings"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"
)

// maxAccomplishments is the cap on resolved issues listed in the delivery section.
const maxAccomplishments = 10

// jiraDeliveryInstruction is appended to the people card prompt when Jira
// delivery data is present. It tells the AI how to use the section.
const jiraDeliveryInstruction = `
If JIRA DELIVERY data is provided above, include a Delivery section in the people card:
- Incorporate delivery metrics (issues closed, cycle time, velocity) into accomplishments and highlights.
- Use expertise tags to identify domain specialization.
- Combine Jira workload signals with Slack activity signals for compound red flags (e.g., high open issues + low Slack responsiveness = bottleneck risk).
- List resolved issues as concrete accomplishments.`

// gatherJiraDelivery collects Jira delivery context for a single user.
// Returns an empty string (no error) when Jira is disabled or the user has no
// Jira activity. The caller can safely append the result to the AI prompt.
func gatherJiraDelivery(database *db.DB, cfg *config.Config, userSlackID, from, to string) (string, error) {
	if !jira.IsFeatureEnabled(cfg, "my_issues") {
		return "", nil
	}

	stats, err := database.GetJiraDeliveryStats(userSlackID, from, to)
	if err != nil {
		return "", fmt.Errorf("jira delivery stats for %s: %w", userSlackID, err)
	}
	if stats == nil {
		return "", nil
	}

	// Skip entirely when there is no Jira activity at all.
	if stats.IssuesClosed == 0 && stats.OpenIssues == 0 && stats.OverdueIssues == 0 &&
		stats.StoryPointsCompleted == 0 && len(stats.Components) == 0 && len(stats.Labels) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("=== JIRA DELIVERY ===\n")

	// Metrics line.
	fmt.Fprintf(&b, "Issues closed: %d, Avg cycle time: %.1f days, Story points: %.1f\n",
		stats.IssuesClosed, stats.AvgCycleTimeDays, stats.StoryPointsCompleted)
	fmt.Fprintf(&b, "Open issues: %d, Overdue: %d\n", stats.OpenIssues, stats.OverdueIssues)

	// Expertise tags from components + labels.
	expertise := jira.MergeUnique(stats.Components, stats.Labels)
	if len(expertise) > 0 {
		fmt.Fprintf(&b, "Expertise: [%s]\n", strings.Join(expertise, ", "))
	}

	// Recent accomplishments: resolved issues within the period (bounded query).
	accomplishments, err := database.GetJiraResolvedIssuesForUser(userSlackID, from, to, maxAccomplishments)
	if err == nil && len(accomplishments) > 0 {
		b.WriteString("\nRecent accomplishments:\n")
		for _, issue := range accomplishments {
			resolvedDate := issue.ResolvedAt
			if len(resolvedDate) > 10 {
				resolvedDate = resolvedDate[:10]
			}
			fmt.Fprintf(&b, "- Resolved %s %q (%s)\n", issue.Key, issue.Summary, resolvedDate)
		}
	}

	// Workload signals and compound red flags.
	signals := buildWorkloadSignals(stats)
	if len(signals) > 0 {
		b.WriteString("\nWorkload signals:\n")
		for _, sig := range signals {
			fmt.Fprintf(&b, "- %s\n", sig)
		}
	}

	return b.String(), nil
}

// buildWorkloadSignals generates human-readable workload observations
// including compound red flags.
func buildWorkloadSignals(stats *db.DeliveryStats) []string {
	var signals []string

	if stats.OpenIssues > 5 {
		signals = append(signals, fmt.Sprintf("%d open issues — high workload", stats.OpenIssues))
	}
	if stats.OverdueIssues > 0 {
		signals = append(signals, fmt.Sprintf("%d overdue issue(s) — potential bottleneck", stats.OverdueIssues))
	}
	if stats.AvgCycleTimeDays > 14 {
		signals = append(signals, fmt.Sprintf("%.1f day avg cycle time — slower than typical", stats.AvgCycleTimeDays))
	}

	// Compound red flag: overdue + high open count → burnout risk.
	if stats.OverdueIssues > 0 && stats.OpenIssues > 10 {
		signals = append(signals, "COMPOUND: overdue issues + high open count — burnout risk signal")
	}

	return signals
}
