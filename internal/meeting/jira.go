package meeting

import (
	"fmt"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// attendeeJira aggregates Jira workload for a single meeting attendee.
type attendeeJira struct {
	slackID     string
	displayName string
	issues      []db.JiraIssue
	totalSP     float64
	blocked     int
	overdue     int
}

// issueRole records how an attendee is involved in a Jira issue (assignee or
// reporter), used for shared-issue detection.
type issueRole struct {
	slackID string
	role    string
}

// gatherJiraMeetingContext builds a Jira context section for the meeting prep prompt.
// It loads open issues for each attendee, computes workload stats, and finds shared issues.
// Returns empty string if Jira is disabled or no attendees have Jira data.
func gatherJiraMeetingContext(database *db.DB, cfg *config.Config, attendeeSlackIDs []string) (string, error) {
	if cfg == nil || !cfg.Jira.Enabled || len(attendeeSlackIDs) == 0 {
		return "", nil
	}
	idSet, ids := dedupAttendeeIDs(attendeeSlackIDs)
	if len(ids) == 0 {
		return "", nil
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	attendees, issueParticipants := loadAttendeeJira(database, ids, today)
	allIssues := indexIssuesAndReporters(attendees, idSet, issueParticipants)

	if !attendeesHaveData(attendees) {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("=== JIRA CONTEXT FOR ATTENDEES ===\n\n")
	formatAttendeeSections(&sb, attendees, today)
	formatSharedIssues(&sb, attendees, allIssues, issueParticipants)
	return sb.String(), nil
}

// dedupAttendeeIDs returns a presence set + ordered list with empties removed.
func dedupAttendeeIDs(attendeeSlackIDs []string) (map[string]bool, []string) {
	idSet := make(map[string]bool, len(attendeeSlackIDs))
	var ids []string
	for _, id := range attendeeSlackIDs {
		if id != "" && !idSet[id] {
			idSet[id] = true
			ids = append(ids, id)
		}
	}
	return idSet, ids
}

// loadAttendeeJira fetches open issues per attendee and computes per-attendee
// workload stats (story points, blocked count, overdue count). Also seeds
// issueParticipants with the assignee role for each issue.
func loadAttendeeJira(database *db.DB, ids []string, today time.Time) ([]attendeeJira, map[string][]issueRole) {
	attendees := make([]attendeeJira, 0, len(ids))
	issueParticipants := make(map[string][]issueRole)

	for _, slackID := range ids {
		issues, err := database.GetJiraIssuesByAssigneeSlackID(slackID)
		if err != nil {
			continue // non-fatal: skip this attendee
		}
		aj := attendeeJira{slackID: slackID, issues: issues}
		for _, iss := range issues {
			if iss.AssigneeDisplayName != "" {
				aj.displayName = iss.AssigneeDisplayName
				break
			}
		}
		for _, iss := range issues {
			if iss.StoryPoints != nil {
				aj.totalSP += *iss.StoryPoints
			}
			if isBlocked(iss) {
				aj.blocked++
			}
			if isOverdue(iss, today) {
				aj.overdue++
			}
			issueParticipants[iss.Key] = append(issueParticipants[iss.Key], issueRole{slackID: slackID, role: "assignee"})
		}
		attendees = append(attendees, aj)
	}
	return attendees, issueParticipants
}

func isBlocked(iss db.JiraIssue) bool {
	return strings.EqualFold(iss.Status, "Blocked") || strings.Contains(strings.ToLower(iss.Status), "blocked")
}

func isOverdue(iss db.JiraIssue, today time.Time) bool {
	if iss.DueDate == "" || strings.EqualFold(iss.StatusCategory, "done") {
		return false
	}
	due, err := time.Parse("2006-01-02", iss.DueDate)
	if err != nil {
		return false
	}
	return due.Before(today)
}

// indexIssuesAndReporters builds the issue lookup map and adds reporter roles
// when the reporter is also an attendee (avoiding duplicates).
func indexIssuesAndReporters(attendees []attendeeJira, idSet map[string]bool, issueParticipants map[string][]issueRole) map[string]db.JiraIssue {
	allIssues := make(map[string]db.JiraIssue)
	for _, aj := range attendees {
		for _, iss := range aj.issues {
			allIssues[iss.Key] = iss
			if iss.ReporterSlackID == "" || !idSet[iss.ReporterSlackID] || iss.ReporterSlackID == aj.slackID {
				continue
			}
			roles := issueParticipants[iss.Key]
			hasReporter := false
			for _, r := range roles {
				if r.slackID == iss.ReporterSlackID && r.role == "reporter" {
					hasReporter = true
					break
				}
			}
			if !hasReporter {
				issueParticipants[iss.Key] = append(roles, issueRole{slackID: iss.ReporterSlackID, role: "reporter"})
			}
		}
	}
	return allIssues
}

func attendeesHaveData(attendees []attendeeJira) bool {
	for _, aj := range attendees {
		if len(aj.issues) > 0 {
			return true
		}
	}
	return false
}

// formatAttendeeSections appends the per-attendee Jira summary block
// (workload stats + top 10 issues) to sb.
func formatAttendeeSections(sb *strings.Builder, attendees []attendeeJira, today time.Time) {
	const issueLimit = 10
	for _, aj := range attendees {
		name := aj.displayName
		if name == "" {
			name = aj.slackID
		}
		sb.WriteString(fmt.Sprintf("@%s (%s):\n", name, aj.slackID))
		sb.WriteString(fmt.Sprintf("- Open issues: %d (%.0f SP), Blocked: %d, Overdue: %d\n",
			len(aj.issues), aj.totalSP, aj.blocked, aj.overdue))

		limit := issueLimit
		if len(aj.issues) < limit {
			limit = len(aj.issues)
		}
		for _, iss := range aj.issues[:limit] {
			sb.WriteString(fmt.Sprintf("- [%s %s %s", iss.Key, iss.Status, iss.Priority))
			if isOverdue(iss, today) {
				sb.WriteString(" OVERDUE")
			}
			sb.WriteString(fmt.Sprintf("] %s\n", iss.Summary))
		}
		if len(aj.issues) > limit {
			sb.WriteString(fmt.Sprintf("  ... and %d more issues\n", len(aj.issues)-limit))
		}
		sb.WriteString("\n")
	}
}

// formatSharedIssues appends the SHARED JIRA ISSUES block listing issues where
// 2+ attendees are involved (assignee or reporter).
func formatSharedIssues(sb *strings.Builder, attendees []attendeeJira, allIssues map[string]db.JiraIssue, issueParticipants map[string][]issueRole) {
	var sharedLines []string
	seen := make(map[string]bool)
	for key, roles := range issueParticipants {
		if len(roles) < 2 || seen[key] {
			continue
		}
		seen[key] = true
		iss, ok := allIssues[key]
		if !ok {
			continue
		}
		parts := make([]string, 0, len(roles))
		for _, r := range roles {
			parts = append(parts, fmt.Sprintf("@%s (%s)", lookupDisplayName(attendees, r.slackID), r.role))
		}
		sharedLines = append(sharedLines, fmt.Sprintf("- [%s %s] %s — %s",
			iss.Key, iss.Status, iss.Summary, strings.Join(parts, ", ")))
	}
	if len(sharedLines) == 0 {
		return
	}
	sb.WriteString("=== SHARED JIRA ISSUES ===\n")
	for _, line := range sharedLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
}

// lookupDisplayName returns the display name for slackID across attendees,
// falling back to the slack ID itself when no name is recorded.
func lookupDisplayName(attendees []attendeeJira, slackID string) string {
	for _, aj := range attendees {
		if aj.slackID == slackID && aj.displayName != "" {
			return aj.displayName
		}
	}
	return slackID
}
