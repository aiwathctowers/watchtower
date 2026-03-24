package slack

import (
	"fmt"
	"strings"
)

// GeneratePermalink builds a Slack message permalink URL.
// Format: https://{domain}.slack.com/archives/{channelID}/p{ts_without_dot}
func GeneratePermalink(domain, channelID, ts string) string {
	tsNoDot := strings.ReplaceAll(ts, ".", "")
	return fmt.Sprintf("https://%s.slack.com/archives/%s/p%s", domain, channelID, tsNoDot)
}

// GenerateDeeplink builds a slack:// deep link that opens the Slack app directly.
// For channels: slack://channel?team={teamID}&id={channelID}
// For messages: slack://channel?team={teamID}&id={channelID}&message={ts}
func GenerateDeeplink(teamID, channelID, ts string) string {
	if ts == "" {
		return fmt.Sprintf("slack://channel?team=%s&id=%s", teamID, channelID)
	}
	return fmt.Sprintf("slack://channel?team=%s&id=%s&message=%s", teamID, channelID, ts)
}
