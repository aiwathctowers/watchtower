package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"watchtower/internal/db"
)

// buildDigestContext returns a text summary of recent digests suitable for
// injecting into an AI system prompt. Returns empty string if no digests found.
func buildDigestContext(database *db.DB) string {
	dayAgo := float64(time.Now().Add(-24 * time.Hour).Unix())

	// Try daily digest first
	dailyDigests, err := database.GetDigests(db.DigestFilter{
		Type:     "daily",
		FromUnix: dayAgo,
		Limit:    1,
	})
	if err == nil && len(dailyDigests) > 0 {
		var sb strings.Builder
		d := dailyDigests[0]
		fmt.Fprintf(&sb, "Daily summary: %s\n", d.Summary)
		appendTopics(&sb, d.Topics)
		return sb.String()
	}

	// Fall back to channel digests
	channelDigests, err := database.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: dayAgo,
		Limit:    20,
	})
	if err != nil || len(channelDigests) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, d := range channelDigests {
		name := d.ChannelID
		if ch, err := database.GetChannelByID(d.ChannelID); err == nil && ch != nil {
			name = "#" + ch.Name
		}
		fmt.Fprintf(&sb, "%s (%d msgs): %s\n", name, d.MessageCount, d.Summary)
	}
	return sb.String()
}

func appendTopics(sb *strings.Builder, topicsJSON string) {
	var topics []string
	if err := json.Unmarshal([]byte(topicsJSON), &topics); err == nil && len(topics) > 0 {
		fmt.Fprintf(sb, "Topics: %s\n", strings.Join(topics, ", "))
	}
}
