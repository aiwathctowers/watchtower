package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple word",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "multiple words",
			input:    "hello world",
			expected: `"hello" "world"`,
		},
		{
			name:     "strips operators",
			input:    "hello AND world OR NOT something",
			expected: `"hello" "world" "something"`,
		},
		{
			name:     "strips quotes",
			input:    `"hello" "world"`,
			expected: `"hello" "world"`,
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "only operators",
			input:    "AND OR NOT NEAR",
			expected: "",
		},
		{
			name:     "mixed case operators",
			input:    "hello and world",
			expected: `"hello" "world"`,
		},
		{
			name:     "operators case insensitive",
			input:    "hello Or world Not bad",
			expected: `"hello" "world" "bad"`,
		},
		{
			name:     "special characters preserved",
			input:    "hello-world test.ing",
			expected: `"hello-world" "test.ing"`,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeFTS5Query(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSearchMessages_InjectionPrevention(t *testing.T) {
	db := openTestDB(t)

	// Seed a message
	seedSearchMessages(t, db)

	// Try FTS5 injection via quotes
	results, err := db.SearchMessages(`"deployment" OR 1=1 --`, SearchOpts{})
	assert.NoError(t, err)
	// Should not crash; results may or may not be empty
	_ = results
}
