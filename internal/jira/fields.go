package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"watchtower/internal/ai"
	"watchtower/internal/db"
)

// FieldDiscovery handles custom field discovery and classification.
type FieldDiscovery struct {
	client     *Client
	db         *db.DB
	aiProvider ai.Provider
	logger     *log.Logger

	// Accumulated usage from LLM calls.
	totalInputTokens  int
	totalOutputTokens int
	totalAPITokens    int
}

// NewFieldDiscovery creates a new FieldDiscovery instance.
func NewFieldDiscovery(client *Client, database *db.DB, aiProvider ai.Provider) *FieldDiscovery {
	return &FieldDiscovery{
		client:     client,
		db:         database,
		aiProvider: aiProvider,
		logger:     log.New(log.Default().Writer(), "[jira-fields] ", log.LstdFlags),
	}
}

// AccumulatedUsage returns token counts accumulated across all LLM calls.
func (fd *FieldDiscovery) AccumulatedUsage() (inputTokens, outputTokens, totalAPITokens int) {
	return fd.totalInputTokens, fd.totalOutputTokens, fd.totalAPITokens
}

// addUsage accumulates token usage from an LLM call.
func (fd *FieldDiscovery) addUsage(usage *ai.Usage) {
	if usage == nil {
		return
	}
	fd.totalInputTokens += usage.InputTokens
	fd.totalOutputTokens += usage.OutputTokens
	fd.totalAPITokens += usage.TotalAPITokens
}

// DiscoverFields fetches all fields from Jira API and stores custom fields in DB.
// Returns the list of discovered custom fields.
func (fd *FieldDiscovery) DiscoverFields(ctx context.Context) ([]db.JiraCustomField, error) {
	type fieldSchema struct {
		Type  string `json:"type"`
		Items string `json:"items"`
	}
	type jiraField struct {
		ID     string       `json:"id"`
		Name   string       `json:"name"`
		Custom bool         `json:"custom"`
		Schema *fieldSchema `json:"schema"`
	}

	var allFields []jiraField
	if err := fd.client.get(ctx, "/rest/api/2/field", &allFields); err != nil {
		return nil, fmt.Errorf("fetching fields: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var customFields []db.JiraCustomField

	for _, f := range allFields {
		if !f.Custom {
			continue
		}

		fieldType := ""
		itemsType := ""
		if f.Schema != nil {
			fieldType = f.Schema.Type
			itemsType = f.Schema.Items
		}

		// Skip internal Jira Service Desk fields (all have "sd-" prefix).
		if strings.HasPrefix(fieldType, "sd-") {
			continue
		}

		cf := db.JiraCustomField{
			ID:        f.ID,
			Name:      f.Name,
			FieldType: fieldType,
			ItemsType: itemsType,
			SyncedAt:  now,
		}
		if err := fd.db.UpsertJiraCustomField(cf); err != nil {
			fd.logger.Printf("warning: could not upsert field %s: %v", f.ID, err)
			continue
		}
		customFields = append(customFields, cf)
	}

	fd.logger.Printf("discovered %d custom fields from %d total", len(customFields), len(allFields))
	return customFields, nil
}

// ClassifyFields uses LLM to classify which custom fields are useful for board analysis.
func (fd *FieldDiscovery) ClassifyFields(ctx context.Context, fields []db.JiraCustomField) error {
	if len(fields) == 0 {
		return nil
	}
	if fd.aiProvider == nil {
		return fmt.Errorf("AI provider not configured")
	}

	// Build compact field list for LLM
	type fieldInput struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Type  string `json:"type"`
		Items string `json:"items,omitempty"`
	}
	var inputs []fieldInput
	for _, f := range fields {
		inputs = append(inputs, fieldInput{
			ID:    f.ID,
			Name:  f.Name,
			Type:  f.FieldType,
			Items: f.ItemsType,
		})
	}

	systemPrompt := `You are a Jira workspace analyzer. Given a list of custom fields, classify each as useful or not for project management board analysis.

Respond with ONLY valid JSON — an array of objects:
[{"id":"customfield_XXXXX","useful":true,"hint":"estimation"},...]

Usage hints (only for useful=true fields):
- "estimation" — story points, t-shirt size, effort, complexity
- "assignee_role" — QA engineer, developer, product manager, delivery manager, tech lead
- "categorization" — area, environment, severity, discipline, region, team
- "tracking" — branch, merge request, release notes, checklist
- "planning" — planned start/end, kick-off, delivery commitment, due dates
- "workflow" — hold reason, flagged, blocked reason, approval status

Mark as useful=false (skip) fields that are:
- Jira internal (rank, color, epic link/status, parent link, development, design)
- Form/checklist metadata (locked forms, total forms, open forms, submitted forms)
- Jira Product Discovery (idea archived, delivery progress/status, insights count, comments count)
- Generic text fields with no clear semantic meaning
- Fields that duplicate standard Jira fields (sprint, labels, components already captured)

Be generous — if a field COULD provide useful context for understanding how a team works, mark it useful.`

	dataJSON, err := json.Marshal(inputs)
	if err != nil {
		return fmt.Errorf("marshaling fields for classification: %w", err)
	}
	userMessage := fmt.Sprintf("Classify these %d custom fields:\n\n%s", len(inputs), string(dataJSON))

	response, usage, err := fd.aiProvider.QuerySync(ctx, systemPrompt, userMessage, "")
	fd.addUsage(usage)
	if err != nil {
		return fmt.Errorf("LLM classification: %w", err)
	}

	// Extract JSON (may be wrapped in markdown code block)
	response = extractJSON(response)

	type classificationResult struct {
		ID     string `json:"id"`
		Useful bool   `json:"useful"`
		Hint   string `json:"hint"`
	}
	var results []classificationResult
	if err := json.Unmarshal([]byte(response), &results); err != nil {
		return fmt.Errorf("parsing LLM classification: %w", err)
	}

	// Update DB
	updated := 0
	useful := 0
	for _, r := range results {
		if err := fd.db.UpdateJiraCustomFieldClassification(r.ID, r.Useful, r.Hint); err != nil {
			fd.logger.Printf("warning: could not update classification for %s: %v", r.ID, err)
			continue
		}
		updated++
		if r.Useful {
			useful++
		}
	}
	fd.logger.Printf("classified %d fields (%d useful)", updated, useful)

	return nil
}

// DiscoverAndClassify runs field discovery and LLM classification in sequence.
func (fd *FieldDiscovery) DiscoverAndClassify(ctx context.Context) error {
	fields, err := fd.DiscoverFields(ctx)
	if err != nil {
		return err
	}
	return fd.ClassifyFields(ctx, fields)
}

// NeedsDiscovery returns true if fields haven't been synced or are stale (>24h).
func (fd *FieldDiscovery) NeedsDiscovery() bool {
	syncedAt, err := fd.db.GetJiraCustomFieldsSyncedAt()
	if err != nil || syncedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, syncedAt)
	if err != nil {
		return true
	}
	return time.Since(t) > 24*time.Hour
}

// MapFieldsForBoard samples issues from a board to determine which custom fields
// are actually used, then asks LLM to assign semantic roles for each.
func (fd *FieldDiscovery) MapFieldsForBoard(ctx context.Context, board db.JiraBoard) ([]db.JiraBoardFieldMap, error) {
	// 1. Get useful fields from DB
	usefulFields, err := fd.db.GetUsefulJiraCustomFields()
	if err != nil || len(usefulFields) == 0 {
		return nil, fmt.Errorf("no useful fields discovered, run field discovery first")
	}

	// 2. Build field ID list for API request
	fieldIDs := make([]string, 0, len(usefulFields))
	fieldIndex := make(map[string]db.JiraCustomField)
	for _, f := range usefulFields {
		fieldIDs = append(fieldIDs, f.ID)
		fieldIndex[f.ID] = f
	}

	// 3. Sample issues from the board's project
	type issueResponse struct {
		Issues []struct {
			Fields map[string]interface{} `json:"fields"`
		} `json:"issues"`
	}

	// Validate project key to prevent JQL injection.
	if !validProjectKeyRe.MatchString(board.ProjectKey) {
		return nil, fmt.Errorf("invalid project key: %s", board.ProjectKey)
	}

	// Use project key to query issues (board_id isn't directly queryable via JQL)
	jql := fmt.Sprintf("project=%s ORDER BY updated DESC", board.ProjectKey)
	fieldsParam := strings.Join(fieldIDs, ",")
	path := fmt.Sprintf("/rest/api/3/search/jql?jql=%s&maxResults=20&fields=%s",
		url.QueryEscape(jql), url.QueryEscape(fieldsParam))

	var resp issueResponse
	if err := fd.client.get(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("sampling issues: %w", err)
	}

	if len(resp.Issues) == 0 {
		fd.logger.Printf("no issues found for board %d (project %s)", board.ID, board.ProjectKey)
		return nil, nil
	}

	// 4. Analyze which fields have non-null values and collect sample values
	type fieldSample struct {
		ID      string        `json:"id"`
		Name    string        `json:"name"`
		Type    string        `json:"type"`
		Hint    string        `json:"hint"`
		NonNull int           `json:"non_null_count"`
		Samples []interface{} `json:"sample_values"`
	}

	fieldStats := make(map[string]*fieldSample)
	for _, issue := range resp.Issues {
		for fid, val := range issue.Fields {
			if val == nil {
				continue
			}
			uf, ok := fieldIndex[fid]
			if !ok {
				continue
			}
			fs, exists := fieldStats[fid]
			if !exists {
				fs = &fieldSample{
					ID:   fid,
					Name: uf.Name,
					Type: uf.FieldType,
					Hint: uf.UsageHint,
				}
				fieldStats[fid] = fs
			}
			fs.NonNull++
			// Keep up to 3 sample values
			if len(fs.Samples) < 3 {
				fs.Samples = append(fs.Samples, val)
			}
		}
	}

	if len(fieldStats) == 0 {
		fd.logger.Printf("no useful custom fields populated in board %d issues", board.ID)
		return nil, nil
	}

	// 5. Send to LLM for role assignment
	var samples []fieldSample
	for _, fs := range fieldStats {
		samples = append(samples, *fs)
	}

	// Sort for deterministic output
	sort.Slice(samples, func(i, j int) bool { return samples[i].ID < samples[j].ID })

	systemPrompt := `You are a Jira board analyzer. Given custom fields that are actually used on a specific board (with sample values), assign a concrete role to each field.

Respond with ONLY valid JSON — an array of objects:
[{"id":"customfield_XXXXX","role":"story_points"},...]

Available roles:
- "story_points" — numeric estimation (story points, effort points)
- "tshirt_size" — text/option estimation (S/M/L/XL, t-shirt sizing)
- "qa_assignee" — QA engineer assigned to the issue
- "developer" — developer assigned (if separate from main assignee)
- "product_manager" — PM or product owner
- "delivery_manager" — delivery/project manager
- "severity" — severity level (P1/P2/P3, Critical/Major/Minor)
- "environment" — target environment (prod/staging/dev)
- "area" — team area or domain
- "team" — team name or identifier
- "branch" — git branch name
- "merge_request" — MR/PR link
- "release_notes" — release notes text
- "planned_start" — planned start date
- "planned_end" — planned end date
- "hold_reason" — reason for hold/block
- "flagged" — flagged/blocked indicator
- "region" — geographic region
- "discipline" — engineering discipline
- "custom" — useful but doesn't fit other roles (include in context)
- "skip" — not useful for THIS board despite global classification

Only include fields that are actually valuable for understanding this board's workflow and team dynamics.
Consider the sample values to determine the correct role.`

	dataJSON, _ := json.Marshal(map[string]interface{}{
		"board":                board.Name,
		"project":              board.ProjectKey,
		"board_type":           board.BoardType,
		"total_issues_sampled": len(resp.Issues),
		"fields":               samples,
	})
	userMessage := fmt.Sprintf("Assign roles to these custom fields for board %q:\n\n%s",
		board.Name, string(dataJSON))

	response, mapUsage, err := fd.aiProvider.QuerySync(ctx, systemPrompt, userMessage, "")
	fd.addUsage(mapUsage)
	if err != nil {
		return nil, fmt.Errorf("LLM field mapping: %w", err)
	}

	response = extractJSON(response)

	type mappingResult struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	var results []mappingResult
	if err := json.Unmarshal([]byte(response), &results); err != nil {
		return nil, fmt.Errorf("parsing LLM field mapping: %w", err)
	}

	// 6. Save to DB
	var mappings []db.JiraBoardFieldMap
	for _, r := range results {
		if r.Role == "skip" || r.Role == "" {
			continue
		}
		mappings = append(mappings, db.JiraBoardFieldMap{
			BoardID: board.ID,
			FieldID: r.ID,
			Role:    r.Role,
		})
	}

	if err := fd.db.UpsertJiraBoardFieldMap(board.ID, mappings); err != nil {
		return nil, fmt.Errorf("saving field mappings: %w", err)
	}

	fd.logger.Printf("mapped %d fields for board %d (%s)", len(mappings), board.ID, board.Name)
	return mappings, nil
}
