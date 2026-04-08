package briefing

import (
	"fmt"
	"strings"
	"time"

	"watchtower/internal/db"
	"watchtower/internal/jira"
)

// gatherJiraContext collects Jira data for the briefing prompt based on
// enabled feature toggles. Returns an empty string when Jira is disabled
// or no relevant data exists (graceful degradation).
func (p *Pipeline) gatherJiraContext(userSlackID string) string {
	if !p.cfg.Jira.Enabled {
		return ""
	}

	var sections []string

	// My Issues — issues assigned to user.
	if jira.IsFeatureEnabled(p.cfg, "my_issues") {
		if ctx := p.gatherMyIssues(userSlackID); ctx != "" {
			sections = append(sections, "=== MY JIRA ISSUES ===\n"+ctx)
		}
	}

	// Awaiting My Input — issues reported by user still in progress, or blocked issues assigned to user.
	if jira.IsFeatureEnabled(p.cfg, "awaiting_input") {
		if ctx := p.gatherAwaitingInput(userSlackID); ctx != "" {
			sections = append(sections, "=== AWAITING MY INPUT ===\n"+ctx)
		}
	}

	// Sprint/Iteration Progress — stats for active sprints on selected boards.
	if jira.IsFeatureEnabled(p.cfg, "iteration_progress") {
		if ctx := p.gatherSprintProgress(); ctx != "" {
			sections = append(sections, "=== SPRINT PROGRESS ===\n"+ctx)
		}
	}

	// Always include stale and overdue issues when Jira is enabled.
	if ctx := p.gatherStaleAndOverdue(userSlackID); ctx != "" {
		sections = append(sections, ctx)
	}

	if len(sections) == 0 {
		return ""
	}

	return strings.Join(sections, "\n\n")
}

// gatherMyIssues returns formatted issues assigned to the user.
func (p *Pipeline) gatherMyIssues(userSlackID string) string {
	issues, err := p.db.GetJiraIssuesByAssigneeSlackID(userSlackID)
	if err != nil {
		p.logger.Printf("briefing: error loading Jira issues for user: %v", err)
		return ""
	}
	if len(issues) == 0 {
		return ""
	}
	return jira.BuildIssueContext(issues)
}

// gatherAwaitingInput returns issues where the user is the reporter and
// the issue is still in todo/in_progress (they may need to follow up),
// plus blocked issues assigned to the user.
func (p *Pipeline) gatherAwaitingInput(userSlackID string) string {
	var parts []string

	// Issues reported by user that are still active (todo or in_progress).
	reported := p.queryReportedActiveIssues(userSlackID)
	if len(reported) > 0 {
		parts = append(parts, "Reported by you (still open):")
		parts = append(parts, jira.BuildIssueContext(reported))
	}

	// Blocked issues assigned to the user.
	assigned, err := p.db.GetJiraIssuesByAssigneeSlackID(userSlackID)
	if err != nil {
		p.logger.Printf("briefing: error loading assigned Jira issues: %v", err)
	} else {
		var blocked []db.JiraIssue
		for _, issue := range assigned {
			if strings.EqualFold(issue.Status, "blocked") ||
				strings.Contains(strings.ToLower(issue.Status), "block") {
				blocked = append(blocked, issue)
			}
		}
		if len(blocked) > 0 {
			parts = append(parts, "Blocked (assigned to you):")
			parts = append(parts, jira.BuildIssueContext(blocked))
		}
	}

	return strings.Join(parts, "\n")
}

// queryReportedActiveIssues queries for issues where user is reporter and
// status_category is todo or in_progress.
func (p *Pipeline) queryReportedActiveIssues(userSlackID string) []db.JiraIssue {
	rows, err := p.db.Query(`SELECT key, summary, status, status_category, priority, due_date, sprint_name
		FROM jira_issues
		WHERE reporter_slack_id = ? AND status_category IN ('todo', 'in_progress') AND is_deleted = 0
		ORDER BY updated_at DESC LIMIT 20`, userSlackID)
	if err != nil {
		p.logger.Printf("briefing: error querying reported issues: %v", err)
		return nil
	}
	defer rows.Close()

	var issues []db.JiraIssue
	for rows.Next() {
		var issue db.JiraIssue
		var sprintName, dueDate, priority *string
		if err := rows.Scan(&issue.Key, &issue.Summary, &issue.Status, &issue.StatusCategory, &priority, &dueDate, &sprintName); err != nil {
			p.logger.Printf("briefing: error scanning reported issue: %v", err)
			continue
		}
		if priority != nil {
			issue.Priority = *priority
		}
		if dueDate != nil {
			issue.DueDate = *dueDate
		}
		if sprintName != nil {
			issue.SprintName = *sprintName
		}
		issues = append(issues, issue)
	}
	return issues
}

// gatherSprintProgress returns sprint stats for all selected boards.
func (p *Pipeline) gatherSprintProgress() string {
	boards, err := p.db.GetJiraSelectedBoards()
	if err != nil {
		p.logger.Printf("briefing: error loading selected Jira boards: %v", err)
		return ""
	}
	if len(boards) == 0 {
		return ""
	}

	var lines []string
	for _, board := range boards {
		stats, err := p.db.GetJiraActiveSprintStats(board.ID)
		if err != nil {
			p.logger.Printf("briefing: error loading sprint stats for board %d: %v", board.ID, err)
			continue
		}
		if stats == nil {
			continue
		}
		line := jira.BuildSprintContext(*stats)
		if line != "" {
			lines = append(lines, fmt.Sprintf("[%s] %s", board.Name, line))
		}
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// gatherStaleAndOverdue returns issues that are stale (in_progress for >7 days
// without status change) or overdue (past due date, not done).
func (p *Pipeline) gatherStaleAndOverdue(userSlackID string) string {
	issues, err := p.db.GetJiraIssuesByAssigneeSlackID(userSlackID)
	if err != nil {
		p.logger.Printf("briefing: error loading Jira issues for stale/overdue check: %v", err)
		return ""
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	staleCutoff := now.AddDate(0, 0, -7)

	var stale, overdue []db.JiraIssue

	for _, issue := range issues {
		// Overdue: due_date < today AND not done.
		if issue.DueDate != "" && issue.DueDate < today &&
			!strings.EqualFold(issue.StatusCategory, "done") {
			overdue = append(overdue, issue)
		}

		// Stale: in_progress and status unchanged for >7 days.
		if strings.EqualFold(issue.StatusCategory, "in_progress") && issue.StatusCategoryChangedAt != "" {
			changedAt, err := parseFlexibleTime(issue.StatusCategoryChangedAt)
			if err == nil && changedAt.Before(staleCutoff) {
				stale = append(stale, issue)
			}
		}
	}

	var parts []string
	if len(stale) > 0 {
		parts = append(parts, "=== STALE JIRA ISSUES (in_progress >7 days) ===\n"+jira.BuildIssueContext(stale))
	}
	if len(overdue) > 0 {
		parts = append(parts, "=== OVERDUE JIRA ISSUES ===\n"+jira.BuildIssueContext(overdue))
	}

	return strings.Join(parts, "\n\n")
}

// parseFlexibleTime tries multiple time formats.
func parseFlexibleTime(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}
