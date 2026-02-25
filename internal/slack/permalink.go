package slack

import (
	"fmt"
	"strings"
)

// GeneratePermalink builds a Slack message permalink URL.
// Format: https://{domain}.slack.com/archives/{channelID}/p{ts_without_dot}
func GeneratePermalink(domain, channelID, ts string) string {
	tsNoDot := strings.Replace(ts, ".", "", 1)
	return fmt.Sprintf("https://%s.slack.com/archives/%s/p%s", domain, channelID, tsNoDot)
}
