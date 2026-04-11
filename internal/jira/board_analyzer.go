package jira

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
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
	BoardName   string          `json:"board_name"`
	ProjectKey  string          `json:"project_key"`
	BoardType   string          `json:"board_type"`
	Config      BoardConfig     `json:"config"`
	Sprints     []SprintSummary `json:"sprints"`
	IssueSample IssueSample     `json:"issue_sample"`
}

// BoardProfile is the LLM-generated analysis of a board's workflow.
type BoardProfile struct {
	WorkflowStages     []WorkflowStage    `json:"workflow_stages"`
	EstimationApproach EstimationApproach `json:"estimation_approach"`
	IterationInfo      IterationInfo      `json:"iteration_info"`
	WorkflowSummary    string             `json:"workflow_summary"`
	StaleThresholds    map[string]int     `json:"stale_thresholds"`
	HealthSignals      []string           `json:"health_signals"`
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
	StaleThresholds map[string]int `json:"stale_thresholds,omitempty"`
}

// BoardAnalyzer performs LLM-based analysis of Jira boards.
type BoardAnalyzer struct {
	client     *Client
	db         *db.DB
	aiProvider ai.Provider
	logger     *log.Logger
}

// NewBoardAnalyzer creates a new BoardAnalyzer.
func NewBoardAnalyzer(client *Client, database *db.DB, aiProvider ai.Provider) *BoardAnalyzer {
	return &BoardAnalyzer{
		client:     client,
		db:         database,
		aiProvider: aiProvider,
		logger:     log.New(os.Stderr, "[jira-analyzer] ", log.LstdFlags),
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
	config, err := a.fetchBoardConfig(ctx, board.ID)
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

	// Check if config is unchanged and profile already exists.
	if board.ConfigHash == hash && board.LLMProfileJSON != "" {
		a.logger.Printf("board %d config unchanged (hash=%s), skipping", board.ID, hash[:8])
		var existing BoardProfile
		if err := json.Unmarshal([]byte(board.LLMProfileJSON), &existing); err == nil {
			return &existing, nil
		}
	}

	rawJSON, _ := json.Marshal(rawData.Config.Columns)
	configJSON, _ := json.Marshal(rawData.Config)

	// Call LLM.
	profile, err := a.callLLM(ctx, rawData)
	if err != nil {
		// On LLM failure, use fallback.
		a.logger.Printf("LLM analysis failed for board %d, using fallback: %v", board.ID, err)
		profile = BuildFallbackProfile(rawData)
	}

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

// BuildFallbackProfile creates a basic profile without LLM, from raw board data.
func BuildFallbackProfile(rawData *BoardRawData) *BoardProfile {
	profile := &BoardProfile{
		StaleThresholds: map[string]int{},
		HealthSignals:   []string{},
	}

	// Map columns to workflow stages.
	for _, col := range rawData.Config.Columns {
		phase := "active_work"
		isTerminal := false
		nameLower := strings.ToLower(col.Name)
		if nameLower == "done" || nameLower == "closed" || nameLower == "resolved" {
			phase = "done"
			isTerminal = true
		} else if nameLower == "backlog" || nameLower == "to do" || nameLower == "todo" || nameLower == "open" {
			phase = "backlog"
		}

		var statuses []string
		for _, s := range col.Statuses {
			statuses = append(statuses, s.Name)
		}

		profile.WorkflowStages = append(profile.WorkflowStages, WorkflowStage{
			Name:             col.Name,
			OriginalStatuses: statuses,
			Phase:            phase,
			IsTerminal:       isTerminal,
		})

		// Default stale threshold: 3 days for active_work, 7 for backlog.
		if phase == "active_work" {
			profile.StaleThresholds[col.Name] = 3
		} else if phase == "backlog" {
			profile.StaleThresholds[col.Name] = 7
		}
	}

	// Estimation approach.
	if rawData.Config.Estimation != nil {
		profile.EstimationApproach = EstimationApproach{
			Type:  "story_points",
			Field: &rawData.Config.Estimation.FieldID,
		}
	} else {
		profile.EstimationApproach = EstimationApproach{Type: "none"}
	}

	// Iteration info.
	profile.IterationInfo.HasIterations = len(rawData.Sprints) > 0 || rawData.BoardType == "scrum"

	// Workflow summary.
	var stageNames []string
	for _, s := range profile.WorkflowStages {
		stageNames = append(stageNames, s.Name)
	}
	profile.WorkflowSummary = fmt.Sprintf("%s board with stages: %s",
		rawData.BoardType, strings.Join(stageNames, " -> "))

	return profile
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
func (a *BoardAnalyzer) fetchBoardConfig(ctx context.Context, boardID int) (*BoardConfig, error) {
	config := &BoardConfig{}

	// Fetch board configuration.
	type columnConfigResp struct {
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
		Estimation *EstimationField `json:"estimation"`
		Filter     struct {
			Query string `json:"query"`
		} `json:"filter"`
	}

	var resp columnConfigResp
	path := fmt.Sprintf("/rest/agile/1.0/board/%d/configuration", boardID)
	if err := a.client.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("fetching board configuration: %w", err)
	}

	for _, col := range resp.ColumnConfig.Columns {
		bc := BoardColumn{Name: col.Name}
		for _, s := range col.Statuses {
			bc.Statuses = append(bc.Statuses, BoardColumnStatus{ID: s.ID})
		}
		config.Columns = append(config.Columns, bc)
	}

	config.Estimation = resp.Estimation
	config.FilterJQL = resp.Filter.Query

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
  "stale_thresholds": {"Column Name": 3},
  "health_signals": ["signal1", "signal2"]
}

Guidelines:
- Group similar statuses into workflow stages
- Identify the phase for each stage: backlog, active_work, review, testing, done, other
- Estimate reasonable stale thresholds (days) per stage
- Suggest health signals relevant to this board's workflow`

	dataJSON, _ := json.Marshal(rawData)
	userMessage := fmt.Sprintf("Analyze this Jira board:\n\n%s", string(dataJSON))

	response, _, err := a.aiProvider.QuerySync(ctx, systemPrompt, userMessage, "board-analysis")
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
