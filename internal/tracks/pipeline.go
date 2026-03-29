// Package tracks provides per-channel extraction of action-item tracks with cross-channel merging.
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
}

// Pipeline extracts and stores tracks for the current user.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	OnProgress ProgressFunc

	// LastStep* fields are set before each OnProgress callback.
	LastStepMessageCount    int
	LastStepPeriodFrom      time.Time
	LastStepPeriodTo        time.Time
	LastStepDurationSeconds float64
	LastStepInputTokens     int
	LastStepOutputTokens    int
	LastStepCostUSD         float64

	// LastFrom/LastTo are set after Run() completes, for callers to pass to CompletePipelineRun.
	LastFrom float64
	LastTo   float64

	// Accumulated token usage across all Generate calls (thread-safe).
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalCostMicro    atomic.Int64 // cost * 1e6 for atomic ops
	totalAPITokens    atomic.Int64

	// caches (populated once per Run)
	cacheMu            sync.RWMutex
	channelNames       map[string]string
	userNames          map[string]string
	profile            *db.UserProfile
	crossChannelCache  string     // pre-formatted cross-channel section
	allActiveTracksRef []db.Track // cached active tracks for the run
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

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()),
		float64(p.totalCostMicro.Load()) / 1e6, int(p.totalAPITokens.Load())
}

// DayWindow returns a 24h window ending at now.
func DayWindow(now time.Time) (from, to float64) {
	to2 := float64(now.Unix())
	from2 := float64(now.Add(-DefaultWindowHours * time.Hour).Unix())
	return from2, to2
}

// lastTracksTime returns the end of the last successful tracks pipeline run,
// or falls back to DayWindow if none found or too old (>24h).
func (p *Pipeline) lastTracksTime() float64 {
	periodTo, err := p.db.GetLatestPipelineRunPeriodTo("tracks")
	if err != nil || periodTo == 0 {
		return 0
	}

	// Cap at 24h ago to avoid processing huge windows after long outages.
	maxLookback := float64(time.Now().Add(-DefaultWindowHours * time.Hour).Unix())
	if periodTo < maxLookback {
		return maxLookback
	}

	return periodTo
}

// Run executes the tracks extraction pipeline.
// Returns (stored count, error).
func (p *Pipeline) Run(ctx context.Context) (int, int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, 0, nil
	}

	currentUserID, err := p.db.GetCurrentUserID()
	if err != nil {
		return 0, 0, fmt.Errorf("getting current user: %w", err)
	}
	if currentUserID == "" {
		p.logger.Println("tracks: no current user set, skipping")
		return 0, 0, nil
	}

	now := time.Now()

	// Use full initial history window on first run, incremental on subsequent runs.
	var from, to float64
	to = float64(now.Unix())
	switch hasTracks, err := p.db.HasTracksForUser(currentUserID); {
	case err != nil:
		p.logger.Printf("tracks: warning: could not check existing tracks: %v", err)
		from, _ = DayWindow(now)
	case !hasTracks:
		days := p.cfg.Sync.InitialHistoryDays
		if days <= 0 {
			days = config.DefaultInitialHistDays
		}
		from = float64(now.AddDate(0, 0, -days).Unix())
		p.logger.Printf("tracks: first run — single window of %d days", days)
	default:
		if lastTo := p.lastTracksTime(); lastTo > 0 {
			from = lastTo
			p.logger.Printf("tracks: incremental from last run period_to=%s",
				time.Unix(int64(lastTo), 0).Format("2006-01-02 15:04"))
		} else {
			from, _ = DayWindow(now)
		}
	}

	p.LastFrom = from
	p.LastTo = to

	p.logger.Printf("tracks: window: from=%s to=%s, user=%s",
		time.Unix(int64(from), 0).Format("2006-01-02 15:04"),
		time.Unix(int64(to), 0).Format("2006-01-02 15:04"),
		currentUserID)

	stored, err := p.RunForWindow(ctx, currentUserID, from, to)
	return stored, 0, err
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

	// Pre-load all active tracks once for cross-channel sections.
	allActive, err := p.db.GetAllActiveTracks()
	if err != nil {
		p.logger.Printf("tracks: warning: failed to pre-load active tracks: %v", err)
	}
	p.cacheMu.Lock()
	p.allActiveTracksRef = allActive
	p.crossChannelCache = "" // reset; built lazily per-channel with exclusion
	p.cacheMu.Unlock()

	// Load channel digests that overlap with the window.
	// Use period_to filter (not period_from) because a digest's period may start
	// before our window but still contain relevant data within the window.
	digests, err := p.db.GetDigestsOverlapping("channel", from, to)
	if err != nil {
		return 0, fmt.Errorf("loading digests: %w", err)
	}

	if len(digests) == 0 {
		p.progress(0, 0, "No digests in window")
		p.logger.Printf("tracks: no digests found in window [%.0f, %.0f]", from, to)
		return 0, nil
	}

	// Filter muted channels.
	mutedIDs, err := p.db.GetMutedChannelIDs()
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load muted channels: %v", err)
	}
	muted := make(map[string]bool, len(mutedIDs))
	for _, id := range mutedIDs {
		muted[id] = true
	}

	// Collect digest IDs and group digests by channel.
	var digestIDs []int
	digestsByChannel := make(map[string][]db.Digest)
	for _, d := range digests {
		if muted[d.ChannelID] {
			continue
		}
		digestIDs = append(digestIDs, d.ID)
		digestsByChannel[d.ChannelID] = append(digestsByChannel[d.ChannelID], d)
	}

	if len(digestIDs) == 0 {
		p.progress(0, 0, "No digests after filtering muted channels")
		return 0, nil
	}

	// Load topics for all digests.
	allTopics, err := p.db.GetDigestTopicsByDigestIDs(digestIDs)
	if err != nil {
		return 0, fmt.Errorf("loading digest topics: %w", err)
	}

	// Group topics by digest ID.
	topicsByDigest := make(map[int][]db.DigestTopic)
	for _, t := range allTopics {
		topicsByDigest[t.DigestID] = append(topicsByDigest[t.DigestID], t)
	}

	// Build digest entries per channel.
	var allEntries []digestEntry
	totalTopicCount := 0
	for chID, chDigests := range digestsByChannel {
		var chTopics []db.DigestTopic
		for _, d := range chDigests {
			chTopics = append(chTopics, topicsByDigest[d.ID]...)
		}
		topicCount := len(chTopics)
		if topicCount == 0 {
			continue
		}
		totalTopicCount += topicCount
		allEntries = append(allEntries, digestEntry{
			channelID:   chID,
			channelName: p.channelName(chID),
			digests:     chDigests,
			topics:      chTopics,
			topicCount:  topicCount,
		})
	}

	if len(allEntries) == 0 {
		p.progress(0, 0, "No topics in digests")
		return 0, nil
	}

	// Sort by topic count descending for better batch packing.
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].topicCount > allEntries[j].topicCount
	})

	// Estimate max topics per batch from context budget.
	// Each digest topic includes summary, decisions, action_items, situations, key_messages.
	// Real cost ~700 tokens/topic (measured: 510K chars / 191 topics ≈ 670 tok in production).
	budget := p.cfg.AI.ContextBudget
	if budget <= 0 {
		budget = config.DefaultAIContextBudget
	}
	const tokensPerTopic = 700
	const promptOverhead = 20000
	maxTopicsPerBatch := (budget - promptOverhead) / tokensPerTopic
	if maxTopicsPerBatch < 20 {
		maxTopicsPerBatch = 20
	}

	batches := groupDigestBatches(allEntries, 15, maxTopicsPerBatch)

	p.logger.Printf("tracks: found %d topics across %d channels → %d batch(es), budget %d tokens",
		totalTopicCount, len(allEntries), len(batches), budget)
	p.progress(0, len(batches), fmt.Sprintf("Scanning %d channels (%d topics) for @%s in %d batch(es)...",
		len(allEntries), totalTopicCount, userName, len(batches)))

	totalStored := 0
	for i, batch := range batches {
		if ctx.Err() != nil {
			break
		}

		batchTopics := 0
		for _, e := range batch {
			batchTopics += e.topicCount
		}

		p.LastStepMessageCount = batchTopics
		p.LastStepPeriodFrom = time.Unix(int64(from), 0)
		p.LastStepPeriodTo = time.Unix(int64(to), 0)
		p.LastStepDurationSeconds = 0
		p.LastStepInputTokens = 0
		p.LastStepOutputTokens = 0
		p.LastStepCostUSD = 0
		p.progress(i, len(batches), fmt.Sprintf("Batch %d/%d (%d channels, %d topics)", i+1, len(batches), len(batch), batchTopics))

		stepStart := time.Now()
		n, err := p.generateBatchTracks(ctx, batch, userID, userName, from, to)
		if err != nil {
			p.logger.Printf("tracks: error in batch %d/%d: %v", i+1, len(batches), err)
		} else {
			totalStored += n
		}
		p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
		p.progress(i+1, len(batches), fmt.Sprintf("Batch %d/%d done (%d tracks)", i+1, len(batches), n))
	}

	p.LastStepDurationSeconds = 0 // reset to avoid duplicate step recording on final progress
	p.progress(len(batches), len(batches), fmt.Sprintf("Found %d tracks for @%s across %d channels", totalStored, userName, len(allEntries)))
	p.logger.Printf("tracks: %d tracks for @%s from %d channels", totalStored, userName, len(allEntries))
	return totalStored, nil //nolint:nilerr // partial results returned; per-batch errors logged above
}

// digestEntry represents a channel's digest data for batch processing.
type digestEntry struct {
	channelID   string
	channelName string
	digests     []db.Digest
	topics      []db.DigestTopic
	topicCount  int // for batching
}

// storeTrackItems validates and persists AI-extracted track items into the database.
// Returns the number of tracks successfully stored/updated.
func (p *Pipeline) storeTrackItems(items []aiItem, userID, channelID, channelName string,
	usage *digest.Usage, promptVersion int, from, to float64) int {
	// Divide token cost across items.
	var inputTokens, outputTokens int
	var costUSD float64
	if usage != nil && len(items) > 0 {
		inputTokens = usage.InputTokens / len(items)
		outputTokens = usage.OutputTokens / len(items)
		costUSD = usage.CostUSD / float64(len(items))
	}

	// Look up related digest IDs for this channel + time window.
	relatedDigestIDs := "[]"
	if digestIDs, err := p.db.FindRelatedDigestIDs(channelID, from, to); err == nil && len(digestIDs) > 0 {
		if b, err := json.Marshal(digestIDs); err == nil {
			relatedDigestIDs = string(b)
		}
	}

	stored := 0
	for _, item := range items {
		priority := item.Priority
		if priority != "high" && priority != "medium" && priority != "low" {
			priority = "medium"
		}

		ownership := item.Ownership
		if ownership != "mine" && ownership != "delegated" && ownership != "watching" {
			ownership = "mine"
		}

		// Post-AI quality filter: drop low-value tracks.
		if shouldDropTrack(ownership, priority, item.Category, item.Blocking) {
			p.logger.Printf("tracks: #%s — dropped low-value track (ownership=%s, priority=%s, category=%s): %.80s",
				channelName, ownership, priority, item.Category, item.Text)
			continue
		}

		var dueDate float64
		if item.DueDate != "" {
			if t, err := time.Parse("2006-01-02", item.DueDate); err == nil {
				dueDate = float64(t.Unix())
			}
		}

		participants := jsonOrEmpty(item.Participants)
		sourceRefs := jsonOrEmpty(item.SourceRefs)
		tags := jsonOrEmpty(item.Tags)
		decisionOptions := jsonOrEmpty(item.DecisionOptions)
		subItems := jsonOrEmpty(item.SubItems)

		category := item.Category
		if !validCategories[category] {
			category = "task"
		}

		var requesterName, requesterUserID string
		if item.Requester != nil {
			requesterName = item.Requester.Name
			requesterUserID = item.Requester.UserID
		}

		fp := extractFingerprint(item.Text, item.Context)
		fpJSON := fingerprintJSON(fp)

		channelIDsJSON := jsonStringArray([]string{channelID})

		track := db.Track{
			AssigneeUserID:   userID,
			Text:             item.Text,
			Context:          item.Context,
			Category:         category,
			Ownership:        ownership,
			BallOn:           item.BallOn,
			OwnerUserID:      item.OwnerUserID,
			RequesterName:    requesterName,
			RequesterUserID:  requesterUserID,
			Blocking:         item.Blocking,
			DecisionSummary:  item.DecisionSummary,
			DecisionOptions:  decisionOptions,
			SubItems:         subItems,
			Participants:     participants,
			SourceRefs:       sourceRefs,
			Tags:             tags,
			ChannelIDs:       channelIDsJSON,
			RelatedDigestIDs: relatedDigestIDs,
			Priority:         priority,
			DueDate:          dueDate,
			Fingerprint:      fpJSON,
			Model:            p.cfg.Digest.Model,
			InputTokens:      inputTokens,
			OutputTokens:     outputTokens,
			CostUSD:          costUSD,
			PromptVersion:    promptVersion,
		}

		// If AI identified this as an update to an existing track, update it.
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
			continue
		}

		// Dedup: fingerprint match against existing tracks.
		if len(fp) > 0 {
			if matches, err := p.db.FindTracksByFingerprint(userID, fp); err == nil && len(matches) > 0 {
				// Update the first matching track instead of creating duplicate.
				if _, err := p.db.UpdateTrackFromExtraction(matches[0].ID, track); err != nil {
					p.logger.Printf("tracks: warning: failed to update fingerprint-matched track %d: %v", matches[0].ID, err)
				} else {
					p.logger.Printf("tracks: merged into existing track #%d via fingerprint %v", matches[0].ID, fp)
					stored++
				}
				continue
			}
		}

		trackID, err := p.db.UpsertTrack(track)
		if err != nil {
			p.logger.Printf("tracks: error storing track: %v", err)
			continue
		}
		_ = trackID
		stored++
	}

	if stored > 0 {
		p.logger.Printf("tracks: #%s → %d tracks", channelName, stored)
	}
	return stored
}

// --- batch tracks extraction ---

// batchChannelResult is the per-channel result from a batch tracks LLM call.
type batchChannelResult struct {
	ChannelID string   `json:"channel_id"`
	Items     []aiItem `json:"items"`
}

// groupDigestBatches groups digest entries into batches not exceeding maxChannels and maxTopics.
func groupDigestBatches(entries []digestEntry, maxChannels, maxTopics int) [][]digestEntry {
	if len(entries) == 0 {
		return nil
	}
	if maxChannels <= 0 {
		return [][]digestEntry{entries}
	}

	var batches [][]digestEntry
	var current []digestEntry
	currentTopics := 0

	for _, e := range entries {
		if len(current) > 0 && (len(current) >= maxChannels || (maxTopics > 0 && currentTopics+e.topicCount > maxTopics)) {
			batches = append(batches, current)
			current = nil
			currentTopics = 0
		}
		current = append(current, e)
		currentTopics += e.topicCount
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// parseBatchTracksResult parses the JSON array returned by a batch tracks LLM call.
func parseBatchTracksResult(raw string) ([]batchChannelResult, error) {
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

	// Find JSON array boundaries
	if start := strings.Index(cleaned, "["); start >= 0 {
		if end := strings.LastIndex(cleaned, "]"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	var results []batchChannelResult
	if err := json.Unmarshal([]byte(cleaned), &results); err != nil {
		return nil, fmt.Errorf("parsing batch tracks JSON: %w (raw: %.200s)", err, raw)
	}

	// Filter out entries with missing channel ID.
	var filtered []batchChannelResult
	for _, r := range results {
		if r.ChannelID != "" && len(r.Items) > 0 {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// generateBatchTracks processes multiple channels' digest data in a single LLM call.
func (p *Pipeline) generateBatchTracks(ctx context.Context, entries []digestEntry,
	userID, userName string, from, to float64) (int, error) {
	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")

	// Build channel blocks from digest topics and collect exclusion set.
	excludeSet := make(map[string]bool, len(entries))
	var channelBlocks strings.Builder

	for _, e := range entries {
		excludeSet[e.channelID] = true

		fmt.Fprintf(&channelBlocks, "--- #%s (%s) ---\n", e.channelName, e.channelID)
		for _, d := range e.digests {
			if d.Summary != "" {
				fmt.Fprintf(&channelBlocks, "Digest summary: %s\n", sanitize(d.Summary))
			}
		}
		for _, t := range e.topics {
			fmt.Fprintf(&channelBlocks, "\nTopic: %s\n%s\n", sanitize(t.Title), sanitize(t.Summary))
			if t.Decisions != "" && t.Decisions != "[]" {
				fmt.Fprintf(&channelBlocks, "Decisions: %s\n", t.Decisions)
			}
			if t.ActionItems != "" && t.ActionItems != "[]" {
				fmt.Fprintf(&channelBlocks, "Action items: %s\n", t.ActionItems)
			}
			if t.Situations != "" && t.Situations != "[]" {
				fmt.Fprintf(&channelBlocks, "Situations: %s\n", t.Situations)
			}
			if t.KeyMessages != "" && t.KeyMessages != "[]" {
				fmt.Fprintf(&channelBlocks, "Key messages: %s\n", t.KeyMessages)
			}
		}

		// Append channel running summary for additional context.
		if runningSummary := p.loadChannelRunningSummary(e.channelID); runningSummary != "" {
			channelBlocks.WriteString(runningSummary)
		}
		channelBlocks.WriteString("\n")
	}

	// Single unified existing tracks section (no per-channel duplication).
	crossChannelSection := p.formatCrossChannelItems(excludeSet, userID)
	profileSection := p.formatProfileContext()
	roleRules := p.formatRoleRules()

	tmpl, promptVersion := p.getPrompt(prompts.TracksExtractBatch)
	prompt := fmt.Sprintf(tmpl,
		userName, userID, fromStr, toStr,
		profileSection,
		p.languageInstruction(),
		roleRules,
		"", // existing tracks per-channel removed — cross-channel section covers all
		crossChannelSection,
		channelBlocks.String(),
	)

	p.logger.Printf("tracks: batch prompt sizes: template=%d profile=%d roles=%d cross=%d channels=%d total=%d chars",
		len(tmpl), len(profileSection), len(roleRules),
		len(crossChannelSection), channelBlocks.Len(), len(prompt))

	systemPrompt, userMessage := digest.SplitPromptAtData(prompt)
	raw, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "tracks.extract_batch"), systemPrompt, userMessage, "")
	if err != nil {
		return 0, fmt.Errorf("batch AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
		p.totalAPITokens.Add(int64(usage.TotalAPITokens))
	}

	results, err := parseBatchTracksResult(raw)
	if err != nil {
		return 0, fmt.Errorf("parsing batch result: %w", err)
	}

	totalStored := 0
	for _, cr := range results {
		chName := p.channelName(cr.ChannelID)
		stored := p.storeTrackItems(cr.Items, userID, cr.ChannelID, chName, usage, promptVersion, from, to)
		totalStored += stored
	}

	return totalStored, nil
}

// FormatActiveTracksForPrompt formats active tracks for injection into rollup prompts.
func (p *Pipeline) FormatActiveTracksForPrompt() (string, error) {
	tracks, err := p.db.GetAllActiveTracks()
	if err != nil {
		return "", fmt.Errorf("loading tracks: %w", err)
	}
	if len(tracks) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, t := range tracks {
		sb.WriteString(fmt.Sprintf("- [track #%d, %s, %s] %s\n  Context: %s\n",
			t.ID, t.Priority, t.Ownership, sanitize(t.Text), sanitize(truncate(t.Context, 200))))
	}
	return sb.String(), nil
}

// --- helpers: prompt sections ---

// maxExistingTracksPerChannel is the maximum number of existing tracks shown per channel.
const maxExistingTracksPerChannel = 15

// formatExistingItems loads tracks for this channel and formats for AI dedup.
func (p *Pipeline) formatExistingItems(channelID, userID string) string {
	tracks, err := p.db.GetTracks(db.TrackFilter{ChannelID: channelID})
	if err != nil {
		p.logger.Printf("tracks: warning: failed to load existing tracks for %s: %v", channelID, err)
		return ""
	}
	if len(tracks) == 0 {
		return ""
	}

	// Sort by updated_at DESC, limit to top N.
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].UpdatedAt > tracks[j].UpdatedAt
	})
	totalCount := len(tracks)
	if len(tracks) > maxExistingTracksPerChannel {
		tracks = tracks[:maxExistingTracksPerChannel]
	}

	var sb strings.Builder
	sb.WriteString("=== EXISTING TRACKS FOR THIS CHANNEL ===\n")
	if totalCount > maxExistingTracksPerChannel {
		fmt.Fprintf(&sb, "Showing %d of %d total.\n", maxExistingTracksPerChannel, totalCount)
	}
	for _, track := range tracks {
		tagsStr := ""
		if track.Tags != "" && track.Tags != "[]" {
			tagsStr = " tags:" + track.Tags
		}
		fmt.Fprintf(&sb, "#%d [%s] %q%s\n", track.ID, track.Ownership, sanitize(track.Text), tagsStr)
		if track.Context != "" {
			fmt.Fprintf(&sb, "    context: %s\n", sanitize(truncate(track.Context, 100)))
		}
	}
	return sb.String()
}

// maxCrossChannelTracks is the maximum number of cross-channel tracks included in prompts.
const maxCrossChannelTracks = 20

// priorityOrder returns sort order for track priority (lower = higher priority).
func priorityOrder(p string) int {
	switch p {
	case "high":
		return 0
	case "medium":
		return 1
	default:
		return 2
	}
}

// formatCrossChannelItems formats tracks from OTHER channels for cross-channel merge.
// Uses cached allActiveTracksRef if available, otherwise loads from DB.
// excludeChannelIDs is a set of channel IDs to exclude (all channels being processed).
func (p *Pipeline) formatCrossChannelItems(excludeChannelIDs map[string]bool, userID string) string {
	p.cacheMu.RLock()
	allTracks := p.allActiveTracksRef
	p.cacheMu.RUnlock()

	// Fallback to DB if cache not populated (e.g. called outside RunForWindow).
	if allTracks == nil {
		var err error
		allTracks, err = p.db.GetAllActiveTracks()
		if err != nil {
			p.logger.Printf("tracks: warning: failed to load cross-channel tracks: %v", err)
			return ""
		}
	}

	if len(allTracks) == 0 {
		return ""
	}

	var filtered []db.Track
	for _, t := range allTracks {
		excluded := false
		for chID := range excludeChannelIDs {
			if strings.Contains(t.ChannelIDs, chID) {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return ""
	}

	// Sort by priority (high first), then by ID desc (newest first).
	sort.Slice(filtered, func(i, j int) bool {
		pi, pj := priorityOrder(filtered[i].Priority), priorityOrder(filtered[j].Priority)
		if pi != pj {
			return pi < pj
		}
		return filtered[i].ID > filtered[j].ID
	})

	totalCount := len(filtered)
	if len(filtered) > maxCrossChannelTracks {
		filtered = filtered[:maxCrossChannelTracks]
	}

	var sb strings.Builder
	sb.WriteString("=== EXISTING TRACKS FROM OTHER CHANNELS ===\n")
	sb.WriteString("If a message relates to any of these tracks, set existing_id to merge (cross-channel).\n")
	if totalCount > maxCrossChannelTracks {
		fmt.Fprintf(&sb, "Showing top %d of %d.\n", maxCrossChannelTracks, totalCount)
	}
	for _, track := range filtered {
		tagsStr := ""
		if track.Tags != "" && track.Tags != "[]" {
			tagsStr = " tags:" + track.Tags
		}
		fmt.Fprintf(&sb, "#%d [%s] %q%s\n", track.ID, track.Ownership, sanitize(track.Text), tagsStr)
	}
	return sb.String()
}

// loadChannelRunningSummary loads a channel's running summary as a prompt section.
func (p *Pipeline) loadChannelRunningSummary(channelID string) string {
	result, err := p.db.GetLatestRunningSummaryWithAge(channelID, "channel")
	if err != nil || result == nil || result.Summary == "" {
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

// formatProfileContext builds the profile context section for the AI prompt.
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
	sb.WriteString("- ball_on = user_id of the person who needs to act NEXT\n")
	sb.WriteString("- If I asked a question and am waiting for reply → ball_on: other person's user_id\n")
	sb.WriteString("- If someone asked me something → ball_on: my user_id\n")

	if profile.Reports != "" && profile.Reports != "[]" {
		fmt.Fprintf(&sb, "\nMY REPORTS (user_ids): %s\n", sanitize(profile.Reports))
		sb.WriteString("Tasks assigned to or owned by these people → ownership: \"delegated\", owner_user_id: their user_id\n")
	}
	if profile.Peers != "" && profile.Peers != "[]" {
		fmt.Fprintf(&sb, "\nMY PEERS (user_ids): %s\n", sanitize(profile.Peers))
	}
	if profile.Manager != "" {
		fmt.Fprintf(&sb, "\nMY MANAGER (user_id): %s\n", sanitize(profile.Manager))
	}
	if profile.StarredChannels != "" && profile.StarredChannels != "[]" {
		fmt.Fprintf(&sb, "\nSTARRED CHANNELS: %s — create more tracks from these channels, lower threshold for relevance\n", sanitize(profile.StarredChannels))
	}
	if profile.StarredPeople != "" && profile.StarredPeople != "[]" {
		fmt.Fprintf(&sb, "\nSTARRED PEOPLE: %s — messages from these people get higher priority\n", sanitize(profile.StarredPeople))
	}

	return sb.String()
}

// formatRoleRules generates role-specific extraction rules.
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
	sb.WriteString("\n=== ROLE-SPECIFIC RULES ===\n")

	if isManager {
		sb.WriteString("ALSO extract tracks for:\n")
		sb.WriteString("- DECISIONS in your area requiring user's input/approval. Category: \"decision_needed\"\n")
		sb.WriteString("- DELEGATED TASKS: tasks of reports that are BLOCKED, AT RISK, or OVERDUE. Category: \"task\"/\"follow_up\", ownership: \"delegated\"\n")
		sb.WriteString("- BLOCKERS & ESCALATIONS: things blocking team requiring manager intervention. Category: \"follow_up\", priority: \"high\"\n")
		sb.WriteString("- CROSS-TEAM COORDINATION: requests from other teams needing response. Category: \"follow_up\"/\"approval\"\n")
		sb.WriteString("\nMaintain quality: \"watching\" only for high-priority items. Do NOT create tracks for routine updates or discussions that resolve without the user.\n")
	} else if isLead {
		sb.WriteString("ALSO extract tracks for:\n")
		sb.WriteString("- TECHNICAL DECISIONS requiring user's input/review. Category: \"decision_needed\"\n")
		sb.WriteString("- CROSS-TEAM DEPENDENCIES needing coordination. Category: \"follow_up\"\n")
		sb.WriteString("\nOnly create watching tracks for high-priority cross-team decisions.\n")
	}

	return sb.String()
}

// --- helpers: prompt template ---

func (p *Pipeline) getPrompt(id string) (string, int) {
	role := ""
	if p.profile != nil {
		role = p.profile.Role
	}
	if p.promptStore != nil {
		tmpl, version, err := p.promptStore.GetForRole(id, role)
		if err == nil {
			roleInstr := prompts.GetRoleInstruction(role)
			if roleInstr != "" {
				tmpl = roleInstr + "\n\n" + tmpl
			}
			return tmpl, version
		}
	}
	tmpl := prompts.Defaults[id]
	roleInstr := prompts.GetRoleInstruction(role)
	if roleInstr != "" {
		tmpl = roleInstr + "\n\n" + tmpl
	}
	return tmpl, 0
}

// --- helpers: caches ---

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

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: Write ALL text values (text, context) in %s.", lang)
	}
	return "Write in the language most commonly used in the messages"
}

// --- helpers: fingerprint & dedup ---

var (
	reTicket  = regexp.MustCompile(`(?i)\b(CEX|FIAT|NOVA|DEV|INFRA|CONVERT|DVSP|BLINC)-\d+\b`)
	reUserID  = regexp.MustCompile(`\bU[A-Z0-9]{8,}\b`)
	reIP      = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reCVE     = regexp.MustCompile(`(?i)\bCVE-\d{4}-\d+\b`)
	reMR      = regexp.MustCompile(`!(\d{4,})\b`)
	reSlackTS = regexp.MustCompile(`\b\d{10}\.\d{6}\b`)
)

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
		add("MR" + m[1:])
	}
	for _, m := range reUserID.FindAllString(combined, -1) {
		add(m)
	}
	for _, m := range reIP.FindAllString(combined, -1) {
		if !reSlackTS.MatchString(m) {
			add(m)
		}
	}

	sort.Strings(result)
	return result
}

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

func shouldDropTrack(ownership, priority, category, blocking string) bool {
	if ownership == "watching" && priority == "low" {
		return true
	}
	if ownership == "watching" && priority == "medium" {
		if (category == "follow_up" || category == "discussion") && blocking == "" {
			return true
		}
	}
	return false
}

// --- helpers: JSON & text ---

func jsonOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "[]"
	}
	if !json.Valid(raw) {
		return "[]"
	}
	return string(raw)
}

func jsonStringArray(arr []string) string {
	if len(arr) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(arr)
	return string(data)
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func sanitize(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "```", "` ` `")
	text = strings.ReplaceAll(text, "===", "= = =")
	text = strings.ReplaceAll(text, "---", "- - -")
	return text
}

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

func validatePriority(p string) string {
	switch p {
	case "high", "medium", "low":
		return p
	default:
		return "medium"
	}
}
