package jira

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// ReleaseEntry holds aggregated release data with progress, at-risk flags, and scope changes.
type ReleaseEntry struct {
	Name         string                `json:"name"`
	ProjectKey   string                `json:"project_key"`
	ReleaseDate  string                `json:"release_date"`
	Released     bool                  `json:"released"`
	IsOverdue    bool                  `json:"is_overdue"`
	AtRisk       bool                  `json:"at_risk"`
	AtRiskReason string                `json:"at_risk_reason"`
	EpicProgress []ReleaseEpicProgress `json:"epic_progress"`
	BlockedCount int                   `json:"blocked_count"`
	ScopeChanges ScopeChange           `json:"scope_changes"`
	TotalIssues  int                   `json:"total_issues"`
	DoneIssues   int                   `json:"done_issues"`
	ProgressPct  float64               `json:"progress_pct"`
}

// ReleaseEpicProgress holds per-epic progress within a release.
type ReleaseEpicProgress struct {
	EpicKey     string  `json:"epic_key"`
	EpicName    string  `json:"epic_name"`
	ProgressPct float64 `json:"progress_pct"`
	StatusBadge string  `json:"status_badge"`
	Total       int     `json:"total"`
	Done        int     `json:"done"`
}

// ScopeChange tracks how many issues were added recently.
type ScopeChange struct {
	AddedLastWeek int `json:"added_last_week"`
}

// BuildReleaseDashboard returns unreleased, non-archived releases with aggregated progress.
// Returns nil, nil when the release_dashboard feature is disabled.
func BuildReleaseDashboard(database *db.DB, cfg *config.Config, projectKey string, now time.Time) ([]ReleaseEntry, error) {
	if !IsFeatureEnabled(cfg, "release_dashboard") {
		return nil, nil
	}

	var releases []db.JiraRelease
	var err error
	if projectKey == "" {
		releases, err = database.GetAllJiraReleases()
	} else {
		releases, err = database.GetJiraReleases(projectKey)
	}
	if err != nil {
		return nil, err
	}

	// Filter: only unreleased, non-archived.
	var filtered []db.JiraRelease
	for _, r := range releases {
		if !r.Released && !r.Archived {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	weekAgo := now.AddDate(0, 0, -7).Format(time.RFC3339)

	// Batch-load all issues with fix versions to avoid N+1 queries per release.
	allIssues, err := database.GetAllJiraIssuesWithFixVersions()
	if err != nil {
		return nil, err
	}

	// Group issues by fix version name.
	issuesByVersion := make(map[string][]db.JiraIssue)
	for _, issue := range allIssues {
		versions := parseFixVersions(issue.FixVersions)
		for _, v := range versions {
			issuesByVersion[v] = append(issuesByVersion[v], issue)
		}
	}

	var entries []ReleaseEntry
	for _, rel := range filtered {
		issues := issuesByVersion[rel.Name]
		entry, err := buildReleaseEntry(database, rel, now, weekAgo, issues)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	// Sort by release_date (empty dates last).
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ReleaseDate == "" && entries[j].ReleaseDate == "" {
			return entries[i].Name < entries[j].Name
		}
		if entries[i].ReleaseDate == "" {
			return false
		}
		if entries[j].ReleaseDate == "" {
			return true
		}
		return entries[i].ReleaseDate < entries[j].ReleaseDate
	})

	return entries, nil
}

// BuildReleaseDetail returns detailed data for a single release by name.
// Returns nil, nil when the release_dashboard feature is disabled or the release is not found.
func BuildReleaseDetail(database *db.DB, cfg *config.Config, releaseName string, now time.Time) (*ReleaseEntry, error) {
	if !IsFeatureEnabled(cfg, "release_dashboard") {
		return nil, nil
	}

	releases, err := database.GetJiraReleasesByName(releaseName)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, nil
	}

	// Use the first matching release.
	rel := releases[0]
	weekAgo := now.AddDate(0, 0, -7).Format(time.RFC3339)

	// Load issues for this specific release.
	issues, err := database.GetJiraIssuesByFixVersion(rel.Name)
	if err != nil {
		return nil, err
	}

	entry, err := buildReleaseEntry(database, rel, now, weekAgo, issues)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// buildReleaseEntry constructs a ReleaseEntry for a single release.
// The issues parameter contains pre-loaded issues for this release.
func buildReleaseEntry(database *db.DB, rel db.JiraRelease, now time.Time, weekAgo string, issues []db.JiraIssue) (ReleaseEntry, error) {
	totalIssues := len(issues)
	doneIssues := 0
	blockedCount := 0

	// Group issues by epic_key for per-epic progress.
	type epicAgg struct {
		total int
		done  int
	}
	epicMap := make(map[string]*epicAgg)

	for _, issue := range issues {
		if issue.StatusCategory == "done" {
			doneIssues++
		}
		if isBlocked(issue) {
			blockedCount++
		}

		epicKey := issue.EpicKey
		if epicKey == "" {
			epicKey = "(no epic)"
		}
		agg, ok := epicMap[epicKey]
		if !ok {
			agg = &epicAgg{}
			epicMap[epicKey] = agg
		}
		agg.total++
		if issue.StatusCategory == "done" {
			agg.done++
		}
	}

	// Build epic progress entries.
	var epicProgress []ReleaseEpicProgress
	// Collect epic keys for name lookup.
	var epicKeys []string
	for k := range epicMap {
		if k != "(no epic)" {
			epicKeys = append(epicKeys, k)
		}
	}

	epicIssues, err := database.GetJiraIssuesByKeysMap(epicKeys)
	if err != nil {
		return ReleaseEntry{}, err
	}

	for epicKey, agg := range epicMap {
		var pct float64
		if agg.total > 0 {
			pct = float64(agg.done) / float64(agg.total) * 100
		}

		badge := releaseEpicBadge(pct)

		epicName := epicKey
		if epicKey != "(no epic)" {
			if issue, ok := epicIssues[epicKey]; ok {
				epicName = issue.Summary
			}
		}

		epicProgress = append(epicProgress, ReleaseEpicProgress{
			EpicKey:     epicKey,
			EpicName:    epicName,
			ProgressPct: pct,
			StatusBadge: badge,
			Total:       agg.total,
			Done:        agg.done,
		})
	}

	// Sort epic progress: behind first, then at_risk, then on_track.
	sort.Slice(epicProgress, func(i, j int) bool {
		oi := badgeOrder(epicProgress[i].StatusBadge)
		oj := badgeOrder(epicProgress[j].StatusBadge)
		if oi != oj {
			return oi < oj
		}
		return epicProgress[i].EpicKey < epicProgress[j].EpicKey
	})

	// Progress percentage.
	var progressPct float64
	if totalIssues > 0 {
		progressPct = float64(doneIssues) / float64(totalIssues) * 100
	}

	// Overdue detection: compare date strings to avoid timezone issues.
	isOverdue := false
	nowDate := now.Format("2006-01-02")
	if rel.ReleaseDate != "" && !rel.Released {
		if rel.ReleaseDate < nowDate {
			isOverdue = true
		}
	}

	// At-risk detection.
	atRisk := false
	atRiskReason := ""

	// Rule 1: >30% issues blocked.
	if totalIssues > 0 {
		blockedPct := float64(blockedCount) / float64(totalIssues) * 100
		if blockedPct > 30 {
			atRisk = true
			atRiskReason = "30%+ issues blocked"
		}
	}

	// Rule 2: deadline <7 days AND progress <80%.
	if !atRisk && rel.ReleaseDate != "" && !rel.Released {
		releaseTime, parseErr := parseReleaseDate(rel.ReleaseDate)
		if parseErr == nil {
			daysUntil := releaseTime.Sub(now.Truncate(24*time.Hour)).Hours() / 24
			if daysUntil >= 0 && daysUntil < 7 && progressPct < 80 {
				atRisk = true
				atRiskReason = "deadline approaching, insufficient progress"
			}
		}
	}

	// Scope changes (approximate: count issues synced in last week).
	addedLastWeek, err := database.GetJiraIssueCountAddedSince(rel.Name, weekAgo)
	if err != nil {
		return ReleaseEntry{}, err
	}

	return ReleaseEntry{
		Name:         rel.Name,
		ProjectKey:   rel.ProjectKey,
		ReleaseDate:  rel.ReleaseDate,
		Released:     rel.Released,
		IsOverdue:    isOverdue,
		AtRisk:       atRisk,
		AtRiskReason: atRiskReason,
		EpicProgress: epicProgress,
		BlockedCount: blockedCount,
		ScopeChanges: ScopeChange{
			AddedLastWeek: addedLastWeek,
		},
		TotalIssues: totalIssues,
		DoneIssues:  doneIssues,
		ProgressPct: progressPct,
	}, nil
}

// isBlocked returns true if an issue is considered blocked.
// An issue is blocked if its status contains "block" (case-insensitive) and it is not done.
func isBlocked(issue db.JiraIssue) bool {
	if issue.StatusCategory == "done" {
		return false
	}
	return strings.Contains(strings.ToLower(issue.Status), "block")
}

// releaseEpicBadge determines a status badge based on completion percentage.
func releaseEpicBadge(progressPct float64) string {
	switch {
	case progressPct >= 100:
		return "on_track"
	case progressPct >= 50:
		return "at_risk"
	default:
		return "behind"
	}
}

// parseReleaseDate tries to parse a release date string in common formats.
func parseReleaseDate(s string) (time.Time, error) {
	// Try ISO date first (YYYY-MM-DD).
	t, err := time.Parse("2006-01-02", s)
	if err == nil {
		return t, nil
	}
	// Try RFC3339.
	t, err = time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	return time.Time{}, err
}

// parseFixVersions parses the fix_versions JSON array string into a slice of version names.
func parseFixVersions(fixVersions string) []string {
	if fixVersions == "" || fixVersions == "[]" {
		return nil
	}
	var versions []string
	if err := json.Unmarshal([]byte(fixVersions), &versions); err != nil {
		return nil
	}
	return versions
}
