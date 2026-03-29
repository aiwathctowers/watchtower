// Package inbox provides detection and AI prioritization of messages awaiting user response.
package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"regexp"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

var (
	slackLinkRe     = regexp.MustCompile(`<(https?://[^|>]+)\|([^>]+)>`)
	slackURLRe      = regexp.MustCompile(`<(https?://[^>]+)>`)
	slackUserRe     = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]+))?>`)
	slackChannelRe  = regexp.MustCompile(`<#[A-Z0-9]+\|([^>]+)>`)
	slackGroupRe    = regexp.MustCompile(`<!subteam\^[A-Z0-9]+(?:\|([^>]+))?>`)
	slackSpecialRe  = regexp.MustCompile(`<!([a-z_]+)(?:\|([^>]+))?>`)
	slackEmojiRe    = regexp.MustCompile(`:[a-z0-9_+-]+:`)
	slackMarkdownRe = regexp.MustCompile("(?s)```[^`]*```")
)

// toWaitingJSON converts a list of user IDs to a JSON array string.
func toWaitingJSON(userIDs []string) string {
	if len(userIDs) == 0 {
		return ""
	}
	data, _ := json.Marshal(userIDs)
	return string(data)
}

// enrichSnippet strips Slack markup and resolves user mentions to real names.
func enrichSnippet(text string, database *db.DB) string {
	s := text
	s = slackMarkdownRe.ReplaceAllString(s, "")
	s = slackLinkRe.ReplaceAllString(s, "$2")
	s = slackURLRe.ReplaceAllString(s, "$1")
	// Resolve <@U123|Name> and <@U123> user mentions
	s = slackUserRe.ReplaceAllStringFunc(s, func(match string) string {
		groups := slackUserRe.FindStringSubmatch(match)
		// groups[1] = user ID, groups[2] = display name (may be empty)
		if groups[2] != "" {
			return "@" + groups[2]
		}
		if database != nil {
			if name, err := database.UserNameByID(groups[1]); err == nil && name != "" {
				return "@" + name
			}
		}
		return ""
	})
	// Resolve <#C123|channel-name> channel refs
	s = slackChannelRe.ReplaceAllString(s, "#$1")
	// Resolve <!subteam^S123|@team-name> group mentions
	s = slackGroupRe.ReplaceAllStringFunc(s, func(match string) string {
		groups := slackGroupRe.FindStringSubmatch(match)
		if groups[1] != "" {
			return groups[1]
		}
		return ""
	})
	// Resolve <!here|here>, <!channel|channel>, <!everyone|everyone>
	s = slackSpecialRe.ReplaceAllStringFunc(s, func(match string) string {
		groups := slackSpecialRe.FindStringSubmatch(match)
		if groups[2] != "" {
			return "@" + groups[2]
		}
		return "@" + groups[1]
	})
	s = slackEmojiRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// cleanSnippet strips Slack markup from message text for display (without DB access).
func cleanSnippet(text string) string {
	return enrichSnippet(text, nil)
}

// DefaultLookbackDays is the default lookback for first-time inbox detection.
const DefaultLookbackDays = 7

// MaxItemsPerAIBatch is the max number of items sent to AI in one call.
const MaxItemsPerAIBatch = 50

// ProgressFunc is called during pipeline execution to report progress.
type ProgressFunc func(done, total int, status string)

// Pipeline detects and prioritizes inbox items from Slack messages.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store
	OnProgress  ProgressFunc

	// Step metrics (set before each OnProgress call).
	LastStepDurationSeconds float64
	LastStepInputTokens     int
	LastStepOutputTokens    int
	LastStepCostUSD         float64

	// Accumulated usage across all AI calls.
	totalInputTokens  int
	totalOutputTokens int
	totalCostMicro    int64 // cost in micro-USD for precision
	totalAPITokens    int
}

// New creates a new inbox pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{
		db:        database,
		cfg:       cfg,
		generator: gen,
		logger:    logger,
	}
}

// SetPromptStore sets an optional prompt store for loading customized prompts.
func (p *Pipeline) SetPromptStore(store *prompts.Store) {
	p.promptStore = store
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return p.totalInputTokens, p.totalOutputTokens, float64(p.totalCostMicro) / 1e6, p.totalAPITokens
}

// Run executes the inbox pipeline: detect new items, check for auto-resolve, then AI prioritize.
// Returns (created count, resolved count, error).
func (p *Pipeline) Run(ctx context.Context) (int, int, error) {
	if p.cfg != nil && !p.cfg.Inbox.Enabled {
		return 0, 0, nil
	}

	currentUserID, err := p.db.GetCurrentUserID()
	if err != nil {
		return 0, 0, fmt.Errorf("getting current user: %w", err)
	}
	if currentUserID == "" {
		p.logger.Println("inbox: no current user set, skipping")
		return 0, 0, nil
	}

	// Get last processed timestamp.
	lastTS, err := p.db.GetInboxLastProcessedTS()
	if err != nil {
		p.logger.Printf("inbox: error getting last processed ts, using default: %v", err)
		lastTS = 0
	}
	lookbackDays := DefaultLookbackDays
	if p.cfg != nil && p.cfg.Inbox.InitialLookbackDays > 0 {
		lookbackDays = p.cfg.Inbox.InitialLookbackDays
	}
	if lastTS == 0 {
		lastTS = float64(time.Now().AddDate(0, 0, -lookbackDays).Unix())
	}

	// Phase 0: Deduplicate existing thread inbox items (cleanup from before thread-grouping).
	if deduped, err := p.db.DeduplicateThreadInboxItems(); err != nil {
		p.logger.Printf("inbox: dedup error: %v", err)
	} else if deduped > 0 {
		p.logger.Printf("inbox: merged %d duplicate thread items", deduped)
	}

	p.progress(0, 4, "Detecting messages...")

	// Phase 1: Detect — find @mentions, DMs, thread replies, reactions.
	stepStart := time.Now()
	mentions, err := p.db.FindPendingMentions(currentUserID, lastTS)
	if err != nil {
		return 0, 0, fmt.Errorf("finding mentions: %w", err)
	}

	dms, err := p.db.FindPendingDMs(currentUserID, lastTS)
	if err != nil {
		return 0, 0, fmt.Errorf("finding DMs: %w", err)
	}

	threadReplies, err := p.db.FindThreadRepliesToUser(currentUserID, lastTS)
	if err != nil {
		p.logger.Printf("inbox: error finding thread replies: %v", err)
	}

	reactions, err := p.db.FindReactionRequests(currentUserID, lastTS)
	if err != nil {
		p.logger.Printf("inbox: error finding reaction requests: %v", err)
	}

	// Insert new candidates, deduplicating by thread.
	// Multiple messages in the same thread → one inbox item (updated to latest message).
	candidates := append(mentions, dms...)
	candidates = append(candidates, threadReplies...)
	candidates = append(candidates, reactions...)

	// Group by (channel, thread): keep latest message + collect all unique senders.
	// Non-threaded messages (ThreadTS="") are grouped by channel using key (channelID, "").
	type threadKey struct{ channelID, threadTS string }
	type threadGroup struct {
		latest  db.InboxCandidate
		senders map[string]bool
	}
	threadGroups := make(map[threadKey]*threadGroup)
	for _, c := range candidates {
		key := threadKey{c.ChannelID, c.ThreadTS} // "" for non-threaded
		grp, ok := threadGroups[key]
		if !ok {
			grp = &threadGroup{latest: c, senders: map[string]bool{c.SenderUserID: true}}
			threadGroups[key] = grp
		} else {
			grp.senders[c.SenderUserID] = true
			if c.TSUnix > grp.latest.TSUnix {
				grp.latest = c
			}
		}
	}

	created := 0
	// Process all groups (threaded and non-threaded).
	for _, grp := range threadGroups {
		c := grp.latest
		snippet := enrichSnippet(c.Text, p.db)
		if snippet == "" {
			continue
		}
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		ctx := p.loadContext(c.ChannelID, c.MessageTS, c.ThreadTS)

		var senderList []string
		for uid := range grp.senders {
			senderList = append(senderList, uid)
		}
		waitingJSON := toWaitingJSON(senderList)

		// Check if there's already a pending inbox item for this thread.
		existingID, _ := p.db.FindPendingInboxByThread(c.ChannelID, c.ThreadTS)
		if existingID > 0 {
			if err := p.db.UpdateInboxItemSnippet(existingID, c.MessageTS, c.SenderUserID, snippet, ctx, c.Text, c.Permalink); err != nil {
				p.logger.Printf("inbox: error updating thread item %d: %v", existingID, err)
			}
			// Merge waiting_user_ids with existing.
			if err := p.db.MergeWaitingUserIDs(existingID, senderList); err != nil {
				p.logger.Printf("inbox: error merging waiting users for item %d: %v", existingID, err)
			}
			continue
		}

		_, err := p.db.CreateInboxItem(db.InboxItem{
			ChannelID:      c.ChannelID,
			MessageTS:      c.MessageTS,
			ThreadTS:       c.ThreadTS,
			SenderUserID:   c.SenderUserID,
			TriggerType:    c.TriggerType,
			Snippet:        snippet,
			Context:        ctx,
			RawText:        c.Text,
			Permalink:      c.Permalink,
			WaitingUserIDs: waitingJSON,
		})
		if err != nil {
			// UNIQUE constraint violation = already exists, skip.
			if strings.Contains(err.Error(), "UNIQUE") {
				continue
			}
			p.logger.Printf("inbox: error creating item: %v", err)
			continue
		}
		created++
	}

	p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
	p.progress(1, 4, fmt.Sprintf("Detected %d mentions, %d DMs, %d thread replies, %d reactions",
		len(mentions), len(dms), len(threadReplies), len(reactions)))

	// Phase 2: Check for auto-resolve — find pending items where user has replied.
	stepStart = time.Now()
	pendingItems, err := p.db.GetInboxItems(db.InboxFilter{Status: "pending"})
	if err != nil {
		return created, 0, fmt.Errorf("loading pending items: %w", err)
	}

	// Resolve replied items algorithmically — no AI needed.
	resolved := 0
	for _, item := range pendingItems {
		replied, err := p.db.CheckUserReplied(currentUserID, item.ChannelID, item.MessageTS, item.ThreadTS)
		if err != nil {
			p.logger.Printf("inbox: error checking reply for item %d: %v", item.ID, err)
			continue
		}
		if replied {
			if err := p.db.ResolveInboxItem(item.ID, "User replied"); err != nil {
				p.logger.Printf("inbox: error resolving item %d: %v", item.ID, err)
				continue
			}
			resolved++
		}
	}

	// Update last processed TS.
	nowTS := float64(time.Now().Unix())
	if err := p.db.SetInboxLastProcessedTS(nowTS); err != nil {
		p.logger.Printf("inbox: error updating last processed ts: %v", err)
	}

	p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
	p.progress(2, 4, fmt.Sprintf("Checked %d pending, %d resolved", len(pendingItems), resolved))

	// Phase 3: AI prioritize — ONLY new items that haven't been prioritized yet.
	// pendingItems was loaded after Phase 1, so freshly created items are included.
	var newItems []db.InboxItem
	for _, item := range pendingItems {
		if item.AIReason == "" {
			newItems = append(newItems, item)
		}
	}

	if len(newItems) == 0 {
		p.progress(4, 4, fmt.Sprintf("Done — %d created, %d resolved, no AI needed", created, resolved))
		return created, resolved, nil
	}

	// AI prioritizes only the new unprioritized items.
	numBatches := (len(newItems) + MaxItemsPerAIBatch - 1) / MaxItemsPerAIBatch
	if numBatches < 1 {
		numBatches = 1
	}
	total := 2 + numBatches + 1
	if p.generator != nil {
		_, err := p.aiPrioritizeNewItems(ctx, currentUserID, newItems, 2, total)
		if err != nil {
			p.logger.Printf("inbox: AI prioritize error: %v", err)
		}
	}

	p.progress(total, total, fmt.Sprintf("Done — %d created, %d resolved", created, resolved))

	return created, resolved, nil
}

// loadContext loads thread or channel context for an inbox item.
func (p *Pipeline) loadContext(channelID, messageTS, threadTS string) string {
	var msgs []struct {
		UserID string
		Text   string
	}
	var err error

	if threadTS != "" {
		msgs, err = p.db.GetThreadContext(channelID, threadTS, 10)
	} else {
		msgs, err = p.db.GetChannelContextBefore(channelID, messageTS, 5)
	}
	if err != nil || len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, m := range msgs {
		name, _ := p.db.UserNameByID(m.UserID)
		if name == "" {
			name = m.UserID
		}
		line := cleanSnippet(m.Text)
		if line == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", name, line))
	}
	result := strings.TrimSpace(sb.String())
	if len(result) > 2000 {
		result = result[:2000] + "..."
	}
	return result
}

// progress is a helper that calls OnProgress if set.
func (p *Pipeline) progress(done, total int, status string) {
	if p.OnProgress != nil {
		p.OnProgress(done, total, status)
	}
}

// formatAge computes a human-readable age from an ISO8601 CreatedAt string.
func formatAge(createdAt string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", createdAt)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// aiPrioritizeItem is the per-item result from AI.
type aiPrioritizeItem struct {
	ID       int    `json:"id"`
	Priority string `json:"priority"`
	Reason   string `json:"reason"`
	Resolved bool   `json:"resolved"`
}

// aiPrioritizeResult is the structured AI response.
type aiPrioritizeResult struct {
	Items []aiPrioritizeItem `json:"items"`
}

// aiPrioritizeNewItems runs AI prioritization only on new unprioritized items.
// baseStep is the last completed step; total is the overall step count.
func (p *Pipeline) aiPrioritizeNewItems(ctx context.Context, currentUserID string, newItems []db.InboxItem, baseStep, total int) (int, error) {
	if len(newItems) == 0 {
		return 0, nil
	}

	profile, _ := p.db.GetUserProfile(currentUserID)
	role := ""
	if profile != nil {
		role = profile.Role
	}

	promptTmpl, _ := p.getPrompt(prompts.InboxPrioritize)

	// Split into batches.
	var batches [][]db.InboxItem
	for i := 0; i < len(newItems); i += MaxItemsPerAIBatch {
		end := i + MaxItemsPerAIBatch
		if end > len(newItems) {
			end = len(newItems)
		}
		batches = append(batches, newItems[i:end])
	}

	allUpdates := make(map[int]struct {
		Priority string
		AIReason string
	})

	for i, batch := range batches {
		step := baseStep + 1 + i
		stepStart := time.Now()

		p.progress(step, total, fmt.Sprintf("AI batch %d/%d (%d items)...", i+1, len(batches), len(batch)))

		var sb strings.Builder
		sb.WriteString("=== NEW ITEMS TO PRIORITIZE ===\n")
		for _, item := range batch {
			sb.WriteString(p.formatItemLine(item))
		}

		systemPrompt := fmt.Sprintf(promptTmpl, role, sb.String())
		response, usage, _, err := p.generator.Generate(ctx, systemPrompt, "Prioritize these items.", "")
		if err != nil {
			p.logger.Printf("inbox: AI batch %d error: %v", i+1, err)
			continue
		}
		if usage != nil {
			p.totalInputTokens += usage.InputTokens
			p.totalOutputTokens += usage.OutputTokens
			p.totalCostMicro += int64(usage.CostUSD * 1e6)
			p.totalAPITokens += usage.TotalAPITokens
			p.LastStepInputTokens = usage.InputTokens
			p.LastStepOutputTokens = usage.OutputTokens
			p.LastStepCostUSD = usage.CostUSD
		}

		p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
		p.progress(step, total, fmt.Sprintf("AI batch %d/%d done", i+1, len(batches)))

		result, err := parseAIResult(response)
		if err != nil {
			p.logger.Printf("inbox: AI batch %d parse error: %v", i+1, err)
			continue
		}

		for _, item := range result.Items {
			if item.Priority != "" {
				allUpdates[item.ID] = struct {
					Priority string
					AIReason string
				}{Priority: item.Priority, AIReason: item.Reason}
			}
		}
	}

	if len(allUpdates) > 0 {
		if err := p.db.BulkUpdateInboxPriorities(allUpdates); err != nil {
			p.logger.Printf("inbox: error bulk updating priorities: %v", err)
		}
	}

	return 0, nil
}

// formatItemLine builds a rich context line for a single inbox item.
func (p *Pipeline) formatItemLine(item db.InboxItem) string {
	senderName, _ := p.db.UserNameByID(item.SenderUserID)
	channelName, _ := p.db.ChannelNameByID(item.ChannelID)
	age := formatAge(item.CreatedAt)

	// Sender role from user profile.
	senderRole := ""
	if profile, _ := p.db.GetUserProfile(item.SenderUserID); profile != nil && profile.Role != "" {
		senderRole = profile.Role
	}

	// Reply count from message.
	var replyCount int
	_ = p.db.QueryRow(`SELECT reply_count FROM messages WHERE channel_id = ? AND ts = ?`,
		item.ChannelID, item.MessageTS).Scan(&replyCount)

	senderStr := senderName
	if senderRole != "" {
		senderStr = fmt.Sprintf("%s(%s)", senderName, senderRole)
	}

	// Use raw_text if available, otherwise snippet.
	text := item.Snippet
	if item.RawText != "" {
		text = enrichSnippet(item.RawText, p.db)
		if len(text) > 500 {
			text = text[:500] + "..."
		}
	}

	line := fmt.Sprintf("- [id=%d type=%s sender=%s channel=#%s age=%s replies=%d] %s\n",
		item.ID, item.TriggerType, senderStr, channelName, age, replyCount, text)

	// Append thread context (truncated to avoid prompt bloat).
	if item.Context != "" {
		ctx := item.Context
		if len(ctx) > 500 {
			ctx = ctx[:500] + "..."
		}
		line += fmt.Sprintf("  Thread context:\n  %s\n", strings.ReplaceAll(ctx, "\n", "\n  "))
	}

	return line
}

func (p *Pipeline) getPrompt(id string) (string, int) {
	if p.promptStore != nil {
		tmpl, version, err := p.promptStore.Get(id)
		if err == nil {
			return tmpl, version
		}
	}
	return prompts.Defaults[id], 0
}

// parseAIResult parses the JSON response from the AI.
func parseAIResult(response string) (*aiPrioritizeResult, error) {
	response = strings.TrimSpace(response)
	// Strip markdown fences if present.
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		if len(lines) > 2 {
			lines = lines[1 : len(lines)-1]
			response = strings.Join(lines, "\n")
		}
	}

	var result aiPrioritizeResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("unmarshaling AI response: %w", err)
	}
	return &result, nil
}
