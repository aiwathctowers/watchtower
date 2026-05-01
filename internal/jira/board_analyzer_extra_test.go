package jira

import (
	"testing"

	"watchtower/internal/ai"
	"watchtower/internal/db"

	"github.com/stretchr/testify/assert"
)

func TestNewBoardAnalyzer_DefaultLanguage(t *testing.T) {
	a := NewBoardAnalyzer(nil, nil, nil)
	assert.Equal(t, "English", a.language, "default language should be English")
	assert.NotNil(t, a.logger)
}

func TestBoardAnalyzer_SetLanguage(t *testing.T) {
	a := NewBoardAnalyzer(nil, nil, nil)
	a.SetLanguage("Russian")
	assert.Equal(t, "Russian", a.language)
}

func TestBoardAnalyzer_SetLanguage_IgnoresEmpty(t *testing.T) {
	a := NewBoardAnalyzer(nil, nil, nil)
	a.SetLanguage("")
	assert.Equal(t, "English", a.language, "empty string should not change language")
}

func TestBoardAnalyzer_AccumulatedUsage_Empty(t *testing.T) {
	a := NewBoardAnalyzer(nil, nil, nil)
	in, out, total := a.AccumulatedUsage()
	assert.Equal(t, 0, in)
	assert.Equal(t, 0, out)
	assert.Equal(t, 0, total)
}

func TestBoardAnalyzer_AddUsage_Accumulates(t *testing.T) {
	a := NewBoardAnalyzer(nil, nil, nil)
	a.addUsage(&ai.Usage{InputTokens: 10, OutputTokens: 20, TotalAPITokens: 30})
	a.addUsage(&ai.Usage{InputTokens: 5, OutputTokens: 15, TotalAPITokens: 25})
	in, out, total := a.AccumulatedUsage()
	assert.Equal(t, 15, in)
	assert.Equal(t, 35, out)
	assert.Equal(t, 55, total)
}

func TestBoardAnalyzer_AddUsage_NilNoOp(t *testing.T) {
	a := NewBoardAnalyzer(nil, nil, nil)
	a.addUsage(nil) // must not panic
	in, _, _ := a.AccumulatedUsage()
	assert.Equal(t, 0, in)
}

func TestComputeConfigHash_Deterministic(t *testing.T) {
	rd := &BoardRawData{
		Config: BoardConfig{
			Columns: []BoardColumn{
				{Name: "To Do", Statuses: []BoardColumnStatus{{Name: "open"}, {Name: "backlog"}}},
				{Name: "Done", Statuses: []BoardColumnStatus{{Name: "done"}}},
			},
			Estimation: &EstimationField{FieldID: "customfield_10001"},
		},
	}
	h1 := ComputeConfigHash(rd)
	h2 := ComputeConfigHash(rd)
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64, "SHA256 hex digest is 64 characters")
}

func TestComputeConfigHash_DifferentColumnsDifferHash(t *testing.T) {
	a := &BoardRawData{
		Config: BoardConfig{Columns: []BoardColumn{
			{Name: "To Do", Statuses: []BoardColumnStatus{{Name: "open"}}},
		}},
	}
	b := &BoardRawData{
		Config: BoardConfig{Columns: []BoardColumn{
			{Name: "In Progress", Statuses: []BoardColumnStatus{{Name: "open"}}},
		}},
	}
	assert.NotEqual(t, ComputeConfigHash(a), ComputeConfigHash(b))
}

func TestComputeConfigHash_StatusOrderIndependent(t *testing.T) {
	// Statuses inside a column are sorted alphabetically before hashing,
	// so reordering them must not change the hash.
	a := &BoardRawData{
		Config: BoardConfig{Columns: []BoardColumn{
			{Name: "To Do", Statuses: []BoardColumnStatus{{Name: "open"}, {Name: "backlog"}}},
		}},
	}
	b := &BoardRawData{
		Config: BoardConfig{Columns: []BoardColumn{
			{Name: "To Do", Statuses: []BoardColumnStatus{{Name: "backlog"}, {Name: "open"}}},
		}},
	}
	assert.Equal(t, ComputeConfigHash(a), ComputeConfigHash(b))
}

func TestGetEffectiveStaleThresholds_OverrideWinsOverProfile(t *testing.T) {
	board := db.JiraBoard{
		LLMProfileJSON:    `{"stale_thresholds":{"In Progress": 5, "Review": 7}}`,
		UserOverridesJSON: `{"stale_thresholds":{"In Progress": 3}}`,
	}
	got, err := GetEffectiveStaleThresholds(board)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 3, got["In Progress"], "user override must win")
	assert.Equal(t, 7, got["Review"], "non-overridden profile entry preserved")
}

func TestGetEffectiveStaleThresholds_NoProfile(t *testing.T) {
	board := db.JiraBoard{
		UserOverridesJSON: `{"stale_thresholds":{"Done": 1}}`,
	}
	got, err := GetEffectiveStaleThresholds(board)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 1, got["Done"])
}

func TestGetEffectiveStaleThresholds_GarbageJSONReturnsEmpty(t *testing.T) {
	board := db.JiraBoard{
		LLMProfileJSON:    `not json`,
		UserOverridesJSON: `also not json`,
	}
	got, err := GetEffectiveStaleThresholds(board)
	assert.NoError(t, err)
	assert.Empty(t, got)
}

func TestExtractJSON_Plain(t *testing.T) {
	got := extractJSON(`{"a":1}`)
	assert.Equal(t, `{"a":1}`, got)
}

func TestExtractJSON_StripsJSONFence(t *testing.T) {
	got := extractJSON("```json\n{\"a\":1}\n```")
	assert.Equal(t, `{"a":1}`, got)
}

func TestExtractJSON_StripsBareFence(t *testing.T) {
	got := extractJSON("```\n{\"a\":1}\n```")
	assert.Equal(t, `{"a":1}`, got)
}

func TestExtractJSON_TrimsWhitespace(t *testing.T) {
	got := extractJSON("\n\n  {\"a\":1}  \n\n")
	assert.Equal(t, `{"a":1}`, got)
}
