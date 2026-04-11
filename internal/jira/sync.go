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

// Syncer performs incremental and full syncs of Jira issues into the local database.
type Syncer struct {
	client        *Client
	db            *db.DB
	mapper        *UserMapper
	logger        *log.Logger
	boardIDs      []int
	boardAnalyzer *BoardAnalyzer // optional, for config change detection
	autoRefresh   bool           // when true, auto re-analyze boards with changed config
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
			// Parse the last sync time and subtract 2 minutes for overlap.
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

// syncWithJQL fetches issues matching the JQL and upserts them into the database.
func (s *Syncer) syncWithJQL(ctx context.Context, jql string, boardID int) (int, error) {
	total := 0
	startAt := 0
	maxResults := 100

	for {
		result, err := s.client.SearchIssues(ctx, jql, startAt, maxResults)
		if err != nil {
			return total, fmt.Errorf("searching issues: %w", err)
		}

		for _, issue := range result.Issues {
			if err := s.upsertIssue(ctx, issue, boardID); err != nil {
				s.logger.Printf("failed to upsert issue %s: %v", issue.Key, err)
				continue
			}
			total++
		}

		if startAt+len(result.Issues) >= result.Total || len(result.Issues) == 0 {
			break
		}
		startAt += len(result.Issues)
	}

	return total, nil
}

// upsertIssue converts a Jira API issue to a DB record and upserts it.
func (s *Syncer) upsertIssue(ctx context.Context, issue Issue, boardID int) error {
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
		SyncedAt:                now,
	}

	if err := s.db.UpsertJiraIssue(dbIssue); err != nil {
		return err
	}

	// Upsert issue links.
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
			dbLink := db.JiraIssueLink{
				ID:        link.ID,
				SourceKey: sourceKey,
				TargetKey: targetKey,
				LinkType:  linkType,
				SyncedAt:  now,
			}
			if err := s.db.UpsertJiraIssueLink(dbLink); err != nil {
				s.logger.Printf("failed to upsert link %s: %v", link.ID, err)
			}
		}
	}

	return nil
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
			id := 0
			if v.ID != "" {
				if parsed, err := strconv.Atoi(v.ID); err == nil {
					id = parsed
				}
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

	return nil
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
