package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		a, b     string
		minScore float64
		maxScore float64
	}{
		{"John Doe", "John Doe", 1.0, 1.0},
		{"john doe", "John Doe", 1.0, 1.0},         // case insensitive
		{"  John Doe  ", "John Doe", 1.0, 1.0},     // trim
		{"John Doe", "Jon Doe", 0.8, 1.0},          // close match
		{"John Doe", "Jane Smith", 0.0, 0.5},       // different
		{"", "", 1.0, 1.0},                         // both empty
		{"Alice", "", 0.0, 0.01},                   // one empty
		{"John", "John Doe", 0.4, 0.7},             // partial
		{"Vadim Trunov", "Vadim Trunov", 1.0, 1.0}, // exact
		{"Vadim Trunov", "vadim trunov", 1.0, 1.0}, // case
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			score := fuzzyMatch(tt.a, tt.b)
			assert.GreaterOrEqual(t, score, tt.minScore, "score %f < min %f", score, tt.minScore)
			assert.LessOrEqual(t, score, tt.maxScore, "score %f > max %f", score, tt.maxScore)
		})
	}
}

func TestFuzzyMatch_Symmetry(t *testing.T) {
	score1 := fuzzyMatch("Alice", "Alicee")
	score2 := fuzzyMatch("Alicee", "Alice")
	assert.InDelta(t, score1, score2, 0.001)
}
