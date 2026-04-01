// Package digest provides digest generation and pipeline for summarizing workspace conversations.
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/prompts"
)

// Usage holds token metrics from an AI generation call.
type Usage struct {
	InputTokens    int     // Our prompt tokens (estimated from prompt size)
	OutputTokens   int     // AI response tokens
	CostUSD        float64 // Deprecated: always 0. Kept for struct compatibility.
	TotalAPITokens int     // Total tokens API processed (input + cache_read + cache_creation)
	Model          string  // Actual model used for this call
}

// Generator generates text responses from a system prompt and user message.
// The default implementation calls Claude CLI; tests can substitute a mock.
// sessionID may be empty for the first call; the returned sessionID should be
// passed to subsequent calls to reuse the same Claude session.
type Generator interface {
	Generate(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, *Usage, string, error)
}

// DigestResult is the structured output from Claude for a digest.
type DigestResult struct {
	Summary        string          `json:"summary"`
	Topics         []Topic         `json:"topics"`
	RunningSummary json.RawMessage `json:"running_summary,omitempty"`
}

// Topic is a self-contained thematic unit within a digest.
// Each topic carries its own decisions, action items, situations, and key messages.
type Topic struct {
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Decisions   []Decision     `json:"decisions"`
	ActionItems []ActionItem   `json:"action_items"`
	Situations  []db.Situation `json:"situations"`
	KeyMessages []string       `json:"key_messages"`
}

// DigestSituationParticipant mirrors db.SituationParticipant for JSON parsing.
// Re-exported here for convenience in tests that build DigestResults.
type DigestSituationParticipant = db.SituationParticipant

// PersonSignals holds signals for one person in a channel digest.
type PersonSignals struct {
	UserID  string   `json:"user_id"`
	Signals []Signal `json:"signals"`
}

// Signal is a typed observation about a person in channel context.
type Signal struct {
	Type       string `json:"type"`
	Detail     string `json:"detail"`
	EvidenceTS string `json:"evidence_ts,omitempty"`
}

// Decision represents a decision extracted from messages.
type Decision struct {
	Text       string `json:"text"`
	By         string `json:"by"`
	MessageTS  string `json:"message_ts"`
	Importance string `json:"importance"` // "high", "medium", "low"
}

// ActionItem represents an action item extracted from messages.
type ActionItem struct {
	Text     string `json:"text"`
	Assignee string `json:"assignee"`
	Status   string `json:"status"`
}

// TrackLinker runs the tracks pipeline between channel digests and rollups.
// Defined as an interface to avoid import cycles (tracks imports digest).
type TrackLinker interface {
	Run(ctx context.Context) (int, int, error)
	FormatActiveTracksForPrompt() (string, error)
}

// ProgressFunc is called during digest generation to report progress.
type ProgressFunc func(done, total int, status string)

// Pipeline generates and stores AI digests for Slack channels.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   Generator
	logger      *log.Logger
	promptStore *prompts.Store

	// SinceOverride, if non-zero, overrides the automatic "since last digest"
	// window. Used by `digest generate --since` to force a custom time range.
	SinceOverride float64

	// OnProgress is called to report progress during digest generation.
	OnProgress ProgressFunc

	// TrackContext is injected by the daemon after tracks pipeline runs.
	// If non-empty, it's prepended to the daily/weekly rollup prompt to make
	// rollups track-aware (collapsing tracked topics instead of repeating them).
	TrackContext string

	// TrackLinker, if set, runs tracks pipeline between channel digests and rollups.
	// Used by `digest generate` to replicate the daemon's phased pipeline.
	TrackLinker TrackLinker

	// accumulated usage across all Generate calls (atomic for concurrent workers)
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalAPITokens    atomic.Int64 // total API tokens (our content + CLI overhead)

	// accumulated stats across all channel digests (atomic for concurrent workers)
	totalMessageCount  atomic.Int64
	earliestPeriodFrom atomic.Int64 // unix timestamp
	latestPeriodTo     atomic.Int64 // unix timestamp

	// LastStep* fields are set before each OnProgress callback with the
	// current step's message count and time window. Read them in OnProgress.
	// Protected by lastStepMu for concurrent worker access.
	lastStepMu              sync.Mutex
	LastStepMessageCount    int
	LastStepPeriodFrom      time.Time
	LastStepPeriodTo        time.Time
	LastStepDurationSeconds float64
	LastStepInputTokens     int
	LastStepOutputTokens    int

	// caches populated during a run
	channelNames map[string]string
	channelTypes map[string]string // channel ID → type (public, private, dm, group_dm)
	userNames    map[string]string
	botUserIDs   map[string]bool // user IDs that are bots
	profile      *db.UserProfile // loaded once per Run, nil if not available
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
// Returns (inputTokens, outputTokens, costUSD, overheadTokens).
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()), 0, int(p.totalAPITokens.Load())
}

// AccumulatedStats returns (totalMessageCount, earliestPeriodFrom, latestPeriodTo)
// accumulated across all channel digest runs.
func (p *Pipeline) AccumulatedStats() (int, float64, float64) {
	return int(p.totalMessageCount.Load()), float64(p.earliestPeriodFrom.Load()), float64(p.latestPeriodTo.Load())
}

func (p *Pipeline) accumulateUsage(usage *Usage) {
	if usage == nil {
		return
	}
	p.totalInputTokens.Add(int64(usage.InputTokens))
	p.totalOutputTokens.Add(int64(usage.OutputTokens))
	p.totalAPITokens.Add(int64(usage.TotalAPITokens))
}

// New creates a new digest pipeline.
func New(database *db.DB, cfg *config.Config, gen Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{
		db:        database,
		cfg:       cfg,
		generator: gen,
		logger:    logger,
	}
}

// SetPromptStore sets an optional prompt store for loading customized prompts.
// If not set, built-in defaults are used.
func (p *Pipeline) SetPromptStore(store *prompts.Store) {
	p.promptStore = store
}

// getPrompt loads a prompt template from the store (if set), falling back to the
// built-in const. Returns the template string and its version (0 = built-in).
// Includes role-specific instructions if available.
func (p *Pipeline) getPrompt(id, fallback string) (string, int) {
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
	tmpl := fallback
	roleInstr := prompts.GetRoleInstruction(role)
	if roleInstr != "" {
		tmpl = roleInstr + "\n\n" + tmpl
	}
	return tmpl, 0
}

// acquireDigestLock acquires an exclusive file lock to prevent concurrent digest runs.
// Returns the lock file (caller must defer Close) and unlock func, or error if already locked.
func (p *Pipeline) acquireDigestLock() (*os.File, func(), error) {
	lockPath := filepath.Join(p.cfg.WorkspaceDir(), "digest.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating lock dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening digest lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("another digest pipeline is already running")
	}
	unlock := func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return f, unlock, nil
}

// Run executes the full digest pipeline: channel digests, then daily rollup.
// Returns the number of channel digests generated and total token usage.
func (p *Pipeline) Run(ctx context.Context) (int, *Usage, error) {
	// Reset accumulated usage from previous run (pipeline is reused across daemon cycles).
	p.totalInputTokens.Store(0)
	p.totalOutputTokens.Store(0)
	p.totalAPITokens.Store(0)

	if !p.cfg.Digest.Enabled {
		return 0, nil, nil
	}

	_, unlock, err := p.acquireDigestLock()
	if err != nil {
		p.logger.Printf("digest: skipping — %v", err)
		return 0, nil, nil
	}
	defer unlock()

	// Clean up duplicate digests from near-simultaneous pipeline runs
	var totalDeduped int64
	if removed, err := p.db.DeduplicateChannelDigests(); err != nil {
		p.logger.Printf("digest: warning: channel dedup cleanup failed: %v", err)
	} else {
		totalDeduped += removed
	}
	if removed, err := p.db.DeduplicateDailyDigests(); err != nil {
		p.logger.Printf("digest: warning: dedup cleanup failed: %v", err)
	} else {
		totalDeduped += removed
	}
	if totalDeduped > 0 {
		p.logger.Printf("digest: cleaned up %d duplicate digests", totalDeduped)
	}

	p.loadCaches()

	n, totalUsage, err := p.RunChannelDigests(ctx)
	if err != nil {
		return n, totalUsage, err
	}

	if ctx.Err() != nil {
		return n, totalUsage, ctx.Err()
	}

	if p.OnProgress != nil {
		p.OnProgress(0, 0, "Creating tracks...")
	}
	p.runTrackLinker(ctx)

	if p.OnProgress != nil {
		p.OnProgress(0, 0, "Generating daily rollup...")
	}
	if err := p.RunDailyRollup(ctx); err != nil {
		p.logger.Printf("digest: daily rollup error: %v", err)
	}

	return n, totalUsage, nil
}

// RunChannelDigestsOnly runs only channel-level digests (no rollups).
// Used by daemon to split channel digests from rollups with chains in between.
func (p *Pipeline) RunChannelDigestsOnly(ctx context.Context) (int, *Usage, error) {
	// Reset accumulated usage from previous run (pipeline is reused across daemon cycles).
	p.totalInputTokens.Store(0)
	p.totalOutputTokens.Store(0)
	p.totalAPITokens.Store(0)

	if !p.cfg.Digest.Enabled {
		return 0, nil, nil
	}

	_, unlock, err := p.acquireDigestLock()
	if err != nil {
		p.logger.Printf("digest: skipping — %v", err)
		return 0, nil, nil
	}
	defer unlock()

	var totalDeduped int64
	if removed, err := p.db.DeduplicateChannelDigests(); err != nil {
		p.logger.Printf("digest: warning: channel dedup cleanup failed: %v", err)
	} else {
		totalDeduped += removed
	}
	if removed, err := p.db.DeduplicateDailyDigests(); err != nil {
		p.logger.Printf("digest: warning: dedup cleanup failed: %v", err)
	} else {
		totalDeduped += removed
	}
	if totalDeduped > 0 {
		p.logger.Printf("digest: cleaned up %d duplicate digests", totalDeduped)
	}

	p.loadCaches()

	return p.RunChannelDigests(ctx)
}

// runTrackLinker runs the tracks pipeline (if configured) and injects track context for rollups.
func (p *Pipeline) runTrackLinker(ctx context.Context) {
	if p.TrackLinker == nil || ctx.Err() != nil {
		return
	}
	p.lastStepMu.Lock()
	p.LastStepMessageCount = 0
	p.LastStepInputTokens = 0
	p.LastStepOutputTokens = 0
	p.LastStepDurationSeconds = 0
	p.lastStepMu.Unlock()

	var trackErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				trackErr = fmt.Errorf("tracks pipeline panicked: %v\n%s", r, debug.Stack())
			}
		}()
		created, updated, err := p.TrackLinker.Run(ctx)
		if err != nil {
			trackErr = err
		} else if created > 0 || updated > 0 {
			p.logger.Printf("digest: tracks created=%d updated=%d", created, updated)
		}
	}()

	if trackErr != nil {
		p.logger.Printf("digest: tracks error: %v", trackErr)
		return
	}

	if trackCtx, err := p.TrackLinker.FormatActiveTracksForPrompt(); err == nil && trackCtx != "" {
		p.TrackContext = trackCtx
	}
}

// RunRollups generates daily/weekly rollups. Used by daemon after tracks pipeline has created tracks.
func (p *Pipeline) RunRollups(ctx context.Context) error {
	if !p.cfg.Digest.Enabled {
		return nil
	}

	_, unlock, err := p.acquireDigestLock()
	if err != nil {
		p.logger.Printf("digest: skipping rollups — %v", err)
		return nil
	}
	defer unlock()

	p.loadCaches()

	if err := p.RunDailyRollup(ctx); err != nil {
		return fmt.Errorf("daily rollup: %w", err)
	}

	return nil
}

// RunChannelDigests generates digests for all channels with new messages
// since the last digest run. Returns the count and accumulated token usage.
// Channels are processed in parallel using digest.workers (default: config.DefaultDigestWorkers).
func (p *Pipeline) RunChannelDigests(ctx context.Context) (int, *Usage, error) {
	sinceUnix := p.SinceOverride
	if sinceUnix == 0 {
		sinceUnix = p.lastDigestTime()
	}
	// Truncate to nearest minute to prevent near-duplicate digests when
	// the pipeline runs twice within seconds (same period_from key).
	sinceUnix = float64(int64(sinceUnix) / 60 * 60)
	nowUnix := float64(time.Now().Unix())
	return p.runChannelDigestsForWindow(ctx, sinceUnix, nowUnix)
}

// runChannelDigestsForWindow generates channel digests for a specific time window.
func (p *Pipeline) runChannelDigestsForWindow(ctx context.Context, sinceUnix, nowUnix float64) (int, *Usage, error) {
	// Ensure caches are populated (lazy init for direct RunChannelDigests calls).
	if p.channelTypes == nil {
		p.loadCaches()
	}

	if p.OnProgress != nil {
		p.OnProgress(0, 0, "Finding channels with new messages...")
	}

	p.logger.Printf("digest: window: since=%s now=%s",
		time.Unix(int64(sinceUnix), 0).Format("2006-01-02 15:04"),
		time.Unix(int64(nowUnix), 0).Format("2006-01-02 15:04"))

	channels, err := p.db.ChannelsWithNewMessages(sinceUnix)
	if err != nil {
		return 0, nil, fmt.Errorf("finding channels with new messages: %w", err)
	}

	p.logger.Printf("digest: found %d channels with new messages", len(channels))

	// Filter out muted channels
	mutedIDs, err := p.db.GetMutedChannelIDs()
	if err != nil {
		p.logger.Printf("digest: warning: failed to load muted channels: %v", err)
	} else if len(mutedIDs) > 0 {
		muted := make(map[string]bool, len(mutedIDs))
		for _, id := range mutedIDs {
			muted[id] = true
		}
		var filtered []string
		for _, ch := range channels {
			if !muted[ch] {
				filtered = append(filtered, ch)
			}
		}
		if skipped := len(channels) - len(filtered); skipped > 0 {
			p.logger.Printf("digest: skipped %d muted channel(s)", skipped)
		}
		channels = filtered
	}

	// Filter out 1:1 DMs — private conversations are not useful in digests.
	// Group DMs are kept (they often contain team discussions).
	{
		var filtered []string
		skippedDM := 0
		for _, ch := range channels {
			if p.channelTypes[ch] == "dm" {
				skippedDM++
				continue
			}
			filtered = append(filtered, ch)
		}
		if skippedDM > 0 {
			p.logger.Printf("digest: skipped %d DM channel(s)", skippedDM)
		}
		channels = filtered
	}

	if len(channels) == 0 {
		p.logger.Println("digest: no channels with new messages")
		return 0, nil, nil
	}

	// Collect all channels with visible messages into unified batch entries.
	var allEntries []batchEntry
	skippedNoVisible := 0
	skippedBotOnly := 0
	for _, channelID := range channels {
		msgs, err := p.db.GetMessagesByTimeRange(channelID, sinceUnix, nowUnix)
		if err != nil {
			p.logger.Printf("digest: error getting messages for %s: %v", channelID, err)
			continue
		}
		if len(msgs) == db.DefaultTimeRangeLimit {
			p.logger.Printf("digest: warning: #%s hit message limit (%d), digest may be based on partial data",
				p.channelName(channelID), db.DefaultTimeRangeLimit)
		}
		// Count visible messages and classify by author (bot vs human).
		// Messages with empty user_id (webhooks/integrations) are counted as bot messages.
		visible := 0
		botVisible := 0
		for _, m := range msgs {
			if m.Text != "" && !m.IsDeleted {
				visible++
				if m.UserID == "" || p.botUserIDs[m.UserID] {
					botVisible++
				}
			}
		}
		if visible == 0 {
			if len(msgs) >= p.cfg.Digest.MinMessages {
				skippedNoVisible++
			}
			continue
		}
		humanVisible := visible - botVisible

		// Bot-heavy channel: ≥90% visible messages from bots.
		if visible > 0 && float64(botVisible)/float64(visible) >= 0.9 {
			if humanVisible == 0 {
				// No human messages at all — skip entirely.
				skippedBotOnly++
				continue
			}
			// Extract only human messages + their thread/surrounding context.
			msgs = p.extractHumanContext(msgs)
			// Recount visible after filtering.
			visible = 0
			for _, m := range msgs {
				if m.Text != "" && !m.IsDeleted {
					visible++
				}
			}
			if visible == 0 {
				skippedBotOnly++
				continue
			}
			p.logger.Printf("digest: #%s is bot-heavy, extracted %d context messages around human replies",
				p.channelName(channelID), visible)
		}

		allEntries = append(allEntries, batchEntry{
			channelID:    channelID,
			channelName:  p.channelName(channelID),
			msgs:         msgs,
			visibleCount: visible,
		})
	}

	p.logger.Printf("digest: %d channels with visible messages, %d skipped (no visible text), %d skipped (bot-only)",
		len(allEntries), skippedNoVisible, skippedBotOnly)

	// Cooldown filter: skip channels that were digested recently and have few new messages.
	// This prevents frequent small AI calls for channels with a trickle of messages.
	cooldownMins := config.DefaultDigestCooldownMins
	minMsgs := p.cfg.Digest.MinMessages
	if minMsgs <= 0 {
		minMsgs = config.DefaultDigestMinMsgs
	}
	skippedCooldown := 0
	{
		now := time.Now()
		var filtered []batchEntry
		for _, e := range allEntries {
			// If channel was digested recently and message count is low, defer it.
			latest, err := p.db.GetLatestDigest(e.channelID, "channel")
			if err == nil && latest != nil {
				if created, perr := time.Parse("2006-01-02T15:04:05Z", latest.CreatedAt); perr == nil {
					digestAge := now.Sub(created)
					if digestAge < time.Duration(cooldownMins)*time.Minute && e.visibleCount < minMsgs {
						skippedCooldown++
						continue
					}
				}
			}
			filtered = append(filtered, e)
		}
		allEntries = filtered
	}
	if skippedCooldown > 0 {
		p.logger.Printf("digest: skipped %d channel(s) (cooldown: digested <%d min ago with <%d messages)", skippedCooldown, cooldownMins, minMsgs)
	}

	if len(allEntries) == 0 {
		p.logger.Println("digest: no channels with visible messages")
		return 0, nil, nil
	}

	// Tiered batching: split channels by activity level for optimal grouping.
	// High-activity channels get individual prompts (better quality).
	// Low-activity channels are grouped aggressively (fewer AI calls).
	maxCh := p.cfg.Digest.BatchMaxChannels
	if maxCh <= 0 {
		maxCh = config.DefaultBatchMaxChannels
	}
	maxMsg := p.cfg.Digest.BatchMaxMessages
	if maxMsg <= 0 {
		maxMsg = config.DefaultBatchMaxMessages
	}

	var highEntries, medEntries, lowEntries []batchEntry
	for _, e := range allEntries {
		switch {
		case e.visibleCount > config.DefaultBatchHighActivityThreshold:
			highEntries = append(highEntries, e)
		case e.visibleCount >= config.DefaultBatchLowActivityThreshold:
			medEntries = append(medEntries, e)
		default:
			lowEntries = append(lowEntries, e)
		}
	}

	// High activity (>50 msgs): individual batch per channel.
	var batches [][]batchEntry
	for _, e := range highEntries {
		batches = append(batches, []batchEntry{e})
	}
	// Medium activity (10-50 msgs): standard limits.
	batches = append(batches, groupIntoBatches(medEntries, maxCh, maxMsg)...)
	// Low activity (<10 msgs): triple channel limit for aggressive grouping.
	batches = append(batches, groupIntoBatches(lowEntries, maxCh*3, maxMsg)...)

	p.logger.Printf("digest: tiered grouping: %d high, %d medium, %d low activity channels",
		len(highEntries), len(medEntries), len(lowEntries))

	// Budget cap: limit total number of AI calls per run.
	maxBatches := config.DefaultMaxBatchesPerRun
	if len(batches) > maxBatches {
		// Sort batches by total message count descending (most active first).
		sort.Slice(batches, func(i, j int) bool {
			iMsgs, jMsgs := 0, 0
			for _, e := range batches[i] {
				iMsgs += e.visibleCount
			}
			for _, e := range batches[j] {
				jMsgs += e.visibleCount
			}
			return iMsgs > jMsgs
		})
		droppedChannels := 0
		for _, b := range batches[maxBatches:] {
			droppedChannels += len(b)
		}
		p.logger.Printf("digest: budget cap: keeping %d of %d batches (%d channels deferred)", maxBatches, len(batches), droppedChannels)
		batches = batches[:maxBatches]
	}

	total := len(allEntries)
	workers := p.cfg.AI.Workers
	if workers <= 0 {
		workers = config.DefaultAIWorkers
	}

	p.logger.Printf("digest: processing %d channels in %d batches with %d workers", total, len(batches), workers)

	if p.OnProgress != nil {
		p.OnProgress(0, total, fmt.Sprintf("Processing %d channels in %d batches...", total, len(batches)))
	}

	batchCh := make(chan []batchEntry, len(batches))
	for _, b := range batches {
		batchCh <- b
	}
	close(batchCh)

	var (
		completed   atomic.Int32
		generated   atomic.Int32
		errCount    atomic.Int32
		totalInput  atomic.Int64
		totalOutput atomic.Int64
		lastErrMu   sync.Mutex
		lastErr     error
		wg          sync.WaitGroup
	)

	for range min(workers, len(batches)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batchCh {
				if ctx.Err() != nil {
					return
				}

				// Single-channel batch: use individual prompt for better quality.
				if len(batch) == 1 {
					e := batch[0]
					c := int(completed.Load())
					p.lastStepMu.Lock()
					p.LastStepMessageCount = len(e.msgs)
					p.LastStepPeriodFrom = time.Unix(int64(sinceUnix), 0)
					p.LastStepPeriodTo = time.Unix(int64(nowUnix), 0)
					p.LastStepDurationSeconds = 0
					p.LastStepInputTokens = 0
					p.LastStepOutputTokens = 0
					if p.OnProgress != nil {
						p.OnProgress(c, total, fmt.Sprintf("#%s (%d msgs)", e.channelName, len(e.msgs)))
					}
					p.lastStepMu.Unlock()

					stepStart := time.Now()
					result, usage, pv, err := p.generateChannelDigest(ctx, e.channelID, e.channelName, e.msgs, sinceUnix, nowUnix)
					if err != nil {
						p.logger.Printf("digest: error generating digest for #%s: %v", e.channelName, err)
						errCount.Add(1)
						lastErrMu.Lock()
						lastErr = err
						lastErrMu.Unlock()
						done := int(completed.Add(1))
						if p.OnProgress != nil {
							p.OnProgress(done, total, fmt.Sprintf("#%s error: %v", e.channelName, err))
						}
						continue
					}

					lastMsgTS := sinceUnix
					for _, m := range e.msgs {
						if m.TSUnix > lastMsgTS {
							lastMsgTS = m.TSUnix
						}
					}

					if err := p.storeDigest(e.channelID, "channel", sinceUnix, lastMsgTS, result, len(e.msgs), usage, pv); err != nil {
						p.logger.Printf("digest: error storing digest for #%s: %v", e.channelName, err)
						errCount.Add(1)
						lastErrMu.Lock()
						lastErr = err
						lastErrMu.Unlock()
						done := int(completed.Add(1))
						if p.OnProgress != nil {
							p.OnProgress(done, total, fmt.Sprintf("#%s store error: %v", e.channelName, err))
						}
						continue
					}

					generated.Add(1)
					p.totalMessageCount.Add(int64(len(e.msgs)))
					p.updatePeriodBounds(sinceUnix, lastMsgTS)

					p.lastStepMu.Lock()
					p.LastStepMessageCount = len(e.msgs)
					p.LastStepPeriodFrom = time.Unix(int64(sinceUnix), 0)
					p.LastStepPeriodTo = time.Unix(int64(lastMsgTS), 0)
					p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
					if usage != nil {
						p.LastStepInputTokens = usage.InputTokens
						p.LastStepOutputTokens = usage.OutputTokens
						totalInput.Add(int64(usage.InputTokens))
						totalOutput.Add(int64(usage.OutputTokens))
						p.accumulateUsage(usage)
					} else {
						p.LastStepInputTokens = 0
						p.LastStepOutputTokens = 0
					}
					done := int(completed.Add(1))
					if p.OnProgress != nil {
						p.OnProgress(done, total, fmt.Sprintf("#%s done", e.channelName))
					}
					p.lastStepMu.Unlock()
					if usage != nil {
						p.logger.Printf("digest: generated for #%s (%d messages, %d+%d tokens)",
							e.channelName, len(e.msgs), usage.InputTokens, usage.OutputTokens)
					} else {
						p.logger.Printf("digest: generated for #%s (%d messages)", e.channelName, len(e.msgs))
					}
					continue
				}

				// Multi-channel batch: use batch prompt.
				batchMsgCount := 0
				for _, e := range batch {
					batchMsgCount += len(e.msgs)
				}

				p.lastStepMu.Lock()
				p.LastStepMessageCount = batchMsgCount
				p.LastStepPeriodFrom = time.Unix(int64(sinceUnix), 0)
				p.LastStepPeriodTo = time.Unix(int64(nowUnix), 0)
				p.LastStepDurationSeconds = 0
				p.LastStepInputTokens = 0
				p.LastStepOutputTokens = 0
				if p.OnProgress != nil {
					c := int(completed.Load())
					p.OnProgress(c, total, fmt.Sprintf("Batch (%d channels, %d msgs)...", len(batch), batchMsgCount))
				}
				p.lastStepMu.Unlock()

				stepStart := time.Now()
				results, usage, pv, err := p.generateBatchDigest(ctx, batch, sinceUnix, nowUnix)
				if err != nil {
					p.logger.Printf("digest: error generating batch (%d channels): %v", len(batch), err)
					errCount.Add(1)
					lastErrMu.Lock()
					lastErr = err
					lastErrMu.Unlock()
					completed.Add(int32(len(batch)))
					continue
				}

				entryMap := make(map[string]*batchEntry, len(batch))
				for i := range batch {
					entryMap[batch[i].channelID] = &batch[i]
				}

				saved := 0
				for rIdx, r := range results {
					entry, ok := entryMap[r.ChannelID]
					if !ok {
						p.logger.Printf("digest: batch result for unknown channel %s, skipping", r.ChannelID)
						continue
					}

					dr := &DigestResult{
						Summary:        r.Summary,
						Topics:         r.Topics,
						RunningSummary: r.RunningSummary,
					}

					lastMsgTS := sinceUnix
					for _, m := range entry.msgs {
						if m.TSUnix > lastMsgTS {
							lastMsgTS = m.TSUnix
						}
					}

					var resultUsage *Usage
					if rIdx == 0 {
						resultUsage = usage
					}

					if err := p.storeDigest(entry.channelID, "channel", sinceUnix, lastMsgTS, dr, len(entry.msgs), resultUsage, pv); err != nil {
						p.logger.Printf("digest: error storing batch digest for #%s: %v", entry.channelName, err)
						errCount.Add(1)
						lastErrMu.Lock()
						lastErr = err
						lastErrMu.Unlock()
						continue
					}

					saved++
					generated.Add(1)
					p.totalMessageCount.Add(int64(len(entry.msgs)))
				}

				if usage != nil {
					totalInput.Add(int64(usage.InputTokens))
					totalOutput.Add(int64(usage.OutputTokens))
					p.accumulateUsage(usage)
				}

				completed.Add(int32(len(batch)))

				p.lastStepMu.Lock()
				p.LastStepMessageCount = batchMsgCount
				p.LastStepPeriodFrom = time.Unix(int64(sinceUnix), 0)
				p.LastStepPeriodTo = time.Unix(int64(nowUnix), 0)
				p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
				if usage != nil {
					p.LastStepInputTokens = usage.InputTokens
					p.LastStepOutputTokens = usage.OutputTokens
				}
				if p.OnProgress != nil {
					done := int(completed.Load())
					p.OnProgress(done, total, fmt.Sprintf("Batch done (%d channels, %d saved)", len(batch), saved))
				}
				p.lastStepMu.Unlock()

				p.logger.Printf("digest: batch: %d channels, %d results from AI, %d saved",
					len(batch), len(results), saved)
			}
		}()
	}

	wg.Wait()

	_, _, _, accAPITokens := p.AccumulatedUsage()
	totalUsage := &Usage{
		InputTokens:    int(totalInput.Load()),
		OutputTokens:   int(totalOutput.Load()),
		CostUSD:        0,
		TotalAPITokens: accAPITokens,
	}

	gen := int(generated.Load())
	errs := int(errCount.Load())

	// If we found channels to process but ALL of them failed, report the error
	// so the caller shows a meaningful message instead of "No new digests needed".
	if gen == 0 && errs > 0 {
		return 0, totalUsage, fmt.Errorf("all %d channel digest(s) failed, last error: %w", errs, lastErr)
	}

	return gen, totalUsage, nil
}

// RunDailyRollup generates a cross-channel daily digest from today's channel digests.
// M12 fix: use UTC for consistent timezone-independent digest deduplication.
func (p *Pipeline) RunDailyRollup(ctx context.Context) error {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return p.runDailyRollupForDate(ctx, dayStart)
}

// runDailyRollupForDate generates a daily rollup for the given date.
func (p *Pipeline) runDailyRollupForDate(ctx context.Context, dayStart time.Time) error {
	dayEnd := dayStart.Add(24*time.Hour - time.Second)
	fromUnix := float64(dayStart.Unix())
	toUnix := float64(dayEnd.Unix())

	channelDigests, err := p.db.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: fromUnix,
		ToUnix:   toUnix,
	})
	if err != nil {
		return fmt.Errorf("getting channel digests for %s: %w", dayStart.Format("2006-01-02"), err)
	}

	if len(channelDigests) < 2 {
		return nil // not enough data for a rollup
	}

	var sb strings.Builder
	for _, d := range channelDigests {
		name := p.channelName(d.ChannelID)
		// Sanitize AI-generated values to prevent prompt injection via prior AI output
		summary := sanitizePromptValue(d.Summary)
		fmt.Fprintf(&sb, "### #%s (%d messages)\nSummary: %s\n", name, d.MessageCount, summary)
		// Include topics with their decisions for the rollup
		topics, _ := p.db.GetDigestTopics(d.ID)
		if len(topics) > 0 {
			fmt.Fprintf(&sb, "Topics:\n")
			for _, t := range topics {
				fmt.Fprintf(&sb, "- %s: %s\n", sanitizePromptValue(t.Title), sanitizePromptValue(t.Summary))
				if t.Decisions != "" && t.Decisions != "[]" {
					fmt.Fprintf(&sb, "  Decisions: %s\n", sanitizePromptValue(t.Decisions))
				}
			}
		} else if d.Decisions != "" && d.Decisions != "[]" {
			// Fallback for old digests without topics
			fmt.Fprintf(&sb, "Decisions: %s\n", sanitizePromptValue(d.Decisions))
		}
		sb.WriteString("\n")
	}

	// Prepend chain context if available (decisions grouped into chains are shown
	// as chain updates rather than repeated individually).
	channelInput := sb.String()
	if p.TrackContext != "" {
		channelInput = p.TrackContext + "\n" + channelInput
	}

	previousContext := p.loadPreviousContext("", "daily")

	dateStr := dayStart.Format("2006-01-02")
	tmpl, pv := p.getPrompt(prompts.DigestDaily, dailyRollupPrompt)
	fullPrompt := fmt.Sprintf(tmpl, dateStr, p.formatProfileContext(), p.languageInstruction(), previousContext, channelInput)
	systemPrompt, userMessage := SplitPromptAtData(fullPrompt)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.daily"), systemPrompt, userMessage, "")
	if err != nil {
		return fmt.Errorf("generating daily rollup: %w", err)
	}
	p.accumulateUsage(usage)

	result, err := parseDigestResult(raw)
	if err != nil {
		return fmt.Errorf("parsing daily rollup: %w", err)
	}

	totalMsgs := 0
	for _, d := range channelDigests {
		totalMsgs += d.MessageCount
	}

	return p.storeDigest("", "daily", fromUnix, toUnix, result, totalMsgs, usage, pv)
}

// RunWeeklyTrends generates a weekly trends digest from daily rollups.
func (p *Pipeline) RunWeeklyTrends(ctx context.Context) error {
	now := time.Now()
	weekStart := now.AddDate(0, 0, -7)

	dailies, err := p.db.GetDigests(db.DigestFilter{
		Type:     "daily",
		FromUnix: float64(weekStart.Unix()),
	})
	if err != nil {
		return fmt.Errorf("getting weekly dailies: %w", err)
	}

	if len(dailies) < 2 {
		return nil
	}

	var sb strings.Builder
	for _, d := range dailies {
		date := time.Unix(int64(d.PeriodFrom), 0).Local().Format("2006-01-02")
		summary := sanitizePromptValue(d.Summary)
		fmt.Fprintf(&sb, "### %s (%d messages)\nSummary: %s\n", date, d.MessageCount, summary)
		topics, _ := p.db.GetDigestTopics(d.ID)
		if len(topics) > 0 {
			fmt.Fprintf(&sb, "Topics:\n")
			for _, t := range topics {
				fmt.Fprintf(&sb, "- %s: %s\n", sanitizePromptValue(t.Title), sanitizePromptValue(t.Summary))
				if t.Decisions != "" && t.Decisions != "[]" {
					fmt.Fprintf(&sb, "  Decisions: %s\n", sanitizePromptValue(t.Decisions))
				}
			}
		} else if d.Decisions != "" && d.Decisions != "[]" {
			fmt.Fprintf(&sb, "Decisions: %s\n", sanitizePromptValue(d.Decisions))
		}
		sb.WriteString("\n")
	}

	previousContext := p.loadPreviousContext("", "weekly")

	fromStr := weekStart.Format("2006-01-02")
	toStr := now.Format("2006-01-02")
	tmpl, pv := p.getPrompt(prompts.DigestWeekly, weeklyTrendsPrompt)
	fullPrompt := fmt.Sprintf(tmpl, now.Format("2006-01-02"), fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), previousContext, sb.String())
	systemPrompt, userMessage := SplitPromptAtData(fullPrompt)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.weekly"), systemPrompt, userMessage, "")
	if err != nil {
		return fmt.Errorf("generating weekly trends: %w", err)
	}
	p.accumulateUsage(usage)

	result, err := parseDigestResult(raw)
	if err != nil {
		return fmt.Errorf("parsing weekly trends: %w", err)
	}

	// Normalize weekStart to midnight for consistent upsert key
	weekStartNorm := time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	fromUnix := float64(weekStartNorm.Unix())
	toUnix := float64(dayEnd.Unix())
	totalMsgs := 0
	for _, d := range dailies {
		totalMsgs += d.MessageCount
	}

	return p.storeDigest("", "weekly", fromUnix, toUnix, result, totalMsgs, usage, pv)
}

// RunPeriodSummary generates a summary across all digests in the given time range.
// Unlike other Run* methods, it returns the result directly instead of storing it,
// so it can be printed immediately by the CLI.
func (p *Pipeline) RunPeriodSummary(ctx context.Context, from, to time.Time) (*DigestResult, *Usage, error) {
	p.loadCaches()

	fromUnix := float64(from.Unix())
	toUnix := float64(to.Unix())

	digests, err := p.db.GetDigests(db.DigestFilter{
		FromUnix: fromUnix,
		ToUnix:   toUnix,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("querying digests: %w", err)
	}

	if len(digests) == 0 {
		return nil, nil, fmt.Errorf("no digests found for %s to %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}

	var sb strings.Builder
	for _, d := range digests {
		var label string
		switch d.Type {
		case "channel":
			label = "#" + p.channelName(d.ChannelID)
		case "daily":
			label = "Daily rollup"
		case "weekly":
			label = "Weekly trends"
		}
		date := time.Unix(int64(d.PeriodFrom), 0).Local().Format("2006-01-02")
		fmt.Fprintf(&sb, "### %s — %s (%d messages)\n%s\n", date, label, d.MessageCount, sanitizePromptValue(d.Summary))
		topics, _ := p.db.GetDigestTopics(d.ID)
		if len(topics) > 0 {
			for _, t := range topics {
				fmt.Fprintf(&sb, "- %s: %s\n", sanitizePromptValue(t.Title), sanitizePromptValue(t.Summary))
			}
		}
		sb.WriteString("\n")
	}

	fromStr := from.Format("2006-01-02")
	toStr := to.Format("2006-01-02")
	tmpl, _ := p.getPrompt(prompts.DigestPeriod, periodSummaryPrompt)
	fullPrompt := fmt.Sprintf(tmpl, fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), sb.String())
	systemPrompt, userMessage := SplitPromptAtData(fullPrompt)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.period"), systemPrompt, userMessage, "")
	if err != nil {
		return nil, nil, fmt.Errorf("generating period summary: %w", err)
	}

	result, err := parseDigestResult(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing period summary: %w", err)
	}

	return result, usage, nil
}

// loadPreviousContext loads the running summary from the latest digest for the
// given channel/type and formats it as a prompt section. Returns empty string
// if no previous context exists, if the context is too old (>30 days), or on error.
// Context older than 7 days is included with an "(outdated)" warning.
func (p *Pipeline) loadPreviousContext(channelID, digestType string) string {
	result, err := p.db.GetLatestRunningSummaryWithAge(channelID, digestType)
	if err != nil {
		p.logger.Printf("digest: warning: failed to load running summary for %s/%s: %v", channelID, digestType, err)
		return ""
	}
	if result == nil || result.Summary == "" {
		return ""
	}

	// TTL: >30 days — don't use at all
	if result.AgeDays > 30 {
		return ""
	}

	var section strings.Builder
	section.WriteString("\n=== PREVIOUS CONTEXT ===\n")

	// TTL: >7 days — mark as outdated
	if result.AgeDays > 7 {
		fmt.Fprintf(&section, "(outdated, from %.0f days ago)\n", result.AgeDays)
	}

	section.WriteString(result.Summary)
	section.WriteString("\n\nRules for PREVIOUS CONTEXT:\n")
	section.WriteString("- Use PREVIOUS CONTEXT to detect continuity: evolving topics, resolved questions, changed decisions.\n")
	section.WriteString("- Say \"continues from [date]\" for ongoing topics, \"resolved since [date]\" for closed ones.\n")
	section.WriteString("- Do NOT repeat decisions/topics from PREVIOUS CONTEXT unless their status changed.\n")
	section.WriteString("- Generate an updated running_summary reflecting the current state after this analysis.\n")

	return section.String()
}

// generateChannelDigest returns the parsed result, usage, prompt version, and error.
func (p *Pipeline) generateChannelDigest(ctx context.Context, channelID, channelName string, msgs []db.Message, from, to float64) (*DigestResult, *Usage, int, error) {
	// Sort messages chronologically (oldest first) for natural reading
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].TSUnix < msgs[j].TSUnix })

	// Load reactions for these messages.
	tss := make([]string, len(msgs))
	for i, m := range msgs {
		tss[i] = m.TS
	}
	reactionMap, _ := p.db.GetReactionsForMessages(channelID, tss)

	formatted := p.formatMessages(msgs, reactionMap)
	if strings.TrimSpace(formatted) == "" {
		return nil, nil, 0, fmt.Errorf("no visible messages after filtering (all empty or deleted)")
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02 15:04")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02 15:04")

	// Skip running context for low-activity channels — context often outweighs messages.
	previousContext := ""
	visible := 0
	for _, m := range msgs {
		if m.Text != "" && !m.IsDeleted {
			visible++
		}
	}
	if visible >= config.DefaultBatchLowActivityThreshold {
		previousContext = p.loadPreviousContext(channelID, "channel")
	}

	tmpl, pv := p.getPrompt(prompts.DigestChannel, channelDigestPrompt)
	fullPrompt := fmt.Sprintf(tmpl, channelName, fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), previousContext, formatted)

	// Split into system prompt (instructions) and user message (data).
	// This enables Claude API prompt caching for the instruction part.
	systemPrompt, userMessage := SplitPromptAtData(fullPrompt)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.channel"), systemPrompt, userMessage, "")
	if err != nil {
		return nil, nil, 0, fmt.Errorf("claude call failed: %w", err)
	}

	result, err := parseDigestResult(raw)
	return result, usage, pv, err
}

func (p *Pipeline) storeDigest(channelID, digestType string, from, to float64, result *DigestResult, msgCount int, usage *Usage, promptVersion int) error {
	// Aggregate topics into flat arrays for legacy columns (backward compat).
	var allTopicTitles []string
	var allDecisions []Decision
	var allActionItems []ActionItem
	var allSituations []db.Situation
	for _, t := range result.Topics {
		allTopicTitles = append(allTopicTitles, t.Title)
		allDecisions = append(allDecisions, t.Decisions...)
		allActionItems = append(allActionItems, t.ActionItems...)
		allSituations = append(allSituations, t.Situations...)
	}

	topics, _ := json.Marshal(allTopicTitles)
	decisions, _ := json.Marshal(allDecisions)
	actionItems, _ := json.Marshal(allActionItems)
	situations, _ := json.Marshal(allSituations)

	// Store running_summary as-is (json.RawMessage → string)
	runningSummary := ""
	if len(result.RunningSummary) > 0 {
		runningSummary = string(result.RunningSummary)
	}

	d := db.Digest{
		ChannelID:      channelID,
		Type:           digestType,
		PeriodFrom:     from,
		PeriodTo:       to,
		Summary:        result.Summary,
		Topics:         string(topics),
		Decisions:      string(decisions),
		ActionItems:    string(actionItems),
		PeopleSignals:  "[]",
		Situations:     string(situations),
		RunningSummary: runningSummary,
		MessageCount:   msgCount,
		Model:          "auto",
		PromptVersion:  promptVersion,
	}
	if usage != nil {
		d.Model = usage.Model
		d.InputTokens = usage.InputTokens
		d.OutputTokens = usage.OutputTokens
		d.CostUSD = 0
	}

	digestID, err := p.db.UpsertDigest(d)
	if err != nil {
		return err
	}

	// Store structured topics in digest_topics table.
	if len(result.Topics) > 0 {
		var dbTopics []db.DigestTopic
		for i, t := range result.Topics {
			dec, _ := json.Marshal(t.Decisions)
			ai, _ := json.Marshal(t.ActionItems)
			sit, _ := json.Marshal(t.Situations)
			km, _ := json.Marshal(filterValidTimestamps(t.KeyMessages))
			dbTopics = append(dbTopics, db.DigestTopic{
				Idx:         i,
				Title:       t.Title,
				Summary:     t.Summary,
				Decisions:   string(dec),
				ActionItems: string(ai),
				Situations:  string(sit),
				KeyMessages: string(km),
			})
		}
		if err := p.db.InsertDigestTopics(digestID, dbTopics); err != nil {
			p.logger.Printf("warning: failed to store digest topics: %v", err)
		}
	}

	return nil
}

// updatePeriodBounds atomically updates the earliest/latest period bounds.
func (p *Pipeline) updatePeriodBounds(sinceUnix, lastMsgTS float64) {
	sinceTS := int64(sinceUnix)
	for {
		old := p.earliestPeriodFrom.Load()
		if old != 0 && old <= sinceTS {
			break
		}
		if p.earliestPeriodFrom.CompareAndSwap(old, sinceTS) {
			break
		}
	}
	lastTS := int64(lastMsgTS)
	for {
		old := p.latestPeriodTo.Load()
		if old >= lastTS {
			break
		}
		if p.latestPeriodTo.CompareAndSwap(old, lastTS) {
			break
		}
	}
}

// batchEntry holds a channel with its messages for batch processing.
type batchEntry struct {
	channelID    string
	channelName  string
	msgs         []db.Message
	visibleCount int
}

// BatchChannelResult is the per-channel result from a batch digest LLM call.
type BatchChannelResult struct {
	ChannelID      string          `json:"channel_id"`
	Summary        string          `json:"summary"`
	Topics         []Topic         `json:"topics"`
	RunningSummary json.RawMessage `json:"running_summary,omitempty"`
}

// UnmarshalJSON handles both structured topics ([]Topic) and flat topics ([]string).
func (b *BatchChannelResult) UnmarshalJSON(data []byte) error {
	type Alias BatchChannelResult
	var raw struct {
		Alias
		Topics json.RawMessage `json:"topics"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*b = BatchChannelResult(raw.Alias)
	// Try structured topics first.
	if err := json.Unmarshal(raw.Topics, &b.Topics); err != nil {
		// Fall back to string topics.
		var titles []string
		if err2 := json.Unmarshal(raw.Topics, &titles); err2 == nil {
			for _, t := range titles {
				b.Topics = append(b.Topics, Topic{Title: t})
			}
		}
	}
	return nil
}

// groupIntoBatches groups entries into batches not exceeding maxChannels and maxMessages.
// If maxChannels <= 0, all entries go into a single batch (no limit).
func groupIntoBatches(entries []batchEntry, maxChannels, maxMessages int) [][]batchEntry {
	if len(entries) == 0 {
		return nil
	}
	if maxChannels <= 0 {
		return [][]batchEntry{entries}
	}

	var batches [][]batchEntry
	var current []batchEntry
	currentMsgs := 0

	for _, e := range entries {
		// Start a new batch if adding this entry would exceed limits.
		if len(current) > 0 && (len(current) >= maxChannels || (maxMessages > 0 && currentMsgs+e.visibleCount > maxMessages)) {
			batches = append(batches, current)
			current = nil
			currentMsgs = 0
		}
		current = append(current, e)
		currentMsgs += e.visibleCount
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// parseBatchDigestResult parses the JSON array returned by a batch digest LLM call.
// Filters out entries with empty ChannelID or Summary.
func parseBatchDigestResult(raw string) ([]BatchChannelResult, error) {
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

	var results []BatchChannelResult
	if err := json.Unmarshal([]byte(cleaned), &results); err != nil {
		return nil, fmt.Errorf("parsing batch digest JSON: %w (raw: %.200s)", err, raw)
	}

	// Filter out entries with missing required fields.
	var filtered []BatchChannelResult
	for _, r := range results {
		if r.ChannelID != "" && r.Summary != "" {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// generateBatchDigest generates a single LLM call for multiple low-activity channels.
// Returns per-channel results, combined usage, prompt version, and error.
func (p *Pipeline) generateBatchDigest(ctx context.Context, entries []batchEntry, from, to float64) ([]BatchChannelResult, *Usage, int, error) {
	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02 15:04")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02 15:04")

	var channelBlocks strings.Builder
	var previousContexts strings.Builder
	totalMsgs := 0

	for _, e := range entries {
		// Sort messages chronologically
		sort.Slice(e.msgs, func(i, j int) bool { return e.msgs[i].TSUnix < e.msgs[j].TSUnix })

		// Load reactions
		tss := make([]string, len(e.msgs))
		for i, m := range e.msgs {
			tss[i] = m.TS
		}
		reactionMap, _ := p.db.GetReactionsForMessages(e.channelID, tss)

		formatted := p.formatMessages(e.msgs, reactionMap)
		if strings.TrimSpace(formatted) == "" {
			continue
		}

		// Skip running context for low-activity channels — context often outweighs messages.
		prevCtx := ""
		if e.visibleCount >= config.DefaultBatchLowActivityThreshold {
			prevCtx = p.loadPreviousContext(e.channelID, "channel")
		}

		fmt.Fprintf(&channelBlocks, "--- #%s (%s) ---\n", e.channelName, e.channelID)
		if prevCtx != "" {
			// Strip the leading "=== PREVIOUS CONTEXT ===" header since we embed it differently
			prevCtx = strings.TrimPrefix(prevCtx, "\n=== PREVIOUS CONTEXT ===\n")
			fmt.Fprintf(&channelBlocks, "[Previous context]\n%s\n", prevCtx)
		}
		channelBlocks.WriteString(formatted)
		channelBlocks.WriteString("\n")
		totalMsgs += len(e.msgs)
	}

	if totalMsgs == 0 {
		return nil, nil, 0, fmt.Errorf("no visible messages in batch")
	}

	prevCtxNote := ""
	if previousContexts.Len() > 0 {
		prevCtxNote = previousContexts.String()
	}

	tmpl, pv := p.getPrompt(prompts.DigestChannelBatch, channelBatchDigestPrompt)
	fullPrompt := fmt.Sprintf(tmpl, fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), prevCtxNote, channelBlocks.String())

	systemPrompt, userMessage := SplitPromptAtData(fullPrompt)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.channel_batch"), systemPrompt, userMessage, "")
	if err != nil {
		return nil, nil, 0, fmt.Errorf("claude batch call failed: %w", err)
	}

	results, err := parseBatchDigestResult(raw)
	return results, usage, pv, err
}

// isFirstRun checks if there are any existing channel digests in the DB.
func (p *Pipeline) isFirstRun() bool {
	digests, err := p.db.GetDigests(db.DigestFilter{Type: "channel", Limit: 1})
	return err != nil || len(digests) == 0
}

func (p *Pipeline) lastDigestTime() float64 {
	// Find the latest channel digest period_to
	digests, err := p.db.GetDigests(db.DigestFilter{Type: "channel", Limit: 1})
	if err == nil && len(digests) > 0 {
		t := time.Unix(int64(digests[0].PeriodTo), 0)
		p.logger.Printf("digest: last digest time: %s", t.Format("2006-01-02 15:04"))
		return digests[0].PeriodTo
	}
	// First run: use initial_history_days from config (set during onboarding)
	days := p.cfg.Sync.InitialHistoryDays
	if days <= 0 {
		days = config.DefaultInitialHistDays
	}
	since := float64(time.Now().AddDate(0, 0, -days).Unix())
	p.logger.Printf("digest: first run — looking back %d days, since=%s (%.0f)", days, time.Unix(int64(since), 0).Format("2006-01-02 15:04"), since)
	return since
}

func (p *Pipeline) formatMessages(msgs []db.Message, reactions map[string][]db.ReactionSummary) string {
	truncateLimit := config.DefaultMessageTruncateLen

	sanitizeText := func(text string) string {
		if strings.Contains(text, "===") || strings.Contains(text, "---") {
			text = strings.ReplaceAll(text, "===", "= = =")
			text = strings.ReplaceAll(text, "---", "- - -")
		}
		// Truncate very long messages to save input tokens.
		if truncateLimit > 0 && len(text) > truncateLimit {
			text = text[:truncateLimit] + "... [truncated]"
		}
		return text
	}

	// Build thread index: parentTS → replies (only actual replies, not self-referencing parents).
	threadReplies := map[string][]db.Message{}
	parentInBatch := map[string]bool{}
	for i := range msgs {
		m := &msgs[i]
		if m.Text == "" || m.IsDeleted {
			continue
		}
		if m.ThreadTS.Valid && m.ThreadTS.String != m.TS {
			threadReplies[m.ThreadTS.String] = append(threadReplies[m.ThreadTS.String], *m)
		}
	}
	// Identify parents present in this batch.
	for _, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		if !m.ThreadTS.Valid || m.ThreadTS.String == m.TS {
			if _, ok := threadReplies[m.TS]; ok {
				parentInBatch[m.TS] = true
			}
		}
	}

	var sb strings.Builder
	emitted := map[string]bool{}
	for _, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		// Thread reply — skip if parent is in batch (will be emitted with parent).
		if m.ThreadTS.Valid && m.ThreadTS.String != m.TS {
			parentTS := m.ThreadTS.String
			if emitted[parentTS] || parentInBatch[parentTS] {
				continue
			}
			// Orphan replies: parent not in batch — emit group once.
			emitted[parentTS] = true
			for _, r := range threadReplies[parentTS] {
				userName := p.userName(r.UserID)
				ts := time.Unix(int64(r.TSUnix), 0).Local().Format("15:04")
				reactStr := db.FormatReactions(reactions[r.TS])
				fmt.Fprintf(&sb, "  ↳ [%s @%s (%s)] %s%s\n", ts, userName, r.UserID, sanitizeText(r.Text), reactStr)
			}
			continue
		}
		// Top-level or thread parent.
		userName := p.userName(m.UserID)
		ts := time.Unix(int64(m.TSUnix), 0).Local().Format("15:04")
		reactStr := db.FormatReactions(reactions[m.TS])
		fmt.Fprintf(&sb, "[%s @%s (%s)] %s%s\n", ts, userName, m.UserID, sanitizeText(m.Text), reactStr)
		// Emit grouped replies if this is a thread parent.
		if replies, ok := threadReplies[m.TS]; ok {
			emitted[m.TS] = true
			for _, r := range replies {
				rUserName := p.userName(r.UserID)
				rTS := time.Unix(int64(r.TSUnix), 0).Local().Format("15:04")
				rReactStr := db.FormatReactions(reactions[r.TS])
				fmt.Fprintf(&sb, "  ↳ [%s @%s (%s)] %s%s\n", rTS, rUserName, r.UserID, sanitizeText(r.Text), rReactStr)
			}
		}
	}
	return sb.String()
}

// SplitPromptAtData splits a formatted prompt into system prompt (instructions)
// and user message (data) at the "=== " delimiter. This enables API-level prompt
// caching for the instruction part. If no delimiter is found, everything goes as
// user message with an empty system prompt.
func SplitPromptAtData(prompt string) (systemPrompt, userMessage string) {
	// Look for the data section delimiter (=== MESSAGES ===, === CHANNEL DIGESTS ===, etc.)
	markers := []string{"=== MESSAGES ===", "=== CHANNELS ===", "=== CHANNEL DIGESTS ===", "=== DAILY DIGESTS ===", "=== DIGESTS ==="}
	for _, marker := range markers {
		if idx := strings.LastIndex(prompt, marker); idx > 0 {
			return strings.TrimSpace(prompt[:idx]), prompt[idx:]
		}
	}
	return "", prompt
}

// extractHumanContext filters messages from a bot-heavy channel to keep only
// human messages and their surrounding context (thread siblings, nearby bot messages).
// This avoids sending hundreds of bot alerts to AI while preserving human interactions.
func (p *Pipeline) extractHumanContext(msgs []db.Message) []db.Message {
	const contextWindow = 3 // bot messages before/after each human message

	// Index messages by position and build thread groups.
	type indexedMsg struct {
		idx int
		msg db.Message
	}
	threadMsgs := make(map[string][]indexedMsg) // threadTS → messages in that thread
	keep := make(map[int]bool)                  // indices to keep

	for i, m := range msgs {
		if m.ThreadTS.Valid && m.ThreadTS.String != "" {
			threadMsgs[m.ThreadTS.String] = append(threadMsgs[m.ThreadTS.String], indexedMsg{i, m})
		}
	}

	for i, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		if m.UserID == "" || p.botUserIDs[m.UserID] {
			continue
		}
		// Human message found. Keep it.
		keep[i] = true

		// If it's in a thread — keep all messages in that thread (parent + replies).
		threadKey := ""
		if m.ThreadTS.Valid && m.ThreadTS.String != "" {
			threadKey = m.ThreadTS.String
		} else if m.ReplyCount > 0 {
			// This is a thread parent.
			threadKey = m.TS
		}
		if threadKey != "" {
			// Keep thread parent.
			for j, other := range msgs {
				if other.TS == threadKey {
					keep[j] = true
					break
				}
			}
			// Keep all thread messages.
			for _, tm := range threadMsgs[threadKey] {
				keep[tm.idx] = true
			}
		}

		// Keep surrounding messages for context (contextWindow before, contextWindow after).
		for d := 1; d <= contextWindow; d++ {
			if i-d >= 0 {
				keep[i-d] = true
			}
			if i+d < len(msgs) {
				keep[i+d] = true
			}
		}
	}

	result := make([]db.Message, 0, len(keep))
	for i, m := range msgs {
		if keep[i] {
			result = append(result, m)
		}
	}
	return result
}

func (p *Pipeline) loadCaches() {
	p.channelNames = make(map[string]string)
	p.channelTypes = make(map[string]string)
	p.userNames = make(map[string]string)
	p.botUserIDs = make(map[string]bool)

	// Load user profile for personalized digests.
	if userID, err := p.db.GetCurrentUserID(); err == nil && userID != "" {
		if profile, err := p.db.GetUserProfile(userID); err == nil {
			p.profile = profile
		}
	}

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
			if u.IsBot {
				p.botUserIDs[u.ID] = true
			}
		}
	}

	// Also treat muted users as bots — their messages are excluded from AI analysis.
	mutedUserIDs, err := p.db.GetMutedUserIDs()
	if err != nil {
		p.logger.Printf("warning: failed to load muted users: %v", err)
	} else {
		for _, id := range mutedUserIDs {
			p.botUserIDs[id] = true
		}
		if len(mutedUserIDs) > 0 {
			p.logger.Printf("digest: %d muted user(s) excluded from analysis", len(mutedUserIDs))
		}
	}

	channels, err := p.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load channel names: %v", err)
	} else {
		for _, ch := range channels {
			name := ch.Name
			p.channelTypes[ch.ID] = ch.Type
			// For DMs, show the other user's display name instead of raw ID
			if ch.Type == "dm" || ch.Type == "im" {
				uid := ""
				if ch.DMUserID.Valid {
					uid = ch.DMUserID.String
				} else {
					uid = ch.Name // name is often the user ID for DMs
				}
				if userName, ok := p.userNames[uid]; ok {
					name = "DM: " + userName
				}
			}
			p.channelNames[ch.ID] = name
		}
	}
}

// formatProfileContext builds the profile context section for digest prompts.
// Returns personalization hints so the AI focuses on what matters to the user.
func (p *Pipeline) formatProfileContext() string {
	if p.profile == nil || p.profile.CustomPromptContext == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== USER PROFILE CONTEXT ===\n")
	sb.WriteString(sanitizePromptValue(p.profile.CustomPromptContext))
	sb.WriteString("\n\nPERSONALIZATION RULES:\n")
	sb.WriteString("- Prioritize decisions and action items relevant to this user's role and responsibilities\n")
	sb.WriteString("- Highlight topics that fall within the user's area of focus\n")

	if p.profile.StarredChannels != "" && p.profile.StarredChannels != "[]" {
		sb.WriteString(fmt.Sprintf("\nSTARRED CHANNELS: %s — provide more detail for these channels, lower threshold for including topics\n", sanitizePromptValue(p.profile.StarredChannels)))
	}
	if p.profile.StarredPeople != "" && p.profile.StarredPeople != "[]" {
		sb.WriteString(fmt.Sprintf("\nSTARRED PEOPLE: %s — highlight decisions and actions by these people\n", sanitizePromptValue(p.profile.StarredPeople)))
	}
	if p.profile.Reports != "" && p.profile.Reports != "[]" {
		sb.WriteString(fmt.Sprintf("\nMY REPORTS: %s — flag action items assigned to these people\n", sanitizePromptValue(p.profile.Reports)))
	}

	return sb.String()
}

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: You MUST write ALL text values (summary, topics, decisions, action_items) in %s. Do NOT use English for any text content.", lang)
	}
	if lang := p.cfg.Digest.Language; lang != "" {
		return "Write all text values in English"
	}
	return "Write in the language most commonly used in the messages"
}

func (p *Pipeline) channelName(id string) string {
	if p.channelNames != nil {
		if name, ok := p.channelNames[id]; ok {
			return sanitizePromptValue(name)
		}
	}
	return id
}

func (p *Pipeline) userName(id string) string {
	if p.userNames != nil {
		if name, ok := p.userNames[id]; ok {
			return sanitizePromptValue(name)
		}
	}
	return id
}

// sanitizePromptValue prevents prompt injection via delimiter spoofing in names.
func sanitizePromptValue(text string) string {
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

// reSlackTS matches a valid Slack message timestamp (e.g. "1774788718.201299").
var reSlackTS = regexp.MustCompile(`^\d{10}\.\d{6}$`)

// filterValidTimestamps removes entries from key_messages that are not valid
// Slack timestamps. AI sometimes returns human-readable text instead.
func filterValidTimestamps(msgs []string) []string {
	filtered := msgs[:0]
	for _, m := range msgs {
		if reSlackTS.MatchString(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// parseDigestResult extracts a DigestResult from Claude's response.
// Handles cases where JSON may be wrapped in markdown fences.
func parseDigestResult(raw string) (*DigestResult, error) {
	// Try to extract JSON from markdown fences
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

	// Try to find JSON object boundaries
	if start := strings.Index(cleaned, "{"); start >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end > start {
			cleaned = cleaned[start : end+1]
		}
	}

	// Use a lenient intermediate format that won't fail on mixed topic types.
	var lenient struct {
		Summary        string          `json:"summary"`
		Topics         json.RawMessage `json:"topics"`
		Decisions      json.RawMessage `json:"decisions"`
		ActionItems    json.RawMessage `json:"action_items"`
		Situations     json.RawMessage `json:"situations"`
		KeyMessages    json.RawMessage `json:"key_messages"`
		RunningSummary json.RawMessage `json:"running_summary,omitempty"`
	}
	if err := json.Unmarshal([]byte(cleaned), &lenient); err != nil {
		return nil, fmt.Errorf("parsing digest JSON: %w (raw: %.500s)", err, raw)
	}

	result := DigestResult{
		Summary:        lenient.Summary,
		RunningSummary: lenient.RunningSummary,
	}

	// Try topics as structured []Topic first, fall back to []string.
	var structuredTopics []Topic
	if err := json.Unmarshal(lenient.Topics, &structuredTopics); err == nil {
		result.Topics = structuredTopics
	} else {
		var stringTopics []string
		if err2 := json.Unmarshal(lenient.Topics, &stringTopics); err2 == nil {
			// Flat format: topics are strings, decisions/action_items/situations at top level.
			var decisions []Decision
			var actionItems []ActionItem
			var situations []db.Situation
			var keyMessages []string
			_ = json.Unmarshal(lenient.Decisions, &decisions)
			_ = json.Unmarshal(lenient.ActionItems, &actionItems)
			_ = json.Unmarshal(lenient.Situations, &situations)
			_ = json.Unmarshal(lenient.KeyMessages, &keyMessages)

			for i, title := range stringTopics {
				t := Topic{Title: title}
				if i == 0 {
					t.Summary = result.Summary
					t.Decisions = decisions
					t.ActionItems = actionItems
					t.Situations = situations
					t.KeyMessages = keyMessages
				}
				result.Topics = append(result.Topics, t)
			}
		}
		// If both fail, proceed with empty topics — summary is still valuable.
	}

	return &result, nil
}
