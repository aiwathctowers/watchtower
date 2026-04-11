package jira

import (
	"encoding/json"
	"testing"
	"time"

	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFallbackProfile(t *testing.T) {
	rawData := &BoardRawData{
		BoardName:  "Sprint Board",
		ProjectKey: "PROJ",
		BoardType:  "scrum",
		Config: BoardConfig{
			Columns: []BoardColumn{
				{Name: "To Do", Statuses: []BoardColumnStatus{{Name: "Open"}}},
				{Name: "In Progress", Statuses: []BoardColumnStatus{{Name: "In Progress"}}},
				{Name: "Code Review", Statuses: []BoardColumnStatus{{Name: "In Review"}}},
				{Name: "Done", Statuses: []BoardColumnStatus{{Name: "Done"}, {Name: "Closed"}}},
			},
			Estimation: &EstimationField{FieldID: "story_points", DisplayName: "Story Points"},
		},
		Sprints: []SprintSummary{
			{Name: "Sprint 1", State: "active"},
		},
	}

	profile := BuildFallbackProfile(rawData)

	assert.Len(t, profile.WorkflowStages, 4)
	assert.Equal(t, "backlog", profile.WorkflowStages[0].Phase)
	assert.Equal(t, "active_work", profile.WorkflowStages[1].Phase)
	assert.Equal(t, "active_work", profile.WorkflowStages[2].Phase)
	assert.Equal(t, "done", profile.WorkflowStages[3].Phase)
	assert.True(t, profile.WorkflowStages[3].IsTerminal)

	assert.Equal(t, "story_points", profile.EstimationApproach.Type)
	assert.True(t, profile.IterationInfo.HasIterations)

	assert.Contains(t, profile.WorkflowSummary, "scrum")
	assert.Greater(t, len(profile.StaleThresholds), 0)
}

func TestBuildFallbackProfile_NoEstimation(t *testing.T) {
	rawData := &BoardRawData{
		BoardType: "kanban",
		Config:    BoardConfig{},
	}

	profile := BuildFallbackProfile(rawData)
	assert.Equal(t, "none", profile.EstimationApproach.Type)
	assert.False(t, profile.IterationInfo.HasIterations)
}

func TestComputeConfigHash(t *testing.T) {
	rawData1 := &BoardRawData{
		Config: BoardConfig{
			Columns: []BoardColumn{
				{Name: "A", Statuses: []BoardColumnStatus{{Name: "Open"}}},
			},
		},
	}
	rawData2 := &BoardRawData{
		Config: BoardConfig{
			Columns: []BoardColumn{
				{Name: "B", Statuses: []BoardColumnStatus{{Name: "Open"}}},
			},
		},
	}

	hash1 := ComputeConfigHash(rawData1)
	hash2 := ComputeConfigHash(rawData2)
	assert.NotEqual(t, hash1, hash2, "different configs should have different hashes")

	// Same config should produce same hash.
	hash1b := ComputeConfigHash(rawData1)
	assert.Equal(t, hash1, hash1b, "same config should have same hash")
}

func TestGetEffectiveStaleThresholds(t *testing.T) {
	tests := []struct {
		name      string
		profile   string
		overrides string
		expected  map[string]int
	}{
		{
			name:     "profile only",
			profile:  `{"stale_thresholds":{"Review":3,"QA":5}}`,
			expected: map[string]int{"Review": 3, "QA": 5},
		},
		{
			name:      "with override",
			profile:   `{"stale_thresholds":{"Review":3,"QA":5}}`,
			overrides: `{"stale_thresholds":{"Review":1}}`,
			expected:  map[string]int{"Review": 1, "QA": 5},
		},
		{
			name:     "empty profile",
			expected: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			board := db.JiraBoard{
				LLMProfileJSON:    tt.profile,
				UserOverridesJSON: tt.overrides,
			}
			result, err := GetEffectiveStaleThresholds(board)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckAndRefreshProfiles_CooldownSkipsRecent(t *testing.T) {
	// Verifies that boards analyzed less than 24h ago are skipped even if config changed.
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	defer d.Close()

	// Insert a board with a profile generated 1 hour ago.
	_, err = d.Exec(`INSERT INTO jira_boards (id, name, project_key, board_type, is_selected, issue_count, synced_at,
		config_hash, profile_generated_at, llm_profile_json, raw_columns_json, raw_config_json, workflow_summary)
		VALUES (1, 'Test Board', 'TEST', 'scrum', 1, 10, '2026-04-09T00:00:00Z',
		'oldhash', ?, '{"stale_thresholds":{"In Progress":3}}', '[]', '{}', 'test')`,
		time.Now().UTC().Add(-1*time.Hour).Format(time.RFC3339))
	require.NoError(t, err)

	// Verify board is returned by GetJiraBoardProfile.
	board, err := d.GetJiraBoardProfile(1)
	require.NoError(t, err)
	require.NotNil(t, board)

	// Verify profile_generated_at is within cooldown.
	generated, err := time.Parse(time.RFC3339, board.ProfileGeneratedAt)
	require.NoError(t, err)
	assert.True(t, time.Since(generated) < RefreshCooldown, "board should be within cooldown period")
}

func TestCheckAndRefreshProfiles_CooldownElapsed(t *testing.T) {
	// Verifies that boards analyzed more than 24h ago are NOT skipped.
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	defer d.Close()

	oldTime := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)

	_, err = d.Exec(`INSERT INTO jira_boards (id, name, project_key, board_type, is_selected, issue_count, synced_at,
		config_hash, profile_generated_at, llm_profile_json, raw_columns_json, raw_config_json, workflow_summary)
		VALUES (1, 'Test Board', 'TEST', 'scrum', 1, 10, '2026-04-09T00:00:00Z',
		'oldhash', ?, '{"stale_thresholds":{"In Progress":3}}', '[]', '{}', 'test')`, oldTime)
	require.NoError(t, err)

	board, err := d.GetJiraBoardProfile(1)
	require.NoError(t, err)
	require.NotNil(t, board)

	generated, err := time.Parse(time.RFC3339, board.ProfileGeneratedAt)
	require.NoError(t, err)
	assert.True(t, time.Since(generated) >= RefreshCooldown, "board should be past cooldown period")
}

func TestMergeUserOverridesLogic(t *testing.T) {
	// Test the override merge at the data level (without DB).
	profile := &BoardProfile{
		StaleThresholds: map[string]int{
			"In Progress": 3,
			"Code Review": 5,
			"QA":          7,
		},
		WorkflowSummary: "test workflow",
	}

	overridesJSON := `{"stale_thresholds":{"Code Review":1,"Deploy":2}}`
	var overrides UserOverrides
	err := json.Unmarshal([]byte(overridesJSON), &overrides)
	require.NoError(t, err)

	// Apply overrides on top.
	for k, v := range overrides.StaleThresholds {
		profile.StaleThresholds[k] = v
	}

	assert.Equal(t, 3, profile.StaleThresholds["In Progress"], "unchanged threshold should remain")
	assert.Equal(t, 1, profile.StaleThresholds["Code Review"], "override should replace LLM value")
	assert.Equal(t, 7, profile.StaleThresholds["QA"], "unchanged threshold should remain")
	assert.Equal(t, 2, profile.StaleThresholds["Deploy"], "new override key should be added")
}

func TestGetJiraSelectedBoardsWithProfile(t *testing.T) {
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	defer d.Close()

	// Board with config_hash (has profile).
	_, err = d.Exec(`INSERT INTO jira_boards (id, name, project_key, board_type, is_selected, issue_count, synced_at,
		config_hash, profile_generated_at, llm_profile_json, raw_columns_json, raw_config_json, workflow_summary)
		VALUES (1, 'Board1', 'B1', 'scrum', 1, 10, '2026-04-09T00:00:00Z',
		'hash1', '2026-04-08T00:00:00Z', '{}', '[]', '{}', 'summary')`)
	require.NoError(t, err)

	// Board without config_hash (no profile yet).
	_, err = d.Exec(`INSERT INTO jira_boards (id, name, project_key, board_type, is_selected, issue_count, synced_at)
		VALUES (2, 'Board2', 'B2', 'kanban', 1, 5, '2026-04-09T00:00:00Z')`)
	require.NoError(t, err)

	// Board with config_hash but not selected.
	_, err = d.Exec(`INSERT INTO jira_boards (id, name, project_key, board_type, is_selected, issue_count, synced_at,
		config_hash, profile_generated_at, llm_profile_json, raw_columns_json, raw_config_json, workflow_summary)
		VALUES (3, 'Board3', 'B3', 'scrum', 0, 10, '2026-04-09T00:00:00Z',
		'hash3', '2026-04-08T00:00:00Z', '{}', '[]', '{}', 'summary')`)
	require.NoError(t, err)

	boards, err := d.GetJiraSelectedBoardsWithProfile()
	require.NoError(t, err)

	// Only board 1 should be returned (selected + has config_hash).
	require.Len(t, boards, 1)
	assert.Equal(t, 1, boards[0].ID)
	assert.Equal(t, "hash1", boards[0].ConfigHash)
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"key":"value"}`, `{"key":"value"}`},
		{"```json\n{\"key\":\"value\"}\n```", `{"key":"value"}`},
		{"```\n{\"key\":\"value\"}\n```", `{"key":"value"}`},
		{"  {\"key\":\"value\"}  ", `{"key":"value"}`},
	}

	for _, tt := range tests {
		result := extractJSON(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}
