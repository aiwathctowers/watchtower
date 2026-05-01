package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// This file hosts read-only Jira analytics queries: per-user delivery stats,
// team workload rollups, epic aggregates, release planning, blocked/stale
// issue scans, and activity helpers (Slack message + meeting hours per user).
// CRUD-style mutations and primary-key lookups stay in jira.go alongside
// the issue scanner (scanJiraIssue).

func (db *DB) GetJiraDeliveryStats(slackID string, from, to string) (*DeliveryStats, error) {
	stats := &DeliveryStats{}

	// Issues closed in range.
	err := db.QueryRow(`SELECT COUNT(*) FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category = 'done' AND resolved_at >= ? AND resolved_at <= ? AND is_deleted = 0`,
		slackID, from, to).Scan(&stats.IssuesClosed)
	if err != nil {
		return nil, fmt.Errorf("counting closed issues: %w", err)
	}

	// Average cycle time (days from created_at to resolved_at) for closed issues.
	var avgCycle sql.NullFloat64
	err = db.QueryRow(`SELECT AVG(
			(julianday(resolved_at) - julianday(created_at))
		) FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category = 'done' AND resolved_at >= ? AND resolved_at <= ?
			AND resolved_at != '' AND is_deleted = 0`,
		slackID, from, to).Scan(&avgCycle)
	if err != nil {
		return nil, fmt.Errorf("computing avg cycle time: %w", err)
	}
	if avgCycle.Valid {
		stats.AvgCycleTimeDays = avgCycle.Float64
	}

	// Story points completed.
	var sp sql.NullFloat64
	err = db.QueryRow(`SELECT COALESCE(SUM(story_points), 0) FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category = 'done' AND resolved_at >= ? AND resolved_at <= ? AND is_deleted = 0`,
		slackID, from, to).Scan(&sp)
	if err != nil {
		return nil, fmt.Errorf("summing story points: %w", err)
	}
	if sp.Valid {
		stats.StoryPointsCompleted = sp.Float64
	}

	// Open issues (non-done).
	err = db.QueryRow(`SELECT COUNT(*) FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category != 'done' AND is_deleted = 0`,
		slackID).Scan(&stats.OpenIssues)
	if err != nil {
		return nil, fmt.Errorf("counting open issues: %w", err)
	}

	// Overdue issues (due_date < today, not done).
	today := time.Now().Format("2006-01-02")
	err = db.QueryRow(`SELECT COUNT(*) FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category != 'done' AND due_date != '' AND due_date < ? AND is_deleted = 0`,
		slackID, today).Scan(&stats.OverdueIssues)
	if err != nil {
		return nil, fmt.Errorf("counting overdue issues: %w", err)
	}

	// Distinct components from closed issues.
	compRows, err := db.Query(`SELECT DISTINCT components FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category = 'done' AND resolved_at >= ? AND resolved_at <= ?
			AND components != '[]' AND is_deleted = 0`,
		slackID, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying components: %w", err)
	}
	defer compRows.Close()

	compSet := make(map[string]bool)
	for compRows.Next() {
		var raw string
		if err := compRows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scanning components: %w", err)
		}
		var arr []string
		if json.Unmarshal([]byte(raw), &arr) == nil {
			for _, c := range arr {
				compSet[c] = true
			}
		}
	}
	if err := compRows.Err(); err != nil {
		return nil, err
	}
	for c := range compSet {
		stats.Components = append(stats.Components, c)
	}

	// Distinct labels from closed issues.
	labelRows, err := db.Query(`SELECT DISTINCT labels FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category = 'done' AND resolved_at >= ? AND resolved_at <= ?
			AND labels != '[]' AND is_deleted = 0`,
		slackID, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying labels: %w", err)
	}
	defer labelRows.Close()

	labelSet := make(map[string]bool)
	for labelRows.Next() {
		var raw string
		if err := labelRows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scanning labels: %w", err)
		}
		var arr []string
		if json.Unmarshal([]byte(raw), &arr) == nil {
			for _, l := range arr {
				labelSet[l] = true
			}
		}
	}
	if err := labelRows.Err(); err != nil {
		return nil, err
	}
	for l := range labelSet {
		stats.Labels = append(stats.Labels, l)
	}

	return stats, nil
}

// GetJiraTeamWorkload returns workload metrics grouped by assignee.
// Only issues with a non-empty assignee_slack_id are included.
func (db *DB) GetJiraTeamWorkload() ([]TeamWorkloadRow, error) {
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")

	rows, err := db.Query(`
		SELECT
			ji.assignee_slack_id,
			ji.assignee_display_name,
			COUNT(*) FILTER (WHERE ji.status_category != 'done') AS open_issues,
			COALESCE(SUM(ji.story_points) FILTER (WHERE ji.status_category != 'done'), 0) AS story_points,
			COUNT(*) FILTER (WHERE ji.status_category != 'done' AND ji.due_date != '' AND ji.due_date < ?) AS overdue_count,
			COUNT(*) FILTER (WHERE ji.status_category != 'done' AND LOWER(ji.status) LIKE '%block%') AS blocked_count,
			AVG(julianday(ji.resolved_at) - julianday(ji.created_at))
				FILTER (WHERE ji.status_category = 'done' AND ji.resolved_at != '' AND ji.resolved_at >= ?) AS avg_cycle_time_days
		FROM jira_issues ji
		WHERE ji.assignee_slack_id != '' AND ji.is_deleted = 0
		GROUP BY ji.assignee_slack_id
		ORDER BY open_issues DESC
	`, today, thirtyDaysAgo)
	if err != nil {
		return nil, fmt.Errorf("querying team workload: %w", err)
	}
	defer rows.Close()

	var result []TeamWorkloadRow
	for rows.Next() {
		var r TeamWorkloadRow
		var sp sql.NullFloat64
		var avgCycle sql.NullFloat64
		var displayName sql.NullString
		if err := rows.Scan(&r.SlackUserID, &displayName, &r.OpenIssues, &sp, &r.OverdueCount, &r.BlockedCount, &avgCycle); err != nil {
			return nil, fmt.Errorf("scanning team workload row: %w", err)
		}
		if displayName.Valid {
			r.DisplayName = displayName.String
		}
		if sp.Valid {
			r.StoryPoints = sp.Float64
		}
		if avgCycle.Valid {
			r.AvgCycleTimeDays = math.Round(avgCycle.Float64*100) / 100
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// EpicAggRow holds aggregated issue counts for a single epic.
type EpicAggRow struct {
	EpicKey          string
	Total            int
	Done             int
	InProgress       int
	ResolvedLastWeek int
	ResolvedLast4W   int
}

// GetJiraEpicAggregates returns aggregated issue counts per epic_key.
// Only non-deleted issues with a non-empty epic_key are included.
// The weekAgo and fourWeeksAgo parameters are ISO8601 datetime strings used to
// count recently resolved issues.
func (db *DB) GetJiraEpicAggregates(weekAgo, fourWeeksAgo string) ([]EpicAggRow, error) {
	rows, err := db.Query(`
		SELECT
			epic_key,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE LOWER(status_category) = 'done') AS done,
			COUNT(*) FILTER (WHERE LOWER(status_category) = 'in_progress' OR LOWER(status_category) = 'in progress' OR LOWER(status_category) = 'indeterminate') AS in_progress,
			COUNT(*) FILTER (WHERE LOWER(status_category) = 'done' AND resolved_at >= ?) AS resolved_last_week,
			COUNT(*) FILTER (WHERE LOWER(status_category) = 'done' AND resolved_at >= ?) AS resolved_last_4w
		FROM jira_issues
		WHERE epic_key != '' AND is_deleted = 0
		GROUP BY epic_key
	`, weekAgo, fourWeeksAgo)
	if err != nil {
		return nil, fmt.Errorf("querying epic aggregates: %w", err)
	}
	defer rows.Close()

	var result []EpicAggRow
	for rows.Next() {
		var r EpicAggRow
		if err := rows.Scan(&r.EpicKey, &r.Total, &r.Done, &r.InProgress, &r.ResolvedLastWeek, &r.ResolvedLast4W); err != nil {
			return nil, fmt.Errorf("scanning epic aggregate: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetJiraIssuesByKeysMap returns issues matching the given keys as a map keyed by issue key.
func (db *DB) GetJiraIssuesByKeysMap(keys []string) (map[string]JiraIssue, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	issues, err := db.GetJiraIssuesByKeys(keys)
	if err != nil {
		return nil, err
	}
	m := make(map[string]JiraIssue, len(issues))
	for _, issue := range issues {
		m[issue.Key] = issue
	}
	return m, nil
}

// ComponentResolver holds a user who resolved issues for a given component.
type ComponentResolver struct {
	SlackUserID string
	DisplayName string
	Count       int
}

// GetTopResolversByComponent returns users who resolved the most issues containing the given component.
// The component is matched as a JSON substring inside the components column (JSON array).
// Results are ordered by count descending, limited to `limit` rows.
func (db *DB) GetTopResolversByComponent(component string, limit int) ([]ComponentResolver, error) {
	if component == "" {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT assignee_slack_id, assignee_display_name, COUNT(*) AS cnt
		FROM jira_issues
		WHERE is_deleted = 0
			AND status_category = 'done'
			AND assignee_slack_id != ''
			AND EXISTS (SELECT 1 FROM json_each(jira_issues.components) WHERE value = ?)
		GROUP BY assignee_slack_id
		ORDER BY cnt DESC
		LIMIT ?`, component, limit)
	if err != nil {
		return nil, fmt.Errorf("querying top resolvers for component %s: %w", component, err)
	}
	defer rows.Close()

	var result []ComponentResolver
	for rows.Next() {
		var r ComponentResolver
		if err := rows.Scan(&r.SlackUserID, &r.DisplayName, &r.Count); err != nil {
			return nil, fmt.Errorf("scanning top resolver: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetJiraIssuesByEpicKey returns all non-deleted child issues for a given epic key.
func (db *DB) GetJiraIssuesByEpicKey(epicKey string) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT `+jiraIssueColumns+`
		FROM jira_issues WHERE epic_key = ? AND is_deleted = 0 ORDER BY key`, epicKey)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues by epic key %s: %w", epicKey, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue by epic key: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraIssuesByEpicKeys returns all non-deleted child issues for the given epic keys,
// grouped by epic key. This avoids N+1 queries when loading children for multiple epics.
func (db *DB) GetJiraIssuesByEpicKeys(epicKeys []string) (map[string][]JiraIssue, error) {
	if len(epicKeys) == 0 {
		return map[string][]JiraIssue{}, nil
	}
	placeholders := strings.Repeat("?,", len(epicKeys))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, len(epicKeys))
	for i, k := range epicKeys {
		args[i] = k
	}

	rows, err := db.Query(`SELECT `+jiraIssueColumns+`
		FROM jira_issues WHERE epic_key IN (`+placeholders+`) AND is_deleted = 0 ORDER BY key`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues by epic keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]JiraIssue)
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue by epic keys: %w", err)
		}
		result[issue.EpicKey] = append(result[issue.EpicKey], issue)
	}
	return result, rows.Err()
}

// GetJiraSlackLinksByIssueKeys returns all Slack links for a set of issue keys.
func (db *DB) GetJiraSlackLinksByIssueKeys(keys []string) ([]JiraSlackLink, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	rows, err := db.Query(`SELECT id, issue_key, channel_id, message_ts, track_id, digest_id, link_type, detected_at
		FROM jira_slack_links WHERE issue_key IN (`+placeholders+`) ORDER BY detected_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying jira slack links by issue keys: %w", err)
	}
	defer rows.Close()

	var links []JiraSlackLink
	for rows.Next() {
		var l JiraSlackLink
		if err := rows.Scan(&l.ID, &l.IssueKey, &l.ChannelID, &l.MessageTS, &l.TrackID, &l.DigestID, &l.LinkType, &l.DetectedAt); err != nil {
			return nil, fmt.Errorf("scanning jira slack link by issue keys: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetJiraDecisionCountByIssueKeys returns the number of slack links with link_type='decision'
// for the given issue keys.
func (db *DB) GetJiraDecisionCountByIssueKeys(keys []string) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM jira_slack_links
		WHERE issue_key IN (`+placeholders+`) AND link_type = 'decision'`, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting jira decisions by issue keys: %w", err)
	}
	return count, nil
}

// UpsertJiraRelease inserts or updates a Jira release (fix version).
func (db *DB) UpsertJiraRelease(r JiraRelease) error {
	_, err := db.Exec(`INSERT INTO jira_releases (id, project_key, name, description, release_date, released, archived, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET project_key=excluded.project_key, name=excluded.name,
			description=excluded.description, release_date=excluded.release_date,
			released=excluded.released, archived=excluded.archived, synced_at=excluded.synced_at`,
		r.ID, r.ProjectKey, r.Name, r.Description, r.ReleaseDate, r.Released, r.Archived, r.SyncedAt)
	if err != nil {
		return fmt.Errorf("upserting jira release %d (%s): %w", r.ID, r.Name, err)
	}
	return nil
}

// GetJiraReleases returns all releases for a project, sorted by release_date.
func (db *DB) GetJiraReleases(projectKey string) ([]JiraRelease, error) {
	rows, err := db.Query(`SELECT id, project_key, name, description, release_date, released, archived, synced_at
		FROM jira_releases WHERE project_key = ? ORDER BY release_date, name`, projectKey)
	if err != nil {
		return nil, fmt.Errorf("querying jira releases for %s: %w", projectKey, err)
	}
	defer rows.Close()

	var releases []JiraRelease
	for rows.Next() {
		var r JiraRelease
		if err := rows.Scan(&r.ID, &r.ProjectKey, &r.Name, &r.Description, &r.ReleaseDate, &r.Released, &r.Archived, &r.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira release: %w", err)
		}
		releases = append(releases, r)
	}
	return releases, rows.Err()
}

// GetJiraReleasesByName returns releases matching a name across all projects.
func (db *DB) GetJiraReleasesByName(name string) ([]JiraRelease, error) {
	rows, err := db.Query(`SELECT id, project_key, name, description, release_date, released, archived, synced_at
		FROM jira_releases WHERE name = ? ORDER BY project_key`, name)
	if err != nil {
		return nil, fmt.Errorf("querying jira releases by name %s: %w", name, err)
	}
	defer rows.Close()

	var releases []JiraRelease
	for rows.Next() {
		var r JiraRelease
		if err := rows.Scan(&r.ID, &r.ProjectKey, &r.Name, &r.Description, &r.ReleaseDate, &r.Released, &r.Archived, &r.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira release by name: %w", err)
		}
		releases = append(releases, r)
	}
	return releases, rows.Err()
}

// GetAllJiraReleases returns all releases across all projects, sorted by release_date.
func (db *DB) GetAllJiraReleases() ([]JiraRelease, error) {
	rows, err := db.Query(`SELECT id, project_key, name, description, release_date, released, archived, synced_at
		FROM jira_releases ORDER BY release_date, name`)
	if err != nil {
		return nil, fmt.Errorf("querying all jira releases: %w", err)
	}
	defer rows.Close()

	var releases []JiraRelease
	for rows.Next() {
		var r JiraRelease
		if err := rows.Scan(&r.ID, &r.ProjectKey, &r.Name, &r.Description, &r.ReleaseDate, &r.Released, &r.Archived, &r.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira release: %w", err)
		}
		releases = append(releases, r)
	}
	return releases, rows.Err()
}

// GetJiraIssuesByFixVersion returns all non-deleted issues that have the given version name in their fix_versions JSON array.
func (db *DB) GetJiraIssuesByFixVersion(versionName string) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT `+jiraIssueColumns+` FROM jira_issues
		WHERE EXISTS (SELECT 1 FROM json_each(jira_issues.fix_versions) WHERE value = ?)
		AND is_deleted = 0`, versionName)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues by fix version %s: %w", versionName, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue by fix version: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraIssueCountAddedSince returns the count of non-deleted issues with the given version name
// in fix_versions whose synced_at is after the given timestamp (approximate scope tracking).
func (db *DB) GetJiraIssueCountAddedSince(versionName string, since string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM jira_issues
		WHERE EXISTS (SELECT 1 FROM json_each(jira_issues.fix_versions) WHERE value = ?)
		AND synced_at > ? AND is_deleted = 0`,
		versionName, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting jira issues added since %s for version %s: %w", since, versionName, err)
	}
	return count, nil
}

// GetAllJiraIssuesWithFixVersions returns all non-deleted issues that have non-empty fix_versions.
// This is used for batch loading in the release dashboard to avoid N+1 queries.
func (db *DB) GetAllJiraIssuesWithFixVersions() ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT ` + jiraIssueColumns + `
		FROM jira_issues WHERE fix_versions != '' AND fix_versions != '[]' AND is_deleted = 0`)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues with fix versions: %w", err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue with fix versions: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetBlockedJiraIssues returns non-done, non-deleted issues whose status contains "block" (case-insensitive).
func (db *DB) GetBlockedJiraIssues() ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT ` + jiraIssueColumns + `
		FROM jira_issues
		WHERE status_category != 'done' AND is_deleted = 0 AND LOWER(status) LIKE '%block%'
		ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("querying blocked jira issues: %w", err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning blocked jira issue: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetStaleJiraIssues returns in_progress, non-deleted issues whose status_category_changed_at
// is before the given cutoff time (RFC3339 string).
func (db *DB) GetStaleJiraIssues(cutoff string) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT `+jiraIssueColumns+`
		FROM jira_issues
		WHERE status_category = 'in_progress' AND is_deleted = 0
			AND status_category_changed_at != '' AND status_category_changed_at < ?
		ORDER BY key`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("querying stale jira issues: %w", err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning stale jira issue: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// uniqueKeys converts a map[string]bool to a string slice.
func uniqueKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// GetUserMessageCount returns the number of messages sent by a user in a time range.
// from and to are time.Time values converted to unix timestamps for the ts_unix column.
func (db *DB) GetUserMessageCount(userID string, from, to time.Time) (int, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM messages
		WHERE user_id = ? AND ts_unix >= ? AND ts_unix < ?`,
		userID, float64(from.Unix()), float64(to.Unix()),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting messages for user %s: %w", userID, err)
	}
	return count, nil
}

// GetUserMeetingHours returns the total meeting hours for a Slack user in a time range.
// It joins calendar_events with calendar_attendee_map to find events where the user
// is an attendee. All-day events are excluded. Hours are computed from start_time/end_time.
func (db *DB) GetUserMeetingHours(slackUserID string, from, to time.Time) (float64, error) {
	fromStr := from.UTC().Format("2006-01-02T15:04:05Z")
	toStr := to.UTC().Format("2006-01-02T15:04:05Z")

	rows, err := db.Query(`
		SELECT ce.start_time, ce.end_time
		FROM calendar_events ce
		JOIN calendar_attendee_map cam ON cam.slack_user_id = ?
		WHERE ce.is_all_day = 0
		  AND ce.start_time >= ? AND ce.start_time < ?
		  AND ce.attendees LIKE '%' || cam.email || '%'`,
		slackUserID, fromStr, toStr,
	)
	if err != nil {
		return 0, fmt.Errorf("querying meeting hours for user %s: %w", slackUserID, err)
	}
	defer rows.Close()

	var totalHours float64
	for rows.Next() {
		var startStr, endStr string
		if err := rows.Scan(&startStr, &endStr); err != nil {
			return 0, fmt.Errorf("scanning meeting hours row: %w", err)
		}
		start, err1 := time.Parse(time.RFC3339, startStr)
		end, err2 := time.Parse(time.RFC3339, endStr)
		if err1 != nil || err2 != nil {
			continue // skip unparseable events
		}
		totalHours += end.Sub(start).Hours()
	}
	return math.Round(totalHours*100) / 100, rows.Err()
}

// UpsertJiraCustomField inserts or updates a custom field.
func (db *DB) UpsertJiraCustomField(f JiraCustomField) error {
	_, err := db.Exec(`INSERT INTO jira_custom_fields (id, name, field_type, items_type, is_useful, usage_hint, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, field_type=excluded.field_type,
		items_type=excluded.items_type, synced_at=excluded.synced_at`,
		f.ID, f.Name, f.FieldType, f.ItemsType, f.IsUseful, f.UsageHint, f.SyncedAt)
	return err
}

// UpdateJiraCustomFieldClassification updates LLM classification for a field.
func (db *DB) UpdateJiraCustomFieldClassification(id string, isUseful bool, usageHint string) error {
	_, err := db.Exec(`UPDATE jira_custom_fields SET is_useful=?, usage_hint=? WHERE id=?`,
		isUseful, usageHint, id)
	return err
}

// GetJiraCustomFields returns all custom fields.
func (db *DB) GetJiraCustomFields() ([]JiraCustomField, error) {
	rows, err := db.Query(`SELECT id, name, field_type, items_type, is_useful, usage_hint, synced_at
		FROM jira_custom_fields ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []JiraCustomField
	for rows.Next() {
		var f JiraCustomField
		if err := rows.Scan(&f.ID, &f.Name, &f.FieldType, &f.ItemsType, &f.IsUseful, &f.UsageHint, &f.SyncedAt); err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// GetUsefulJiraCustomFields returns only fields marked as useful by LLM.
func (db *DB) GetUsefulJiraCustomFields() ([]JiraCustomField, error) {
	rows, err := db.Query(`SELECT id, name, field_type, items_type, is_useful, usage_hint, synced_at
		FROM jira_custom_fields WHERE is_useful = 1 ORDER BY usage_hint, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []JiraCustomField
	for rows.Next() {
		var f JiraCustomField
		if err := rows.Scan(&f.ID, &f.Name, &f.FieldType, &f.ItemsType, &f.IsUseful, &f.UsageHint, &f.SyncedAt); err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// GetJiraCustomFieldsSyncedAt returns the most recent synced_at for custom fields.
func (db *DB) GetJiraCustomFieldsSyncedAt() (string, error) {
	var syncedAt string
	err := db.QueryRow(`SELECT COALESCE(MAX(synced_at), '') FROM jira_custom_fields`).Scan(&syncedAt)
	return syncedAt, err
}

// UpsertJiraBoardFieldMap sets the field mapping for a board. Replaces all existing mappings.
func (db *DB) UpsertJiraBoardFieldMap(boardID int, mappings []JiraBoardFieldMap) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM jira_board_field_map WHERE board_id = ?`, boardID); err != nil {
		return err
	}
	for _, m := range mappings {
		if _, err := tx.Exec(`INSERT INTO jira_board_field_map (board_id, field_id, role) VALUES (?, ?, ?)`,
			boardID, m.FieldID, m.Role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetJiraBoardFieldMap returns field mappings for a board.
func (db *DB) GetJiraBoardFieldMap(boardID int) ([]JiraBoardFieldMap, error) {
	rows, err := db.Query(`SELECT board_id, field_id, role FROM jira_board_field_map WHERE board_id = ?`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mappings []JiraBoardFieldMap
	for rows.Next() {
		var m JiraBoardFieldMap
		if err := rows.Scan(&m.BoardID, &m.FieldID, &m.Role); err != nil {
			return nil, err
		}
		mappings = append(mappings, m)
	}
	return mappings, rows.Err()
}
