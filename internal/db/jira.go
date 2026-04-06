package db

import (
	"database/sql"
	"fmt"
)

// UpsertJiraBoard inserts or updates a Jira board.
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
		labels, components, created_at, updated_at, resolved_at, raw_json, synced_at, is_deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			created_at=excluded.created_at, updated_at=excluded.updated_at, resolved_at=excluded.resolved_at,
			raw_json=excluded.raw_json, synced_at=excluded.synced_at, is_deleted=excluded.is_deleted`,
		issue.Key, issue.ID, issue.ProjectKey, issue.BoardID,
		issue.Summary, issue.DescriptionText,
		issue.IssueType, issue.IssueTypeCategory, issue.IsBug,
		issue.Status, issue.StatusCategory, issue.StatusCategoryChangedAt,
		issue.AssigneeAccountID, issue.AssigneeEmail, issue.AssigneeDisplayName, issue.AssigneeSlackID,
		issue.ReporterAccountID, issue.ReporterEmail, issue.ReporterDisplayName, issue.ReporterSlackID,
		issue.Priority, issue.StoryPoints, issue.DueDate,
		issue.SprintID, issue.SprintName, issue.EpicKey,
		issue.Labels, issue.Components,
		issue.CreatedAt, issue.UpdatedAt, issue.ResolvedAt,
		issue.RawJSON, issue.SyncedAt, issue.IsDeleted)
	if err != nil {
		return fmt.Errorf("upserting jira issue %s: %w", issue.Key, err)
	}
	return nil
}

// GetJiraIssueByKey returns a Jira issue by its key.
func (db *DB) GetJiraIssueByKey(key string) (*JiraIssue, error) {
	row := db.QueryRow(`SELECT key, id, project_key, board_id, summary, description_text,
		issue_type, issue_type_category, is_bug, status, status_category, status_category_changed_at,
		assignee_account_id, assignee_email, assignee_display_name, assignee_slack_id,
		reporter_account_id, reporter_email, reporter_display_name, reporter_slack_id,
		priority, story_points, due_date, sprint_id, sprint_name, epic_key,
		labels, components, created_at, updated_at, resolved_at, raw_json, synced_at, is_deleted
		FROM jira_issues WHERE key = ?`, key)

	var issue JiraIssue
	err := row.Scan(&issue.Key, &issue.ID, &issue.ProjectKey, &issue.BoardID,
		&issue.Summary, &issue.DescriptionText,
		&issue.IssueType, &issue.IssueTypeCategory, &issue.IsBug,
		&issue.Status, &issue.StatusCategory, &issue.StatusCategoryChangedAt,
		&issue.AssigneeAccountID, &issue.AssigneeEmail, &issue.AssigneeDisplayName, &issue.AssigneeSlackID,
		&issue.ReporterAccountID, &issue.ReporterEmail, &issue.ReporterDisplayName, &issue.ReporterSlackID,
		&issue.Priority, &issue.StoryPoints, &issue.DueDate,
		&issue.SprintID, &issue.SprintName, &issue.EpicKey,
		&issue.Labels, &issue.Components,
		&issue.CreatedAt, &issue.UpdatedAt, &issue.ResolvedAt,
		&issue.RawJSON, &issue.SyncedAt, &issue.IsDeleted)
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

// ClearJiraData deletes all data from jira_* tables.
func (db *DB) ClearJiraData() error {
	tables := []string{
		"jira_issue_links",
		"jira_issues",
		"jira_sprints",
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
