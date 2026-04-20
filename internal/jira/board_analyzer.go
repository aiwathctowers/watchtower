package jira

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"watchtower/internal/ai"
	"watchtower/internal/db"
)

// BoardConfig describes the configuration of a Jira board.
type BoardConfig struct {
	Columns    []BoardColumn    `json:"columns"`
	Estimation *EstimationField `json:"estimation"`
	FilterJQL  string           `json:"filter_jql"`
}

// BoardColumn represents a column on a Jira board.
type BoardColumn struct {
	Name     string              `json:"name"`
	Statuses []BoardColumnStatus `json:"statuses"`
}

// BoardColumnStatus represents a status within a board column.
type BoardColumnStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CategoryKey  string `json:"category_key"`
	CategoryName string `json:"category_name"`
}

// EstimationField describes the estimation field used on a board.
type EstimationField struct {
	FieldID     string `json:"fieldId"`
	DisplayName string `json:"displayName"`
}

// SprintSummary summarizes a sprint for board analysis.
type SprintSummary struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Goal      string `json:"goal"`
}

// IssueSample provides aggregate statistics about issues on a board.
type IssueSample struct {
	TotalCount           int            `json:"total_count"`
	StatusDistribution   map[string]int `json:"status_distribution"`
	AssigneeDistribution map[string]int `json:"assignee_distribution"`
	PriorityDistribution map[string]int `json:"priority_distribution"`
}

// BoardRawData is the collected raw data for a board before LLM analysis.
type BoardRawData struct {
	BoardName    string               `json:"board_name"`
	ProjectKey   string               `json:"project_key"`
	BoardType    string               `json:"board_type"`
	Config       BoardConfig          `json:"config"`
	Sprints      []SprintSummary      `json:"sprints"`
	IssueSample  IssueSample          `json:"issue_sample"`
	CustomFields []CustomFieldContext `json:"custom_fields,omitempty"`
}

// CustomFieldContext provides custom field mapping info for LLM context.
type CustomFieldContext struct {
	FieldID string `json:"field_id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Type    string `json:"type"`
}

// BoardProfile is the LLM-generated analysis of a board's workflow.
type BoardProfile struct {
	WorkflowStages     []WorkflowStage      `json:"workflow_stages"`
	EstimationApproach EstimationApproach   `json:"estimation_approach"`
	IterationInfo      IterationInfo        `json:"iteration_info"`
	WorkflowSummary    string               `json:"workflow_summary"`
	StaleThresholds    map[string]int       `json:"stale_thresholds"`
	HealthSignals      []string             `json:"health_signals"`
	CustomFields       []CustomFieldContext `json:"custom_fields,omitempty"`
}

// WorkflowStage represents a stage in the board's workflow.
type WorkflowStage struct {
	Name                  string   `json:"name"`
	OriginalStatuses      []string `json:"original_statuses"`
	Phase                 string   `json:"phase"`
	IsTerminal            bool     `json:"is_terminal"`
	TypicalDurationSignal string   `json:"typical_duration_signal"`
}

// EstimationApproach describes how issues are estimated.
type EstimationApproach struct {
	Type  string  `json:"type"`
	Field *string `json:"field"`
}

// IterationInfo describes the board's iteration/sprint pattern.
type IterationInfo struct {
	HasIterations     bool `json:"has_iterations"`
	TypicalLengthDays int  `json:"typical_length_days"`
	AvgThroughput     int  `json:"avg_throughput"`
}

// UserOverrides stores user-specified overrides for board analysis.
type UserOverrides struct {
	StaleThresholds map[string]int    `json:"stale_thresholds,omitempty"`
	TerminalStages  map[string]bool   `json:"terminal_stages,omitempty"` // status name → is_terminal override
	PhaseOverrides  map[string]string `json:"phase_overrides,omitempty"` // status name → phase (backlog|active_work|review|testing|done|other)
}

// BoardAnalyzer performs LLM-based analysis of Jira boards.
type BoardAnalyzer struct {
	client     *Client
	db         *db.DB
	aiProvider ai.Provider
	language   string
	logger     *log.Logger

	// Accumulated usage from LLM calls.
	totalInputTokens  int
	totalOutputTokens int
	totalAPITokens    int
}

// NewBoardAnalyzer creates a new BoardAnalyzer.
func NewBoardAnalyzer(client *Client, database *db.DB, aiProvider ai.Provider) *BoardAnalyzer {
	return &BoardAnalyzer{
		client:     client,
		db:         database,
		aiProvider: aiProvider,
		language:   "English",
		logger:     log.New(os.Stderr, "[jira-analyzer] ", log.LstdFlags),
	}
}

// AccumulatedUsage returns token counts accumulated across all LLM calls.
func (a *BoardAnalyzer) AccumulatedUsage() (inputTokens, outputTokens, totalAPITokens int) {
	return a.totalInputTokens, a.totalOutputTokens, a.totalAPITokens
}

// addUsage accumulates token usage from an LLM call.
func (a *BoardAnalyzer) addUsage(usage *ai.Usage) {
	if usage == nil {
		return
	}
	a.totalInputTokens += usage.InputTokens
	a.totalOutputTokens += usage.OutputTokens
	a.totalAPITokens += usage.TotalAPITokens
}

// SetLanguage sets the output language for board analysis.
func (a *BoardAnalyzer) SetLanguage(lang string) {
	if lang != "" {
		a.language = lang
	}
}

// FetchBoardRawData collects board configuration and issue statistics from Jira API and local DB.
func (a *BoardAnalyzer) FetchBoardRawData(ctx context.Context, board db.JiraBoard) (*BoardRawData, error) {
	raw := &BoardRawData{
		BoardName:  board.Name,
		ProjectKey: board.ProjectKey,
		BoardType:  board.BoardType,
	}

	// Fetch board configuration (columns + estimation).
	config, err := a.fetchBoardConfig(ctx, board.ID, board.ProjectKey)
	if err != nil {
		a.logger.Printf("warning: could not fetch board config for %d: %v", board.ID, err)
	} else {
		raw.Config = *config
	}

	// Fetch sprints from local DB.
	sprints, err := a.db.GetJiraActiveSprints(board.ID)
	if err != nil {
		a.logger.Printf("warning: could not fetch sprints for board %d: %v", board.ID, err)
	}
	for _, s := range sprints {
		raw.Sprints = append(raw.Sprints, SprintSummary{
			Name:      s.Name,
			State:     s.State,
			StartDate: s.StartDate,
			EndDate:   s.EndDate,
			Goal:      s.Goal,
		})
	}

	// Build issue sample from local DB.
	raw.IssueSample = a.buildIssueSample(board.ID)

	return raw, nil
}

// AnalyzeBoard runs LLM analysis on a single board and stores the result.
func (a *BoardAnalyzer) AnalyzeBoard(ctx context.Context, board db.JiraBoard) (*BoardProfile, error) {
	rawData, err := a.FetchBoardRawData(ctx, board)
	if err != nil {
		return nil, fmt.Errorf("fetching raw data: %w", err)
	}

	hash := ComputeConfigHash(rawData)

	// Check if config is unchanged and profile already exists and is valid.
	if board.ConfigHash == hash && hash != "" && board.LLMProfileJSON != "" {
		var existing BoardProfile
		if err := json.Unmarshal([]byte(board.LLMProfileJSON), &existing); err == nil && len(existing.WorkflowStages) > 0 {
			a.logger.Printf("board %d config unchanged (hash=%s), skipping", board.ID, hash[:8])
			return &existing, nil
		}
	}

	// Enrich with custom field context (best-effort).
	fd := NewFieldDiscovery(a.client, a.db, a.aiProvider)
	if fd.NeedsDiscovery() {
		if discErr := fd.DiscoverAndClassify(ctx); discErr != nil {
			a.logger.Printf("warning: field discovery failed: %v", discErr)
		}
	}
	mappings, _ := a.db.GetJiraBoardFieldMap(board.ID)
	if len(mappings) == 0 {
		if mapped, mapErr := fd.MapFieldsForBoard(ctx, board); mapErr != nil {
			a.logger.Printf("warning: field mapping failed for board %d: %v", board.ID, mapErr)
		} else {
			mappings = mapped
		}
	}
	// Collect usage from field discovery LLM calls.
	fdIn, fdOut, fdAPI := fd.AccumulatedUsage()
	a.totalInputTokens += fdIn
	a.totalOutputTokens += fdOut
	a.totalAPITokens += fdAPI
	if len(mappings) > 0 {
		usefulFields, _ := a.db.GetUsefulJiraCustomFields()
		fieldIndex := make(map[string]db.JiraCustomField)
		for _, f := range usefulFields {
			fieldIndex[f.ID] = f
		}
		for _, m := range mappings {
			f := fieldIndex[m.FieldID]
			rawData.CustomFields = append(rawData.CustomFields, CustomFieldContext{
				FieldID: m.FieldID,
				Name:    f.Name,
				Role:    m.Role,
				Type:    f.FieldType,
			})
		}
	}

	rawJSON, _ := json.Marshal(rawData.Config.Columns)
	configJSON, _ := json.Marshal(rawData.Config)

	// Call LLM.
	profile, err := a.callLLM(ctx, rawData)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis for board %d: %w", board.ID, err)
	}
	if len(profile.WorkflowStages) == 0 {
		return nil, fmt.Errorf("LLM returned empty workflow for board %d", board.ID)
	}

	// Store custom field mappings in the profile.
	profile.CustomFields = rawData.CustomFields

	profileJSON, _ := json.Marshal(profile)
	now := time.Now().UTC().Format(time.RFC3339)

	if err := a.db.UpdateJiraBoardProfile(board.ID,
		string(rawJSON), string(configJSON), string(profileJSON),
		profile.WorkflowSummary, hash, now); err != nil {
		return nil, fmt.Errorf("storing profile: %w", err)
	}

	return profile, nil
}

// AnalyzeAllSelected analyzes all selected boards. Returns count of analyzed boards.
func (a *BoardAnalyzer) AnalyzeAllSelected(ctx context.Context) (int, error) {
	boards, err := a.db.GetJiraSelectedBoards()
	if err != nil {
		return 0, fmt.Errorf("getting selected boards: %w", err)
	}

	count := 0
	for _, board := range boards {
		full, err := a.db.GetJiraBoardProfile(board.ID)
		if err != nil || full == nil {
			full = &board
		}

		if _, err := a.AnalyzeBoard(ctx, *full); err != nil {
			a.logger.Printf("failed to analyze board %d (%s): %v", board.ID, board.Name, err)
			continue
		}
		count++
	}
	return count, nil
}

// CheckConfigChanged returns board IDs whose config hash has changed since last analysis.
func (a *BoardAnalyzer) CheckConfigChanged(ctx context.Context) ([]int, error) {
	boards, err := a.db.GetJiraSelectedBoards()
	if err != nil {
		return nil, err
	}

	var changed []int
	for _, board := range boards {
		full, err := a.db.GetJiraBoardProfile(board.ID)
		if err != nil || full == nil {
			changed = append(changed, board.ID)
			continue
		}

		rawData, err := a.FetchBoardRawData(ctx, *full)
		if err != nil {
			continue
		}

		newHash := ComputeConfigHash(rawData)
		if newHash != full.ConfigHash {
			changed = append(changed, board.ID)
		}
	}
	return changed, nil
}

// RefreshCooldown is the minimum interval between automatic re-analyses of a board.
const RefreshCooldown = 24 * time.Hour

// RefreshResult describes the outcome of a single board refresh attempt.
type RefreshResult struct {
	BoardID   int
	BoardName string
	Refreshed bool
	Skipped   bool // true if cooldown not elapsed
	Error     error
}

// CheckAndRefreshProfiles checks selected boards for config changes and re-analyzes
// those whose config hash changed and whose cooldown (24h) has elapsed.
// User overrides are preserved across re-analysis: after the new LLM profile is
// generated, existing user_overrides_json stale_thresholds are merged on top.
// If autoRefresh is false, only logs which boards need refresh without running LLM.
func (a *BoardAnalyzer) CheckAndRefreshProfiles(ctx context.Context, autoRefresh bool) ([]RefreshResult, error) {
	boards, err := a.db.GetJiraSelectedBoards()
	if err != nil {
		return nil, fmt.Errorf("getting selected boards: %w", err)
	}

	var results []RefreshResult

	for _, board := range boards {
		full, err := a.db.GetJiraBoardProfile(board.ID)
		if err != nil || full == nil {
			// No profile yet — not a "changed config" case, skip.
			continue
		}

		// Fetch current raw data to compute fresh hash.
		rawData, err := a.FetchBoardRawData(ctx, *full)
		if err != nil {
			a.logger.Printf("warning: could not fetch raw data for board %d: %v", board.ID, err)
			continue
		}

		newHash := ComputeConfigHash(rawData)
		if newHash == full.ConfigHash {
			continue // config unchanged
		}

		// Check cooldown.
		if full.ProfileGeneratedAt != "" {
			generated, err := time.Parse(time.RFC3339, full.ProfileGeneratedAt)
			if err == nil && time.Since(generated) < RefreshCooldown {
				a.logger.Printf("board %d (%s): config changed but cooldown not elapsed (generated %s ago)",
					board.ID, board.Name, time.Since(generated).Truncate(time.Minute))
				results = append(results, RefreshResult{
					BoardID:   board.ID,
					BoardName: board.Name,
					Skipped:   true,
				})
				continue
			}
		}

		a.logger.Printf("board %d (%s): config changed (hash %s -> %s)",
			board.ID, board.Name, full.ConfigHash[:min(8, len(full.ConfigHash))], newHash[:min(8, len(newHash))])

		if !autoRefresh {
			a.logger.Printf("board %d (%s): needs re-analyze — run 'watchtower jira boards analyze' or use --auto",
				board.ID, board.Name)
			results = append(results, RefreshResult{
				BoardID:   board.ID,
				BoardName: board.Name,
			})
			continue
		}

		// Save existing user overrides before re-analysis.
		existingOverrides := full.UserOverridesJSON

		// Clear config hash to force re-analysis.
		full.ConfigHash = ""
		profile, err := a.AnalyzeBoard(ctx, *full)
		if err != nil {
			a.logger.Printf("warning: re-analysis failed for board %d (%s): %v", board.ID, board.Name, err)
			results = append(results, RefreshResult{
				BoardID:   board.ID,
				BoardName: board.Name,
				Error:     err,
			})
			continue
		}

		// Merge user overrides back on top of new profile.
		if existingOverrides != "" {
			if err := a.mergeUserOverrides(board.ID, profile, existingOverrides); err != nil {
				a.logger.Printf("warning: failed to merge overrides for board %d: %v", board.ID, err)
			}
		}

		a.logger.Printf("board %d (%s): re-analyzed successfully", board.ID, board.Name)
		results = append(results, RefreshResult{
			BoardID:   board.ID,
			BoardName: board.Name,
			Refreshed: true,
		})
	}

	return results, nil
}

// mergeUserOverrides re-applies user override stale thresholds on top of a freshly
// generated LLM profile and saves the updated profile to DB.
func (a *BoardAnalyzer) mergeUserOverrides(boardID int, profile *BoardProfile, overridesJSON string) error {
	var overrides UserOverrides
	if err := json.Unmarshal([]byte(overridesJSON), &overrides); err != nil {
		return fmt.Errorf("parsing user overrides: %w", err)
	}

	if len(overrides.StaleThresholds) == 0 {
		return nil
	}

	// Apply overrides on top of LLM-generated thresholds.
	if profile.StaleThresholds == nil {
		profile.StaleThresholds = make(map[string]int)
	}
	for k, v := range overrides.StaleThresholds {
		profile.StaleThresholds[k] = v
	}

	// Re-save profile with merged thresholds.
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("marshaling merged profile: %w", err)
	}

	// We only need to update llm_profile_json; other columns stay the same.
	board, err := a.db.GetJiraBoardProfile(boardID)
	if err != nil || board == nil {
		return fmt.Errorf("getting board for merge: %w", err)
	}

	return a.db.UpdateJiraBoardProfile(boardID,
		board.RawColumnsJSON, board.RawConfigJSON, string(profileJSON),
		board.WorkflowSummary, board.ConfigHash, board.ProfileGeneratedAt)
}

// ComputeConfigHash computes a SHA256 hash of the board configuration for change detection.
func ComputeConfigHash(rawData *BoardRawData) string {
	var parts []string

	// Canonicalize columns.
	for _, col := range rawData.Config.Columns {
		var statusNames []string
		for _, s := range col.Statuses {
			statusNames = append(statusNames, s.Name)
		}
		sort.Strings(statusNames)
		parts = append(parts, fmt.Sprintf("%s:%s", col.Name, strings.Join(statusNames, ",")))
	}

	// Add estimation field.
	if rawData.Config.Estimation != nil {
		parts = append(parts, "est:"+rawData.Config.Estimation.FieldID)
	}

	data := strings.Join(parts, "|")
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

// GetEffectiveStaleThresholds returns stale thresholds with user overrides applied.
func GetEffectiveStaleThresholds(board db.JiraBoard) (map[string]int, error) {
	result := make(map[string]int)

	// Load profile thresholds.
	if board.LLMProfileJSON != "" {
		var profile BoardProfile
		if err := json.Unmarshal([]byte(board.LLMProfileJSON), &profile); err == nil {
			for k, v := range profile.StaleThresholds {
				result[k] = v
			}
		}
	}

	// Apply user overrides.
	if board.UserOverridesJSON != "" {
		var overrides UserOverrides
		if err := json.Unmarshal([]byte(board.UserOverridesJSON), &overrides); err == nil {
			for k, v := range overrides.StaleThresholds {
				result[k] = v
			}
		}
	}

	return result, nil
}

// fetchBoardConfig fetches board configuration from the Jira API.
// Falls back to project statuses API when board configuration endpoint is unavailable.
func (a *BoardAnalyzer) fetchBoardConfig(ctx context.Context, boardID int, projectKey string) (*BoardConfig, error) {
	// Fetch board configuration.
	type columnConfigResp struct {
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Statuses []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
		Estimation *EstimationField `json:"estimation"`
		Filter     struct {
			Query string `json:"query"`
		} `json:"filter"`
	}

	// Primary: project statuses (always available with basic scopes).
	config, err := a.fetchProjectStatuses(ctx, projectKey)
	if err != nil {
		return nil, err
	}

	// Try to enrich with board-specific config (estimation, filter, column grouping).
	var resp columnConfigResp
	path := fmt.Sprintf("/rest/agile/1.0/board/%d/configuration", boardID)
	if err := a.client.get(ctx, path, &resp); err == nil {
		// Override columns with board-specific grouping if available.
		if len(resp.ColumnConfig.Columns) > 0 {
			config.Columns = nil
			for _, col := range resp.ColumnConfig.Columns {
				bc := BoardColumn{Name: col.Name}
				for _, s := range col.Statuses {
					bc.Statuses = append(bc.Statuses, BoardColumnStatus{ID: s.ID, Name: s.Name})
				}
				config.Columns = append(config.Columns, bc)
			}
		}
		config.Estimation = resp.Estimation
		config.FilterJQL = resp.Filter.Query
	}

	return config, nil
}

// fetchProjectStatuses builds board config from /rest/api/2/project/{key}/statuses.
func (a *BoardAnalyzer) fetchProjectStatuses(ctx context.Context, projectKey string) (*BoardConfig, error) {
	if projectKey == "" {
		return nil, fmt.Errorf("no project key for status fallback")
	}

	type statusEntry struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		StatusCategory struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"statusCategory"`
	}
	type issueTypeStatuses struct {
		Statuses []statusEntry `json:"statuses"`
	}

	var resp []issueTypeStatuses
	path := fmt.Sprintf("/rest/api/2/project/%s/statuses", url.PathEscape(projectKey))
	if err := a.client.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("fetching project statuses: %w", err)
	}

	// Deduplicate statuses and group by category.
	type statusInfo struct {
		ID          string
		Name        string
		CategoryKey string
	}
	seen := map[string]bool{}
	categoryStatuses := map[string][]statusInfo{}
	for _, it := range resp {
		for _, s := range it.Statuses {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			catKey := s.StatusCategory.Key
			categoryStatuses[catKey] = append(categoryStatuses[catKey], statusInfo{
				ID: s.ID, Name: s.Name, CategoryKey: catKey,
			})
		}
	}

	// Map category keys to columns in workflow order.
	catColumnOrder := []struct{ key, name string }{
		{"new", "To Do"},
		{"indeterminate", "In Progress"},
		{"done", "Done"},
	}

	config := &BoardConfig{}
	for _, cat := range catColumnOrder {
		statuses, ok := categoryStatuses[cat.key]
		if !ok {
			continue
		}
		bc := BoardColumn{Name: cat.name}
		for _, s := range statuses {
			bc.Statuses = append(bc.Statuses, BoardColumnStatus{
				ID: s.ID, Name: s.Name, CategoryKey: s.CategoryKey,
			})
		}
		config.Columns = append(config.Columns, bc)
	}

	return config, nil
}

// buildIssueSample generates aggregate statistics from locally synced issues.
func (a *BoardAnalyzer) buildIssueSample(boardID int) IssueSample {
	sample := IssueSample{
		StatusDistribution:   make(map[string]int),
		AssigneeDistribution: make(map[string]int),
		PriorityDistribution: make(map[string]int),
	}

	rows, err := a.db.Query(`SELECT status, assignee_display_name, priority FROM jira_issues
		WHERE board_id = ? AND is_deleted = 0`, boardID)
	if err != nil {
		return sample
	}
	defer rows.Close()

	for rows.Next() {
		var status, assignee, priority string
		if err := rows.Scan(&status, &assignee, &priority); err != nil {
			continue
		}
		sample.TotalCount++
		sample.StatusDistribution[status]++
		if assignee != "" {
			sample.AssigneeDistribution[assignee]++
		}
		if priority != "" {
			sample.PriorityDistribution[priority]++
		}
	}

	return sample
}

// callLLM invokes the AI provider to analyze board data.
func (a *BoardAnalyzer) callLLM(ctx context.Context, rawData *BoardRawData) (*BoardProfile, error) {
	systemPrompt := `You are a Jira board workflow analyzer. Given board configuration data,
analyze the workflow and produce a structured JSON profile.

Respond with ONLY valid JSON matching this structure:
{
  "workflow_stages": [{"name":"...", "original_statuses":["..."], "phase":"backlog|active_work|review|testing|done|other", "is_terminal":false, "typical_duration_signal":"hours|days|weeks"}],
  "estimation_approach": {"type":"story_points|time|none", "field":"fieldId or null"},
  "iteration_info": {"has_iterations":true, "typical_length_days":14, "avg_throughput":0},
  "workflow_summary": "One paragraph describing the workflow",
  "stale_thresholds": {"Status Name": 3},
  "health_signals": ["signal1", "signal2", "signal3"]
}

Guidelines:
- Group similar statuses into workflow stages (e.g. "Triage" and "New" → backlog; "Declined" → other or done)
- Identify the phase for each stage: backlog, active_work, review, testing, done, other
- Set DIFFERENT stale thresholds (days) per status based on expected duration in that phase:
  - backlog statuses (Backlog, Triage, New): 7-14 days (items can wait)
  - active_work (In Progress, On Track): 2-3 days (should move quickly)
  - review (Code Review, DEV REVIEW): 1-2 days (fast feedback loops)
  - testing (QA, Ready for test): 3-5 days (may need test cycles)
  - NEVER set the same threshold for all statuses — different phases have different expected durations
- IMPORTANT: Generate 3-8 specific, actionable health signals based on the board's workflow structure and issue data. Examples:
  - "Review bottleneck risk — DEV REVIEW + Review have X issues" (when review stages have many items)
  - "High WIP: N active issues across In Progress / On Track" (when active_work has high counts)
  - "Missing estimation — T-Shirt Size field available but most issues lack estimates"
  - "Backlog growing — N issues in Triage/New without assignee"
  - "No sprint configured — consider time-boxing work for better predictability"
  - "Testing backlog — Ready for test has N issues waiting"
  - "Blocked issues — N issues in Declined status need attention"
  - "Unbalanced workload — top assignee has X issues vs average Y"
  Tailor signals to the actual issue_sample data (status_distribution, assignee_distribution, priority_distribution).
  ALWAYS return at least 3 health signals. Never return an empty array.
- If custom_fields are present, incorporate them into the analysis:
  - Use estimation fields (story_points, tshirt_size) to set estimation_approach
  - Mention relevant role fields (qa_assignee, developer) in workflow summary
  - Note categorization fields (area, severity) as available dimensions for health signals
- LANGUAGE: Write workflow_summary and health_signals in ` + a.language + `. Keep JSON keys and phase values in English.`

	dataJSON, _ := json.Marshal(rawData)
	userMessage := fmt.Sprintf("Analyze this Jira board:\n\n%s", string(dataJSON))

	response, usage, err := a.aiProvider.QuerySync(ctx, systemPrompt, userMessage, "")
	a.addUsage(usage)
	if err != nil {
		return nil, fmt.Errorf("LLM query: %w", err)
	}

	// Extract JSON from response (may be wrapped in markdown code block).
	response = extractJSON(response)

	var profile BoardProfile
	if err := json.Unmarshal([]byte(response), &profile); err != nil {
		return nil, fmt.Errorf("parsing LLM response: %w", err)
	}

	return &profile, nil
}

// extractJSON extracts JSON from a response that may be wrapped in markdown code blocks.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
