package jira

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"watchtower/internal/config"
	"watchtower/internal/db"
)

// BlockerUrgency indicates the severity of a blocker.
type BlockerUrgency string

const (
	UrgencyRed    BlockerUrgency = "red"
	UrgencyYellow BlockerUrgency = "yellow"
	UrgencyGray   BlockerUrgency = "gray"
)

// PingTarget is a person to contact about a blocker.
type PingTarget struct {
	SlackUserID string `json:"slack_user_id"`
	DisplayName string `json:"display_name"`
	Reason      string `json:"reason"` // "assignee_blocker", "assignee", "expert", "reporter", "slack_participant"
}

// BlockerEntry represents a single blocked or stale issue with context.
type BlockerEntry struct {
	IssueKey        string         `json:"issue_key"`
	Summary         string         `json:"summary"`
	Status          string         `json:"status"`
	AssigneeSlackID string         `json:"assignee_slack_id"`
	AssigneeName    string         `json:"assignee_name"`
	BlockedDays     int            `json:"blocked_days"`
	BlockerType     string         `json:"blocker_type"` // "blocked" or "stale"
	BlockingChain   []string       `json:"blocking_chain"`
	DownstreamCount int            `json:"downstream_count"`
	WhoToPing       []PingTarget   `json:"who_to_ping"`
	SlackContext    string         `json:"slack_context"`
	Urgency         BlockerUrgency `json:"urgency"`
}

const maxChainDepth = 5
const maxPingTargets = 3
const snippetMaxLen = 100

// ComputeBlockerMap computes blocked and stale issues with chains and ping targets.
func ComputeBlockerMap(d *db.DB, cfg *config.Config) ([]BlockerEntry, error) {
	if !IsFeatureEnabled(cfg, "blocker_map") {
		return nil, nil
	}

	now := nowFunc()

	// Step 1: Find blocked issues (status contains "block", not done).
	blockedIssues, err := findBlockedIssues(d)
	if err != nil {
		return nil, fmt.Errorf("finding blocked issues: %w", err)
	}

	// Step 2: Find stale issues (in_progress too long), excluding already-blocked.
	blockedKeys := make(map[string]bool, len(blockedIssues))
	for _, issue := range blockedIssues {
		blockedKeys[issue.Key] = true
	}

	staleIssues, err := findStaleIssues(d, now, blockedKeys)
	if err != nil {
		return nil, fmt.Errorf("finding stale issues: %w", err)
	}

	// Collect all keys for bulk link loading.
	allKeys := make([]string, 0, len(blockedIssues)+len(staleIssues))
	for _, issue := range blockedIssues {
		allKeys = append(allKeys, issue.Key)
	}
	for _, issue := range staleIssues {
		allKeys = append(allKeys, issue.Key)
	}

	if len(allKeys) == 0 {
		return []BlockerEntry{}, nil
	}

	// Preload all links for these issues (we'll expand as needed for chains).
	allLinks, err := d.GetJiraIssueLinksByKeys(allKeys)
	if err != nil {
		return nil, fmt.Errorf("loading issue links: %w", err)
	}

	// Build link index.
	linkIndex := buildLinkIndex(allLinks)

	// Build entries for blocked issues.
	var entries []BlockerEntry
	for _, issue := range blockedIssues {
		entry := buildEntry(issue, "blocked", now)
		entry.BlockingChain = buildBlockingChain(issue.Key, linkIndex, d)
		entry.DownstreamCount = countDownstream(issue.Key, linkIndex, d)
		entry.WhoToPing = ComputeWhoToPing(d, issue, entry.BlockingChain)
		entry.SlackContext = fetchSlackContext(issue.Key, d)
		entry.Urgency = computeUrgency(entry.BlockedDays, entry.DownstreamCount)
		entries = append(entries, entry)
	}

	// Build entries for stale issues.
	for _, issue := range staleIssues {
		entry := buildEntry(issue, "stale", now)
		entry.BlockingChain = buildBlockingChain(issue.Key, linkIndex, d)
		entry.DownstreamCount = countDownstream(issue.Key, linkIndex, d)
		entry.WhoToPing = ComputeWhoToPing(d, issue, entry.BlockingChain)
		entry.SlackContext = fetchSlackContext(issue.Key, d)
		entry.Urgency = computeUrgency(entry.BlockedDays, entry.DownstreamCount)
		entries = append(entries, entry)
	}

	// Sort: downstream_count DESC, blocked_days DESC.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].DownstreamCount != entries[j].DownstreamCount {
			return entries[i].DownstreamCount > entries[j].DownstreamCount
		}
		return entries[i].BlockedDays > entries[j].BlockedDays
	})

	return entries, nil
}

// findBlockedIssues returns non-done issues whose status contains "block" (case-insensitive).
func findBlockedIssues(d *db.DB) ([]db.JiraIssue, error) {
	return d.GetBlockedJiraIssues()
}

// findStaleIssues returns in_progress issues that haven't changed status in >7 days,
// excluding keys already in the blockedKeys set.
func findStaleIssues(d *db.DB, now time.Time, blockedKeys map[string]bool) ([]db.JiraIssue, error) {
	cutoff := now.AddDate(0, 0, -7).Format(time.RFC3339)

	allStale, err := d.GetStaleJiraIssues(cutoff)
	if err != nil {
		return nil, err
	}

	// Filter out already-blocked keys.
	var issues []db.JiraIssue
	for _, issue := range allStale {
		if !blockedKeys[issue.Key] {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

// linkIndex is a helper for traversing issue links efficiently.
type linkIndex struct {
	// blockerOf maps issue key → list of keys that block it (source_key where link_type contains "block").
	blockerOf map[string][]string
	// blockedBy maps issue key → list of keys it blocks (target_key where link_type contains "block").
	blockedBy map[string][]string
}

func buildLinkIndex(links []db.JiraIssueLink) *linkIndex {
	idx := &linkIndex{
		blockerOf: make(map[string][]string),
		blockedBy: make(map[string][]string),
	}
	for _, l := range links {
		if !isBlockerLink(l.LinkType) {
			continue
		}
		// Convention: source_key blocks target_key.
		idx.blockerOf[l.TargetKey] = append(idx.blockerOf[l.TargetKey], l.SourceKey)
		idx.blockedBy[l.SourceKey] = append(idx.blockedBy[l.SourceKey], l.TargetKey)
	}
	return idx
}

func isBlockerLink(linkType string) bool {
	return strings.Contains(strings.ToLower(linkType), "block")
}

// buildBlockingChain follows blocker links from issueKey back to root cause.
// Returns a chain of issue keys: [issueKey, blocker1, blocker2, ..., rootCause].
func buildBlockingChain(issueKey string, idx *linkIndex, d *db.DB) []string {
	chain := []string{issueKey}
	visited := map[string]bool{issueKey: true}

	current := issueKey
	for depth := 0; depth < maxChainDepth; depth++ {
		blockers := idx.blockerOf[current]
		if len(blockers) == 0 {
			// Try loading from DB if not in index.
			blockers = loadBlockersFromDB(current, d, visited)
		}
		if len(blockers) == 0 {
			break
		}
		// Follow the first unvisited blocker.
		found := false
		for _, b := range blockers {
			if !visited[b] {
				visited[b] = true
				chain = append(chain, b)
				// Update index for future lookups.
				idx.blockerOf[current] = blockers
				current = b
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	return chain
}

// loadBlockersFromDB loads blocker links for a key directly from DB (for chain expansion).
func loadBlockersFromDB(key string, d *db.DB, visited map[string]bool) []string {
	links, err := d.GetJiraIssueLinksByKey(key)
	if err != nil {
		log.Printf("blockers: failed to load links for %s: %v", key, err)
		return nil
	}
	var blockers []string
	for _, l := range links {
		if !isBlockerLink(l.LinkType) {
			continue
		}
		// source_key blocks target_key; if target_key == our key, source is the blocker.
		if l.TargetKey == key && !visited[l.SourceKey] {
			blockers = append(blockers, l.SourceKey)
		}
	}
	return blockers
}

// countDownstream counts how many issues are transitively blocked by issueKey.
func countDownstream(issueKey string, idx *linkIndex, d *db.DB) int {
	visited := map[string]bool{issueKey: true}
	queue := []string{issueKey}
	count := 0

	for len(queue) > 0 && count < 100 { // safety cap
		current := queue[0]
		queue = queue[1:]

		targets := idx.blockedBy[current]
		if len(targets) == 0 {
			targets = loadDownstreamFromDB(current, d, visited)
		}

		for _, t := range targets {
			if !visited[t] {
				visited[t] = true
				count++
				if len(visited) <= maxChainDepth*10 { // don't explode
					queue = append(queue, t)
				}
			}
		}
	}

	return count
}

// loadDownstreamFromDB loads downstream (blocked-by-us) links from DB.
func loadDownstreamFromDB(key string, d *db.DB, visited map[string]bool) []string {
	links, err := d.GetJiraIssueLinksByKey(key)
	if err != nil {
		log.Printf("blockers: failed to load downstream for %s: %v", key, err)
		return nil
	}
	var targets []string
	for _, l := range links {
		if !isBlockerLink(l.LinkType) {
			continue
		}
		if l.SourceKey == key && !visited[l.TargetKey] {
			targets = append(targets, l.TargetKey)
		}
	}
	return targets
}

func buildEntry(issue db.JiraIssue, blockerType string, now time.Time) BlockerEntry {
	days := 0
	if issue.StatusCategoryChangedAt != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"} {
			if t, err := time.Parse(layout, issue.StatusCategoryChangedAt); err == nil {
				days = int(now.Sub(t).Hours() / 24)
				if days < 0 {
					days = 0
				}
				break
			}
		}
	}

	return BlockerEntry{
		IssueKey:        issue.Key,
		Summary:         issue.Summary,
		Status:          issue.Status,
		AssigneeSlackID: issue.AssigneeSlackID,
		AssigneeName:    issue.AssigneeDisplayName,
		BlockedDays:     days,
		BlockerType:     blockerType,
	}
}

// fetchSlackParticipants returns Slack users who discussed this issue.
func fetchSlackParticipants(issueKey string, d *db.DB) []PingTarget {
	links, err := d.GetJiraSlackLinksByIssue(issueKey)
	if err != nil || len(links) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []PingTarget

	for _, link := range links {
		if link.ChannelID == "" || link.MessageTS == "" {
			continue
		}
		msgs, err := d.GetMessagesByTS(link.ChannelID, []string{link.MessageTS})
		if err != nil || len(msgs) == 0 {
			continue
		}
		for _, msg := range msgs {
			if msg.UserID != "" && !seen[msg.UserID] {
				seen[msg.UserID] = true
				result = append(result, PingTarget{
					SlackUserID: msg.UserID,
					DisplayName: "", // we don't have display name from messages
					Reason:      "slack_participant",
				})
			}
		}
		if len(result) >= maxPingTargets {
			break
		}
	}

	return result
}

// fetchSlackContext returns a snippet from the latest Slack message about this issue.
func fetchSlackContext(issueKey string, d *db.DB) string {
	links, err := d.GetJiraSlackLinksByIssue(issueKey)
	if err != nil || len(links) == 0 {
		return ""
	}

	// Links are ordered by detected_at DESC, so first is latest.
	for _, link := range links {
		if link.ChannelID == "" || link.MessageTS == "" {
			continue
		}
		msgs, err := d.GetMessagesByTS(link.ChannelID, []string{link.MessageTS})
		if err != nil || len(msgs) == 0 {
			continue
		}
		text := msgs[0].Text
		if utf8.RuneCountInString(text) > snippetMaxLen {
			runes := []rune(text)
			text = string(runes[:snippetMaxLen])
		}
		return text
	}

	return ""
}

func computeUrgency(blockedDays, downstreamCount int) BlockerUrgency {
	if blockedDays > 5 || downstreamCount > 2 {
		return UrgencyRed
	}
	if blockedDays > 2 {
		return UrgencyYellow
	}
	return UrgencyGray
}
