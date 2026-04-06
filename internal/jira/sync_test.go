package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeStatusCategory(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"new", "todo"},
		{"New", "todo"},
		{"NEW", "todo"},
		{"indeterminate", "in_progress"},
		{"Indeterminate", "in_progress"},
		{"done", "done"},
		{"Done", "done"},
		{"DONE", "done"},
		{"unknown", "unknown"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeStatusCategory(tt.input))
		})
	}
}

func TestNormalizeIssueTypeCategory(t *testing.T) {
	tests := []struct {
		level    int
		expected string
	}{
		{1, "epic"},
		{-1, "subtask"},
		{0, "standard"},
		{2, "standard"},
		{-2, "standard"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeIssueTypeCategory(tt.level))
		})
	}
}

func TestIsBug(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"Bug", true},
		{"bug", true},
		{"BUG", true},
		{"Sub-bug", true},
		{"Critical Bug", true},
		{"Task", false},
		{"Story", false},
		{"Epic", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isBug(tt.name))
		})
	}
}

func TestExtractDescriptionText(t *testing.T) {
	// nil description.
	assert.Equal(t, "", extractDescriptionText(nil))

	// String description.
	assert.Equal(t, "hello world", extractDescriptionText("hello world"))

	// ADF description.
	adf := map[string]interface{}{
		"type":    "doc",
		"version": float64(1),
		"content": []interface{}{
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "First paragraph",
					},
				},
			},
			map[string]interface{}{
				"type": "paragraph",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Second paragraph",
					},
				},
			},
		},
	}
	result := extractDescriptionText(adf)
	assert.Contains(t, result, "First paragraph")
	assert.Contains(t, result, "Second paragraph")
}
