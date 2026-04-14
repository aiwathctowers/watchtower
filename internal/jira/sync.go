package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/db"
)

// validProjectKeyRe validates Jira project keys to prevent JQL injection.
var validProjectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// SyncProgress reports the current state of a Jira sync operation.
type SyncProgress struct {
	Done  int    // issues synced so far
	Total int    // total issues to sync (from API)
	Phase string // "issues", "sprints", "releases", "done"
}

// Syncer performs incremental and full syncs of Jira issues into the local database.
type Syncer struct {
	client        *Client
	db            *db.DB
	mapper        *UserMapper
	logger        *log.Logger
	boardIDs      []int
	boardAnalyzer *BoardAnalyzer                 // optional, for config change detection
	autoRefresh   bool                           // when true, auto re-analyze boards with changed config
	fieldMapCache map[int][]db.JiraBoardFieldMap // boardID -> field mappings
	OnProgress    func(SyncProgress)             // optional progress callback
}

// NewSyncer creates a Syncer.
func NewSyncer(client *Client, database *db.DB, mapper *UserMapper, boardIDs []int) *Syncer {
	return &Syncer{
		client:   client,
		db:       database,
		mapper:   mapper,
		logger:   log.New(os.Stderr, "[jira-sync] ", log.LstdFlags),
		boardIDs: boardIDs,
	}
}

// SetLogger replaces the syncer's logger.
func (s *Syncer) SetLogger(l *log.Logger) {
	s.logger = l
}

// SetBoardAnalyzer sets an optional board analyzer for config change detection during sync.
func (s *Syncer) SetBoardAnalyzer(analyzer *BoardAnalyzer) {
	s.boardAnalyzer = analyzer
}

// BoardAnalyzerUsage returns accumulated LLM usage from the board analyzer, if any.
func (s *Syncer) BoardAnalyzerUsage() (inputTokens, outputTokens, totalAPITokens int) {
	if s.boardAnalyzer == nil {
		return 0, 0, 0
	}
	return s.boardAnalyzer.AccumulatedUsage()
}

// SetAutoRefresh enables automatic re-analysis of boards with changed config after sync.
func (s *Syncer) SetAutoRefresh(auto bool) {
	s.autoRefresh = auto
}

// Sync performs an incremental sync: fetches issues updated since last sync minus 2 minutes overlap.
func (s *Syncer) Sync(ctx context.Context) (int, error) {
	total := 0

	boards, err := s.db.GetJiraSelectedBoards()
	if err != nil {
		return 0, fmt.Errorf("getting selected boards: %w", err)
	}

	if len(boards) == 0 {
		s.logger.Println("no boards selected, skipping sync")
		return 0, nil
	}

	for _, board := range boards {
		projectKey := board.ProjectKey
		if projectKey == "" {
			continue
		}

		if !validProjectKeyRe.MatchString(projectKey) {
			s.logger.Printf("skipping board %d: invalid project key %q", board.ID, projectKey)
			continue
		}

		syncState, _ := s.db.GetJiraSyncState(projectKey)
		var jql string
		if syncState != nil && syncState.LastSyncedAt != "" {
			// Incremental sync: issues updated since last sync minus 2 minutes overlap.
			t, err := time.Parse(time.RFC3339, syncState.LastSyncedAt)
			if err == nil {
				t = t.Add(-2 * time.Minute)
				jql = fmt.Sprintf("project = %s AND updated >= \"%s\" ORDER BY updated ASC",
					projectKey, t.Format("2006-01-02 15:04"))
			} else {
				jql = fmt.Sprintf("project = %s ORDER BY updated ASC", projectKey)
			}
		} else {
			jql = fmt.Sprintf("project = %s ORDER BY updated ASC", projectKey)
		}

		n, err := s.syncWithJQL(ctx, jql, board.ID)
		if err != nil {
			s.logger.Printf("sync error for project %s: %v", projectKey, err)
			if syncState == nil {
				syncState = &db.JiraSyncState{ProjectKey: projectKey}
			}
			syncState.LastError = err.Error()
			syncState.LastErrorAt = time.Now().UTC().Format(time.RFC3339)
			_ = s.db.UpdateJiraSyncState(syncState.ProjectKey, syncState.LastSyncedAt, syncState.IssuesSynced)
			continue
		}

		total += n
		now := time.Now().UTC().Format(time.RFC3339)
		issuesSynced := n
		if syncState != nil {
			issuesSynced += syncState.IssuesSynced
		}
		_ = s.db.UpdateJiraSyncState(projectKey, now, issuesSynced)
		_ = s.db.UpdateJiraBoardIssueCount(board.ID)
	}

	// Sync sprints for selected boards.
	if err := s.SyncSprints(ctx); err != nil {
		s.logger.Printf("sprint sync error: %v", err)
	}

	// Sync releases (fix versions) for selected boards.
	if err := s.syncReleases(ctx, boards); err != nil {
		s.logger.Printf("releases sync error: %v", err)
	}

	// Check if board configs changed since last analysis and optionally auto-refresh.
	if s.boardAnalyzer != nil {
		results, err := s.boardAnalyzer.CheckAndRefreshProfiles(ctx, s.autoRefresh)
		if err != nil {
			s.logger.Printf("board config refresh check error: %v", err)
		} else {
			for _, r := range results {
				if r.Error != nil {
					s.logger.Printf("board %d (%s): refresh failed: %v", r.BoardID, r.BoardName, r.Error)
				} else if r.Refreshed {
					s.logger.Printf("board %d (%s): auto-refreshed profile", r.BoardID, r.BoardName)
				}
			}
		}
	}

	return total, nil
}

// SyncBoard syncs a single board by ID.
// Only syncs non-terminal (active) issues for fast initial load.
// Terminal/closed issues are picked up by the daemon's regular Sync() cycle.
func (s *Syncer) SyncBoard(ctx context.Context, boardID int) (int, error) {
	board, err := s.db.GetJiraBoardProfile(boardID)
	if err != nil {
		return 0, fmt.Errorf("getting board %d: %w", boardID, err)
	}
	if board.ProjectKey == "" {
		return 0, fmt.Errorf("board %d has no project key", boardID)
	}
	if !validProjectKeyRe.MatchString(board.ProjectKey) {
		return 0, fmt.Errorf("board %d: invalid project key %q", boardID, board.ProjectKey)
	}

	// Build JQL: exclude done issues for fast initial load.
	// statusCategory != Done covers all terminal statuses regardless of name.
	jql := fmt.Sprintf("project = %s AND statusCategory != Done ORDER BY updated ASC", board.ProjectKey)
	n, err := s.syncWithJQL(ctx, jql, boardID)
	if err != nil {
		return n, fmt.Errorf("syncing active issues for %s: %w", board.ProjectKey, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.db.UpdateJiraSyncState(board.ProjectKey, now, n)
	_ = s.db.UpdateJiraBoardIssueCount(boardID)

	if s.OnProgress != nil {
		s.OnProgress(SyncProgress{Done: n, Total: n, Phase: "done"})
	}

	return n, nil
}

// parseTerminalStatuses extracts terminal status names from a board's LLM profile JSON,
// applying user overrides from user_overrides_json (terminal_stages map).
func parseTerminalStatuses(llmProfileJSON, userOverridesJSON string) []string {
	if llmProfileJSON == "" {
		return nil
	}
	var profile struct {
		WorkflowStages []struct {
			Name             string   `json:"name"`
			IsTerminal       bool     `json:"is_terminal"`
			OriginalStatuses []string `json:"original_statuses"`
		} `json:"workflow_stages"`
	}
	if err := json.Unmarshal([]byte(llmProfileJSON), &profile); err != nil {
		return nil
	}

	// Load user overrides for terminal stages.
	var overrides UserOverrides
	if userOverridesJSON != "" {
		_ = json.Unmarshal([]byte(userOverridesJSON), &overrides)
	}

	var statuses []string
	for _, stage := range profile.WorkflowStages {
		for _, status := range stage.OriginalStatuses {
			isTerminal := stage.IsTerminal
			if override, ok := overrides.TerminalStages[status]; ok {
				isTerminal = override
			}
			if isTerminal {
				statuses = append(statuses, status)
			}
		}
	}
	return statuses
}

// buildStatusNotIn builds a JQL "NOT IN" value list: "\"Done\",\"Closed\"".
func buildStatusNotIn(statuses []string) string {
	quoted := make([]string, len(statuses))
	for i, s := range statuses {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ",")
}

// InitialLoad performs a full backlog sync without the updated filter.
func (s *Syncer) InitialLoad(ctx context.Context) (int, error) {
	total := 0

	boards, err := s.db.GetJiraSelectedBoards()
	if err != nil {
		return 0, fmt.Errorf("getting selected boards: %w", err)
	}

	for _, board := range boards {
		projectKey := board.ProjectKey
		if projectKey == "" {
			continue
		}

		if !validProjectKeyRe.MatchString(projectKey) {
			s.logger.Printf("skipping board %d: invalid project key %q", board.ID, projectKey)
			continue
		}

		jql := fmt.Sprintf("project = %s ORDER BY updated ASC", projectKey)
		n, err := s.syncWithJQL(ctx, jql, board.ID)
		if err != nil {
			s.logger.Printf("initial load error for project %s: %v", projectKey, err)
			continue
		}
		total += n

		now := time.Now().UTC().Format(time.RFC3339)
		_ = s.db.UpdateJiraSyncState(projectKey, now, n)
	}

	if err := s.SyncSprints(ctx); err != nil {
		s.logger.Printf("sprint sync error: %v", err)
	}

	return total, nil
}

// fetchedPage holds a raw API page for the writer to process.
type fetchedPage struct {
	issues []Issue
	isLast bool
}

// syncWithJQL fetches issues matching the JQL and upserts them into the database.
// Uses a pipeline: reader goroutine fetches pages from Jira API and buffers them,
// writer loop converts and writes batches to DB. DB access stays on the writer side
// to avoid deadlock with MaxOpenConns=1.
func (s *Syncer) syncWithJQL(ctx context.Context, jql string, boardID int) (int, error) {
	maxResults := 100
	pageCh := make(chan fetchedPage, 2) // buffer 2 pages ahead
	fetchErr := make(chan error, 1)

	// Reader: fetch pages from Jira API using cursor-based pagination (no DB access).
	go func() {
		defer close(pageCh)
		nextToken := ""
		for {
			result, err := s.client.SearchIssues(ctx, jql, maxResults, nextToken)
			if err != nil {
				fetchErr <- fmt.Errorf("searching issues: %w", err)
				return
			}

			if len(result.Issues) > 0 {
				pageCh <- fetchedPage{issues: result.Issues, isLast: result.IsLast}
			}

			if result.IsLast || len(result.Issues) == 0 || result.NextPageToken == "" {
				return
			}
			nextToken = result.NextPageToken
		}
	}()

	// Writer: convert and write batches to DB.
	written := 0
	for page := range pageCh {
		dbIssues, dbLinks := s.prepareIssueBatch(ctx, page.issues, boardID)

		if err := s.db.UpsertJiraIssueBatch(dbIssues, dbLinks); err != nil {
			s.logger.Printf("batch upsert error: %v", err)
		}
		written += len(dbIssues)
		_ = s.db.UpdateJiraBoardIssueCount(boardID)

		if s.OnProgress != nil {
			s.OnProgress(SyncProgress{Done: written, Total: 0, Phase: "issues"})
		}
	}

	// Check if reader exited with error.
	select {
	case err := <-fetchErr:
		return written, err
	default:
		return written, nil
	}
}

// prepareIssueBatch converts API issues to DB records without writing to the database.
func (s *Syncer) prepareIssueBatch(ctx context.Context, issues []Issue, boardID int) ([]db.JiraIssue, []db.JiraIssueLink) {
	dbIssues := make([]db.JiraIssue, 0, len(issues))
	var dbLinks []db.JiraIssueLink

	for _, issue := range issues {
		dbIssue, links := s.convertIssue(ctx, issue, boardID)
		dbIssues = append(dbIssues, dbIssue)
		dbLinks = append(dbLinks, links...)
	}
	return dbIssues, dbLinks
}

// convertIssue converts a Jira API issue to DB records without writing to the database.
func (s *Syncer) convertIssue(ctx context.Context, issue Issue, boardID int) (db.JiraIssue, []db.JiraIssueLink) {
	f := issue.Fields
	now := time.Now().UTC().Format(time.RFC3339)

	// Resolve assignee.
	assigneeAccountID, assigneeEmail, assigneeDisplayName, assigneeSlackID := "", "", "", ""
	if f.Assignee != nil {
		assigneeAccountID = f.Assignee.AccountID
		assigneeEmail = f.Assignee.EmailAddress
		assigneeDisplayName = f.Assignee.DisplayName
		s.ensureUserMap(f.Assignee)
		if m, _ := s.mapper.ResolveOne(ctx, f.Assignee.AccountID); m != nil {
			assigneeSlackID = m.SlackUserID
		}
	}

	// Resolve reporter.
	reporterAccountID, reporterEmail, reporterDisplayName, reporterSlackID := "", "", "", ""
	if f.Reporter != nil {
		reporterAccountID = f.Reporter.AccountID
		reporterEmail = f.Reporter.EmailAddress
		reporterDisplayName = f.Reporter.DisplayName
		s.ensureUserMap(f.Reporter)
		if m, _ := s.mapper.ResolveOne(ctx, f.Reporter.AccountID); m != nil {
			reporterSlackID = m.SlackUserID
		}
	}

	priority := ""
	if f.Priority != nil {
		priority = f.Priority.Name
	}

	dueDate := ""
	if f.DueDate != nil {
		dueDate = *f.DueDate
	}

	sprintID := 0
	sprintName := ""
	if f.Sprint != nil {
		sprintID = f.Sprint.ID
		sprintName = f.Sprint.Name
	}

	epicKey := ""
	if f.Epic != nil {
		epicKey = f.Epic.Key
	}
	if f.Parent != nil && epicKey == "" {
		epicKey = f.Parent.Key
	}

	labelsJSON, _ := json.Marshal(f.Labels)
	if f.Labels == nil {
		labelsJSON = []byte("[]")
	}

	componentNames := make([]string, 0, len(f.Components))
	for _, c := range f.Components {
		componentNames = append(componentNames, c.Name)
	}
	componentsJSON, _ := json.Marshal(componentNames)

	fixVersionNames := make([]string, 0, len(f.FixVersions))
	for _, fv := range f.FixVersions {
		fixVersionNames = append(fixVersionNames, fv.Name)
	}
	fixVersionsJSON, _ := json.Marshal(fixVersionNames)

	// Compute project key from issue key.
	projectKey := ""
	if idx := strings.LastIndex(issue.Key, "-"); idx > 0 {
		projectKey = issue.Key[:idx]
	}

	// Extract description text (ADF or plain).
	descText := extractDescriptionText(f.Description)

	resolvedAt := ""
	if f.Resolved != nil {
		resolvedAt = *f.Resolved
	}

	rawJSON, _ := json.Marshal(issue)

	// Extract custom field values from raw JSON.
	var storyPoints *float64
	customFieldsMap := make(map[string]interface{})

	fieldMappings := s.getFieldMap(boardID)
	if len(fieldMappings) > 0 {
		// Parse raw issue JSON to access custom fields.
		var rawIssue struct {
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(rawJSON, &rawIssue); err == nil {
			for _, fm := range fieldMappings {
				rawVal, ok := rawIssue.Fields[fm.FieldID]
				if !ok || string(rawVal) == "null" {
					continue
				}

				if fm.Role == "story_points" {
					var sp float64
					if err := json.Unmarshal(rawVal, &sp); err == nil {
						storyPoints = &sp
					}
				} else {
					// For other roles, extract a display value.
					var val interface{}
					if err := json.Unmarshal(rawVal, &val); err == nil {
						displayVal := extractDisplayValue(val)
						if displayVal != "" {
							customFieldsMap[fm.Role] = displayVal
						}
					}
				}
			}
		}
	}

	var customFieldsJSON string
	if len(customFieldsMap) > 0 {
		if b, err := json.Marshal(customFieldsMap); err == nil {
			customFieldsJSON = string(b)
		}
	}

	statusCatChanged := "" // Jira API doesn't expose this directly in basic search

	dbIssue := db.JiraIssue{
		Key:                     issue.Key,
		ID:                      issue.ID,
		ProjectKey:              projectKey,
		BoardID:                 boardID,
		Summary:                 f.Summary,
		DescriptionText:         descText,
		IssueType:               f.IssueType.Name,
		IssueTypeCategory:       normalizeIssueTypeCategory(f.IssueType.HierarchyLevel),
		IsBug:                   isBug(f.IssueType.Name),
		Status:                  f.Status.Name,
		StatusCategory:          normalizeStatusCategory(f.Status.StatusCategory.Key),
		StatusCategoryChangedAt: statusCatChanged,
		AssigneeAccountID:       assigneeAccountID,
		AssigneeEmail:           assigneeEmail,
		AssigneeDisplayName:     assigneeDisplayName,
		AssigneeSlackID:         assigneeSlackID,
		ReporterAccountID:       reporterAccountID,
		ReporterEmail:           reporterEmail,
		ReporterDisplayName:     reporterDisplayName,
		ReporterSlackID:         reporterSlackID,
		Priority:                priority,
		StoryPoints:             storyPoints,
		DueDate:                 dueDate,
		SprintID:                sprintID,
		SprintName:              sprintName,
		EpicKey:                 epicKey,
		Labels:                  string(labelsJSON),
		Components:              string(componentsJSON),
		FixVersions:             string(fixVersionsJSON),
		CreatedAt:               f.Created,
		UpdatedAt:               f.Updated,
		ResolvedAt:              resolvedAt,
		RawJSON:                 string(rawJSON),
		CustomFieldsJSON:        customFieldsJSON,
		SyncedAt:                now,
	}

	// Collect issue links.
	var links []db.JiraIssueLink
	for _, link := range f.IssueLinks {
		sourceKey := issue.Key
		targetKey := ""
		linkType := link.Type.Name
		if link.OutwardIssue != nil {
			targetKey = link.OutwardIssue.Key
		} else if link.InwardIssue != nil {
			targetKey = link.InwardIssue.Key
		}
		if targetKey != "" {
			links = append(links, db.JiraIssueLink{
				ID:        link.ID,
				SourceKey: sourceKey,
				TargetKey: targetKey,
				LinkType:  linkType,
				SyncedAt:  now,
			})
		}
	}

	return dbIssue, links
}

// ensureUserMap creates a user map entry if it doesn't exist.
func (s *Syncer) ensureUserMap(u *User) {
	if u == nil || u.AccountID == "" {
		return
	}
	existing, _ := s.db.GetJiraUserMapByAccountID(u.AccountID)
	if existing != nil {
		return
	}
	_ = s.db.UpsertJiraUserMap(db.JiraUserMap{
		JiraAccountID: u.AccountID,
		Email:         u.EmailAddress,
		DisplayName:   u.DisplayName,
	})
}

// SyncSprints syncs active and recent closed sprints for all selected boards.
func (s *Syncer) SyncSprints(ctx context.Context) error {
	boards, err := s.db.GetJiraSelectedBoards()
	if err != nil {
		return err
	}

	for _, board := range boards {
		for _, state := range []string{"active", "closed"} {
			params := url.Values{
				"state":      {state},
				"maxResults": {"50"},
			}
			path := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint", board.ID)
			var resp SprintList
			if err := s.client.getWithQuery(ctx, path, params, &resp); err != nil {
				s.logger.Printf("failed to fetch %s sprints for board %d: %v", state, board.ID, err)
				continue
			}

			now := time.Now().UTC().Format(time.RFC3339)
			for _, sprint := range resp.Values {
				dbSprint := db.JiraSprint{
					ID:           sprint.ID,
					BoardID:      board.ID,
					Name:         sprint.Name,
					State:        sprint.State,
					Goal:         sprint.Goal,
					StartDate:    sprint.StartDate,
					EndDate:      sprint.EndDate,
					CompleteDate: sprint.CompleteDate,
					SyncedAt:     now,
				}
				if err := s.db.UpsertJiraSprint(dbSprint); err != nil {
					s.logger.Printf("failed to upsert sprint %d: %v", sprint.ID, err)
				}
			}
		}
	}

	return nil
}

// syncReleases fetches fix versions for each unique project key and upserts them.
// Errors are logged but do not block the main sync.
func (s *Syncer) syncReleases(ctx context.Context, boards []db.JiraBoard) error {
	// Collect unique project keys.
	seen := make(map[string]bool)
	var projectKeys []string
	for _, board := range boards {
		if board.ProjectKey == "" || seen[board.ProjectKey] {
			continue
		}
		if !validProjectKeyRe.MatchString(board.ProjectKey) {
			continue
		}
		seen[board.ProjectKey] = true
		projectKeys = append(projectKeys, board.ProjectKey)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, projectKey := range projectKeys {
		versions, err := s.client.GetProjectVersions(ctx, projectKey)
		if err != nil {
			s.logger.Printf("failed to fetch versions for project %s: %v", projectKey, err)
			continue
		}

		for _, v := range versions {
			if v.ID == "" {
				s.logger.Printf("skipping version %q with empty ID", v.Name)
				continue
			}
			id, err := strconv.Atoi(v.ID)
			if err != nil {
				s.logger.Printf("skipping version %q: invalid ID %q", v.Name, v.ID)
				continue
			}
			release := db.JiraRelease{
				ID:          id,
				ProjectKey:  projectKey,
				Name:        v.Name,
				Description: v.Description,
				ReleaseDate: v.ReleaseDate,
				Released:    v.Released,
				Archived:    v.Archived,
				SyncedAt:    now,
			}
			if err := s.db.UpsertJiraRelease(release); err != nil {
				s.logger.Printf("failed to upsert release %q for project %s: %v", v.Name, projectKey, err)
			}
		}

		s.logger.Printf("synced %d releases for project %s", len(versions), projectKey)
	}

	// Always returns nil — individual version errors are logged but non-blocking by design.
	return nil
}

// getFieldMap returns the custom field mappings for a board, using a cache.
func (s *Syncer) getFieldMap(boardID int) []db.JiraBoardFieldMap {
	if s.fieldMapCache == nil {
		s.fieldMapCache = make(map[int][]db.JiraBoardFieldMap)
	}
	if cached, ok := s.fieldMapCache[boardID]; ok {
		return cached
	}
	mappings, err := s.db.GetJiraBoardFieldMap(boardID)
	if err != nil {
		return nil
	}
	s.fieldMapCache[boardID] = mappings
	return mappings
}

// extractDisplayValue gets a human-readable value from a Jira field value.
// Jira fields can be strings, numbers, objects with "name" or "displayName", or arrays thereof.
func extractDisplayValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case float64:
		if v == float64(int(v)) {
			return strconv.Itoa(int(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case map[string]interface{}:
		// Jira objects often have "name", "displayName", or "value"
		if name, ok := v["name"].(string); ok && name != "" {
			return name
		}
		if name, ok := v["displayName"].(string); ok && name != "" {
			return name
		}
		if val, ok := v["value"].(string); ok && val != "" {
			return val
		}
		return ""
	case []interface{}:
		var parts []string
		for _, item := range v {
			if s := extractDisplayValue(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

// normalizeStatusCategory maps Jira status category keys to normalized values.
func normalizeStatusCategory(jiraKey string) string {
	switch strings.ToLower(jiraKey) {
	case "new":
		return "todo"
	case "indeterminate":
		return "in_progress"
	case "done":
		return "done"
	default:
		return strings.ToLower(jiraKey)
	}
}

// normalizeIssueTypeCategory maps Jira hierarchy levels to categories.
func normalizeIssueTypeCategory(hierarchyLevel int) string {
	switch hierarchyLevel {
	case 1:
		return "epic"
	case -1:
		return "subtask"
	default:
		return "standard"
	}
}

// isBug returns true if the issue type name contains "bug" (case-insensitive).
func isBug(issueTypeName string) bool {
	return strings.Contains(strings.ToLower(issueTypeName), "bug")
}

// extractDescriptionText extracts plain text from ADF or returns plain string.
func extractDescriptionText(desc interface{}) string {
	if desc == nil {
		return ""
	}
	switch v := desc.(type) {
	case string:
		return v
	case map[string]interface{}:
		// Atlassian Document Format — extract text nodes recursively.
		return extractADFText(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func extractADFText(node map[string]interface{}) string {
	var parts []string

	if text, ok := node["text"].(string); ok {
		parts = append(parts, text)
	}

	if content, ok := node["content"].([]interface{}); ok {
		for _, child := range content {
			if childMap, ok := child.(map[string]interface{}); ok {
				parts = append(parts, extractADFText(childMap))
			}
		}
	}

	return strings.Join(parts, " ")
}
