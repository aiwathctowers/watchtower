package ai

import (
	"fmt"
	"strings"
	"time"

	"watchtower/internal/db"
)

// tokenRatio is the heuristic for estimating tokens: 1 token ≈ 4 characters.
const tokenRatio = 4

// ContextBuilder assembles message context from the database for a parsed query,
// staying within a configurable token budget.
type ContextBuilder struct {
	db     *db.DB
	budget int    // total token budget
	domain string // workspace domain for permalinks
}

// NewContextBuilder creates a ContextBuilder.
func NewContextBuilder(database *db.DB, contextBudget int, domain string) *ContextBuilder {
	if contextBudget <= 0 {
		contextBudget = 150000
	}
	return &ContextBuilder{
		db:     database,
		budget: contextBudget,
		domain: domain,
	}
}

// Build assembles a context string from the database based on the parsed query.
// The context is divided into tiers:
//   - Workspace summary (~1K tokens)
//   - Priority context (40%) — watched channels/users
//   - Relevant context (50%) — query-matched messages
//   - Broad context (10%) — activity overview
func (cb *ContextBuilder) Build(query ParsedQuery) (string, error) {
	// Reserve ~2K tokens for system prompt (handled externally), ~1K for summary.
	summaryBudget := 1000
	remaining := cb.budget - 2000 - summaryBudget
	if remaining < 0 {
		remaining = cb.budget / 2
	}

	priorityBudget := remaining * 40 / 100
	relevantBudget := remaining * 50 / 100
	broadBudget := remaining * 10 / 100

	var sections []string

	// 1. Workspace summary
	summary, err := cb.buildWorkspaceSummary(summaryBudget)
	if err != nil {
		return "", fmt.Errorf("building workspace summary: %w", err)
	}
	if summary != "" {
		sections = append(sections, summary)
	}

	// 2. Priority context — watched channels and users
	priority, err := cb.buildPriorityContext(query, priorityBudget)
	if err != nil {
		return "", fmt.Errorf("building priority context: %w", err)
	}
	if priority != "" {
		sections = append(sections, priority)
	}

	// 3. Relevant context — query-specific messages
	relevant, err := cb.buildRelevantContext(query, relevantBudget)
	if err != nil {
		return "", fmt.Errorf("building relevant context: %w", err)
	}
	if relevant != "" {
		sections = append(sections, relevant)
	}

	// 4. Broad context — activity summary
	broad, err := cb.buildBroadContext(query, broadBudget)
	if err != nil {
		return "", fmt.Errorf("building broad context: %w", err)
	}
	if broad != "" {
		sections = append(sections, broad)
	}

	return strings.Join(sections, "\n\n"), nil
}

// buildWorkspaceSummary creates a brief overview of the workspace.
func (cb *ContextBuilder) buildWorkspaceSummary(budget int) (string, error) {
	ws, err := cb.db.GetWorkspace()
	if err != nil {
		return "", err
	}

	stats, err := cb.db.GetStats()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("=== Workspace Summary ===\n")

	if ws != nil {
		b.WriteString(fmt.Sprintf("Workspace: %s (domain: %s)\n", ws.Name, ws.Domain))
	}
	b.WriteString(fmt.Sprintf("Channels: %d | Users: %d | Messages: %d | Threads: %d\n",
		stats.ChannelCount, stats.UserCount, stats.MessageCount, stats.ThreadCount))

	// Watch list info
	watchList, err := cb.db.GetWatchList()
	if err != nil {
		return "", err
	}
	if len(watchList) > 0 {
		var watchedChannels, watchedUsers []string
		for _, w := range watchList {
			if w.EntityType == "channel" {
				label := w.EntityName
				if w.Priority == "high" {
					label += " [high]"
				}
				watchedChannels = append(watchedChannels, label)
			} else if w.EntityType == "user" {
				label := w.EntityName
				if w.Priority == "high" {
					label += " [high]"
				}
				watchedUsers = append(watchedUsers, label)
			}
		}
		if len(watchedChannels) > 0 {
			b.WriteString(fmt.Sprintf("Watched channels: %s\n", strings.Join(watchedChannels, ", ")))
		}
		if len(watchedUsers) > 0 {
			b.WriteString(fmt.Sprintf("Watched users: %s\n", strings.Join(watchedUsers, ", ")))
		}
	}

	return truncateToTokens(b.String(), budget), nil
}

// buildPriorityContext fetches recent messages from high-priority watched entities.
func (cb *ContextBuilder) buildPriorityContext(query ParsedQuery, budget int) (string, error) {
	watchList, err := cb.db.GetWatchList()
	if err != nil {
		return "", err
	}
	if len(watchList) == 0 {
		return "", nil
	}

	// Determine time range for priority messages
	from, to := cb.effectiveTimeRange(query)

	var b strings.Builder
	b.WriteString("=== Priority Context (Watched) ===\n")
	tokensUsed := estimateTokens(b.String())

	// Process watched channels first (high priority before normal)
	for _, w := range watchList {
		if tokensUsed >= budget {
			break
		}
		if w.EntityType == "channel" {
			section, err := cb.formatChannelMessages(w.EntityID, w.EntityName, from, to, budget-tokensUsed)
			if err != nil {
				continue
			}
			if section != "" {
				b.WriteString(section)
				tokensUsed += estimateTokens(section)
			}
		}
	}

	// Process watched users
	for _, w := range watchList {
		if tokensUsed >= budget {
			break
		}
		if w.EntityType == "user" {
			section, err := cb.formatUserMessages(w.EntityID, w.EntityName, from, to, budget-tokensUsed)
			if err != nil {
				continue
			}
			if section != "" {
				b.WriteString(section)
				tokensUsed += estimateTokens(section)
			}
		}
	}

	result := b.String()
	if result == "=== Priority Context (Watched) ===\n" {
		return "", nil
	}
	return result, nil
}

// buildRelevantContext assembles context specific to the query: channel messages,
// user messages, FTS5 search results, and time-range filtered messages.
func (cb *ContextBuilder) buildRelevantContext(query ParsedQuery, budget int) (string, error) {
	var b strings.Builder
	b.WriteString("=== Relevant Context ===\n")
	tokensUsed := estimateTokens(b.String())

	seen := make(map[string]bool) // key: channelID+ts to deduplicate

	// Channel-specific messages
	if len(query.Channels) > 0 {
		channelBudget := budget / 2
		if len(query.Topics) == 0 && len(query.Users) == 0 {
			channelBudget = budget * 3 / 4
		}
		for _, chName := range query.Channels {
			if tokensUsed >= budget {
				break
			}
			ch, err := cb.db.GetChannelByName(chName)
			if err != nil || ch == nil {
				continue
			}
			from, to := cb.effectiveTimeRange(query)
			section, msgKeys, err := cb.formatChannelMessagesDedup(ch.ID, ch.Name, from, to, channelBudget-tokensUsed+estimateTokens(b.String()), seen)
			if err != nil {
				continue
			}
			if section != "" {
				b.WriteString(section)
				tokensUsed += estimateTokens(section)
				for _, k := range msgKeys {
					seen[k] = true
				}
			}
		}
	}

	// User-specific messages
	if len(query.Users) > 0 {
		userBudget := budget / 4
		if len(query.Channels) == 0 && len(query.Topics) == 0 {
			userBudget = budget * 3 / 4
		}
		for _, userName := range query.Users {
			if tokensUsed >= budget {
				break
			}
			u, err := cb.db.GetUserByName(userName)
			if err != nil || u == nil {
				continue
			}
			from, to := cb.effectiveTimeRange(query)
			section, msgKeys, err := cb.formatUserMessagesDedup(u.ID, u.Name, from, to, userBudget, seen)
			if err != nil {
				continue
			}
			if section != "" {
				b.WriteString(section)
				tokensUsed += estimateTokens(section)
				for _, k := range msgKeys {
					seen[k] = true
				}
			}
		}
	}

	// FTS5 search results
	if len(query.Topics) > 0 {
		searchBudget := budget / 4
		if len(query.Channels) == 0 && len(query.Users) == 0 {
			searchBudget = budget * 3 / 4
		}
		ftsQuery := strings.Join(query.Topics, " ")
		from, to := cb.effectiveTimeRange(query)

		// Resolve channel names to IDs for search filtering
		var channelIDs []string
		for _, chName := range query.Channels {
			ch, err := cb.db.GetChannelByName(chName)
			if err == nil && ch != nil {
				channelIDs = append(channelIDs, ch.ID)
			}
		}
		var userIDs []string
		for _, userName := range query.Users {
			u, err := cb.db.GetUserByName(userName)
			if err == nil && u != nil {
				userIDs = append(userIDs, u.ID)
			}
		}

		results, err := cb.db.SearchMessages(ftsQuery, db.SearchOpts{
			ChannelIDs: channelIDs,
			UserIDs:    userIDs,
			FromUnix:   from,
			ToUnix:     to,
			Limit:      100,
		})
		if err == nil && len(results) > 0 {
			section := cb.formatSearchResults(results, searchBudget, seen)
			if section != "" {
				b.WriteString(section)
				tokensUsed += estimateTokens(section)
			}
		}
	}

	result := b.String()
	if result == "=== Relevant Context ===\n" {
		return "", nil
	}
	return result, nil
}

// buildBroadContext creates an activity overview across all channels.
func (cb *ContextBuilder) buildBroadContext(query ParsedQuery, budget int) (string, error) {
	var b strings.Builder
	b.WriteString("=== Activity Overview ===\n")

	from, to := cb.effectiveTimeRange(query)

	// Get all channels and find ones with recent activity
	channels, err := cb.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		return "", err
	}

	type channelActivity struct {
		name     string
		id       string
		msgCount int
	}

	var activities []channelActivity
	for _, ch := range channels {
		if ch.IsArchived {
			continue
		}
		msgs, err := cb.db.GetMessagesByTimeRange(ch.ID, from, to)
		if err != nil {
			continue
		}
		if len(msgs) > 0 {
			activities = append(activities, channelActivity{
				name:     ch.Name,
				id:       ch.ID,
				msgCount: len(msgs),
			})
		}
	}

	// Sort by message count descending (simple selection sort for small slice)
	for i := 0; i < len(activities); i++ {
		maxIdx := i
		for j := i + 1; j < len(activities); j++ {
			if activities[j].msgCount > activities[maxIdx].msgCount {
				maxIdx = j
			}
		}
		activities[i], activities[maxIdx] = activities[maxIdx], activities[i]
	}

	tokensUsed := estimateTokens(b.String())

	if len(activities) > 0 {
		b.WriteString("Active channels (by message count):\n")
		limit := 10
		if len(activities) < limit {
			limit = len(activities)
		}
		for i := 0; i < limit; i++ {
			line := fmt.Sprintf("  #%s: %d messages\n", activities[i].name, activities[i].msgCount)
			if tokensUsed+estimateTokens(line) > budget {
				break
			}
			b.WriteString(line)
			tokensUsed += estimateTokens(line)
		}
	}

	// Top active users in the time range
	userActivity := make(map[string]int) // userID -> count
	for _, a := range activities {
		msgs, err := cb.db.GetMessagesByTimeRange(a.id, from, to)
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			if msg.UserID != "" {
				userActivity[msg.UserID]++
			}
		}
	}

	if len(userActivity) > 0 {
		// Find top users
		type userCount struct {
			id    string
			count int
		}
		var topUsers []userCount
		for id, count := range userActivity {
			topUsers = append(topUsers, userCount{id, count})
		}
		for i := 0; i < len(topUsers); i++ {
			maxIdx := i
			for j := i + 1; j < len(topUsers); j++ {
				if topUsers[j].count > topUsers[maxIdx].count {
					maxIdx = j
				}
			}
			topUsers[i], topUsers[maxIdx] = topUsers[maxIdx], topUsers[i]
		}

		b.WriteString("Top active users:\n")
		limit := 5
		if len(topUsers) < limit {
			limit = len(topUsers)
		}
		for i := 0; i < limit; i++ {
			u, err := cb.db.GetUserByID(topUsers[i].id)
			name := topUsers[i].id
			if err == nil && u != nil {
				name = u.Name
				if u.DisplayName != "" {
					name = u.DisplayName
				}
			}
			line := fmt.Sprintf("  @%s: %d messages\n", name, topUsers[i].count)
			if tokensUsed+estimateTokens(line) > budget {
				break
			}
			b.WriteString(line)
			tokensUsed += estimateTokens(line)
		}
	}

	result := b.String()
	if result == "=== Activity Overview ===\n" {
		return "", nil
	}
	return result, nil
}

// formatChannelMessages formats recent messages from a channel within the token budget.
func (cb *ContextBuilder) formatChannelMessages(channelID, channelName string, from, to float64, budget int) (string, error) {
	msgs, err := cb.db.GetMessagesByTimeRange(channelID, from, to)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n--- #%s ---\n", channelName))
	tokensUsed := estimateTokens(b.String())

	for _, msg := range msgs {
		line := cb.formatMessage(channelName, msg)
		lineTokens := estimateTokens(line)
		if tokensUsed+lineTokens > budget {
			break
		}
		b.WriteString(line)
		tokensUsed += lineTokens

		// Include thread summary if the message has replies
		if msg.ReplyCount > 0 {
			threadSummary, err := cb.formatThreadSummary(channelID, msg.TS, channelName, budget-tokensUsed)
			if err == nil && threadSummary != "" {
				b.WriteString(threadSummary)
				tokensUsed += estimateTokens(threadSummary)
			}
		}
	}

	return b.String(), nil
}

// formatChannelMessagesDedup is like formatChannelMessages but tracks and skips duplicates.
func (cb *ContextBuilder) formatChannelMessagesDedup(channelID, channelName string, from, to float64, budget int, seen map[string]bool) (string, []string, error) {
	msgs, err := cb.db.GetMessagesByTimeRange(channelID, from, to)
	if err != nil {
		return "", nil, err
	}
	if len(msgs) == 0 {
		return "", nil, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n--- #%s ---\n", channelName))
	tokensUsed := estimateTokens(b.String())
	var keys []string

	for _, msg := range msgs {
		key := msg.ChannelID + "|" + msg.TS
		if seen[key] {
			continue
		}
		line := cb.formatMessage(channelName, msg)
		lineTokens := estimateTokens(line)
		if tokensUsed+lineTokens > budget {
			break
		}
		b.WriteString(line)
		tokensUsed += lineTokens
		keys = append(keys, key)

		if msg.ReplyCount > 0 {
			threadSummary, err := cb.formatThreadSummary(channelID, msg.TS, channelName, budget-tokensUsed)
			if err == nil && threadSummary != "" {
				b.WriteString(threadSummary)
				tokensUsed += estimateTokens(threadSummary)
			}
		}
	}

	if len(keys) == 0 {
		return "", nil, nil
	}
	return b.String(), keys, nil
}

// formatUserMessages formats recent messages from a specific user.
func (cb *ContextBuilder) formatUserMessages(userID, userName string, from, to float64, budget int) (string, error) {
	msgs, err := cb.db.GetMessages(db.MessageOpts{
		UserID: userID,
		Limit:  200,
	})
	if err != nil {
		return "", err
	}

	// Filter by time range
	var filtered []db.Message
	for _, msg := range msgs {
		if msg.TSUnix >= from && msg.TSUnix <= to {
			filtered = append(filtered, msg)
		}
	}
	if len(filtered) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n--- Messages from @%s ---\n", userName))
	tokensUsed := estimateTokens(b.String())

	for _, msg := range filtered {
		chName := cb.resolveChannelName(msg.ChannelID)
		line := cb.formatMessage(chName, msg)
		lineTokens := estimateTokens(line)
		if tokensUsed+lineTokens > budget {
			break
		}
		b.WriteString(line)
		tokensUsed += lineTokens
	}

	return b.String(), nil
}

// formatUserMessagesDedup is like formatUserMessages but tracks and skips duplicates.
func (cb *ContextBuilder) formatUserMessagesDedup(userID, userName string, from, to float64, budget int, seen map[string]bool) (string, []string, error) {
	msgs, err := cb.db.GetMessages(db.MessageOpts{
		UserID: userID,
		Limit:  200,
	})
	if err != nil {
		return "", nil, err
	}

	var filtered []db.Message
	for _, msg := range msgs {
		if msg.TSUnix >= from && msg.TSUnix <= to {
			filtered = append(filtered, msg)
		}
	}
	if len(filtered) == 0 {
		return "", nil, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n--- Messages from @%s ---\n", userName))
	tokensUsed := estimateTokens(b.String())
	var keys []string

	for _, msg := range filtered {
		key := msg.ChannelID + "|" + msg.TS
		if seen[key] {
			continue
		}
		chName := cb.resolveChannelName(msg.ChannelID)
		line := cb.formatMessage(chName, msg)
		lineTokens := estimateTokens(line)
		if tokensUsed+lineTokens > budget {
			break
		}
		b.WriteString(line)
		tokensUsed += lineTokens
		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return "", nil, nil
	}
	return b.String(), keys, nil
}

// formatSearchResults formats FTS5 search results within the budget, skipping seen messages.
func (cb *ContextBuilder) formatSearchResults(msgs []db.Message, budget int, seen map[string]bool) string {
	var b strings.Builder
	b.WriteString("\n--- Search Results ---\n")
	tokensUsed := estimateTokens(b.String())

	for _, msg := range msgs {
		key := msg.ChannelID + "|" + msg.TS
		if seen[key] {
			continue
		}
		chName := cb.resolveChannelName(msg.ChannelID)
		line := cb.formatMessage(chName, msg)
		lineTokens := estimateTokens(line)
		if tokensUsed+lineTokens > budget {
			break
		}
		b.WriteString(line)
		tokensUsed += lineTokens
		seen[key] = true
	}

	result := b.String()
	if result == "\n--- Search Results ---\n" {
		return ""
	}
	return result
}

// formatMessage formats a single message in the compact format:
// #channel | 2025-02-24 14:30 | @user (Display Name): message text
func (cb *ContextBuilder) formatMessage(channelName string, msg db.Message) string {
	t := time.Unix(int64(msg.TSUnix), 0).UTC()
	timeStr := t.Format("2006-01-02 15:04")

	userName, displayName := cb.resolveUser(msg.UserID)
	var userLabel string
	if displayName != "" && displayName != userName {
		userLabel = fmt.Sprintf("@%s (%s)", userName, displayName)
	} else {
		userLabel = fmt.Sprintf("@%s", userName)
	}

	text := msg.Text
	if len(text) > 500 {
		text = text[:497] + "..."
	}

	return fmt.Sprintf("#%s | %s | %s: %s\n", channelName, timeStr, userLabel, text)
}

// formatThreadSummary adds a brief summary of thread replies.
func (cb *ContextBuilder) formatThreadSummary(channelID, threadTS, channelName string, budget int) (string, error) {
	replies, err := cb.db.GetThreadReplies(channelID, threadTS)
	if err != nil {
		return "", err
	}

	// Skip the parent message (first reply is the parent if ts == threadTS)
	var threadReplies []db.Message
	for _, r := range replies {
		if r.TS != threadTS {
			threadReplies = append(threadReplies, r)
		}
	}
	if len(threadReplies) == 0 {
		return "", nil
	}

	var b strings.Builder

	// Latest reply time
	latest := threadReplies[len(threadReplies)-1]
	latestTime := time.Unix(int64(latest.TSUnix), 0).UTC().Format("15:04")
	b.WriteString(fmt.Sprintf("  [%d replies, latest: %s]\n", len(threadReplies), latestTime))

	tokensUsed := estimateTokens(b.String())

	// Show up to 3 replies
	limit := 3
	if len(threadReplies) < limit {
		limit = len(threadReplies)
	}
	for i := 0; i < limit; i++ {
		reply := threadReplies[i]
		userName, _ := cb.resolveUser(reply.UserID)
		text := reply.Text
		if len(text) > 200 {
			text = text[:197] + "..."
		}
		line := fmt.Sprintf("    > @%s: %s\n", userName, text)
		if tokensUsed+estimateTokens(line) > budget {
			break
		}
		b.WriteString(line)
		tokensUsed += estimateTokens(line)
	}

	return b.String(), nil
}

// effectiveTimeRange returns the unix timestamps for the query's time range.
// Falls back to last 24 hours if no time range is specified.
func (cb *ContextBuilder) effectiveTimeRange(query ParsedQuery) (from, to float64) {
	if query.TimeRange != nil {
		return float64(query.TimeRange.From.Unix()), float64(query.TimeRange.To.Unix())
	}
	now := time.Now()
	return float64(now.Add(-24 * time.Hour).Unix()), float64(now.Unix())
}

// resolveChannelName looks up a channel name from its ID.
func (cb *ContextBuilder) resolveChannelName(channelID string) string {
	ch, err := cb.db.GetChannelByID(channelID)
	if err != nil || ch == nil {
		return channelID
	}
	return ch.Name
}

// resolveUser returns the username and display name for a user ID.
func (cb *ContextBuilder) resolveUser(userID string) (name, displayName string) {
	u, err := cb.db.GetUserByID(userID)
	if err != nil || u == nil {
		return userID, ""
	}
	name = u.Name
	if name == "" {
		name = userID
	}
	displayName = u.DisplayName
	return name, displayName
}

// estimateTokens estimates the number of tokens for a string using the 4 chars/token heuristic.
func estimateTokens(s string) int {
	return (len(s) + tokenRatio - 1) / tokenRatio
}

// truncateToTokens truncates a string to approximately fit within a token budget.
func truncateToTokens(s string, budget int) string {
	maxChars := budget * tokenRatio
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}
