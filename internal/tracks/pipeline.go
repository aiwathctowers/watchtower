// Package tracks provides track pipeline and generation for workspace conversation grouping.
package tracks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
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

// DefaultWindowHours is the default lookback period for track extraction.
const DefaultWindowHours = 24

// DefaultWorkers is the number of parallel AI workers.
const DefaultWorkers = 3

// validCategories is the set of allowed track categories.
var validCategories = map[string]bool{
	"code_review": true, "decision_needed": true, "info_request": true,
	"task": true, "approval": true, "follow_up": true,
	"bug_fix": true, "discussion": true,
}

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
	Ownership       string          `json:"ownership"`     // "mine", "delegated", "watching"
	BallOn          string          `json:"ball_on"`       // user_id of next actor
	OwnerUserID     string          `json:"owner_user_id"` // owner of the track
	ChainID         *int            `json:"chain_id"`      // optional: link to existing chain
}

// Pipeline extracts and stores tracks for the current user.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	OnProgress ProgressFunc

	// ChainContext is injected by the daemon after chains pipeline runs.
	// If non-empty, it's appended to the extraction prompt so AI can link tracks to chains.
	ChainContext string

	// LastStep* fields are set before each OnProgress callback with the
	// current step's message count and time window. Read them in OnProgress.
	LastStepMessageCount    int
	LastStepPeriodFrom      time.Time
	LastStepPeriodTo        time.Time
	LastStepDurationSeconds float64
	LastStepInputTokens     int
	LastStepOutputTokens    int
	LastStepCostUSD         float64

	// Accumulated token usage across all Generate calls (thread-safe).
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalCostMicro    atomic.Int64 // cost * 1e6 for atomic ops
	totalAPITokens    atomic.Int64

	// caches (populated once per Run/CheckForUpdates, read by workers)
	cacheMu      sync.RWMutex
	channelNames map[string]string
	userNames    map[string]string
	profile      *db.UserProfile // loaded once per Run, nil if not available
}

// New creates a new tracks pipeline.
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

func (p *Pipeline) getPrompt(id string) (string, int) {
	role := ""
	if p.profile != nil {
		role = p.profile.Role
	}

	if p.promptStore != nil {
		tmpl, version, err := p.promptStore.GetForRole(id, role)
		if err == nil {
			// Prepend role instruction if available
			roleInstr := prompts.GetRoleInstruction(role)
			if roleInstr != "" {
				tmpl = roleInstr + "\n\n" + tmpl
			}
			return tmpl, version
		}
	}

	// Fallback to default
	tmpl := prompts.Defaults[id]
	roleInstr := prompts.GetRoleInstruction(role)
	if roleInstr != "" {
		tmpl = roleInstr + "\n\n" + tmpl
	}
	return tmpl, 0
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
// Returns (inputTokens, outputTokens, costUSD, overheadTokens).
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()), float64(p.totalCostMicro.Load()) / 1e6, int(p.totalAPITokens.Load())
}

// ReactivateSnoozed checks and reactivates snoozed tracks whose snooze_until has passed.
func (p *Pipeline) ReactivateSnoozed(_ context.Context) (int, error) {
	return p.db.ReactivateSnoozedTracks()
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

// Run executes the tracks pipeline for the current user.
// Returns the number of new tracks found.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	currentUserID, err := p.db.GetCurrentUserID()
	if err != nil {
		return 0, fmt.Errorf("getting current user: %w", err)
	}
	if currentUserID == "" {
		p.logger.Println("tracks: no current user set, skipping")
		return 0, nil
	}

	now := time.Now()

	// Use full initial history window on first run, otherwise just today.
	var from, to float64
	switch hasTracks, err := p.db.HasTracksForUser(currentUserID); {
	case err != nil:
		p.logger.Printf("tracks: warning: could not check existing tracks: %v", err)
		from, to = DayWindow(now)
	case !hasTracks:
		days := p.cfg.Sync.InitialHistoryDays
		if days <= 0 {
			days = config.DefaultInitialHistDays
		}
		from = float64(now.AddDate(0, 0, -days).Unix())
		to = float64(now.Unix())
		p.logger.Printf("tracks: first run — single window of %d days", days)
	default:
		from, to = DayWindow(now)
	}

	p.logger.Printf("tracks: window: from=%s to=%s, user=%s",
		time.Unix(int64(from), 0).Format("2006-01-02 15:04"),
		time.Unix(int64(to), 0).Format("2006-01-02 15:04"),
		currentUserID)

	return p.RunForWindow(ctx, currentUserID, from, to)
}

// RunForWindow executes track extraction for a specific time window and user.
func (p *Pipeline) RunForWindow(ctx context.Context, userID string, from, to float64) (int, error) {
	p.loadCaches()

	// Load user profile for ownership determination.
	profile, err := p.db.GetUserProfile(userID)
	if err != nil {
		p.logger.Printf("tracks: failed to load user profile: %v (ownership defaults to 'mine')", err)
	}
	p.cacheMu.Lock()
	p.profile = profile
	p.cacheMu.Unlock()

	userName := p.userName(userID)

	// Get channels with messages in the window (non-DM only)
	channelMsgs, err := p.getMessagesByChannel(from, to)
	if err != nil {
		return 0, err
	}

	if len(channelMsgs) == 0 {
		p.progress(0, 0, "No messages in window")
		p.logger.Printf("tracks: no messages found in window [%.0f, %.0f] — check sync status", from, to)
		return 0, nil
	}

	// Log channel/message breakdown for diagnostics.
	totalMsgCount := 0
	for _, msgs := range channelMsgs {
		totalMsgCount += len(msgs)
	}
	p.logger.Printf("tracks: found %d messages across %d channels in window", totalMsgCount, len(channelMsgs))

	// Delete stale inbox tracks from this window before inserting new ones.
	if _, err := p.db.DeleteTracksForWindow(userID, from, to); err != nil {
		p.logger.Printf("tracks: warning: cleanup failed: %v", err)
	}

	total := len(channelMsgs)
	workers := DefaultWorkers
	if workers > total {
		workers = total
	}

	p.progress(0, total, fmt.Sprintf("Scanning %d channels (%d messages) for @%s (%d workers)...", total, totalMsgCount, userName, workers))
	p.logger.Printf("tracks: scanning %d channels with %d workers", total, workers)

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
				p.LastStepMessageCount = len(t.msgs)
				p.LastStepPeriodFrom = time.Unix(int64(from), 0)
				p.LastStepPeriodTo = time.Unix(int64(to), 0)
				p.LastStepDurationSeconds = 0
				p.LastStepInputTokens = 0
				p.LastStepOutputTokens = 0
				p.LastStepCostUSD = 0
				p.progress(c, total, fmt.Sprintf("#%s (%d messages)", channelName, len(t.msgs)))

				stepStart := time.Now()
				n, err := p.processChannel(ctx, userID, userName, t.channelID, channelName, t.msgs, from, to)
				if err != nil {
					p.logger.Printf("tracks: error processing #%s: %v", channelName, err)
				} else if n > 0 {
					totalStored.Add(int32(n))
				}
				p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
				completed.Add(1)
				p.progress(int(completed.Load()), total, fmt.Sprintf("#%s done", channelName))
			}
		}()
	}

	wg.Wait()

	stored := int(totalStored.Load())

	p.progress(total, total, fmt.Sprintf("Found %d tracks for @%s across %d channels", stored, userName, total))
	p.logger.Printf("tracks: %d tracks for @%s from %d channels", stored, userName, total)
	return stored, nil
}

// batchUpdateResult is the parsed JSON response from a batched update check.
type batchUpdateResult struct {
	Results []batchUpdateItem `json:"results"`
}

type batchUpdateItem struct {
	TrackID        int    `json:"track_id"`
	HasUpdate      bool   `json:"has_update"`
	UpdatedContext string `json:"updated_context"`
	StatusHint     string `json:"status_hint"`
	BallOn         string `json:"ball_on"`
}

// batchUpdatePrompt is the prompt template for checking multiple tracks against
// new messages in the same channel in a single AI call.
const batchUpdatePrompt = `You are checking whether new Slack messages in #%[1]s contain meaningful updates for any of the existing tracks listed below.

%[4]s

%[2]s

=== TRACKS TO CHECK ===
%[3]s

=== NEW MESSAGES ===
%[5]s

For EACH track, determine if any of the new messages contain a meaningful update (progress, completion, blocker, scope change, deadline change, etc.).

Return ONLY a JSON object (no markdown fences, no explanation):

{
  "results": [
    {
      "track_id": 123,
      "has_update": true,
      "updated_context": "brief summary of what changed",
      "status_hint": "done",
      "ball_on": "U123"
    }
  ]
}

Rules:
- Include an entry for EVERY track listed above, even if has_update is false
- has_update: true only if messages contain genuine progress, completion, or meaningful change related to THAT SPECIFIC track
- has_update: false for unrelated chatter, bot messages, emoji-only reactions, or messages about other topics
- updated_context: 1-2 sentences summarizing the update. Only when has_update is true.
- status_hint: "done" (completed), "active" (progress but not done), "unchanged" (no update)
- ball_on: user_id of the person who needs to act next. Empty string "" if unchanged or unclear.
- Return valid JSON only, no other text`

// CheckForUpdates checks for new thread activity on existing tracks.
// Tracks are batched by channel — one AI call per channel instead of per track.
// Returns the number of tracks that have new updates.
func (p *Pipeline) CheckForUpdates(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	tracks, err := p.db.GetTracksForUpdateCheck()
	if err != nil {
		return 0, fmt.Errorf("getting tracks for update check: %w", err)
	}

	if len(tracks) == 0 {
		p.logger.Println("tracks: no tracks to check for updates")
		return 0, nil
	}

	p.loadCaches()

	// Group tracks by channel for batched AI calls.
	byChannel := make(map[string][]db.Track)
	for _, t := range tracks {
		byChannel[t.ChannelID] = append(byChannel[t.ChannelID], t)
	}

	p.logger.Printf("tracks: checking %d tracks across %d channels for updates", len(tracks), len(byChannel))
	p.progress(0, len(byChannel), fmt.Sprintf("Checking %d tracks in %d channels...", len(tracks), len(byChannel)))

	type channelTask struct {
		channelID string
		tracks    []db.Track
	}

	taskCh := make(chan channelTask, len(byChannel))
	for chID, chTracks := range byChannel {
		taskCh <- channelTask{channelID: chID, tracks: chTracks}
	}
	close(taskCh)

	// Worker pool — one task per channel now, so fewer workers needed.
	const maxWorkers = 2
	workers := maxWorkers
	if workers > len(byChannel) {
		workers = len(byChannel)
	}

	var completed atomic.Int32
	var updatedCount atomic.Int32
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ct := range taskCh {
				if ctx.Err() != nil {
					return
				}

				channelName := p.channelName(ct.channelID)
				n, err := p.checkChannelTracksForUpdates(ctx, ct.channelID, ct.tracks)
				if err != nil {
					p.logger.Printf("tracks: error checking channel #%s: %v", channelName, err)
				} else if n > 0 {
					updatedCount.Add(int32(n))
				}

				c := int(completed.Add(1))
				p.progress(c, len(byChannel), fmt.Sprintf("#%s done (%d tracks)", channelName, len(ct.tracks)))
			}
		}()
	}

	wg.Wait()

	total := int(updatedCount.Load())
	p.progress(len(byChannel), len(byChannel), fmt.Sprintf("Found updates for %d tracks across %d channels", total, len(byChannel)))
	p.logger.Printf("tracks: %d tracks have new updates (checked %d channels)", total, len(byChannel))
	return total, nil
}

// checkChannelTracksForUpdates checks all tracks in a channel for updates in a single AI call.
// Returns the number of tracks that have updates.
func (p *Pipeline) checkChannelTracksForUpdates(ctx context.Context, channelID string, tracks []db.Track) (int, error) {
	// Find the earliest afterTS among all tracks in this channel.
	// This ensures we fetch all messages that any track might need.
	earliestAfterTS := ""
	for _, t := range tracks {
		afterTS := t.SourceMessageTS
		if t.LastCheckedTS != "" {
			afterTS = t.LastCheckedTS
		}
		if earliestAfterTS == "" || afterTS < earliestAfterTS {
			earliestAfterTS = afterTS
		}
	}

	// Get thread replies for each track's source thread.
	seenTS := make(map[string]bool)
	var allMessages []db.Message
	for _, t := range tracks {
		afterTS := t.SourceMessageTS
		if t.LastCheckedTS != "" {
			afterTS = t.LastCheckedTS
		}
		replies, err := p.db.GetThreadRepliesAfterTS(channelID, t.SourceMessageTS, afterTS)
		if err != nil {
			p.logger.Printf("tracks: warning: failed to get thread replies for track %d: %v", t.ID, err)
			continue
		}
		for _, r := range replies {
			if !seenTS[r.TS] {
				seenTS[r.TS] = true
				allMessages = append(allMessages, r)
			}
		}
	}

	// Get channel-level messages after the earliest cutoff.
	channelMsgs, err := p.db.GetChannelMessagesAfterTS(channelID, earliestAfterTS, 200)
	if err != nil {
		p.logger.Printf("tracks: warning: failed to get channel messages for %s: %v", channelID, err)
	}
	for _, m := range channelMsgs {
		if !seenTS[m.TS] {
			seenTS[m.TS] = true
			allMessages = append(allMessages, m)
		}
	}

	if len(allMessages) == 0 {
		return 0, nil
	}

	sort.Slice(allMessages, func(i, j int) bool { return allMessages[i].TSUnix < allMessages[j].TSUnix })

	formatted := p.formatMessages(allMessages)
	if strings.TrimSpace(formatted) == "" {
		return 0, nil
	}

	// Build the tracks section for the prompt.
	var tracksSB strings.Builder
	for _, t := range tracks {
		fmt.Fprintf(&tracksSB, "Track #%d: %s\n  Context: %s\n\n", t.ID, sanitize(t.Text), sanitize(truncate(t.Context, 300)))
	}

	channelName := p.channelName(channelID)
	prompt := fmt.Sprintf(batchUpdatePrompt,
		channelName,
		p.languageInstruction(),
		tracksSB.String(),
		p.formatProfileContext(),
		formatted,
	)

	updateSys, updateUser := digest.SplitPromptAtData(prompt)
	raw, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "tracks.update"), updateSys, updateUser, "")
	if err != nil {
		return 0, fmt.Errorf("AI generation failed for #%s: %w", channelName, err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
		p.totalAPITokens.Add(int64(usage.TotalAPITokens))
		p.LastStepInputTokens += usage.InputTokens
		p.LastStepOutputTokens += usage.OutputTokens
		p.LastStepCostUSD += usage.CostUSD
	}

	batch, err := parseBatchUpdateResult(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing batch update result for #%s: %w", channelName, err)
	}

	// Build a lookup for tracks by ID.
	trackByID := make(map[int]db.Track, len(tracks))
	for _, t := range tracks {
		trackByID[t.ID] = t
	}

	// Find the latest message TS for updating last_checked_ts.
	latestTS := allMessages[len(allMessages)-1].TS

	// Update last_checked_ts for ALL tracks in this channel.
	for _, t := range tracks {
		if err := p.db.UpdateLastCheckedTS(t.ID, latestTS); err != nil {
			p.logger.Printf("tracks: warning: failed to update last_checked_ts for track %d: %v", t.ID, err)
		}
	}

	// Apply updates from AI results.
	updated := 0
	for _, item := range batch.Results {
		if !item.HasUpdate {
			continue
		}

		track, ok := trackByID[item.TrackID]
		if !ok {
			p.logger.Printf("tracks: warning: AI returned unknown track_id %d in #%s", item.TrackID, channelName)
			continue
		}

		if err := p.db.SetTrackHasUpdates(track.ID, true); err != nil {
			p.logger.Printf("tracks: warning: failed to set has_updates for track %d: %v", track.ID, err)
		}

		if item.UpdatedContext != "" {
			if err := p.db.UpdateTrackContext(track.ID, item.UpdatedContext); err != nil {
				p.logger.Printf("tracks: warning: failed to update context for track %d: %v", track.ID, err)
			}
		}

		if item.BallOn != "" && item.BallOn != track.BallOn {
			if err := p.db.UpdateTrackBallOn(track.ID, item.BallOn); err != nil {
				p.logger.Printf("tracks: warning: failed to update ball_on for track %d: %v", track.ID, err)
			} else {
				p.logger.Printf("tracks: track %d ball moved to %s", track.ID, item.BallOn)
			}
		}

		if item.StatusHint == "done" {
			if err := p.db.UpdateTrackStatus(track.ID, "done"); err != nil {
				p.logger.Printf("tracks: warning: failed to mark track %d as done: %v", track.ID, err)
			} else {
				p.logger.Printf("tracks: track %d auto-completed based on channel activity", track.ID)
			}
		}

		updated++
	}

	return updated, nil
}

func parseBatchUpdateResult(raw string) (*batchUpdateResult, error) {
	cleaned := cleanJSON(raw)
	var result batchUpdateResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing batch update check JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}

// processChannel extracts tracks for one channel.
func (p *Pipeline) processChannel(ctx context.Context, userID, userName, channelID, channelName string, msgs []db.Message, from, to float64) (int, error) {
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].TSUnix < msgs[j].TSUnix })

	formatted := p.formatMessages(msgs)
	if strings.TrimSpace(formatted) == "" {
		return 0, nil
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")

	// Load existing tracks for this channel to help AI deduplicate.
	existingSection := p.formatExistingItems(channelID, userID)

	// Load compact summary of recently resolved tracks to prevent re-extraction.
	resolvedSection := p.formatResolvedSummary(userID)

	// Load related digest decisions for context.
	decisionsSection := p.formatDigestDecisions(channelID, from, to)

	// Load existing tracks from OTHER channels for cross-channel completion detection.
	crossChannelSection := p.formatCrossChannelItems(channelID, userID)

	profileSection := p.formatProfileContext()

	tmpl, promptVersion := p.getPrompt(prompts.TracksExtract)
	roleRules := p.formatRoleRules()
	prompt := fmt.Sprintf(tmpl, userName, userID, channelName, channelID, fromStr, toStr, p.languageInstruction(), existingSection, decisionsSection, crossChannelSection, formatted, profileSection, roleRules)

	// Append compact summary of recently resolved tracks.
	if resolvedSection != "" {
		prompt += "\n" + resolvedSection
	}

	// Append channel running summary for additional context if available.
	if runningSummary := p.loadChannelRunningSummary(channelID); runningSummary != "" {
		prompt += runningSummary
	}

	// Append active chains context so AI can link tracks to existing chains.
	if p.ChainContext != "" {
		prompt += "\n" + p.ChainContext + "\nIf a track relates to one of the active chains above, include \"chain_id\": <id> in your response for that track.\n"
	}

	systemPrompt, userMessage := digest.SplitPromptAtData(prompt)
	raw, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "tracks.extract"), systemPrompt, userMessage, "")
	if err != nil {
		return 0, fmt.Errorf("AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
		p.totalAPITokens.Add(int64(usage.TotalAPITokens))
		p.LastStepInputTokens += usage.InputTokens
		p.LastStepOutputTokens += usage.OutputTokens
		p.LastStepCostUSD += usage.CostUSD
	}

	result, err := parseResult(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing result: %w", err)
	}

	if len(result.Items) == 0 {
		p.logger.Printf("tracks: #%s → 0 items from %d messages", channelName, len(msgs))
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

		// Serialize JSON fields as strings, defaulting to "[]" to match schema defaults.
		participants := jsonOrEmpty(item.Participants)
		sourceRefs := jsonOrEmpty(item.SourceRefs)
		tags := jsonOrEmpty(item.Tags)
		decisionOptions := jsonOrEmpty(item.DecisionOptions)
		subItems := jsonOrEmpty(item.SubItems)

		// Validate category.
		category := item.Category
		if !validCategories[category] {
			category = "task"
		}

		var requesterName, requesterUserID string
		if item.Requester != nil {
			requesterName = item.Requester.Name
			requesterUserID = item.Requester.UserID
		}

		// Validate ownership.
		ownership := item.Ownership
		if ownership != "mine" && ownership != "delegated" && ownership != "watching" {
			ownership = "mine"
		}

		// Extract entity fingerprint for dedup.
		fp := extractFingerprint(item.Text, item.Context)
		fpJSON := fingerprintJSON(fp)

		track := db.Track{
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
			PromptVersion:     promptVersion,
			Ownership:         ownership,
			BallOn:            item.BallOn,
			OwnerUserID:       item.OwnerUserID,
			Fingerprint:       fpJSON,
		}

		// If AI identified this as an update to an existing track, update it.
		// M2: validate that existing_id belongs to the current user before updating.
		if item.ExistingID != nil && *item.ExistingID > 0 {
			if owner, err := p.db.GetTrackAssignee(*item.ExistingID); err != nil || owner != userID {
				p.logger.Printf("tracks: ignoring existing_id %d (owner mismatch or not found)", *item.ExistingID)
				item.ExistingID = nil
			}
		}
		if item.ExistingID != nil && *item.ExistingID > 0 {
			if _, err := p.db.UpdateTrackFromExtraction(*item.ExistingID, track); err != nil {
				p.logger.Printf("tracks: error updating track #%d: %v", *item.ExistingID, err)
			} else {
				stored++
			}

			// Handle status_hint: if AI detected that the track is done, mark it.
			if item.StatusHint == "done" {
				if err := p.db.SetTrackHasUpdates(*item.ExistingID, true); err != nil {
					p.logger.Printf("tracks: warning: failed to set has_updates for track %d: %v", *item.ExistingID, err)
				}
				if err := p.db.UpdateTrackStatus(*item.ExistingID, "done"); err != nil {
					p.logger.Printf("tracks: warning: failed to mark track %d as done: %v", *item.ExistingID, err)
				} else {
					p.logger.Printf("tracks: track #%d auto-completed based on channel activity", *item.ExistingID)
				}
			}
			continue
		}

		// Dedup layer 1: exact source_message_ts match against resolved tracks.
		if track.SourceMessageTS != "" {
			if resolved, err := p.db.HasResolvedTrackForMessage(channelID, userID, track.SourceMessageTS); err != nil {
				p.logger.Printf("tracks: warning: resolved check failed: %v", err)
			} else if resolved {
				p.logger.Printf("tracks: skipping track from %s — already resolved (same message)", track.SourceMessageTS)
				continue
			}
		}

		// Dedup layer 2: fingerprint entity match against resolved tracks.
		// If a resolved track shares entities (ticket IDs, user_ids, etc.):
		//   - done → reopen to inbox (user completed it but topic resurfaced)
		//   - dismissed → append activity only (user explicitly rejected it)
		if len(fp) > 0 {
			if resolvedID, resolvedStatus, found, err := p.db.FindResolvedTrackByFingerprint(channelID, userID, fp); err != nil {
				p.logger.Printf("tracks: warning: fingerprint dedup check failed: %v", err)
			} else if found {
				if resolvedStatus == "done" {
					if err := p.db.ReopenTrack(resolvedID, item.Context); err != nil {
						p.logger.Printf("tracks: warning: failed to reopen track %d: %v", resolvedID, err)
					} else {
						p.logger.Printf("tracks: reopened done track #%d — new activity with matching entities %v", resolvedID, fp)
						stored++
					}
				} else {
					// dismissed — record activity but don't reopen
					if err := p.db.AppendTrackActivity(resolvedID, item.Context); err != nil {
						p.logger.Printf("tracks: warning: failed to append activity to track %d: %v", resolvedID, err)
					} else {
						p.logger.Printf("tracks: appended activity to dismissed track #%d — entities %v", resolvedID, fp)
					}
				}
				continue
			}
		}

		trackID, err := p.db.UpsertTrack(track)
		if err != nil {
			p.logger.Printf("tracks: error storing track: %v", err)
			continue
		}
		stored++

		// Link track to chain if AI specified chain_id.
		if item.ChainID != nil && *item.ChainID > 0 && trackID > 0 {
			ref := db.ChainRef{
				ChainID:   *item.ChainID,
				RefType:   "track",
				TrackID:   int(trackID),
				ChannelID: channelID,
				Timestamp: to,
			}
			if err := p.db.InsertChainRef(ref); err != nil {
				p.logger.Printf("tracks: chain ref error for track %d → chain %d: %v", trackID, *item.ChainID, err)
			} else {
				_ = p.db.AddChannelToChain(*item.ChainID, channelID)
			}
		}
	}

	if stored > 0 {
		p.logger.Printf("tracks: #%s → %d tracks", channelName, stored)
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
		p.logger.Printf("tracks: warning: message limit (%d) reached, some messages may be skipped", msgLimit)
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

// formatExistingItems loads active/inbox tracks for a channel and formats them
// as a prompt section for AI deduplication.
func (p *Pipeline) formatExistingItems(channelID, userID string) string {
	tracks, err := p.db.GetExistingTracksForChannel(channelID, userID)
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load existing tracks for %s: %v", channelID, err)
		return ""
	}
	if len(tracks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== EXISTING TRACKS FOR THIS CHANNEL ===\n")
	for _, track := range tracks {
		fmt.Fprintf(&sb, "#%d [%s] %q\n", track.ID, track.Status, sanitize(track.Text))
		if track.DecisionSummary != "" {
			fmt.Fprintf(&sb, "    decision: %q\n", sanitize(track.DecisionSummary))
		}
		if track.Tags != "" {
			fmt.Fprintf(&sb, "    tags: %s\n", track.Tags)
		}
		if track.RelatedDigestIDs != "" {
			fmt.Fprintf(&sb, "    digests: %s\n", track.RelatedDigestIDs)
		}
		if track.Context != "" {
			fmt.Fprintf(&sb, "    context: %s\n", sanitize(truncate(track.Context, 200)))
		}
	}
	return sb.String()
}

// formatCrossChannelItems loads active/inbox tracks from OTHER channels
// so the AI can detect cross-channel completion signals.
func (p *Pipeline) formatCrossChannelItems(excludeChannelID, userID string) string {
	tracks, err := p.db.GetExistingTracksExcludingChannel(excludeChannelID, userID)
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load cross-channel tracks: %v", err)
		return ""
	}
	if len(tracks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== EXISTING TRACKS FROM OTHER CHANNELS ===\n")
	sb.WriteString("If a message in this channel confirms completion of any of these tracks, return it with existing_id and status_hint.\n")
	for _, track := range tracks {
		chName := p.channelName(track.ChannelID)
		fmt.Fprintf(&sb, "#%d [%s] #%s: %q\n", track.ID, track.Status, chName, sanitize(track.Text))
		if track.Context != "" {
			fmt.Fprintf(&sb, "    context: %s\n", sanitize(truncate(track.Context, 150)))
		}
	}
	return sb.String()
}

// formatResolvedSummary returns a compact section listing recently resolved tracks
// so the AI avoids re-extracting them. Minimal context growth (~100-200 tokens).
func (p *Pipeline) formatResolvedSummary(userID string) string {
	since := time.Now().Add(-7 * 24 * time.Hour)
	summary, err := p.db.GetResolvedTracksSummary(userID, since)
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load resolved summary: %v", err)
		return ""
	}
	if summary == "" {
		return ""
	}
	return "=== RECENTLY RESOLVED TRACKS (do NOT re-extract these) ===\n" + summary + "\n"
}

// formatDigestDecisions loads recent decisions from related digests.
func (p *Pipeline) formatDigestDecisions(channelID string, from, to float64) string {
	decisions, err := p.db.GetDigestDecisionsForChannel(channelID, from, to)
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load digest decisions for %s: %v", channelID, err)
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

// loadChannelRunningSummary returns the channel's running summary as a prompt
// section, or empty string if none exists or if too old (>30 days).
func (p *Pipeline) loadChannelRunningSummary(channelID string) string {
	result, err := p.db.GetLatestRunningSummaryWithAge(channelID, "channel")
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load running summary for %s: %v", channelID, err)
		return ""
	}
	if result == nil || result.Summary == "" {
		return ""
	}
	if result.AgeDays > 30 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n=== CHANNEL CONTEXT ===\n")
	if result.AgeDays > 7 {
		fmt.Fprintf(&sb, "(outdated, from %.0f days ago)\n", result.AgeDays)
	}
	sb.WriteString(result.Summary)
	sb.WriteString("\nUse this context to better understand ongoing topics and avoid creating duplicate tracks for known discussions.\n")
	return sb.String()
}

// Regex patterns for entity extraction.
var (
	reTicket  = regexp.MustCompile(`(?i)\b(CEX|FIAT|NOVA|DEV|INFRA|CONVERT|DVSP|BLINC)-\d+\b`)
	reUserID  = regexp.MustCompile(`\bU[A-Z0-9]{8,}\b`)
	reIP      = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reCVE     = regexp.MustCompile(`(?i)\bCVE-\d{4}-\d+\b`)
	reMR      = regexp.MustCompile(`!(\d{4,})\b`)
	reSlackTS = regexp.MustCompile(`\b\d{10}\.\d{6}\b`) // skip Slack timestamps — not entities
)

// extractFingerprint extracts key entities from track text and context
// for programmatic deduplication. Returns a deduplicated, sorted slice.
func extractFingerprint(text, ctx string) []string {
	combined := text + " " + ctx
	seen := make(map[string]struct{})
	var result []string

	add := func(s string) {
		upper := strings.ToUpper(s)
		if _, ok := seen[upper]; !ok {
			seen[upper] = struct{}{}
			result = append(result, upper)
		}
	}

	for _, m := range reTicket.FindAllString(combined, -1) {
		add(m)
	}
	for _, m := range reCVE.FindAllString(combined, -1) {
		add(m)
	}
	for _, m := range reMR.FindAllString(combined, -1) {
		add("MR" + m[1:]) // normalize "!6584" → "MR6584"
	}
	for _, m := range reUserID.FindAllString(combined, -1) {
		add(m)
	}
	for _, m := range reIP.FindAllString(combined, -1) {
		// Skip Slack-timestamp-like patterns (handled separately).
		if !reSlackTS.MatchString(m) {
			add(m)
		}
	}

	sort.Strings(result)
	return result
}

// fingerprintJSON returns the fingerprint as a JSON string.
func fingerprintJSON(fp []string) string {
	if len(fp) == 0 {
		return "[]"
	}
	data, err := json.Marshal(fp)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func truncate(s string, maxLen int) string {
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
	channelNames := make(map[string]string)
	userNames := make(map[string]string)

	users, err := p.db.GetUsers(db.UserFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load user names: %v", err)
	} else {
		for _, u := range users {
			name := u.DisplayName
			if name == "" {
				name = u.Name
			}
			userNames[u.ID] = name
		}
	}

	channels, err := p.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load channel names: %v", err)
	} else {
		for _, ch := range channels {
			name := ch.Name
			if name == "" && ch.DMUserID.Valid && ch.DMUserID.String != "" {
				if uname, ok := userNames[ch.DMUserID.String]; ok {
					name = "DM: " + uname
				}
			}
			if name == "" {
				name = ch.ID
			}
			channelNames[ch.ID] = name
		}
	}

	p.cacheMu.Lock()
	p.channelNames = channelNames
	p.userNames = userNames
	p.cacheMu.Unlock()
}

// formatProfileContext builds the profile context section for the AI prompt.
// Returns ownership rules based on the user's profile (reports, peers, role).
func (p *Pipeline) formatProfileContext() string {
	p.cacheMu.RLock()
	profile := p.profile
	p.cacheMu.RUnlock()

	if profile == nil || profile.CustomPromptContext == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== USER PROFILE CONTEXT ===\n")
	sb.WriteString(sanitize(profile.CustomPromptContext))
	sb.WriteString("\n\nOWNERSHIP RULES (based on user profile):\n")
	sb.WriteString("- If the track is a task/question/request directed at ME → ownership: \"mine\"\n")
	sb.WriteString("- If the track involves one of MY REPORTS as the responsible person → ownership: \"delegated\", owner_user_id: report's user_id\n")
	sb.WriteString("- If the track is a decision/discussion that affects my area but I'm not the actor → ownership: \"watching\"\n")
	sb.WriteString("- If unsure → ownership: \"mine\" (better to surface than miss)\n")
	sb.WriteString("\nBALL RULES:\n")
	sb.WriteString("- ball_on = user_id of the person who needs to act next\n")
	sb.WriteString("- If I asked a question and am waiting for reply → ball_on: other person's user_id\n")
	sb.WriteString("- If someone asked me something → ball_on: my user_id\n")

	// Add specific report user_ids if available.
	if profile.Reports != "" && profile.Reports != "[]" {
		sb.WriteString(fmt.Sprintf("\nMY REPORTS (user_ids): %s\n", sanitize(profile.Reports)))
		sb.WriteString("Tasks assigned to or owned by these people → ownership: \"delegated\", owner_user_id: their user_id\n")
	}
	if profile.Peers != "" && profile.Peers != "[]" {
		sb.WriteString(fmt.Sprintf("\nMY PEERS (user_ids): %s\n", sanitize(profile.Peers)))
	}
	if profile.Manager != "" {
		sb.WriteString(fmt.Sprintf("\nMY MANAGER (user_id): %s\n", sanitize(profile.Manager)))
	}
	if profile.StarredChannels != "" && profile.StarredChannels != "[]" {
		sb.WriteString(fmt.Sprintf("\nSTARRED CHANNELS: %s — create more tracks from these channels, lower threshold for relevance\n", sanitize(profile.StarredChannels)))
	}
	if profile.StarredPeople != "" && profile.StarredPeople != "[]" {
		sb.WriteString(fmt.Sprintf("\nSTARRED PEOPLE: %s — messages from these people get higher priority\n", sanitize(profile.StarredPeople)))
	}

	// Category weighting based on role.
	if role := strings.ToLower(profile.Role); role != "" {
		sb.WriteString("\nCATEGORY PRIORITY (based on role):\n")
		switch {
		case strings.Contains(role, "pm") || strings.Contains(role, "product manager"):
			sb.WriteString("HIGH priority categories: decision_needed, approval\n")
			sb.WriteString("NORMAL priority categories: info_request, follow_up\n")
		case strings.Contains(role, "engineering manager") || strings.Contains(role, "em ") || role == "em":
			sb.WriteString("HIGH priority categories: decision_needed, follow_up\n")
			sb.WriteString("NORMAL priority categories: code_review, bug_fix\n")
		case strings.Contains(role, "manager"):
			sb.WriteString("HIGH priority categories: decision_needed, follow_up\n")
			sb.WriteString("NORMAL priority categories: code_review, bug_fix\n")
		case strings.Contains(role, "lead") || strings.Contains(role, "tl"):
			sb.WriteString("HIGH priority categories: decision_needed, code_review\n")
			sb.WriteString("NORMAL priority categories: approval, follow_up\n")
		default: // IC / engineer / other
			sb.WriteString("HIGH priority categories: code_review, bug_fix, task\n")
			sb.WriteString("NORMAL priority categories: decision_needed\n")
		}
		sb.WriteString("When a track falls into a HIGH priority category, prefer priority: \"high\" or \"medium\" over \"low\".\n")
	}

	return sb.String()
}

// formatRoleRules generates role-specific extraction rules.
// For manager roles, this broadens extraction criteria beyond "clear actionable requests"
// to include strategic discussions, delegated tasks, and decisions in their area.
// For IC roles, returns empty string (default strict rules apply).
func (p *Pipeline) formatRoleRules() string {
	p.cacheMu.RLock()
	profile := p.profile
	p.cacheMu.RUnlock()

	if profile == nil {
		return ""
	}

	role := strings.ToLower(profile.Role)
	if role == "" {
		return ""
	}

	isManager := role == "top_management" || role == "direction_owner" || role == "middle_management" ||
		strings.Contains(role, "manager") || strings.Contains(role, "director") ||
		strings.Contains(role, "vp") || strings.Contains(role, "head") ||
		strings.Contains(role, "cto") || strings.Contains(role, "ceo")

	isLead := strings.Contains(role, "lead") || strings.Contains(role, "tl") ||
		strings.Contains(role, "principal") || strings.Contains(role, "staff")

	if !isManager && !isLead {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n=== ROLE-SPECIFIC RULES (override strict extraction for this role) ===\n")
	sb.WriteString("The rules above are designed for individual contributors. For YOUR role, EXPAND the extraction:\n\n")

	if isManager {
		sb.WriteString("ALSO extract tracks for:\n")
		sb.WriteString("- DECISIONS in your area: any discussion where a choice is being made or was made that affects your team/domain, even if you're not mentioned by name. Category: \"decision_needed\"\n")
		sb.WriteString("- DELEGATED TASKS: tasks assigned to or being worked on by your reports — you need visibility. Category: \"task\" or \"follow_up\", ownership: \"delegated\"\n")
		sb.WriteString("- BLOCKERS & ESCALATIONS: anything blocking your team members, requests for help, delays, conflicts. Category: \"follow_up\", priority: \"high\"\n")
		sb.WriteString("- STATUS UPDATES that reveal problems: if someone reports something is late, broken, or at risk. Category: \"follow_up\"\n")
		sb.WriteString("- CROSS-TEAM COORDINATION: requests or discussions involving other teams that affect your area. Category: \"follow_up\" or \"approval\"\n")
		sb.WriteString("- STRATEGIC DISCUSSIONS: architectural decisions, process changes, tool evaluations, resource allocation. Category: \"decision_needed\" or \"discussion\"\n")
		sb.WriteString("\nFor these manager-specific tracks:\n")
		sb.WriteString("- Lower the threshold: include items even if the user is not explicitly mentioned\n")
		sb.WriteString("- Use ownership \"watching\" for discussions where the user is not the primary actor but needs awareness\n")
		sb.WriteString("- Use ownership \"delegated\" when a report is the responsible person\n")
		sb.WriteString("- Prefer creating a track over skipping — better to surface too much than miss something important\n")
	} else if isLead {
		sb.WriteString("ALSO extract tracks for:\n")
		sb.WriteString("- TECHNICAL DECISIONS: architectural choices, tech stack decisions, design tradeoffs. Category: \"decision_needed\"\n")
		sb.WriteString("- CODE QUALITY SIGNALS: discussions about tech debt, performance issues, refactoring needs. Category: \"discussion\"\n")
		sb.WriteString("- TEAM COORDINATION: cross-team technical dependencies, integration work. Category: \"follow_up\"\n")
		sb.WriteString("- MENTORING OPPORTUNITIES: junior team members asking questions in your area of expertise. Category: \"info_request\"\n")
		sb.WriteString("\nFor these lead-specific tracks:\n")
		sb.WriteString("- Include technical discussions where your expertise is relevant, even without direct mention\n")
		sb.WriteString("- Use ownership \"watching\" for cross-team decisions that may affect your codebase\n")
	}

	return sb.String()
}

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: Write ALL text values (text, context) in %s.", lang)
	}
	return "Write in the language most commonly used in the messages"
}

func (p *Pipeline) channelName(id string) string {
	p.cacheMu.RLock()
	name, ok := p.channelNames[id]
	p.cacheMu.RUnlock()
	if ok {
		return sanitize(name)
	}
	return id
}

func (p *Pipeline) userName(id string) string {
	p.cacheMu.RLock()
	name, ok := p.userNames[id]
	p.cacheMu.RUnlock()
	if ok {
		return sanitize(name)
	}
	return id
}

func (p *Pipeline) progress(done, total int, status string) {
	if p.OnProgress != nil {
		p.OnProgress(done, total, status)
	}
}

// jsonOrEmpty returns the JSON string if it's valid and non-null, or "[]" as default.
func jsonOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "[]"
	}
	// Validate that the raw message is valid JSON before storing.
	if !json.Valid(raw) {
		return "[]"
	}
	return string(raw)
}

func sanitize(text string) string {
	// Strip newlines to prevent prompt structure injection via display names.
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	// Strip backticks to prevent markdown code fence injection.
	text = strings.ReplaceAll(text, "```", "` ` `")
	// Strip section markers that could alter prompt structure.
	text = strings.ReplaceAll(text, "===", "= = =")
	text = strings.ReplaceAll(text, "---", "- - -")
	return text
}

// cleanJSON strips markdown fences and trims to the outermost JSON braces.
func cleanJSON(raw string) string {
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
	return cleaned
}

func parseResult(raw string) (*aiResult, error) {
	cleaned := cleanJSON(raw)
	var result aiResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing tracks JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}
