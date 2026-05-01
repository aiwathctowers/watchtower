package db

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// UpsertJiraBoard inserts or updates a Jira board. Profile columns are NOT overwritten on conflict.
func (db *DB) UpsertJiraBoard(board JiraBoard) error {
	_, err := db.Exec(`INSERT INTO jira_boards (id, name, project_key, board_type, is_selected, issue_count, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, project_key=excluded.project_key,
			board_type=excluded.board_type, issue_count=excluded.issue_count, synced_at=excluded.synced_at`,
		board.ID, board.Name, board.ProjectKey, board.BoardType, board.IsSelected, board.IssueCount, board.SyncedAt)
	if err != nil {
		return fmt.Errorf("upserting jira board %d: %w", board.ID, err)
	}
	return nil
}

// UpdateJiraBoardProfile updates board analysis profile columns.
func (db *DB) UpdateJiraBoardProfile(boardID int, rawColumnsJSON, rawConfigJSON, llmProfileJSON, workflowSummary, configHash, profileGeneratedAt string) error {
	_, err := db.Exec(`UPDATE jira_boards SET raw_columns_json=?, raw_config_json=?, llm_profile_json=?,
		workflow_summary=?, config_hash=?, profile_generated_at=? WHERE id=?`,
		rawColumnsJSON, rawConfigJSON, llmProfileJSON, workflowSummary, configHash, profileGeneratedAt, boardID)
	if err != nil {
		return fmt.Errorf("updating jira board profile %d: %w", boardID, err)
	}
	return nil
}

// UpdateJiraBoardIssueCount sets issue_count from the actual number of issues in the database.
func (db *DB) UpdateJiraBoardIssueCount(boardID int) error {
	_, err := db.Exec(`UPDATE jira_boards SET issue_count = (SELECT COUNT(*) FROM jira_issues WHERE board_id = ?) WHERE id = ?`, boardID, boardID)
	return err
}

// UpdateJiraBoardUserOverrides updates user overrides for a board.
func (db *DB) UpdateJiraBoardUserOverrides(boardID int, userOverridesJSON string) error {
	_, err := db.Exec(`UPDATE jira_boards SET user_overrides_json=? WHERE id=?`, userOverridesJSON, boardID)
	if err != nil {
		return fmt.Errorf("updating jira board user overrides %d: %w", boardID, err)
	}
	return nil
}

// GetJiraBoardProfile returns a board with all profile columns by ID.
func (db *DB) GetJiraBoardProfile(boardID int) (*JiraBoard, error) {
	row := db.QueryRow(`SELECT id, name, project_key, board_type, is_selected, issue_count, synced_at,
		raw_columns_json, raw_config_json, llm_profile_json, workflow_summary,
		user_overrides_json, config_hash, profile_generated_at
		FROM jira_boards WHERE id = ?`, boardID)

	var b JiraBoard
	err := row.Scan(&b.ID, &b.Name, &b.ProjectKey, &b.BoardType, &b.IsSelected, &b.IssueCount, &b.SyncedAt,
		&b.RawColumnsJSON, &b.RawConfigJSON, &b.LLMProfileJSON, &b.WorkflowSummary,
		&b.UserOverridesJSON, &b.ConfigHash, &b.ProfileGeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning jira board profile %d: %w", boardID, err)
	}
	return &b, nil
}

// GetJiraBoards returns all Jira boards.
func (db *DB) GetJiraBoards() ([]JiraBoard, error) {
	rows, err := db.Query(`SELECT id, name, project_key, board_type, is_selected, issue_count, synced_at FROM jira_boards ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying jira boards: %w", err)
	}
	defer rows.Close()

	var boards []JiraBoard
	for rows.Next() {
		var b JiraBoard
		if err := rows.Scan(&b.ID, &b.Name, &b.ProjectKey, &b.BoardType, &b.IsSelected, &b.IssueCount, &b.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira board: %w", err)
		}
		boards = append(boards, b)
	}
	return boards, rows.Err()
}

// GetJiraSelectedBoards returns boards where is_selected = 1.
func (db *DB) GetJiraSelectedBoards() ([]JiraBoard, error) {
	rows, err := db.Query(`SELECT id, name, project_key, board_type, is_selected, issue_count, synced_at FROM jira_boards WHERE is_selected = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying selected jira boards: %w", err)
	}
	defer rows.Close()

	var boards []JiraBoard
	for rows.Next() {
		var b JiraBoard
		if err := rows.Scan(&b.ID, &b.Name, &b.ProjectKey, &b.BoardType, &b.IsSelected, &b.IssueCount, &b.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira board: %w", err)
		}
		boards = append(boards, b)
	}
	return boards, rows.Err()
}

// GetJiraSelectedBoardsWithProfile returns selected boards that have a profile (config_hash != ”).
// Useful for detecting config changes by comparing stored config_hash with freshly computed hashes.
func (db *DB) GetJiraSelectedBoardsWithProfile() ([]JiraBoard, error) {
	rows, err := db.Query(`SELECT id, name, project_key, board_type, is_selected, issue_count, synced_at,
		raw_columns_json, raw_config_json, llm_profile_json, workflow_summary,
		user_overrides_json, config_hash, profile_generated_at
		FROM jira_boards WHERE is_selected = 1 AND config_hash != ''
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying boards with changed config: %w", err)
	}
	defer rows.Close()

	var boards []JiraBoard
	for rows.Next() {
		var b JiraBoard
		if err := rows.Scan(&b.ID, &b.Name, &b.ProjectKey, &b.BoardType, &b.IsSelected, &b.IssueCount, &b.SyncedAt,
			&b.RawColumnsJSON, &b.RawConfigJSON, &b.LLMProfileJSON, &b.WorkflowSummary,
			&b.UserOverridesJSON, &b.ConfigHash, &b.ProfileGeneratedAt); err != nil {
			return nil, fmt.Errorf("scanning board with changed config: %w", err)
		}
		boards = append(boards, b)
	}
	return boards, rows.Err()
}

// SetJiraBoardSelected updates the is_selected flag for a Jira board.
func (db *DB) SetJiraBoardSelected(boardID int, selected bool) error {
	_, err := db.Exec(`UPDATE jira_boards SET is_selected = ? WHERE id = ?`, selected, boardID)
	if err != nil {
		return fmt.Errorf("setting jira board %d selected=%v: %w", boardID, selected, err)
	}
	return nil
}

// UpsertJiraIssue inserts or updates a Jira issue.
func (db *DB) UpsertJiraIssue(issue JiraIssue) error {
	_, err := db.Exec(`INSERT INTO jira_issues (key, id, project_key, board_id, summary, description_text,
		issue_type, issue_type_category, is_bug, status, status_category, status_category_changed_at,
		assignee_account_id, assignee_email, assignee_display_name, assignee_slack_id,
		reporter_account_id, reporter_email, reporter_display_name, reporter_slack_id,
		priority, story_points, due_date, sprint_id, sprint_name, epic_key,
		labels, components, fix_versions, created_at, updated_at, resolved_at, raw_json, custom_fields_json, synced_at, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			id=excluded.id, project_key=excluded.project_key, board_id=excluded.board_id,
			summary=excluded.summary, description_text=excluded.description_text,
			issue_type=excluded.issue_type, issue_type_category=excluded.issue_type_category, is_bug=excluded.is_bug,
			status=excluded.status, status_category=excluded.status_category,
			status_category_changed_at=excluded.status_category_changed_at,
			assignee_account_id=excluded.assignee_account_id, assignee_email=excluded.assignee_email,
			assignee_display_name=excluded.assignee_display_name, assignee_slack_id=excluded.assignee_slack_id,
			reporter_account_id=excluded.reporter_account_id, reporter_email=excluded.reporter_email,
			reporter_display_name=excluded.reporter_display_name, reporter_slack_id=excluded.reporter_slack_id,
			priority=excluded.priority, story_points=excluded.story_points,
			due_date=excluded.due_date, sprint_id=excluded.sprint_id, sprint_name=excluded.sprint_name,
			epic_key=excluded.epic_key, labels=excluded.labels, components=excluded.components,
			fix_versions=excluded.fix_versions,
			created_at=excluded.created_at, updated_at=excluded.updated_at, resolved_at=excluded.resolved_at,
			raw_json=excluded.raw_json, custom_fields_json=excluded.custom_fields_json,
			synced_at=excluded.synced_at, is_deleted=excluded.is_deleted`,
		issue.Key, issue.ID, issue.ProjectKey, issue.BoardID,
		issue.Summary, issue.DescriptionText,
		issue.IssueType, issue.IssueTypeCategory, issue.IsBug,
		issue.Status, issue.StatusCategory, issue.StatusCategoryChangedAt,
		issue.AssigneeAccountID, issue.AssigneeEmail, issue.AssigneeDisplayName, issue.AssigneeSlackID,
		issue.ReporterAccountID, issue.ReporterEmail, issue.ReporterDisplayName, issue.ReporterSlackID,
		issue.Priority, issue.StoryPoints, issue.DueDate,
		issue.SprintID, issue.SprintName, issue.EpicKey,
		issue.Labels, issue.Components, issue.FixVersions,
		issue.CreatedAt, issue.UpdatedAt, issue.ResolvedAt,
		issue.RawJSON, issue.CustomFieldsJSON, issue.SyncedAt, issue.IsDeleted)
	if err != nil {
		return fmt.Errorf("upserting jira issue %s: %w", issue.Key, err)
	}
	return nil
}

// UpsertJiraIssueBatch inserts or updates multiple issues in a single transaction.
func (db *DB) UpsertJiraIssueBatch(issues []JiraIssue, links []JiraIssueLink) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i := range issues {
		issue := &issues[i]
		_, err := tx.Exec(`INSERT INTO jira_issues (key, id, project_key, board_id, summary, description_text,
			issue_type, issue_type_category, is_bug, status, status_category, status_category_changed_at,
			assignee_account_id, assignee_email, assignee_display_name, assignee_slack_id,
			reporter_account_id, reporter_email, reporter_display_name, reporter_slack_id,
			priority, story_points, due_date, sprint_id, sprint_name, epic_key,
			labels, components, fix_versions, created_at, updated_at, resolved_at, raw_json, custom_fields_json, synced_at, is_deleted)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET
				id=excluded.id, project_key=excluded.project_key, board_id=excluded.board_id,
				summary=excluded.summary, description_text=excluded.description_text,
				issue_type=excluded.issue_type, issue_type_category=excluded.issue_type_category, is_bug=excluded.is_bug,
				status=excluded.status, status_category=excluded.status_category,
				status_category_changed_at=excluded.status_category_changed_at,
				assignee_account_id=excluded.assignee_account_id, assignee_email=excluded.assignee_email,
				assignee_display_name=excluded.assignee_display_name, assignee_slack_id=excluded.assignee_slack_id,
				reporter_account_id=excluded.reporter_account_id, reporter_email=excluded.reporter_email,
				reporter_display_name=excluded.reporter_display_name, reporter_slack_id=excluded.reporter_slack_id,
				priority=excluded.priority, story_points=excluded.story_points,
				due_date=excluded.due_date, sprint_id=excluded.sprint_id, sprint_name=excluded.sprint_name,
				epic_key=excluded.epic_key, labels=excluded.labels, components=excluded.components,
				fix_versions=excluded.fix_versions,
				created_at=excluded.created_at, updated_at=excluded.updated_at, resolved_at=excluded.resolved_at,
				raw_json=excluded.raw_json, custom_fields_json=excluded.custom_fields_json,
				synced_at=excluded.synced_at, is_deleted=excluded.is_deleted`,
			issue.Key, issue.ID, issue.ProjectKey, issue.BoardID,
			issue.Summary, issue.DescriptionText,
			issue.IssueType, issue.IssueTypeCategory, issue.IsBug,
			issue.Status, issue.StatusCategory, issue.StatusCategoryChangedAt,
			issue.AssigneeAccountID, issue.AssigneeEmail, issue.AssigneeDisplayName, issue.AssigneeSlackID,
			issue.ReporterAccountID, issue.ReporterEmail, issue.ReporterDisplayName, issue.ReporterSlackID,
			issue.Priority, issue.StoryPoints, issue.DueDate,
			issue.SprintID, issue.SprintName, issue.EpicKey,
			issue.Labels, issue.Components, issue.FixVersions,
			issue.CreatedAt, issue.UpdatedAt, issue.ResolvedAt,
			issue.RawJSON, issue.CustomFieldsJSON, issue.SyncedAt, issue.IsDeleted)
		if err != nil {
			return fmt.Errorf("upserting jira issue %s: %w", issue.Key, err)
		}
	}

	for i := range links {
		link := &links[i]
		_, err := tx.Exec(`INSERT INTO jira_issue_links (id, source_key, target_key, link_type, synced_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET source_key=excluded.source_key, target_key=excluded.target_key,
				link_type=excluded.link_type, synced_at=excluded.synced_at`,
			link.ID, link.SourceKey, link.TargetKey, link.LinkType, link.SyncedAt)
		if err != nil {
			return fmt.Errorf("upserting jira issue link %s: %w", link.ID, err)
		}
	}

	return tx.Commit()
}

// GetJiraIssueByKey returns a Jira issue by its key.
func (db *DB) GetJiraIssueByKey(key string) (*JiraIssue, error) {
	row := db.QueryRow(`SELECT `+jiraIssueColumns+` FROM jira_issues WHERE key = ?`, key)

	issue, err := scanJiraIssue(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning jira issue %s: %w", key, err)
	}
	return &issue, nil
}

// GetJiraIssueCount returns the total number of Jira issues in the database.
func (db *DB) GetJiraIssueCount() (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM jira_issues WHERE is_deleted = 0`).Scan(&count)
	return count, err
}

// UpsertJiraSprint inserts or updates a Jira sprint.
func (db *DB) UpsertJiraSprint(sprint JiraSprint) error {
	_, err := db.Exec(`INSERT INTO jira_sprints (id, board_id, name, state, goal, start_date, end_date, complete_date, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET board_id=excluded.board_id, name=excluded.name, state=excluded.state,
			goal=excluded.goal, start_date=excluded.start_date, end_date=excluded.end_date,
			complete_date=excluded.complete_date, synced_at=excluded.synced_at`,
		sprint.ID, sprint.BoardID, sprint.Name, sprint.State, sprint.Goal,
		sprint.StartDate, sprint.EndDate, sprint.CompleteDate, sprint.SyncedAt)
	if err != nil {
		return fmt.Errorf("upserting jira sprint %d: %w", sprint.ID, err)
	}
	return nil
}

// GetJiraActiveSprints returns active sprints for a given board.
func (db *DB) GetJiraActiveSprints(boardID int) ([]JiraSprint, error) {
	rows, err := db.Query(`SELECT id, board_id, name, state, goal, start_date, end_date, complete_date, synced_at
		FROM jira_sprints WHERE board_id = ? AND state = 'active' ORDER BY start_date`, boardID)
	if err != nil {
		return nil, fmt.Errorf("querying active sprints for board %d: %w", boardID, err)
	}
	defer rows.Close()

	var sprints []JiraSprint
	for rows.Next() {
		var s JiraSprint
		if err := rows.Scan(&s.ID, &s.BoardID, &s.Name, &s.State, &s.Goal,
			&s.StartDate, &s.EndDate, &s.CompleteDate, &s.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira sprint: %w", err)
		}
		sprints = append(sprints, s)
	}
	return sprints, rows.Err()
}

// UpsertJiraIssueLink inserts or updates a Jira issue link.
func (db *DB) UpsertJiraIssueLink(link JiraIssueLink) error {
	_, err := db.Exec(`INSERT INTO jira_issue_links (id, source_key, target_key, link_type, synced_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET source_key=excluded.source_key, target_key=excluded.target_key,
			link_type=excluded.link_type, synced_at=excluded.synced_at`,
		link.ID, link.SourceKey, link.TargetKey, link.LinkType, link.SyncedAt)
	if err != nil {
		return fmt.Errorf("upserting jira issue link %s: %w", link.ID, err)
	}
	return nil
}

// GetJiraIssueLinksByKey returns all issue links where source_key OR target_key matches the given key.
func (db *DB) GetJiraIssueLinksByKey(key string) ([]JiraIssueLink, error) {
	rows, err := db.Query(`SELECT id, source_key, target_key, link_type, synced_at
		FROM jira_issue_links WHERE source_key = ? OR target_key = ? ORDER BY id`, key, key)
	if err != nil {
		return nil, fmt.Errorf("querying jira issue links by key %s: %w", key, err)
	}
	defer rows.Close()

	var links []JiraIssueLink
	for rows.Next() {
		var l JiraIssueLink
		if err := rows.Scan(&l.ID, &l.SourceKey, &l.TargetKey, &l.LinkType, &l.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira issue link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetJiraIssueLinksByKeys returns all issue links where source_key OR target_key matches any of the given keys.
func (db *DB) GetJiraIssueLinksByKeys(keys []string) ([]JiraIssueLink, error) {
	if len(keys) == 0 {
		return []JiraIssueLink{}, nil
	}
	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, len(keys)*2)
	for i, k := range keys {
		args[i] = k
		args[len(keys)+i] = k
	}

	rows, err := db.Query(`SELECT id, source_key, target_key, link_type, synced_at
		FROM jira_issue_links WHERE source_key IN (`+placeholders+`) OR target_key IN (`+placeholders+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying jira issue links by keys: %w", err)
	}
	defer rows.Close()

	var links []JiraIssueLink
	for rows.Next() {
		var l JiraIssueLink
		if err := rows.Scan(&l.ID, &l.SourceKey, &l.TargetKey, &l.LinkType, &l.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning jira issue link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// UpsertJiraUserMap inserts or updates a Jira user mapping.
func (db *DB) UpsertJiraUserMap(mapping JiraUserMap) error {
	_, err := db.Exec(`INSERT INTO jira_user_map (jira_account_id, email, slack_user_id, display_name, match_method, match_confidence, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jira_account_id) DO UPDATE SET email=excluded.email, slack_user_id=excluded.slack_user_id,
			display_name=excluded.display_name, match_method=excluded.match_method,
			match_confidence=excluded.match_confidence, resolved_at=excluded.resolved_at`,
		mapping.JiraAccountID, mapping.Email, mapping.SlackUserID, mapping.DisplayName,
		mapping.MatchMethod, mapping.MatchConfidence, mapping.ResolvedAt)
	if err != nil {
		return fmt.Errorf("upserting jira user map %s: %w", mapping.JiraAccountID, err)
	}
	return nil
}

// BackfillJiraSlackIDs updates assignee_slack_id and reporter_slack_id on existing issues from jira_user_map.
func (db *DB) BackfillJiraSlackIDs() error {
	_, err := db.Exec(`UPDATE jira_issues SET assignee_slack_id = COALESCE(
		(SELECT jum.slack_user_id FROM jira_user_map jum
		 WHERE jum.jira_account_id = jira_issues.assignee_account_id AND jum.slack_user_id != ''), '')
		WHERE assignee_account_id != '' AND assignee_slack_id = ''`)
	if err != nil {
		return fmt.Errorf("backfilling assignee slack IDs: %w", err)
	}
	_, err = db.Exec(`UPDATE jira_issues SET reporter_slack_id = COALESCE(
		(SELECT jum.slack_user_id FROM jira_user_map jum
		 WHERE jum.jira_account_id = jira_issues.reporter_account_id AND jum.slack_user_id != ''), '')
		WHERE reporter_account_id != '' AND reporter_slack_id = ''`)
	if err != nil {
		return fmt.Errorf("backfilling reporter slack IDs: %w", err)
	}
	return nil
}

// GetJiraUserMaps returns all Jira user mappings.
func (db *DB) GetJiraUserMaps() ([]JiraUserMap, error) {
	rows, err := db.Query(`SELECT jira_account_id, email, slack_user_id, display_name, match_method, match_confidence, resolved_at
		FROM jira_user_map ORDER BY display_name`)
	if err != nil {
		return nil, fmt.Errorf("querying jira user maps: %w", err)
	}
	defer rows.Close()

	var maps []JiraUserMap
	for rows.Next() {
		var m JiraUserMap
		if err := rows.Scan(&m.JiraAccountID, &m.Email, &m.SlackUserID, &m.DisplayName,
			&m.MatchMethod, &m.MatchConfidence, &m.ResolvedAt); err != nil {
			return nil, fmt.Errorf("scanning jira user map: %w", err)
		}
		maps = append(maps, m)
	}
	return maps, rows.Err()
}

// GetJiraUserMapByAccountID returns a user mapping by Jira account ID.
func (db *DB) GetJiraUserMapByAccountID(id string) (*JiraUserMap, error) {
	row := db.QueryRow(`SELECT jira_account_id, email, slack_user_id, display_name, match_method, match_confidence, resolved_at
		FROM jira_user_map WHERE jira_account_id = ?`, id)

	var m JiraUserMap
	err := row.Scan(&m.JiraAccountID, &m.Email, &m.SlackUserID, &m.DisplayName,
		&m.MatchMethod, &m.MatchConfidence, &m.ResolvedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning jira user map %s: %w", id, err)
	}
	return &m, nil
}

// UpdateJiraSyncState inserts or updates the sync state for a Jira project.
func (db *DB) UpdateJiraSyncState(projectKey, lastSyncedAt string, issuesSynced int) error {
	_, err := db.Exec(`INSERT INTO jira_sync_state (project_key, last_synced_at, issues_synced)
		VALUES (?, ?, ?)
		ON CONFLICT(project_key) DO UPDATE SET last_synced_at=excluded.last_synced_at,
			issues_synced=excluded.issues_synced`,
		projectKey, lastSyncedAt, issuesSynced)
	if err != nil {
		return fmt.Errorf("updating jira sync state %s: %w", projectKey, err)
	}
	return nil
}

// GetJiraSyncState returns the sync state for a Jira project.
func (db *DB) GetJiraSyncState(projectKey string) (*JiraSyncState, error) {
	row := db.QueryRow(`SELECT project_key, last_synced_at, issues_synced, last_error, last_error_at
		FROM jira_sync_state WHERE project_key = ?`, projectKey)

	var s JiraSyncState
	err := row.Scan(&s.ProjectKey, &s.LastSyncedAt, &s.IssuesSynced, &s.LastError, &s.LastErrorAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning jira sync state %s: %w", projectKey, err)
	}
	return &s, nil
}

// GetJiraSyncStates returns all Jira sync states.
func (db *DB) GetJiraSyncStates() ([]JiraSyncState, error) {
	rows, err := db.Query(`SELECT project_key, last_synced_at, issues_synced, last_error, last_error_at FROM jira_sync_state ORDER BY project_key`)
	if err != nil {
		return nil, fmt.Errorf("querying jira sync states: %w", err)
	}
	defer rows.Close()

	var states []JiraSyncState
	for rows.Next() {
		var s JiraSyncState
		if err := rows.Scan(&s.ProjectKey, &s.LastSyncedAt, &s.IssuesSynced, &s.LastError, &s.LastErrorAt); err != nil {
			return nil, fmt.Errorf("scanning jira sync state: %w", err)
		}
		states = append(states, s)
	}
	return states, rows.Err()
}

// UpsertJiraSlackLink inserts or updates a Jira-Slack link.
func (db *DB) UpsertJiraSlackLink(link JiraSlackLink) error {
	_, err := db.Exec(`INSERT INTO jira_slack_links (issue_key, channel_id, message_ts, track_id, digest_id, link_type)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_key, channel_id, message_ts) DO UPDATE SET
			track_id=COALESCE(excluded.track_id, jira_slack_links.track_id),
			digest_id=COALESCE(excluded.digest_id, jira_slack_links.digest_id),
			link_type=COALESCE(excluded.link_type, jira_slack_links.link_type)`,
		link.IssueKey, link.ChannelID, link.MessageTS, link.TrackID, link.DigestID, link.LinkType)
	if err != nil {
		return fmt.Errorf("upserting jira slack link %s: %w", link.IssueKey, err)
	}
	return nil
}

// GetJiraSlackLinksByIssue returns all Slack links for a given Jira issue key.
func (db *DB) GetJiraSlackLinksByIssue(issueKey string) ([]JiraSlackLink, error) {
	rows, err := db.Query(`SELECT id, issue_key, channel_id, message_ts, track_id, digest_id, link_type, detected_at
		FROM jira_slack_links WHERE issue_key = ? ORDER BY detected_at DESC`, issueKey)
	if err != nil {
		return nil, fmt.Errorf("querying jira slack links by issue %s: %w", issueKey, err)
	}
	defer rows.Close()

	var links []JiraSlackLink
	for rows.Next() {
		var l JiraSlackLink
		if err := rows.Scan(&l.ID, &l.IssueKey, &l.ChannelID, &l.MessageTS, &l.TrackID, &l.DigestID, &l.LinkType, &l.DetectedAt); err != nil {
			return nil, fmt.Errorf("scanning jira slack link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetJiraSlackLinksByMessage returns all Jira links for a specific Slack message.
func (db *DB) GetJiraSlackLinksByMessage(channelID, messageTS string) ([]JiraSlackLink, error) {
	rows, err := db.Query(`SELECT id, issue_key, channel_id, message_ts, track_id, digest_id, link_type, detected_at
		FROM jira_slack_links WHERE channel_id = ? AND message_ts = ? ORDER BY detected_at DESC`, channelID, messageTS)
	if err != nil {
		return nil, fmt.Errorf("querying jira slack links by message: %w", err)
	}
	defer rows.Close()

	var links []JiraSlackLink
	for rows.Next() {
		var l JiraSlackLink
		if err := rows.Scan(&l.ID, &l.IssueKey, &l.ChannelID, &l.MessageTS, &l.TrackID, &l.DigestID, &l.LinkType, &l.DetectedAt); err != nil {
			return nil, fmt.Errorf("scanning jira slack link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetKnownProjectKeys returns distinct project keys from jira_issues and jira_boards.
func (db *DB) GetKnownProjectKeys() ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT project_key FROM jira_issues WHERE project_key != ''
		UNION SELECT DISTINCT project_key FROM jira_boards WHERE project_key != ''`)
	if err != nil {
		return nil, fmt.Errorf("querying known project keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scanning project key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ClearJiraData deletes all data from jira_* tables.
func (db *DB) ClearJiraData() error {
	tables := []string{
		"jira_slack_links",
		"jira_issue_links",
		"jira_issues",
		"jira_sprints",
		"jira_releases",
		"jira_user_map",
		"jira_sync_state",
		"jira_boards",
	}
	for _, table := range tables {
		if _, err := db.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("clearing %s: %w", table, err)
		}
	}
	return nil
}

// jiraIssueColumns is the common column list for scanning JiraIssue rows (unqualified, for single-table queries).
const jiraIssueColumns = `key, id, project_key, board_id, summary, description_text,
	issue_type, issue_type_category, is_bug, status, status_category, status_category_changed_at,
	assignee_account_id, assignee_email, assignee_display_name, assignee_slack_id,
	reporter_account_id, reporter_email, reporter_display_name, reporter_slack_id,
	priority, story_points, due_date, sprint_id, sprint_name, epic_key,
	labels, components, fix_versions, created_at, updated_at, resolved_at, raw_json, custom_fields_json, synced_at, is_deleted`

// jiraIssueColumnsQualified is the same column list but qualified with table name (for JOINs).
const jiraIssueColumnsQualified = `jira_issues.key, jira_issues.id, jira_issues.project_key, jira_issues.board_id,
	jira_issues.summary, jira_issues.description_text,
	jira_issues.issue_type, jira_issues.issue_type_category, jira_issues.is_bug,
	jira_issues.status, jira_issues.status_category, jira_issues.status_category_changed_at,
	jira_issues.assignee_account_id, jira_issues.assignee_email, jira_issues.assignee_display_name, jira_issues.assignee_slack_id,
	jira_issues.reporter_account_id, jira_issues.reporter_email, jira_issues.reporter_display_name, jira_issues.reporter_slack_id,
	jira_issues.priority, jira_issues.story_points, jira_issues.due_date, jira_issues.sprint_id, jira_issues.sprint_name, jira_issues.epic_key,
	jira_issues.labels, jira_issues.components, jira_issues.fix_versions, jira_issues.created_at, jira_issues.updated_at, jira_issues.resolved_at,
	jira_issues.raw_json, jira_issues.custom_fields_json, jira_issues.synced_at, jira_issues.is_deleted`

func scanJiraIssue(scanner interface{ Scan(dest ...any) error }) (JiraIssue, error) {
	var issue JiraIssue
	err := scanner.Scan(&issue.Key, &issue.ID, &issue.ProjectKey, &issue.BoardID,
		&issue.Summary, &issue.DescriptionText,
		&issue.IssueType, &issue.IssueTypeCategory, &issue.IsBug,
		&issue.Status, &issue.StatusCategory, &issue.StatusCategoryChangedAt,
		&issue.AssigneeAccountID, &issue.AssigneeEmail, &issue.AssigneeDisplayName, &issue.AssigneeSlackID,
		&issue.ReporterAccountID, &issue.ReporterEmail, &issue.ReporterDisplayName, &issue.ReporterSlackID,
		&issue.Priority, &issue.StoryPoints, &issue.DueDate,
		&issue.SprintID, &issue.SprintName, &issue.EpicKey,
		&issue.Labels, &issue.Components, &issue.FixVersions,
		&issue.CreatedAt, &issue.UpdatedAt, &issue.ResolvedAt,
		&issue.RawJSON, &issue.CustomFieldsJSON, &issue.SyncedAt, &issue.IsDeleted)
	return issue, err
}

// GetJiraIssuesForTrack returns Jira issues linked to a track via jira_slack_links.
func (db *DB) GetJiraIssuesForTrack(trackID int) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT DISTINCT `+jiraIssueColumnsQualified+`
		FROM jira_issues
		JOIN jira_slack_links ON jira_slack_links.issue_key = jira_issues.key
		WHERE jira_slack_links.track_id = ?
		ORDER BY jira_issues.updated_at DESC`, trackID)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues for track %d: %w", trackID, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue for track %d: %w", trackID, err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraIssuesForTracks returns Jira issues linked to multiple tracks in a single query.
// Results are grouped by track ID. Tracks with no linked issues are omitted from the map.
func (db *DB) GetJiraIssuesForTracks(trackIDs []int) (map[int][]JiraIssue, error) {
	if len(trackIDs) == 0 {
		return nil, nil
	}

	// Step 1: Get track_id → issue_key mappings
	placeholders := make([]string, len(trackIDs))
	args := make([]interface{}, len(trackIDs))
	for i, id := range trackIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	linkRows, err := db.Query(`SELECT DISTINCT track_id, issue_key FROM jira_slack_links
		WHERE track_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying jira slack links for tracks: %w", err)
	}
	defer linkRows.Close()

	trackToKeys := make(map[int][]string)
	allKeys := make(map[string]bool)
	for linkRows.Next() {
		var trackID int
		var issueKey string
		if err := linkRows.Scan(&trackID, &issueKey); err != nil {
			return nil, fmt.Errorf("scanning jira slack link: %w", err)
		}
		trackToKeys[trackID] = append(trackToKeys[trackID], issueKey)
		allKeys[issueKey] = true
	}
	if err := linkRows.Err(); err != nil {
		return nil, err
	}

	if len(allKeys) == 0 {
		return nil, nil
	}

	// Step 2: Batch-load issues by keys (reuses scanJiraIssue)
	issues, err := db.GetJiraIssuesByKeys(uniqueKeys(allKeys))
	if err != nil {
		return nil, fmt.Errorf("batch loading jira issues: %w", err)
	}

	issueMap := make(map[string]JiraIssue, len(issues))
	for _, iss := range issues {
		issueMap[iss.Key] = iss
	}

	// Step 3: Build result map
	result := make(map[int][]JiraIssue)
	for trackID, keys := range trackToKeys {
		for _, key := range keys {
			if iss, ok := issueMap[key]; ok {
				result[trackID] = append(result[trackID], iss)
			}
		}
	}
	return result, nil
}

// GetJiraIssuesForDigest returns Jira issues linked to a digest via jira_slack_links.
func (db *DB) GetJiraIssuesForDigest(digestID int) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT DISTINCT `+jiraIssueColumnsQualified+`
		FROM jira_issues
		JOIN jira_slack_links ON jira_slack_links.issue_key = jira_issues.key
		WHERE jira_slack_links.digest_id = ?
		ORDER BY jira_issues.updated_at DESC`, digestID)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues for digest %d: %w", digestID, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue for digest %d: %w", digestID, err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraIssuesByAssigneeSlackID returns non-done issues assigned to a Slack user, ordered by priority.
func (db *DB) GetJiraIssuesByAssigneeSlackID(slackID string) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT `+jiraIssueColumns+`
		FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category != 'done' AND is_deleted = 0
		ORDER BY CASE priority
			WHEN 'Highest' THEN 1 WHEN 'High' THEN 2 WHEN 'Medium' THEN 3
			WHEN 'Low' THEN 4 WHEN 'Lowest' THEN 5 ELSE 6 END,
			updated_at DESC`, slackID)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues by assignee slack id %s: %w", slackID, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue by assignee: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraIssuesByKeys returns Jira issues matching the given keys.
func (db *DB) GetJiraIssuesByKeys(keys []string) ([]JiraIssue, error) {
	if len(keys) == 0 {
		return []JiraIssue{}, nil
	}
	placeholders := strings.Repeat("?,", len(keys))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma

	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	rows, err := db.Query(`SELECT `+jiraIssueColumns+`
		FROM jira_issues WHERE key IN (`+placeholders+`) ORDER BY key`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying jira issues by keys: %w", err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue by key: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraActiveSprintStats returns aggregated stats for the active sprint of a board.
// Returns nil if no active sprint exists.
func (db *DB) GetJiraActiveSprintStats(boardID int) (*SprintStats, error) {
	// Find the active sprint.
	var sprintName, endDate string
	var sprintID int
	err := db.QueryRow(`SELECT id, name, end_date FROM jira_sprints
		WHERE board_id = ? AND state = 'active' ORDER BY start_date LIMIT 1`, boardID).
		Scan(&sprintID, &sprintName, &endDate)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying active sprint for board %d: %w", boardID, err)
	}

	stats := &SprintStats{SprintName: sprintName}

	// Count issues by status_category.
	rows, err := db.Query(`SELECT status_category, COUNT(*) FROM jira_issues
		WHERE sprint_id = ? AND is_deleted = 0 GROUP BY status_category`, sprintID)
	if err != nil {
		return nil, fmt.Errorf("querying sprint stats for board %d: %w", boardID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cat string
		var cnt int
		if err := rows.Scan(&cat, &cnt); err != nil {
			return nil, fmt.Errorf("scanning sprint stats: %w", err)
		}
		stats.Total += cnt
		switch cat {
		case "done":
			stats.Done += cnt
		case "in_progress":
			stats.InProgress += cnt
		default: // "todo", "new", etc.
			stats.Todo += cnt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Calculate days left.
	if endDate != "" {
		// Try multiple date formats.
		var endTime time.Time
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"} {
			if t, e := time.Parse(layout, endDate); e == nil {
				endTime = t
				break
			}
		}
		if !endTime.IsZero() {
			days := time.Until(endTime).Hours() / 24
			stats.DaysLeft = int(math.Ceil(days))
			if stats.DaysLeft < 0 {
				stats.DaysLeft = 0
			}
		}
	}

	return stats, nil
}

// GetJiraIssuesForUser returns issues assigned to a Slack user, optionally filtered by status category.
func (db *DB) GetJiraIssuesForUser(slackID string, statusCategory string) ([]JiraIssue, error) {
	var rows *sql.Rows
	var err error
	if statusCategory != "" {
		rows, err = db.Query(`SELECT `+jiraIssueColumns+`
			FROM jira_issues
			WHERE assignee_slack_id = ? AND status_category = ? AND is_deleted = 0
			ORDER BY updated_at DESC`, slackID, statusCategory)
	} else {
		rows, err = db.Query(`SELECT `+jiraIssueColumns+`
			FROM jira_issues
			WHERE assignee_slack_id = ? AND is_deleted = 0
			ORDER BY updated_at DESC`, slackID)
	}
	if err != nil {
		return nil, fmt.Errorf("querying jira issues for user %s: %w", slackID, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira issue for user: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraResolvedIssuesForUser returns resolved issues for a user within a time
// window, limited to at most `limit` rows. The from/to parameters are compared
// against resolved_at (ISO-8601 date or datetime strings).
func (db *DB) GetJiraResolvedIssuesForUser(slackID, from, to string, limit int) ([]JiraIssue, error) {
	rows, err := db.Query(`SELECT `+jiraIssueColumns+`
		FROM jira_issues
		WHERE assignee_slack_id = ? AND status_category = 'done' AND is_deleted = 0
			AND resolved_at >= ? AND resolved_at <= ?
		ORDER BY resolved_at DESC
		LIMIT ?`, slackID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("querying jira resolved issues for user %s: %w", slackID, err)
	}
	defer rows.Close()

	var issues []JiraIssue
	for rows.Next() {
		issue, err := scanJiraIssue(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning jira resolved issue for user: %w", err)
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetJiraSlackLinksByTrackID returns all Jira-Slack links for a given track ID.
func (db *DB) GetJiraSlackLinksByTrackID(trackID int) ([]JiraSlackLink, error) {
	rows, err := db.Query(`SELECT id, issue_key, channel_id, message_ts, track_id, digest_id, link_type, detected_at
		FROM jira_slack_links WHERE track_id = ? ORDER BY detected_at DESC`, trackID)
	if err != nil {
		return nil, fmt.Errorf("querying jira slack links by track %d: %w", trackID, err)
	}
	defer rows.Close()

	var links []JiraSlackLink
	for rows.Next() {
		var l JiraSlackLink
		if err := rows.Scan(&l.ID, &l.IssueKey, &l.ChannelID, &l.MessageTS, &l.TrackID, &l.DigestID, &l.LinkType, &l.DetectedAt); err != nil {
			return nil, fmt.Errorf("scanning jira slack link by track: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

// GetJiraDeliveryStats returns delivery metrics for a user in a date range.
// from/to are ISO8601 date strings (e.g. "2026-04-01").
