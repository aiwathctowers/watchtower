package jira

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// WithoutJiraWarning represents a Slack channel with active discussions
// that have no associated Jira issues linked via jira_slack_links.
type WithoutJiraWarning struct {
	TopicTitle    string `json:"topic_title"`
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	DaysDiscussed int    `json:"days_discussed"`
	MessageCount  int    `json:"message_count"`
	FirstSeen     string `json:"first_seen"` // date YYYY-MM-DD
	LastSeen      string `json:"last_seen"`  // date YYYY-MM-DD
}

// minDaysDiscussed is the minimum number of distinct digest days required
// for a channel to be flagged as "without Jira".
const minDaysDiscussed = 3

// minMessageCount is the minimum total message count across digests
// for a channel to be flagged.
const minMessageCount = 10

// DetectWithoutJira finds channels with active Slack discussions (digests)
// but no associated Jira issue links within the given time period.
//
// Logic:
//  1. Check feature toggle — if disabled, return nil.
//  2. Query channel-level digests since `since`, aggregating per channel:
//     distinct digest dates (days_discussed), total message_count,
//     first/last created_at.
//  3. Exclude channels that have any jira_slack_links in the period.
//  4. Apply thresholds: days_discussed >= 3 AND message_count >= 10.
//  5. Resolve channel names and sort by days_discussed DESC, message_count DESC.
func DetectWithoutJira(d *db.DB, cfg *config.Config, since time.Time) ([]WithoutJiraWarning, error) {
	if !IsFeatureEnabled(cfg, "without_jira") {
		return nil, nil
	}

	sinceISO := since.UTC().Format(time.RFC3339)
	sinceUnix := float64(since.Unix())

	// Step 1: Get channels with digest activity in the period.
	// We use period_from >= sinceUnix to filter digests created for the period.
	// Aggregate: distinct dates, total messages, first/last created_at.
	rows, err := d.Query(`
		SELECT
			channel_id,
			COUNT(DISTINCT date(created_at)) AS days_discussed,
			SUM(message_count)               AS total_messages,
			MIN(created_at)                   AS first_seen,
			MAX(created_at)                   AS last_seen
		FROM digests
		WHERE type = 'channel'
		  AND channel_id != ''
		  AND period_from >= ?
		GROUP BY channel_id
		HAVING days_discussed >= ? AND total_messages >= ?
	`, sinceUnix, minDaysDiscussed, minMessageCount)
	if err != nil {
		return nil, fmt.Errorf("querying digests for without-jira detection: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		channelID     string
		daysDiscussed int
		messageCount  int
		firstSeen     string
		lastSeen      string
	}

	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.channelID, &c.daysDiscussed, &c.messageCount, &c.firstSeen, &c.lastSeen); err != nil {
			return nil, fmt.Errorf("scanning digest candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating digest candidates: %w", err)
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Step 2: Batch-check which channels have jira_slack_links in the period.
	channelIDs := make([]string, len(candidates))
	for i, c := range candidates {
		channelIDs[i] = c.channelID
	}

	linkedChannels := make(map[string]bool)
	if len(channelIDs) > 0 {
		placeholders := make([]string, len(channelIDs))
		args := make([]interface{}, 0, len(channelIDs)+1)
		for i, id := range channelIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		args = append(args, sinceISO)

		linkRows, err := d.Query(`
			SELECT DISTINCT channel_id FROM jira_slack_links
			WHERE channel_id IN (`+strings.Join(placeholders, ",")+`) AND detected_at >= ?
		`, args...)
		if err != nil {
			return nil, fmt.Errorf("batch checking jira links: %w", err)
		}
		defer linkRows.Close()

		for linkRows.Next() {
			var chID string
			if err := linkRows.Scan(&chID); err != nil {
				return nil, fmt.Errorf("scanning linked channel: %w", err)
			}
			linkedChannels[chID] = true
		}
		if err := linkRows.Err(); err != nil {
			return nil, fmt.Errorf("iterating linked channels: %w", err)
		}
	}

	// Filter and resolve channel names.
	var warnings []WithoutJiraWarning
	for _, c := range candidates {
		if linkedChannels[c.channelID] {
			continue
		}

		// Resolve channel name.
		var channelName string
		err := d.QueryRow(`SELECT name FROM channels WHERE id = ?`, c.channelID).Scan(&channelName)
		if err != nil {
			channelName = c.channelID // fallback to ID if not found
		}

		// Extract date portion from ISO timestamps.
		firstDate := extractDate(c.firstSeen)
		lastDate := extractDate(c.lastSeen)

		warnings = append(warnings, WithoutJiraWarning{
			TopicTitle:    channelName, // channel name as topic proxy
			ChannelID:     c.channelID,
			ChannelName:   channelName,
			DaysDiscussed: c.daysDiscussed,
			MessageCount:  c.messageCount,
			FirstSeen:     firstDate,
			LastSeen:      lastDate,
		})
	}

	// Sort: days_discussed DESC, message_count DESC.
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].DaysDiscussed != warnings[j].DaysDiscussed {
			return warnings[i].DaysDiscussed > warnings[j].DaysDiscussed
		}
		return warnings[i].MessageCount > warnings[j].MessageCount
	})

	if len(warnings) == 0 {
		return nil, nil
	}

	return warnings, nil
}

// extractDate returns the YYYY-MM-DD portion from an ISO8601 string,
// or the original string if parsing fails.
func extractDate(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		// Try the SQLite default format without timezone.
		t, err = time.Parse("2006-01-02T15:04:05Z", iso)
		if err != nil {
			if len(iso) >= 10 {
				return iso[:10]
			}
			return iso
		}
	}
	return t.Format("2006-01-02")
}
