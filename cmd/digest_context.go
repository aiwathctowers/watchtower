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
		appendTopicsFromDB(&sb, database, d.ID, d.Topics)
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

// appendTopics renders old-style flat topic names from a JSON string array.
// Retained for backward compatibility with tests and legacy digests.
func appendTopics(sb *strings.Builder, topicsJSON string) {
	var topicNames []string
	if err := json.Unmarshal([]byte(topicsJSON), &topicNames); err == nil && len(topicNames) > 0 {
		fmt.Fprintf(sb, "Topics: %s\n", strings.Join(topicNames, ", "))
	}
}

func appendTopicsFromDB(sb *strings.Builder, database *db.DB, digestID int, topicsJSON string) {
	// Try topic-structured data first
	topics, _ := database.GetDigestTopics(digestID)
	if len(topics) > 0 {
		names := make([]string, len(topics))
		for i, t := range topics {
			names[i] = t.Title
		}
		fmt.Fprintf(sb, "Topics: %s\n", strings.Join(names, ", "))
		return
	}

	// Fallback to old flat topics
	var topicNames []string
	if err := json.Unmarshal([]byte(topicsJSON), &topicNames); err == nil && len(topicNames) > 0 {
		fmt.Fprintf(sb, "Topics: %s\n", strings.Join(topicNames, ", "))
	}
}
