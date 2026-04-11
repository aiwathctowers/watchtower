package jira

import (
	"sort"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// ProjectMapEpic holds the full project map entry for a single epic.
type ProjectMapEpic struct {
	EpicKey           string               `json:"epic_key"`
	EpicName          string               `json:"epic_name"`
	Owner             *PingTarget          `json:"owner"`
	ProgressPct       float64              `json:"progress_pct"`
	StatusBadge       string               `json:"status_badge"`
	TotalIssues       int                  `json:"total_issues"`
	DoneIssues        int                  `json:"done_issues"`
	InProgressIssues  int                  `json:"in_progress_issues"`
	ForecastWeeks     float64              `json:"forecast_weeks"`
	StaleCount        int                  `json:"stale_count"`
	BlockedCount      int                  `json:"blocked_count"`
	KeyDecisionsCount int                  `json:"key_decisions_count"`
	Participants      []PingTarget         `json:"participants"`
	SlackDiscussions  []SlackDiscussionRef `json:"slack_discussions"`
	Issues            []ProjectMapIssue    `json:"issues"`
	StaleIssues       []ProjectMapIssue    `json:"stale_issues"`
}

// ProjectMapIssue represents a child issue within an epic in the project map.
type ProjectMapIssue struct {
	Key             string `json:"key"`
	Summary         string `json:"summary"`
	Status          string `json:"status"`
	StatusCategory  string `json:"status_category"`
	AssigneeName    string `json:"assignee_name"`
	AssigneeSlackID string `json:"assignee_slack_id"`
	DaysInStatus    int    `json:"days_in_status"`
	IsStale         bool   `json:"is_stale"`
	IsBlocked       bool   `json:"is_blocked"`
}

// SlackDiscussionRef is a reference to a Slack message linked to a Jira issue.
type SlackDiscussionRef struct {
	ChannelID string `json:"channel_id"`
	MessageTS string `json:"message_ts"`
	TrackID   *int64 `json:"track_id"`
	DigestID  *int64 `json:"digest_id"`
}

// defaultStaleDays is the fallback threshold for considering an issue stale.
const defaultStaleDays = 7

// BuildProjectMap returns a full project map with all epics that have >= minEpicIssues
// child issues, enriched with participants, stale issues, Slack discussions, and decisions.
// Returns nil, nil when the epic_progress feature is disabled.
func BuildProjectMap(database *db.DB, cfg *config.Config, now time.Time) ([]ProjectMapEpic, error) {
	if !IsFeatureEnabled(cfg, "epic_progress") {
		return nil, nil
	}

	weekAgo := now.AddDate(0, 0, -7).Format(time.RFC3339)
	fourWeeksAgo := now.AddDate(0, 0, -28).Format(time.RFC3339)

	aggs, err := database.GetJiraEpicAggregates(weekAgo, fourWeeksAgo)
	if err != nil {
		return nil, err
	}

	if len(aggs) == 0 {
		return nil, nil
	}

	// Filter to epics with enough children and build key list.
	var filteredAggs []db.EpicAggRow
	epicKeys := make([]string, 0, len(aggs))
	for _, a := range aggs {
		if a.Total < minEpicIssues {
			continue
		}
		filteredAggs = append(filteredAggs, a)
		epicKeys = append(epicKeys, a.EpicKey)
	}

	if len(filteredAggs) == 0 {
		return nil, nil
	}

	// Bulk-load epic issues for names and owner info.
	epicIssues, err := database.GetJiraIssuesByKeysMap(epicKeys)
	if err != nil {
		return nil, err
	}

	var result []ProjectMapEpic

	for _, a := range filteredAggs {
		// Load child issues for this epic.
		children, err := database.GetJiraIssuesByEpicKey(a.EpicKey)
		if err != nil {
			return nil, err
		}

		// Compute progress metrics (reuse epic_progress.go logic).
		progressPct := float64(a.Done) / float64(a.Total) * 100
		velocity := float64(a.ResolvedLast4W) / 4.0
		remaining := float64(a.Total - a.Done)
		var forecast float64
		if velocity > 0 {
			forecast = remaining / velocity
		} else {
			forecast = maxForecastWeeks
		}
		badge := computeStatusBadge(a, velocity)

		// Build epic entry.
		epic := ProjectMapEpic{
			EpicKey:          a.EpicKey,
			TotalIssues:      a.Total,
			DoneIssues:       a.Done,
			InProgressIssues: a.InProgress,
			ProgressPct:      progressPct,
			StatusBadge:      badge,
			ForecastWeeks:    forecast,
		}

		// Epic name and owner from the epic issue itself.
		if issue, ok := epicIssues[a.EpicKey]; ok {
			epic.EpicName = issue.Summary
			if issue.AssigneeSlackID != "" || issue.AssigneeDisplayName != "" {
				epic.Owner = &PingTarget{
					SlackUserID: issue.AssigneeSlackID,
					DisplayName: issue.AssigneeDisplayName,
					Reason:      "assignee",
				}
			}
		}

		// Process child issues: build issues list, detect stale/blocked, collect participants.
		participantsSeen := make(map[string]bool)
		var childKeys []string

		for _, child := range children {
			childKeys = append(childKeys, child.Key)

			mi := toProjectMapIssue(child, now)
			epic.Issues = append(epic.Issues, mi)

			if mi.IsStale {
				epic.StaleCount++
				epic.StaleIssues = append(epic.StaleIssues, mi)
			}
			if mi.IsBlocked {
				epic.BlockedCount++
			}

			// Collect unique participants (assignees and reporters).
			addParticipant(&epic.Participants, &participantsSeen, child.AssigneeSlackID, child.AssigneeDisplayName, "assignee")
			addParticipant(&epic.Participants, &participantsSeen, child.ReporterSlackID, child.ReporterDisplayName, "reporter")
		}

		// Slack discussions from jira_slack_links for all child issue keys.
		if len(childKeys) > 0 {
			links, err := database.GetJiraSlackLinksByIssueKeys(childKeys)
			if err != nil {
				return nil, err
			}
			for _, l := range links {
				ref := SlackDiscussionRef{
					ChannelID: l.ChannelID,
					MessageTS: l.MessageTS,
				}
				if l.TrackID != nil {
					v := int64(*l.TrackID)
					ref.TrackID = &v
				}
				if l.DigestID != nil {
					v := int64(*l.DigestID)
					ref.DigestID = &v
				}
				epic.SlackDiscussions = append(epic.SlackDiscussions, ref)
			}

			// Key decisions count.
			decCount, err := database.GetJiraDecisionCountByIssueKeys(childKeys)
			if err != nil {
				return nil, err
			}
			epic.KeyDecisionsCount = decCount
		}

		result = append(result, epic)
	}

	// Sort: at_risk/behind first, then by progress_pct ASC (same as epic_progress).
	sort.Slice(result, func(i, j int) bool {
		oi := badgeOrder(result[i].StatusBadge)
		oj := badgeOrder(result[j].StatusBadge)
		if oi != oj {
			return oi < oj
		}
		return result[i].ProgressPct < result[j].ProgressPct
	})

	return result, nil
}

// BuildProjectMapForEpic returns the project map entry for a single epic.
// Returns nil, nil when the epic_progress feature is disabled or the epic has < minEpicIssues children.
func BuildProjectMapForEpic(database *db.DB, cfg *config.Config, epicKey string, now time.Time) (*ProjectMapEpic, error) {
	if !IsFeatureEnabled(cfg, "epic_progress") {
		return nil, nil
	}

	// Load the epic issue itself.
	epicIssue, err := database.GetJiraIssueByKey(epicKey)
	if err != nil {
		return nil, err
	}
	if epicIssue == nil {
		return nil, nil
	}

	// Load child issues.
	children, err := database.GetJiraIssuesByEpicKey(epicKey)
	if err != nil {
		return nil, err
	}
	if len(children) < minEpicIssues {
		return nil, nil
	}

	// Compute aggregates from children.
	total := len(children)
	var done, inProgress, resolvedLastWeek, resolvedLast4W int
	weekAgo := now.AddDate(0, 0, -7)
	fourWeeksAgo := now.AddDate(0, 0, -28)

	for _, c := range children {
		cat := strings.ToLower(c.StatusCategory)
		switch {
		case cat == "done":
			done++
			if c.ResolvedAt != "" {
				if t, err := time.Parse(time.RFC3339, c.ResolvedAt); err == nil {
					if t.After(weekAgo) {
						resolvedLastWeek++
					}
					if t.After(fourWeeksAgo) {
						resolvedLast4W++
					}
				}
			}
		case cat == "in_progress" || cat == "in progress" || cat == "indeterminate":
			inProgress++
		}
	}

	agg := db.EpicAggRow{
		EpicKey:          epicKey,
		Total:            total,
		Done:             done,
		InProgress:       inProgress,
		ResolvedLastWeek: resolvedLastWeek,
		ResolvedLast4W:   resolvedLast4W,
	}

	progressPct := float64(done) / float64(total) * 100
	velocity := float64(resolvedLast4W) / 4.0
	remaining := float64(total - done)
	var forecast float64
	if velocity > 0 {
		forecast = remaining / velocity
	} else {
		forecast = maxForecastWeeks
	}
	badge := computeStatusBadge(agg, velocity)

	epic := ProjectMapEpic{
		EpicKey:          epicKey,
		EpicName:         epicIssue.Summary,
		TotalIssues:      total,
		DoneIssues:       done,
		InProgressIssues: inProgress,
		ProgressPct:      progressPct,
		StatusBadge:      badge,
		ForecastWeeks:    forecast,
	}

	// Owner from epic assignee.
	if epicIssue.AssigneeSlackID != "" || epicIssue.AssigneeDisplayName != "" {
		epic.Owner = &PingTarget{
			SlackUserID: epicIssue.AssigneeSlackID,
			DisplayName: epicIssue.AssigneeDisplayName,
			Reason:      "assignee",
		}
	}

	// Process child issues.
	participantsSeen := make(map[string]bool)
	var childKeys []string

	for _, child := range children {
		childKeys = append(childKeys, child.Key)

		mi := toProjectMapIssue(child, now)
		epic.Issues = append(epic.Issues, mi)

		if mi.IsStale {
			epic.StaleCount++
			epic.StaleIssues = append(epic.StaleIssues, mi)
		}
		if mi.IsBlocked {
			epic.BlockedCount++
		}

		addParticipant(&epic.Participants, &participantsSeen, child.AssigneeSlackID, child.AssigneeDisplayName, "assignee")
		addParticipant(&epic.Participants, &participantsSeen, child.ReporterSlackID, child.ReporterDisplayName, "reporter")
	}

	// Slack discussions.
	if len(childKeys) > 0 {
		links, err := database.GetJiraSlackLinksByIssueKeys(childKeys)
		if err != nil {
			return nil, err
		}
		for _, l := range links {
			ref := SlackDiscussionRef{
				ChannelID: l.ChannelID,
				MessageTS: l.MessageTS,
			}
			if l.TrackID != nil {
				v := int64(*l.TrackID)
				ref.TrackID = &v
			}
			if l.DigestID != nil {
				v := int64(*l.DigestID)
				ref.DigestID = &v
			}
			epic.SlackDiscussions = append(epic.SlackDiscussions, ref)
		}

		decCount, err := database.GetJiraDecisionCountByIssueKeys(childKeys)
		if err != nil {
			return nil, err
		}
		epic.KeyDecisionsCount = decCount
	}

	return &epic, nil
}

// toProjectMapIssue converts a JiraIssue to a ProjectMapIssue, computing days in status,
// stale and blocked flags.
func toProjectMapIssue(issue db.JiraIssue, now time.Time) ProjectMapIssue {
	mi := ProjectMapIssue{
		Key:             issue.Key,
		Summary:         issue.Summary,
		Status:          issue.Status,
		StatusCategory:  issue.StatusCategory,
		AssigneeName:    issue.AssigneeDisplayName,
		AssigneeSlackID: issue.AssigneeSlackID,
	}

	mi.DaysInStatus = computeDaysInStatus(issue.StatusCategoryChangedAt, now)

	// Stale: not done, in status > defaultStaleDays.
	if strings.ToLower(issue.StatusCategory) != "done" && mi.DaysInStatus > defaultStaleDays {
		mi.IsStale = true
	}

	// Blocked: status contains "block" (case-insensitive) and not done.
	if strings.ToLower(issue.StatusCategory) != "done" && strings.Contains(strings.ToLower(issue.Status), "block") {
		mi.IsBlocked = true
	}

	return mi
}

// computeDaysInStatus parses status_category_changed_at and returns days since then.
func computeDaysInStatus(changedAt string, now time.Time) int {
	if changedAt == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"} {
		if t, err := time.Parse(layout, changedAt); err == nil {
			days := int(now.Sub(t).Hours() / 24)
			if days < 0 {
				days = 0
			}
			return days
		}
	}
	return 0
}

// addParticipant adds a unique participant to the list.
func addParticipant(participants *[]PingTarget, seen *map[string]bool, slackID, displayName, reason string) {
	if slackID == "" || (*seen)[slackID] {
		return
	}
	(*seen)[slackID] = true
	*participants = append(*participants, PingTarget{
		SlackUserID: slackID,
		DisplayName: displayName,
		Reason:      reason,
	})
}
