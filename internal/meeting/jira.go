package meeting

import (
	"fmt"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"
)

// gatherJiraMeetingContext builds a Jira context section for the meeting prep prompt.
// It loads open issues for each attendee, computes workload stats, and finds shared issues.
// Returns empty string if Jira is disabled or no attendees have Jira data.
func gatherJiraMeetingContext(database *db.DB, cfg *config.Config, attendeeSlackIDs []string) (string, error) {
	if !jira.IsFeatureEnabled(cfg, "track_linking") {
		return "", nil
	}

	if len(attendeeSlackIDs) == 0 {
		return "", nil
	}

	// Deduplicate and filter empty IDs.
	idSet := make(map[string]bool, len(attendeeSlackIDs))
	var ids []string
	for _, id := range attendeeSlackIDs {
		if id != "" && !idSet[id] {
			idSet[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", nil
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	type attendeeJira struct {
		slackID     string
		displayName string
		issues      []db.JiraIssue
		totalSP     float64
		blocked     int
		overdue     int
	}

	attendees := make([]attendeeJira, 0, len(ids))
	// issueKey → list of (slackID, role) for shared issue detection.
	type issueRole struct {
		slackID string
		role    string // "assignee" or "reporter"
	}
	issueParticipants := make(map[string][]issueRole)

	for _, slackID := range ids {
		issues, err := database.GetJiraIssuesByAssigneeSlackID(slackID)
		if err != nil {
			// Non-fatal: skip this attendee.
			continue
		}

		aj := attendeeJira{slackID: slackID, issues: issues}

		// Determine display name from the first issue if available.
		for _, iss := range issues {
			if iss.AssigneeDisplayName != "" {
				aj.displayName = iss.AssigneeDisplayName
				break
			}
		}

		for _, iss := range issues {
			// Story points.
			if iss.StoryPoints != nil {
				aj.totalSP += *iss.StoryPoints
			}

			// Blocked detection (status contains "blocked" case-insensitive).
			if strings.EqualFold(iss.Status, "Blocked") || strings.Contains(strings.ToLower(iss.Status), "blocked") {
				aj.blocked++
			}

			// Overdue detection.
			if iss.DueDate != "" && !strings.EqualFold(iss.StatusCategory, "done") {
				if due, err := time.Parse("2006-01-02", iss.DueDate); err == nil {
					if due.Before(today) {
						aj.overdue++
					}
				}
			}

			// Track assignee participation.
			issueParticipants[iss.Key] = append(issueParticipants[iss.Key], issueRole{slackID: slackID, role: "assignee"})
		}

		// Also check if this person is a reporter on issues assigned to other attendees.
		// We'll do this after collecting all issues.

		attendees = append(attendees, aj)
	}

	// Build a map of all issues across attendees for reporter cross-reference.
	allIssues := make(map[string]db.JiraIssue)
	for _, aj := range attendees {
		for _, iss := range aj.issues {
			allIssues[iss.Key] = iss
			// Check if the reporter is another attendee.
			if iss.ReporterSlackID != "" && idSet[iss.ReporterSlackID] && iss.ReporterSlackID != aj.slackID {
				// Add reporter role (avoid duplicates).
				roles := issueParticipants[iss.Key]
				hasReporter := false
				for _, r := range roles {
					if r.slackID == iss.ReporterSlackID && r.role == "reporter" {
						hasReporter = true
						break
					}
				}
				if !hasReporter {
					issueParticipants[iss.Key] = append(issueParticipants[iss.Key], issueRole{slackID: iss.ReporterSlackID, role: "reporter"})
				}
			}
		}
	}

	// Check if we have any data at all.
	hasData := false
	for _, aj := range attendees {
		if len(aj.issues) > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("=== JIRA CONTEXT FOR ATTENDEES ===\n\n")

	for _, aj := range attendees {
		name := aj.displayName
		if name == "" {
			name = aj.slackID
		}

		sb.WriteString(fmt.Sprintf("@%s (%s):\n", name, aj.slackID))
		sb.WriteString(fmt.Sprintf("- Open issues: %d (%.0f SP), Blocked: %d, Overdue: %d\n",
			len(aj.issues), aj.totalSP, aj.blocked, aj.overdue))

		// Show top issues (max 10).
		limit := 10
		if len(aj.issues) < limit {
			limit = len(aj.issues)
		}
		for _, iss := range aj.issues[:limit] {
			sb.WriteString(fmt.Sprintf("- [%s %s %s", iss.Key, iss.Status, iss.Priority))
			if iss.DueDate != "" && !strings.EqualFold(iss.StatusCategory, "done") {
				if due, err := time.Parse("2006-01-02", iss.DueDate); err == nil && due.Before(today) {
					sb.WriteString(" OVERDUE")
				}
			}
			sb.WriteString(fmt.Sprintf("] %s\n", iss.Summary))
		}
		if len(aj.issues) > limit {
			sb.WriteString(fmt.Sprintf("  ... and %d more issues\n", len(aj.issues)-limit))
		}

		sb.WriteString("\n")
	}

	// Shared issues: issues where 2+ attendees are involved (as assignee or reporter).
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

		var parts []string
		for _, r := range roles {
			name := r.slackID
			// Try to find display name.
			for _, aj := range attendees {
				if aj.slackID == r.slackID && aj.displayName != "" {
					name = aj.displayName
					break
				}
			}
			parts = append(parts, fmt.Sprintf("@%s (%s)", name, r.role))
		}

		sharedLines = append(sharedLines, fmt.Sprintf("- [%s %s] %s — %s",
			iss.Key, iss.Status, iss.Summary, strings.Join(parts, ", ")))
	}

	if len(sharedLines) > 0 {
		sb.WriteString("=== SHARED JIRA ISSUES ===\n")
		for _, line := range sharedLines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}
