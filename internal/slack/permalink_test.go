package slack

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeneratePermalink(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		channelID string
		ts        string
		want      string
	}{
		{
			name:      "standard message",
			domain:    "mycompany",
			channelID: "C024BE91L",
			ts:        "1234567890.123456",
			want:      "https://mycompany.slack.com/archives/C024BE91L/p1234567890123456",
		},
		{
			name:      "timestamp with trailing zeros",
			domain:    "acme",
			channelID: "C0001",
			ts:        "1700000000.000000",
			want:      "https://acme.slack.com/archives/C0001/p1700000000000000",
		},
		{
			name:      "dm channel",
			domain:    "team",
			channelID: "D0123ABC",
			ts:        "1609459200.001200",
			want:      "https://team.slack.com/archives/D0123ABC/p1609459200001200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GeneratePermalink(tt.domain, tt.channelID, tt.ts)
			assert.Equal(t, tt.want, got)
		})
	}
}
