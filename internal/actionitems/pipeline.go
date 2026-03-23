package actionitems

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// DefaultWindowHours is the default lookback period for action item extraction.
const DefaultWindowHours = 24

// DefaultWorkers is the number of parallel AI workers.
const DefaultWorkers = 10

// ProgressFunc is called during pipeline execution to report progress.
// done = channels processed, total = total channels, status = description.
type ProgressFunc func(done, total int, status string)

// aiResult is the parsed JSON response from the AI.
type aiResult struct {
	Items []aiItem `json:"items"`
}

type aiRequester struct {
	Name   string `json:"name"`
	UserID string `json:"user_id"`
}

type aiItem struct {
	ExistingID      *int            `json:"existing_id"` // non-nil → update existing item
	StatusHint      string          `json:"status_hint"` // "done", "active", or empty
	Text            string          `json:"text"`
	Context         string          `json:"context"`
	ChannelID       string          `json:"channel_id"`
	ChannelName     string          `json:"channel_name"`
	SourceMsgTS     string          `json:"source_message_ts"`
	Priority        string          `json:"priority"`
	DueDate         string          `json:"due_date"` // ISO YYYY-MM-DD or empty
	Requester       *aiRequester    `json:"requester"`
	Category        string          `json:"category"`
	Blocking        string          `json:"blocking"`
	Tags            json.RawMessage `json:"tags"`
	DecisionSummary string          `json:"decision_summary"`
	DecisionOptions json.RawMessage `json:"decision_options"`
	Participants    json.RawMessage `json:"participants"`
	SourceRefs      json.RawMessage `json:"source_refs"`
	SubItems        json.RawMessage `json:"sub_items"`
}

// Pipeline extracts and stores action items for the current user.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	OnProgress ProgressFunc

	// Accumulated token usage across all Generate calls (thread-safe).
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalCostMicro    atomic.Int64 // cost * 1e6 for atomic ops

	// caches
	channelNames map[string]string
	userNames    map[string]string
}

// New creates a new action items pipeline.
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

func (p *Pipeline) getPrompt(id, fallback string) (string, int) {
	if p.promptStore != nil {
		tmpl, version, err := p.promptStore.Get(id)
		if err == nil {
			return tmpl, version
		}
	}
	return fallback, 0
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
func (p *Pipeline) AccumulatedUsage() (int, int, float64) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()), float64(p.totalCostMicro.Load()) / 1e6
}

// ReactivateSnoozed checks and reactivates snoozed items whose snooze_until has passed.
func (p *Pipeline) ReactivateSnoozed(ctx context.Context) (int, error) {
	return p.db.ReactivateSnoozedItems()
}

// DayWindow returns day-aligned boundaries for the given time.
// from = start of today (midnight local), to = start of tomorrow.
// Using fixed boundaries prevents duplicate extraction when the daemon
// runs repeatedly within the same day — all runs share the same window.
func DayWindow(now time.Time) (from, to float64) {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// M1: use time.Date for next day instead of Add(24h) to handle DST correctly
	dayEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	return float64(dayStart.Unix()), float64(dayEnd.Unix())
}

// Run executes the action items pipeline for the current user.
// Returns the number of new action items found.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	currentUserID, err := p.db.GetCurrentUserID()
	if err != nil {
		return 0, fmt.Errorf("getting current user: %w", err)
	}
	if currentUserID == "" {
		p.logger.Println("action-items: no current user set, skipping")
		return 0, nil
	}

	from, to := DayWindow(time.Now())

	return p.RunForWindow(ctx, currentUserID, from, to)
}

// RunForWindow executes action item extraction for a specific time window and user.
func (p *Pipeline) RunForWindow(ctx context.Context, userID string, from, to float64) (int, error) {
	p.loadCaches()

	userName := p.userName(userID)

	// Get channels with messages in the window (non-DM only)
	channelMsgs, err := p.getMessagesByChannel(from, to)
	if err != nil {
		return 0, err
	}

	if len(channelMsgs) == 0 {
		p.progress(0, 0, "No messages in window")
		return 0, nil
	}

	// Delete stale open items from this window before inserting new ones
	if _, err := p.db.DeleteActionItemsForWindow(userID, from, to); err != nil {
		p.logger.Printf("action-items: warning: cleanup failed: %v", err)
	}

	total := len(channelMsgs)
	workers := DefaultWorkers
	if workers > total {
		workers = total
	}

	p.progress(0, total, fmt.Sprintf("Scanning %d channels for @%s (%d workers)...", total, userName, workers))
	p.logger.Printf("action-items: scanning %d channels with %d workers", total, workers)

	type task struct {
		channelID string
		msgs      []db.Message
	}

	taskCh := make(chan task, total)
	for chID, msgs := range channelMsgs {
		taskCh <- task{channelID: chID, msgs: msgs}
	}
	close(taskCh)

	var completed atomic.Int32
	var totalStored atomic.Int32
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				if ctx.Err() != nil {
					return
				}

				channelName := p.channelName(t.channelID)
				c := int(completed.Load())
				p.progress(c, total, fmt.Sprintf("#%s (%d messages)", channelName, len(t.msgs)))

				n, err := p.processChannel(ctx, userID, userName, t.channelID, channelName, t.msgs, from, to)
				if err != nil {
					p.logger.Printf("action-items: error processing #%s: %v", channelName, err)
				} else if n > 0 {
					totalStored.Add(int32(n)) //nolint:gosec // safe conversion within expected range
				}
				completed.Add(1)
				p.progress(int(completed.Load()), total, fmt.Sprintf("#%s done", channelName))
			}
		}()
	}

	wg.Wait()

	stored := int(totalStored.Load())
	p.progress(total, total, fmt.Sprintf("Found %d action items for @%s across %d channels", stored, userName, total))
	p.logger.Printf("action-items: %d items for @%s from %d channels", stored, userName, total)
	return stored, nil
}

// updateCheckResult is the parsed JSON response from the AI for update checks.
type updateCheckResult struct {
	HasUpdate      bool   `json:"has_update"`
	UpdatedContext string `json:"updated_context"`
	StatusHint     string `json:"status_hint"` // "done", "active", "unchanged"
}

// CheckForUpdates checks for new thread activity on existing action items.
// Returns the number of items that have new updates.
func (p *Pipeline) CheckForUpdates(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	items, err := p.db.GetActionItemsForUpdateCheck()
	if err != nil {
		return 0, fmt.Errorf("getting action items for update check: %w", err)
	}

	if len(items) == 0 {
		p.logger.Println("action-items: no items to check for updates")
		return 0, nil
	}

	p.loadCaches()

	p.logger.Printf("action-items: checking %d items for thread updates", len(items))
	p.progress(0, len(items), fmt.Sprintf("Checking %d action items for updates...", len(items)))

	// Worker pool -- lighter workload, use at most 3 workers.
	const maxWorkers = 3
	workers := maxWorkers
	if workers > len(items) {
		workers = len(items)
	}

	type task struct {
		item db.ActionItem
	}

	taskCh := make(chan task, len(items))
	for _, item := range items {
		taskCh <- task{item: item}
	}
	close(taskCh)

	var completed atomic.Int32
	var updatedCount atomic.Int32
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				if ctx.Err() != nil {
					return
				}

				updated, err := p.checkItemForUpdates(ctx, t.item)
				if err != nil {
					p.logger.Printf("action-items: error checking item %d: %v", t.item.ID, err)
				} else if updated {
					updatedCount.Add(1)
				}

				c := int(completed.Add(1))
				p.progress(c, len(items), fmt.Sprintf("Checked %d/%d items", c, len(items)))
			}
		}()
	}

	wg.Wait()

	total := int(updatedCount.Load())
	p.progress(len(items), len(items), fmt.Sprintf("Found updates for %d items", total))
	p.logger.Printf("action-items: %d items have new updates", total)
	return total, nil
}

// checkItemForUpdates checks a single action item for thread updates.
// Returns true if the item was flagged as having updates.
func (p *Pipeline) checkItemForUpdates(ctx context.Context, item db.ActionItem) (bool, error) {
	// Determine the cutoff: use last_checked_ts if available, else source_message_ts itself.
	afterTS := item.SourceMessageTS
	if item.LastCheckedTS != "" {
		afterTS = item.LastCheckedTS
	}

	// Get new thread replies after the cutoff.
	replies, err := p.db.GetThreadRepliesAfterTS(item.ChannelID, item.SourceMessageTS, afterTS)
	if err != nil {
		return false, fmt.Errorf("getting thread replies: %w", err)
	}

	// Get new channel-level messages after the cutoff (completion signals, status updates).
	channelMsgs, err := p.db.GetChannelMessagesAfterTS(item.ChannelID, afterTS, 100)
	if err != nil {
		p.logger.Printf("action-items: warning: failed to get channel messages for item %d: %v", item.ID, err)
		// Non-fatal: continue with thread replies only.
	}

	// Merge and deduplicate (thread replies + channel messages).
	allMessages := replies
	seenTS := make(map[string]bool, len(replies))
	for _, r := range replies {
		seenTS[r.TS] = true
	}
	for _, m := range channelMsgs {
		if !seenTS[m.TS] {
			allMessages = append(allMessages, m)
		}
	}

	if len(allMessages) == 0 {
		return false, nil
	}

	// Sort by timestamp.
	sort.Slice(allMessages, func(i, j int) bool { return allMessages[i].TSUnix < allMessages[j].TSUnix })

	// Format the new messages for the AI prompt.
	formatted := p.formatMessages(allMessages)
	if strings.TrimSpace(formatted) == "" {
		return false, nil
	}

	channelName := p.channelName(item.ChannelID)
	tmpl, _ := p.getPrompt(prompts.ActionItemsUpdate, updateCheckPrompt)
	prompt := fmt.Sprintf(tmpl,
		sanitize(item.Text),
		sanitize(item.Context),
		channelName,
		p.languageInstruction(),
		formatted,
	)

	raw, _, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return false, fmt.Errorf("AI generation failed: %w", err)
	}

	result, err := parseUpdateCheckResult(raw)
	if err != nil {
		return false, fmt.Errorf("parsing update check result: %w", err)
	}

	// Find the latest message TS to update last_checked_ts.
	latestTS := allMessages[len(allMessages)-1].TS

	// Update last_checked_ts regardless of whether there's an update.
	if err := p.db.UpdateLastCheckedTS(item.ID, latestTS); err != nil {
		p.logger.Printf("action-items: warning: failed to update last_checked_ts for item %d: %v", item.ID, err)
	}

	if result.HasUpdate {
		if err := p.db.SetActionItemHasUpdates(item.ID, true); err != nil {
			p.logger.Printf("action-items: warning: failed to set has_updates for item %d: %v", item.ID, err)
		}

		if result.UpdatedContext != "" {
			if err := p.db.UpdateActionItemContext(item.ID, result.UpdatedContext); err != nil {
				p.logger.Printf("action-items: warning: failed to update context for item %d: %v", item.ID, err)
			}
		}

		if result.StatusHint == "done" {
			if err := p.db.UpdateActionItemStatus(item.ID, "done"); err != nil {
				p.logger.Printf("action-items: warning: failed to mark item %d as done: %v", item.ID, err)
			} else {
				p.logger.Printf("action-items: item %d auto-completed based on channel activity", item.ID)
			}
		}

		return true, nil
	}

	return false, nil
}

func parseUpdateCheckResult(raw string) (*updateCheckResult, error) {
	cleaned := raw
	if idx := strings.Index(raw, "```json"); idx >= 0 {
		cleaned = raw[idx+7:]
		if end := strings.Index(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
	} else if idx := strings.Index(raw, "```"); idx >= 0 {
		cleaned = raw[idx+3:]
		if end := strings.Index(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
	}

	cleaned = strings.TrimSpace(cleaned)
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	var result updateCheckResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing update check JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}

// processChannel extracts action items for one channel.
func (p *Pipeline) processChannel(ctx context.Context, userID, userName, channelID, channelName string, msgs []db.Message, from, to float64) (int, error) {
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].TSUnix < msgs[j].TSUnix })

	formatted := p.formatMessages(msgs)
	if strings.TrimSpace(formatted) == "" {
		return 0, nil
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")

	// Load existing action items for this channel to help AI deduplicate.
	existingSection := p.formatExistingItems(channelID, userID)

	// Load related digest decisions for context.
	decisionsSection := p.formatDigestDecisions(channelID, from, to)

	// Load existing action items from OTHER channels for cross-channel completion detection.
	crossChannelSection := p.formatCrossChannelItems(channelID, userID)

	tmpl, _ := p.getPrompt(prompts.ActionItemsExtract, actionItemsPrompt)
	prompt := fmt.Sprintf(tmpl, userName, userID, channelName, channelID, fromStr, toStr, p.languageInstruction(), existingSection, decisionsSection, crossChannelSection, formatted)

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return 0, fmt.Errorf("AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
	}

	result, err := parseResult(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing result: %w", err)
	}

	if len(result.Items) == 0 {
		return 0, nil
	}

	// Divide token cost across items to avoid inflating totals when summed.
	var inputTokens, outputTokens int
	var costUSD float64
	if usage != nil && len(result.Items) > 0 {
		inputTokens = usage.InputTokens / len(result.Items)
		outputTokens = usage.OutputTokens / len(result.Items)
		costUSD = usage.CostUSD / float64(len(result.Items))
	}

	// Look up related digest IDs for this channel + time window.
	relatedDigestIDs := ""
	if digestIDs, err := p.db.FindRelatedDigestIDs(channelID, from, to); err == nil && len(digestIDs) > 0 {
		if b, err := json.Marshal(digestIDs); err == nil {
			relatedDigestIDs = string(b)
		}
	}

	stored := 0
	for _, item := range result.Items {
		priority := item.Priority
		if priority == "" {
			priority = "medium"
		}
		if priority != "high" && priority != "medium" && priority != "low" {
			priority = "medium"
		}

		var dueDate float64
		if item.DueDate != "" {
			if t, err := time.Parse("2006-01-02", item.DueDate); err == nil {
				dueDate = float64(t.Unix())
			}
		}

		// Serialize JSON fields as strings.
		var participants, sourceRefs, tags, decisionOptions, subItems string
		if len(item.Participants) > 0 && string(item.Participants) != "null" {
			participants = string(item.Participants)
		}
		if len(item.SourceRefs) > 0 && string(item.SourceRefs) != "null" {
			sourceRefs = string(item.SourceRefs)
		}
		if len(item.Tags) > 0 && string(item.Tags) != "null" {
			tags = string(item.Tags)
		}
		if len(item.DecisionOptions) > 0 && string(item.DecisionOptions) != "null" {
			decisionOptions = string(item.DecisionOptions)
		}
		if len(item.SubItems) > 0 && string(item.SubItems) != "null" {
			subItems = string(item.SubItems)
		}

		// Validate category.
		category := item.Category
		validCategories := map[string]bool{
			"code_review": true, "decision_needed": true, "info_request": true,
			"task": true, "approval": true, "follow_up": true,
			"bug_fix": true, "discussion": true,
		}
		if !validCategories[category] {
			category = "task"
		}

		var requesterName, requesterUserID string
		if item.Requester != nil {
			requesterName = item.Requester.Name
			requesterUserID = item.Requester.UserID
		}

		ai := db.ActionItem{
			ChannelID:         channelID,
			AssigneeUserID:    userID,
			AssigneeRaw:       "@" + userName,
			Text:              item.Text,
			Context:           item.Context,
			SourceMessageTS:   item.SourceMsgTS,
			SourceChannelName: channelName,
			Status:            "inbox",
			Priority:          priority,
			DueDate:           dueDate,
			PeriodFrom:        from,
			PeriodTo:          to,
			Model:             p.cfg.Digest.Model,
			InputTokens:       inputTokens,
			OutputTokens:      outputTokens,
			CostUSD:           costUSD,
			Participants:      participants,
			SourceRefs:        sourceRefs,
			RequesterName:     requesterName,
			RequesterUserID:   requesterUserID,
			Category:          category,
			Blocking:          item.Blocking,
			Tags:              tags,
			DecisionSummary:   item.DecisionSummary,
			DecisionOptions:   decisionOptions,
			RelatedDigestIDs:  relatedDigestIDs,
			SubItems:          subItems,
		}

		// If AI identified this as an update to an existing item, update it.
		// M2: validate that existing_id belongs to the current user before updating.
		if item.ExistingID != nil && *item.ExistingID > 0 {
			if owner, err := p.db.GetActionItemAssignee(*item.ExistingID); err != nil || owner != userID {
				p.logger.Printf("action-items: ignoring existing_id %d (owner mismatch or not found)", *item.ExistingID)
				item.ExistingID = nil
			}
		}
		if item.ExistingID != nil && *item.ExistingID > 0 {
			if _, err := p.db.UpdateActionItemFromExtraction(*item.ExistingID, ai); err != nil {
				p.logger.Printf("action-items: error updating item #%d: %v", *item.ExistingID, err)
			} else {
				stored++
			}

			// Handle status_hint: if AI detected that the item is done, mark it.
			if item.StatusHint == "done" {
				if err := p.db.SetActionItemHasUpdates(*item.ExistingID, true); err != nil {
					p.logger.Printf("action-items: warning: failed to set has_updates for item %d: %v", *item.ExistingID, err)
				}
				if err := p.db.UpdateActionItemStatus(*item.ExistingID, "done"); err != nil {
					p.logger.Printf("action-items: warning: failed to mark item %d as done: %v", *item.ExistingID, err)
				} else {
					p.logger.Printf("action-items: item #%d auto-completed based on channel activity", *item.ExistingID)
				}
			}
			continue
		}

		if _, err := p.db.UpsertActionItem(ai); err != nil {
			p.logger.Printf("action-items: error storing item: %v", err)
			continue
		}
		stored++
	}

	if stored > 0 {
		p.logger.Printf("action-items: #%s → %d items", channelName, stored)
	}
	return stored, nil
}

// getMessagesByChannel returns messages grouped by channel (excluding DMs).
func (p *Pipeline) getMessagesByChannel(from, to float64) (map[string][]db.Message, error) {
	const msgLimit = 50000
	msgs, err := p.db.GetMessages(db.MessageOpts{
		FromUnix:   from,
		ToUnix:     to,
		Limit:      msgLimit,
		ExcludeDMs: true,
	})
	if err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}
	if len(msgs) >= msgLimit {
		p.logger.Printf("action-items: warning: message limit (%d) reached, some messages may be skipped", msgLimit)
	}

	byChannel := make(map[string][]db.Message)
	for _, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		byChannel[m.ChannelID] = append(byChannel[m.ChannelID], m)
	}
	return byChannel, nil
}

// formatExistingItems loads active/inbox items for a channel and formats them
// as a prompt section for AI deduplication.
func (p *Pipeline) formatExistingItems(channelID, userID string) string {
	items, err := p.db.GetExistingActionItemsForChannel(channelID, userID)
	if err != nil {
		p.logger.Printf("action-items: warning: failed to load existing items for %s: %v", channelID, err)
		return ""
	}
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== EXISTING ACTION ITEMS FOR THIS CHANNEL ===\n")
	for _, item := range items {
		fmt.Fprintf(&sb, "#%d [%s] %q\n", item.ID, item.Status, sanitize(item.Text))
		if item.DecisionSummary != "" {
			fmt.Fprintf(&sb, "    decision: %q\n", sanitize(item.DecisionSummary))
		}
		if item.Tags != "" {
			fmt.Fprintf(&sb, "    tags: %s\n", item.Tags)
		}
		if item.RelatedDigestIDs != "" {
			fmt.Fprintf(&sb, "    digests: %s\n", item.RelatedDigestIDs)
		}
		if item.Context != "" {
			fmt.Fprintf(&sb, "    context: %s\n", sanitize(truncateStr(item.Context, 200)))
		}
	}
	return sb.String()
}

// formatCrossChannelItems loads active/inbox items from OTHER channels
// so the AI can detect cross-channel completion signals.
func (p *Pipeline) formatCrossChannelItems(excludeChannelID, userID string) string {
	items, err := p.db.GetExistingActionItemsExcludingChannel(excludeChannelID, userID)
	if err != nil {
		p.logger.Printf("action-items: warning: failed to load cross-channel items: %v", err)
		return ""
	}
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== EXISTING ACTION ITEMS FROM OTHER CHANNELS ===\n")
	sb.WriteString("If a message in this channel confirms completion of any of these items, return it with existing_id and status_hint.\n")
	for _, item := range items {
		chName := p.channelName(item.ChannelID)
		fmt.Fprintf(&sb, "#%d [%s] #%s: %q\n", item.ID, item.Status, chName, sanitize(item.Text))
		if item.Context != "" {
			fmt.Fprintf(&sb, "    context: %s\n", sanitize(truncateStr(item.Context, 150)))
		}
	}
	return sb.String()
}

// formatDigestDecisions loads recent decisions from related digests.
func (p *Pipeline) formatDigestDecisions(channelID string, from, to float64) string {
	decisions, err := p.db.GetDigestDecisionsForChannel(channelID, from, to)
	if err != nil {
		p.logger.Printf("action-items: warning: failed to load digest decisions for %s: %v", channelID, err)
		return ""
	}
	if len(decisions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== RECENT DECISIONS FROM DIGESTS ===\n")
	for _, d := range decisions {
		dateStr := time.Unix(int64(d.PeriodTo), 0).Local().Format("Jan 2")
		fmt.Fprintf(&sb, "Digest #%d (%s, #%s):\n", d.DigestID, dateStr, sanitize(d.ChannelName))
		fmt.Fprintf(&sb, "  - %s\n", sanitize(d.Decision))
	}
	return sb.String()
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func (p *Pipeline) formatMessages(msgs []db.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		userName := p.userName(m.UserID)
		ts := time.Unix(int64(m.TSUnix), 0).Local().Format("15:04")
		text := sanitize(m.Text)
		threadMarker := ""
		if m.ThreadTS.Valid {
			threadMarker = " [thread reply]"
		}
		fmt.Fprintf(&sb, "[%s] @%s (ts:%s): %s%s\n", ts, userName, m.TS, text, threadMarker)
	}
	return sb.String()
}

func (p *Pipeline) loadCaches() {
	p.channelNames = make(map[string]string)
	p.userNames = make(map[string]string)

	users, err := p.db.GetUsers(db.UserFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load user names: %v", err)
	} else {
		for _, u := range users {
			name := u.DisplayName
			if name == "" {
				name = u.Name
			}
			p.userNames[u.ID] = name
		}
	}

	channels, err := p.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load channel names: %v", err)
	} else {
		for _, ch := range channels {
			p.channelNames[ch.ID] = ch.Name
		}
	}
}

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: Write ALL text values (text, context) in %s.", lang)
	}
	return "Write in the language most commonly used in the messages"
}

func (p *Pipeline) channelName(id string) string {
	if p.channelNames != nil {
		if name, ok := p.channelNames[id]; ok {
			return sanitize(name)
		}
	}
	return id
}

func (p *Pipeline) userName(id string) string {
	if p.userNames != nil {
		if name, ok := p.userNames[id]; ok {
			return sanitize(name)
		}
	}
	return id
}

func (p *Pipeline) progress(done, total int, status string) {
	if p.OnProgress != nil {
		p.OnProgress(done, total, status)
	}
}

func sanitize(text string) string {
	// Strip newlines to prevent prompt structure injection via display names.
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	if !strings.Contains(text, "===") && !strings.Contains(text, "---") {
		return text
	}
	text = strings.ReplaceAll(text, "===", "= = =")
	text = strings.ReplaceAll(text, "---", "- - -")
	return text
}

func parseResult(raw string) (*aiResult, error) {
	cleaned := raw
	if idx := strings.Index(raw, "```json"); idx >= 0 {
		cleaned = raw[idx+7:]
		if end := strings.Index(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
	} else if idx := strings.Index(raw, "```"); idx >= 0 {
		cleaned = raw[idx+3:]
		if end := strings.Index(cleaned, "```"); end >= 0 {
			cleaned = cleaned[:end]
		}
	}

	cleaned = strings.TrimSpace(cleaned)
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	var result aiResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing action items JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}
