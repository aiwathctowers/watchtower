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

	// closingSignals is a set of short acknowledgment/closing phrases that don't need a reply.
	closingSignals = map[string]bool{
		// EN
		"thanks": true, "thank you": true, "thx": true, "ty": true,
		"got it": true, "ok": true, "okay": true, "cool": true,
		"great": true, "perfect": true, "awesome": true,
		"np": true, "no problem": true, "will do": true,
		"sounds good": true, "noted": true, "ack": true,
		// RU
		"спасибо": true, "спс": true, "ок": true,
		"понял": true, "понятно": true, "принял": true,
		"ясно": true, "хорошо": true, "отлично": true,
		"ладно": true, "круто": true, "пон": true,
		// Emoji-only
		"👍": true, "🙏": true, "🙌": true, "👌": true, "✅": true,
	}

	trailingPunctRe = regexp.MustCompile(`[.!?,;:…]+$`)
)

// isClosingSignal returns true if the message text is a short closing/acknowledgment phrase.
func isClosingSignal(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" || len(s) > 80 {
		return false
	}
	// Strip trailing punctuation.
	s = trailingPunctRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return closingSignals[s]
}

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

	// Current user identity (set via SetCurrentUser or resolved from DB in Run).
	currentUserID    string
	currentUserEmail string

	// pinnedSelector runs an AI call to select the top-pinned items.
	pinnedSelector *PinnedSelector

	// Step metrics (set before each OnProgress call).
	LastStepDurationSeconds float64
	LastStepInputTokens     int
	LastStepOutputTokens    int
	// Accumulated usage across all AI calls.
	totalInputTokens  int
	totalOutputTokens int
	totalAPITokens    int
}

// New creates a new inbox pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{
		db:             database,
		cfg:            cfg,
		generator:      gen,
		logger:         logger,
		pinnedSelector: NewPinnedSelector(database, gen, cfg.Digest.Language),
	}
}

// SetCurrentUser sets the current user identity used by the pipeline for
// per-source detectors (Jira, Calendar) and auto-resolve logic.
func (p *Pipeline) SetCurrentUser(id, email string) {
	p.currentUserID = id
	p.currentUserEmail = email
}

// SetPromptStore sets an optional prompt store for loading customized prompts.
func (p *Pipeline) SetPromptStore(store *prompts.Store) {
	p.promptStore = store
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return p.totalInputTokens, p.totalOutputTokens, 0, p.totalAPITokens
}

// Run executes the inbox pipeline: detect new items, classify, learn, AI prioritize,
// select pinned, auto-resolve, auto-archive, then unsnooze.
// Returns (created count, resolved count, error).
func (p *Pipeline) Run(ctx context.Context) (int, int, error) {
	// Reset accumulated usage from previous run (pipeline is reused across daemon cycles).
	p.totalInputTokens = 0
	p.totalOutputTokens = 0
	p.totalAPITokens = 0

	if p.cfg != nil && !p.cfg.Inbox.Enabled {
		return 0, 0, nil
	}

	// Resolve current user: prefer explicitly set identity, fall back to DB.
	currentUserID := p.currentUserID
	if currentUserID == "" {
		var err error
		currentUserID, err = p.db.GetCurrentUserID()
		if err != nil {
			return 0, 0, fmt.Errorf("getting current user: %w", err)
		}
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
	sinceTime := time.Unix(int64(lastTS), 0)

	// Phase 0: Deduplicate existing thread inbox items (cleanup from before thread-grouping).
	if deduped, err := p.db.DeduplicateThreadInboxItems(); err != nil {
		p.logger.Printf("inbox: dedup error: %v", err)
	} else if deduped > 0 {
		p.logger.Printf("inbox: merged %d duplicate thread items", deduped)
	}

	p.progress(0, 6, "Detecting messages...")

	// Phase 1: Detection — Slack + external sources (all non-fatal).
	stepStart := time.Now()
	createdSlack, createdJira, createdCalendar, createdWatchtower := p.detectAll(ctx, currentUserID, lastTS, sinceTime, true)
	created := createdSlack + createdJira + createdCalendar + createdWatchtower

	p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
	p.progress(1, 6, fmt.Sprintf("Detected %d new items", created))

	// Phase 2: Classify new items — assign item_class based on trigger_type for any unclassified items.
	if err := p.classifyNewItems(ctx); err != nil {
		p.logger.Printf("inbox: classify error: %v", err)
	}

	// Phase 3: Implicit learning — update mute rules from dismiss patterns.
	var learnedRuleUpdates int
	if n, err := RunImplicitLearner(ctx, p.db, 30*24*time.Hour); err != nil {
		p.logger.Printf("inbox: learner error: %v", err)
	} else {
		learnedRuleUpdates = n
	}

	// Phase 4: Auto-resolve — rule-based resolution for all source types.
	stepStart = time.Now()
	resolved := p.autoResolveByRules(ctx, currentUserID)

	p.LastStepDurationSeconds = time.Since(stepStart).Seconds()

	// Reload pending items after rule-based resolution for AI prioritization.
	pendingItems, err := p.db.GetInboxItems(db.InboxFilter{Status: "pending"})
	if err != nil {
		return created, resolved, fmt.Errorf("loading pending items for AI: %w", err)
	}

	p.progress(2, 6, fmt.Sprintf("Checked pending, %d resolved", resolved))

	// Phase 4a: AI prioritize — only new unprioritized items.
	var newItems []db.InboxItem
	for _, item := range pendingItems {
		if item.AIReason == "" {
			newItems = append(newItems, item)
		}
	}

	if len(newItems) > 0 && p.generator != nil {
		numBatches := (len(newItems) + MaxItemsPerAIBatch - 1) / MaxItemsPerAIBatch
		if numBatches < 1 {
			numBatches = 1
		}
		total := 3 + numBatches + 1
		aiResolved, err := p.aiPrioritizeNewItems(ctx, currentUserID, newItems, 3, total)
		if err != nil {
			p.logger.Printf("inbox: AI prioritize error: %v", err)
		}
		resolved += aiResolved
	}

	p.progress(4, 6, "Selecting pinned items...")

	// Phase 4b: AI select pinned (separate AI call, non-fatal; skipped when no generator).
	var pinned int
	if p.pinnedSelector != nil && p.generator != nil {
		if n, err := p.pinnedSelector.Run(ctx); err != nil {
			p.logger.Printf("inbox: pinned selector error: %v", err)
		} else {
			pinned = n
		}
	}

	// Phase 5: Auto-archive expired/stale items (non-fatal).
	var archived int
	if n, err := p.db.ArchiveExpiredAmbient(7 * 24 * time.Hour); err != nil {
		p.logger.Printf("inbox: archive ambient error: %v", err)
	} else {
		archived += n
	}
	if n, err := p.db.ArchiveStaleActionable(14 * 24 * time.Hour); err != nil {
		p.logger.Printf("inbox: archive stale error: %v", err)
	} else {
		archived += n
	}

	// Phase 6: Unsnooze expired snoozed items.
	if _, err := p.db.UnsnoozeExpiredInboxItems(); err != nil {
		p.logger.Printf("inbox: unsnooze error: %v", err)
	}

	// Advance watermark.
	// Use a 30-minute buffer instead of wall-clock time to account for
	// Slack search API indexing delays — messages may arrive in the DB
	// with ts_unix values behind wall-clock time.
	bufferTS := float64(time.Now().Add(-30 * time.Minute).Unix())
	if bufferTS < lastTS {
		bufferTS = lastTS // never go backwards
	}
	if err := p.db.SetInboxLastProcessedTS(bufferTS); err != nil {
		p.logger.Printf("inbox: error updating last processed ts: %v", err)
	}

	p.progress(6, 6, fmt.Sprintf("Done — %d created, %d resolved", created, resolved))

	p.logger.Printf("inbox: +%d new (S%d J%d C%d I%d), %d pinned, %d auto-resolved, %d auto-archived, %d learned-rule-updates",
		created, createdSlack, createdJira, createdCalendar, createdWatchtower,
		pinned, resolved, archived, learnedRuleUpdates)

	return created, resolved, nil
}

// RunFastDetection runs a lightweight subset of the pipeline: dedup, Slack/Jira/
// Calendar detection, classification and rule-based auto-resolve. It skips the
// digest-dependent decision_made/briefing_ready detector, the implicit learner,
// AI prioritization, the pinned selector, archival, and the watermark advance —
// all of which the full Run still performs afterwards.
//
// This lets the daemon surface DMs/mentions in the UI immediately after a Slack
// sync, instead of waiting for the LLM-heavy digest+tracks phases to finish.
func (p *Pipeline) RunFastDetection(ctx context.Context) error {
	if p.cfg != nil && !p.cfg.Inbox.Enabled {
		return nil
	}

	currentUserID := p.currentUserID
	if currentUserID == "" {
		var err error
		currentUserID, err = p.db.GetCurrentUserID()
		if err != nil {
			return fmt.Errorf("getting current user: %w", err)
		}
	}
	if currentUserID == "" {
		return nil
	}

	lastTS, err := p.db.GetInboxLastProcessedTS()
	if err != nil {
		p.logger.Printf("inbox fast: error getting last processed ts, using default: %v", err)
		lastTS = 0
	}
	lookbackDays := DefaultLookbackDays
	if p.cfg != nil && p.cfg.Inbox.InitialLookbackDays > 0 {
		lookbackDays = p.cfg.Inbox.InitialLookbackDays
	}
	if lastTS == 0 {
		lastTS = float64(time.Now().AddDate(0, 0, -lookbackDays).Unix())
	}
	sinceTime := time.Unix(int64(lastTS), 0)

	if deduped, err := p.db.DeduplicateThreadInboxItems(); err != nil {
		p.logger.Printf("inbox fast: dedup error: %v", err)
	} else if deduped > 0 {
		p.logger.Printf("inbox fast: merged %d duplicate thread items", deduped)
	}

	createdSlack, createdJira, createdCalendar, _ := p.detectAll(ctx, currentUserID, lastTS, sinceTime, false)
	created := createdSlack + createdJira + createdCalendar

	if err := p.classifyNewItems(ctx); err != nil {
		p.logger.Printf("inbox fast: classify error: %v", err)
	}

	resolved := p.autoResolveByRules(ctx, currentUserID)

	p.logger.Printf("inbox fast: +%d new (S%d J%d C%d), %d auto-resolved",
		created, createdSlack, createdJira, createdCalendar, resolved)

	return nil
}

// detectAll runs the per-source detectors and returns counts. When
// includeWatchtower is false, the watchtower-internal detector
// (decision_made / briefing_ready, depends on digests + briefings) is skipped —
// used by RunFastDetection so it can run before the digest pipeline.
func (p *Pipeline) detectAll(ctx context.Context, currentUserID string, lastTS float64, sinceTime time.Time, includeWatchtower bool) (slack, jira, cal, wt int) {
	if n, err := p.detectSlackTriggers(ctx, currentUserID, lastTS); err != nil {
		p.logger.Printf("inbox: slack detect error: %v", err)
	} else {
		slack = n
	}
	if n, err := DetectJira(ctx, p.db, currentUserID, sinceTime); err != nil {
		p.logger.Printf("inbox: jira detect error: %v", err)
	} else {
		jira = n
	}
	if n, err := DetectCalendar(ctx, p.db, p.currentUserEmail, sinceTime); err != nil {
		p.logger.Printf("inbox: calendar detect error: %v", err)
	} else {
		cal = n
	}
	if includeWatchtower {
		if n, err := DetectWatchtowerInternal(ctx, p.db, sinceTime); err != nil {
			p.logger.Printf("inbox: watchtower detect error: %v", err)
		} else {
			wt = n
		}
	}
	return
}

// detectSlackTriggers detects @mentions, DMs, thread replies and reactions from Slack messages.
// Returns the count of newly created inbox items.
func (p *Pipeline) detectSlackTriggers(ctx context.Context, currentUserID string, lastTS float64) (int, error) {
	mentions, err := p.db.FindPendingMentions(currentUserID, lastTS)
	if err != nil {
		return 0, fmt.Errorf("finding mentions: %w", err)
	}

	dms, err := p.db.FindPendingDMs(currentUserID, lastTS)
	if err != nil {
		return 0, fmt.Errorf("finding DMs: %w", err)
	}

	threadReplies, err := p.db.FindThreadRepliesToUser(currentUserID, lastTS)
	if err != nil {
		p.logger.Printf("inbox: error finding thread replies: %v", err)
	}

	reactions, err := p.db.FindReactionRequests(currentUserID, lastTS)
	if err != nil {
		p.logger.Printf("inbox: error finding reaction requests: %v", err)
	}

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
		key := threadKey{c.ChannelID, c.ThreadTS}
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
	for _, grp := range threadGroups {
		c := grp.latest
		snippet := enrichSnippet(c.Text, p.db)
		if snippet == "" {
			continue
		}

		// Pre-filter: skip closing signals ("thanks", "ok", etc.) when user already replied before.
		if isClosingSignal(c.Text) {
			repliedBefore, _ := p.db.CheckUserRepliedBefore(currentUserID, c.ChannelID, c.MessageTS, c.ThreadTS)
			if repliedBefore {
				continue
			}
		}

		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		itemCtx := p.loadContext(c.ChannelID, c.MessageTS, c.ThreadTS)

		var senderList []string
		for uid := range grp.senders {
			senderList = append(senderList, uid)
		}
		waitingJSON := toWaitingJSON(senderList)

		existingID, _ := p.db.FindPendingInboxByThread(c.ChannelID, c.ThreadTS)
		if existingID > 0 {
			if err := p.db.UpdateInboxItemSnippet(existingID, c.MessageTS, c.SenderUserID, snippet, itemCtx, c.Text, c.Permalink); err != nil {
				p.logger.Printf("inbox: error updating thread item %d: %v", existingID, err)
			}
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
			Context:        itemCtx,
			RawText:        c.Text,
			Permalink:      c.Permalink,
			WaitingUserIDs: waitingJSON,
		})
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				continue
			}
			p.logger.Printf("inbox: error creating item: %v", err)
			continue
		}
		created++
	}

	p.logger.Printf("inbox: slack detected %d mentions, %d DMs, %d thread replies, %d reactions → %d created",
		len(mentions), len(dms), len(threadReplies), len(reactions), created)
	return created, nil
}

// classifyNewItems assigns item_class to inbox items that have an empty class,
// using DefaultItemClass based on trigger_type. Items set by detectors already
// have a class; this function acts as a backfill for any that don't.
func (p *Pipeline) classifyNewItems(_ context.Context) error {
	rows, err := p.db.Query(`SELECT id, trigger_type FROM inbox_items WHERE item_class='' OR item_class IS NULL`)
	if err != nil {
		return err
	}
	// Drain cursor before issuing updates (avoids SQLite single-connection deadlock).
	type update struct {
		id    int64
		class string
	}
	var updates []update
	for rows.Next() {
		var id int64
		var trig string
		if err := rows.Scan(&id, &trig); err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, update{id: id, class: DefaultItemClass(trig)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range updates {
		if err := p.db.SetInboxItemClass(u.id, u.class); err != nil {
			p.logger.Printf("inbox: classify item %d: %v", u.id, err)
		}
	}
	return nil
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
	allResolved := make(map[int]string)

	for i, batch := range batches {
		step := baseStep + 1 + i
		stepStart := time.Now()

		p.progress(step, total, fmt.Sprintf("AI batch %d/%d (%d items)...", i+1, len(batches), len(batch)))

		var sb strings.Builder
		sb.WriteString("=== NEW ITEMS TO PRIORITIZE ===\n")
		for _, item := range batch {
			sb.WriteString(p.formatItemLine(item))
		}

		// Prepend user preferences (learned mute/boost rules) to the items block.
		userPrefs, _ := buildUserPreferencesBlock(p.db, batch)
		itemsBlock := sb.String()
		if userPrefs != "" {
			itemsBlock = userPrefs + "\n" + itemsBlock
		}
		systemPrompt := fmt.Sprintf(promptTmpl, prompts.Directive(p.cfg.Digest.Language), role, itemsBlock)
		response, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "inbox.prioritize"), systemPrompt, "Prioritize these items.", "")
		if err != nil {
			p.logger.Printf("inbox: AI batch %d error: %v", i+1, err)
			continue
		}
		if usage != nil {
			p.totalInputTokens += usage.InputTokens
			p.totalOutputTokens += usage.OutputTokens
			p.totalAPITokens += usage.TotalAPITokens
			p.LastStepInputTokens = usage.InputTokens
			p.LastStepOutputTokens = usage.OutputTokens
		}

		p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
		p.progress(step, total, fmt.Sprintf("AI batch %d/%d done", i+1, len(batches)))

		result, err := parseAIResult(response)
		if err != nil {
			p.logger.Printf("inbox: AI batch %d parse error: %v", i+1, err)
			continue
		}

		for _, item := range result.Items {
			if item.Resolved {
				allResolved[item.ID] = item.Reason
			} else if item.Priority != "" {
				allUpdates[item.ID] = struct {
					Priority string
					AIReason string
				}{Priority: item.Priority, AIReason: item.Reason}
			}
		}
	}

	resolved := 0
	for id, reason := range allResolved {
		if err := p.db.ResolveInboxItem(id, "AI: "+reason); err != nil {
			p.logger.Printf("inbox: error AI-resolving item %d: %v", id, err)
			continue
		}
		resolved++
	}

	if len(allUpdates) > 0 {
		if err := p.db.BulkUpdateInboxPriorities(allUpdates); err != nil {
			p.logger.Printf("inbox: error bulk updating priorities: %v", err)
		}
	}

	return resolved, nil
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

// autoResolveByRules runs all rule-based auto-resolve checks across Slack,
// Jira, and Calendar sources. Returns the total number of items resolved.
func (p *Pipeline) autoResolveByRules(ctx context.Context, currentUserID string) int {
	resolved := 0
	resolved += p.autoResolveSlack(ctx, currentUserID)
	resolved += p.autoResolveJira(ctx)
	resolved += p.autoResolveCalendar(ctx)
	return resolved
}

// autoResolveSlack resolves pending Slack inbox items where the current user
// has already replied in the thread or channel.
func (p *Pipeline) autoResolveSlack(ctx context.Context, currentUserID string) int {
	items, err := p.db.GetInboxItems(db.InboxFilter{Status: "pending"})
	if err != nil {
		p.logger.Printf("inbox: autoResolveSlack: loading items: %v", err)
		return 0
	}
	resolved := 0
	for _, item := range items {
		// Only Slack-sourced items: trigger types that come from Slack messages.
		switch item.TriggerType {
		case "mention", "dm", "thread_reply", "reaction_request":
		default:
			continue
		}
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
	return resolved
}

// autoResolveJira resolves pending jira_comment_mention and jira_assigned items
// when the current user has authored a comment on the issue after the item was created.
// If the jira_comments table does not exist, this method is a no-op.
func (p *Pipeline) autoResolveJira(_ context.Context) int {
	if !jiraCommentsTableExists(p.db) {
		return 0
	}
	if p.currentUserID == "" {
		return 0
	}

	// Drain cursor before any secondary queries (SQLite single-connection deadlock).
	rows, err := p.db.Query(`SELECT id, channel_id, created_at FROM inbox_items
		WHERE trigger_type IN ('jira_comment_mention','jira_assigned') AND status='pending'`)
	if err != nil {
		p.logger.Printf("inbox: autoResolveJira: query: %v", err)
		return 0
	}
	type candidate struct {
		id        int64
		issueKey  string
		createdAt string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.issueKey, &c.createdAt); err != nil {
			rows.Close() //nolint:errcheck
			p.logger.Printf("inbox: autoResolveJira: scan: %v", err)
			return 0
		}
		candidates = append(candidates, c)
	}
	rows.Close() //nolint:errcheck

	resolved := 0
	for _, c := range candidates {
		var n int
		p.db.QueryRow(`SELECT COUNT(*) FROM jira_comments
			WHERE issue_key=? AND author_id=? AND created_at >= ?`,
			c.issueKey, p.currentUserID, c.createdAt).Scan(&n) //nolint:errcheck
		if n > 0 {
			if _, err := p.db.Exec(`UPDATE inbox_items SET status='resolved', resolved_reason='User commented on issue', updated_at=? WHERE id=?`,
				time.Now().UTC().Format(time.RFC3339), c.id); err != nil {
				p.logger.Printf("inbox: autoResolveJira: update item %d: %v", c.id, err)
				continue
			}
			resolved++
		}
	}
	return resolved
}

// autoResolveCalendar resolves pending calendar_invite and calendar_time_change
// items when the current user's RSVP status is no longer 'needsAction'.
func (p *Pipeline) autoResolveCalendar(_ context.Context) int {
	if p.currentUserEmail == "" {
		return 0
	}

	// Drain cursor before any secondary queries (SQLite single-connection deadlock).
	rows, err := p.db.Query(`SELECT id, channel_id FROM inbox_items
		WHERE trigger_type IN ('calendar_invite','calendar_time_change') AND status='pending'`)
	if err != nil {
		p.logger.Printf("inbox: autoResolveCalendar: query: %v", err)
		return 0
	}
	type candidate struct {
		id      int64
		eventID string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.eventID); err != nil {
			rows.Close() //nolint:errcheck
			p.logger.Printf("inbox: autoResolveCalendar: scan: %v", err)
			return 0
		}
		candidates = append(candidates, c)
	}
	rows.Close() //nolint:errcheck

	resolved := 0
	for _, c := range candidates {
		var att string
		p.db.QueryRow(`SELECT attendees FROM calendar_events WHERE id=?`, c.eventID).Scan(&att) //nolint:errcheck
		var list []calAttendee
		_ = json.Unmarshal([]byte(att), &list)
		for _, a := range list {
			if a.Email == p.currentUserEmail && a.RSVPStatus != "needsAction" && a.RSVPStatus != "" {
				if _, err := p.db.Exec(`UPDATE inbox_items SET status='resolved', resolved_reason='User responded to invite', updated_at=? WHERE id=?`,
					time.Now().UTC().Format(time.RFC3339), c.id); err != nil {
					p.logger.Printf("inbox: autoResolveCalendar: update item %d: %v", c.id, err)
				} else {
					resolved++
				}
				break
			}
		}
	}
	return resolved
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
