// Package digest provides digest generation and pipeline for summarizing workspace conversations.
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

// Usage holds token and cost metrics from an AI generation call.
type Usage struct {
	InputTokens    int     // Our prompt tokens (estimated from prompt size)
	OutputTokens   int     // AI response tokens
	CostUSD        float64 // Actual cost from CLI (includes all caching)
	TotalAPITokens int     // Total tokens API processed (input + cache_read + cache_creation)
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
	totalCostMicro    atomic.Int64 // cost * 1e6 for atomic ops
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
	LastStepCostUSD         float64

	// caches populated during a run
	channelNames map[string]string
	userNames    map[string]string
	profile      *db.UserProfile // loaded once per Run, nil if not available
}

// AccumulatedUsage returns the total token usage accumulated across all Generate calls.
// Returns (inputTokens, outputTokens, costUSD, overheadTokens).
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()), float64(p.totalCostMicro.Load()) / 1e6, int(p.totalAPITokens.Load())
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
	p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
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
	p.LastStepCostUSD = 0
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

	if len(channels) == 0 {
		p.logger.Println("digest: no channels with new messages")
		return 0, nil, nil
	}

	// Filter channels by min_messages and pre-fetch messages
	type channelTask struct {
		channelID   string
		channelName string
		msgs        []db.Message
	}
	var tasks []channelTask
	skippedBelowMin := 0
	skippedNoVisible := 0
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
		// Count only messages with visible text (skip empty/deleted).
		visible := 0
		for _, m := range msgs {
			if m.Text != "" && !m.IsDeleted {
				visible++
			}
		}
		if visible < p.cfg.Digest.MinMessages {
			if visible == 0 && len(msgs) >= p.cfg.Digest.MinMessages {
				skippedNoVisible++
			} else {
				skippedBelowMin++
			}
			continue
		}
		tasks = append(tasks, channelTask{
			channelID:   channelID,
			channelName: p.channelName(channelID),
			msgs:        msgs,
		})
	}

	p.logger.Printf("digest: %d channels pass min_messages=%d, %d skipped (below threshold), %d skipped (no visible text)",
		len(tasks), p.cfg.Digest.MinMessages, skippedBelowMin, skippedNoVisible)

	if len(tasks) == 0 {
		p.logger.Println("digest: no channels above min_messages threshold")
		return 0, nil, nil
	}

	total := len(tasks)
	workers := p.cfg.AI.Workers
	if workers <= 0 {
		workers = config.DefaultAIWorkers
	}
	if workers > total {
		workers = total
	}

	p.logger.Printf("digest: processing %d channels with %d workers", total, workers)

	if p.OnProgress != nil {
		p.OnProgress(0, total, fmt.Sprintf("Processing %d channels (%d workers)...", total, workers))
	}

	taskCh := make(chan channelTask, total)
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	var (
		completed   atomic.Int32
		generated   atomic.Int32
		errCount    atomic.Int32
		totalInput  atomic.Int64
		totalOutput atomic.Int64
		totalCostU  atomic.Int64 // cost * 1e6
		lastErrMu   sync.Mutex
		lastErr     error
		wg          sync.WaitGroup
	)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				if ctx.Err() != nil {
					return
				}

				c := int(completed.Load())
				p.lastStepMu.Lock()
				p.LastStepMessageCount = len(t.msgs)
				p.LastStepPeriodFrom = time.Unix(int64(sinceUnix), 0)
				p.LastStepPeriodTo = time.Unix(int64(nowUnix), 0)
				p.LastStepDurationSeconds = 0
				p.LastStepInputTokens = 0
				p.LastStepOutputTokens = 0
				p.LastStepCostUSD = 0
				if p.OnProgress != nil {
					p.OnProgress(c, total, fmt.Sprintf("#%s (%d msgs)", t.channelName, len(t.msgs)))
				}
				p.lastStepMu.Unlock()

				stepStart := time.Now()
				result, usage, pv, err := p.generateChannelDigest(ctx, t.channelID, t.channelName, t.msgs, sinceUnix, nowUnix)
				if err != nil {
					p.logger.Printf("digest: error generating digest for #%s: %v", t.channelName, err)
					errCount.Add(1)
					lastErrMu.Lock()
					lastErr = err
					lastErrMu.Unlock()
					done := int(completed.Add(1))
					if p.OnProgress != nil {
						p.OnProgress(done, total, fmt.Sprintf("#%s error: %v", t.channelName, err))
					}
					continue
				}

				// Use the actual last message timestamp as periodTo
				// instead of time.Now(), so the UI shows when the last
				// source message was posted, not when the digest was generated.
				lastMsgTS := sinceUnix
				for _, m := range t.msgs {
					if m.TSUnix > lastMsgTS {
						lastMsgTS = m.TSUnix
					}
				}

				if err := p.storeDigest(t.channelID, "channel", sinceUnix, lastMsgTS, result, len(t.msgs), usage, pv); err != nil {
					p.logger.Printf("digest: error storing digest for #%s: %v", t.channelName, err)
					errCount.Add(1)
					lastErrMu.Lock()
					lastErr = err
					lastErrMu.Unlock()
					done := int(completed.Add(1))
					if p.OnProgress != nil {
						p.OnProgress(done, total, fmt.Sprintf("#%s store error: %v", t.channelName, err))
					}
					continue
				}

				generated.Add(1)
				p.totalMessageCount.Add(int64(len(t.msgs)))
				// Update earliest period_from (use CompareAndSwap for atomicity)
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
				// Update latest period_to
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
				p.lastStepMu.Lock()
				p.LastStepMessageCount = len(t.msgs)
				p.LastStepPeriodFrom = time.Unix(int64(sinceUnix), 0)
				p.LastStepPeriodTo = time.Unix(int64(lastMsgTS), 0)
				p.LastStepDurationSeconds = time.Since(stepStart).Seconds()
				if usage != nil {
					p.LastStepInputTokens = usage.InputTokens
					p.LastStepOutputTokens = usage.OutputTokens
					p.LastStepCostUSD = usage.CostUSD
				} else {
					p.LastStepInputTokens = 0
					p.LastStepOutputTokens = 0
					p.LastStepCostUSD = 0
				}
				if usage != nil {
					totalInput.Add(int64(usage.InputTokens))
					totalOutput.Add(int64(usage.OutputTokens))
					totalCostU.Add(int64(usage.CostUSD * 1e6))
					p.accumulateUsage(usage)
				}
				done := int(completed.Add(1))
				if p.OnProgress != nil {
					p.OnProgress(done, total, fmt.Sprintf("#%s done", t.channelName))
				}
				p.lastStepMu.Unlock()
				if usage != nil {
					p.logger.Printf("digest: generated for #%s (%d messages, %d+%d tokens, $%.4f)",
						t.channelName, len(t.msgs), usage.InputTokens, usage.OutputTokens, usage.CostUSD)
				} else {
					p.logger.Printf("digest: generated for #%s (%d messages)", t.channelName, len(t.msgs))
				}
			}
		}()
	}

	wg.Wait()

	totalUsage := &Usage{
		InputTokens:  int(totalInput.Load()),
		OutputTokens: int(totalOutput.Load()),
		CostUSD:      float64(totalCostU.Load()) / 1e6,
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

	formatted := p.formatMessages(msgs)
	if strings.TrimSpace(formatted) == "" {
		return nil, nil, 0, fmt.Errorf("no visible messages after filtering (all empty or deleted)")
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02 15:04")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02 15:04")

	previousContext := p.loadPreviousContext(channelID, "channel")

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
		Model:          p.cfg.Digest.Model,
		PromptVersion:  promptVersion,
	}
	if usage != nil {
		d.InputTokens = usage.InputTokens
		d.OutputTokens = usage.OutputTokens
		d.CostUSD = usage.CostUSD
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
			km, _ := json.Marshal(t.KeyMessages)
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

func (p *Pipeline) formatMessages(msgs []db.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		userName := p.userName(m.UserID)
		ts := time.Unix(int64(m.TSUnix), 0).Local().Format("15:04")
		// Sanitize message text to prevent prompt injection via delimiter spoofing.
		text := m.Text
		if strings.Contains(text, "===") || strings.Contains(text, "---") {
			text = strings.ReplaceAll(text, "===", "= = =")
			text = strings.ReplaceAll(text, "---", "- - -")
		}
		fmt.Fprintf(&sb, "[%s @%s (%s)] %s\n", ts, userName, m.UserID, text)
	}
	return sb.String()
}

// SplitPromptAtData splits a formatted prompt into system prompt (instructions)
// and user message (data) at the "=== " delimiter. This enables API-level prompt
// caching for the instruction part. If no delimiter is found, everything goes as
// user message with an empty system prompt.
func SplitPromptAtData(prompt string) (systemPrompt, userMessage string) {
	// Look for the data section delimiter (=== MESSAGES ===, === CHANNEL DIGESTS ===, etc.)
	markers := []string{"=== MESSAGES ===", "=== CHANNEL DIGESTS ===", "=== DAILY DIGESTS ===", "=== DIGESTS ==="}
	for _, marker := range markers {
		if idx := strings.LastIndex(prompt, marker); idx > 0 {
			return strings.TrimSpace(prompt[:idx]), prompt[idx:]
		}
	}
	return "", prompt
}

func (p *Pipeline) loadCaches() {
	p.channelNames = make(map[string]string)
	p.userNames = make(map[string]string)

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
		}
	}

	channels, err := p.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load channel names: %v", err)
	} else {
		for _, ch := range channels {
			name := ch.Name
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

	var result DigestResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		// Fallback: AI may return flat format (topics as strings, decisions/action_items at top level).
		// Convert to structured Topic format.
		var flat flatDigestResult
		if flatErr := json.Unmarshal([]byte(cleaned), &flat); flatErr != nil {
			return nil, fmt.Errorf("parsing digest JSON: %w (raw: %.200s)", err, raw)
		}
		result = flat.toDigestResult()
		return &result, nil
	}

	return &result, nil
}

// flatDigestResult is the legacy AI response format with topics as strings
// and decisions/action_items at the top level (not nested within topics).
type flatDigestResult struct {
	Summary        string          `json:"summary"`
	Topics         []string        `json:"topics"`
	Decisions      []Decision      `json:"decisions"`
	ActionItems    []ActionItem    `json:"action_items"`
	KeyMessages    []string        `json:"key_messages"`
	Situations     []db.Situation  `json:"situations"`
	RunningSummary json.RawMessage `json:"running_summary,omitempty"`
}

func (f flatDigestResult) toDigestResult() DigestResult {
	var topics []Topic
	if len(f.Topics) > 0 {
		// Put all decisions/action_items/situations into a single topic
		// or distribute evenly if multiple topics exist.
		if len(f.Topics) == 1 {
			topics = []Topic{{
				Title:       f.Topics[0],
				Summary:     f.Summary,
				Decisions:   f.Decisions,
				ActionItems: f.ActionItems,
				Situations:  f.Situations,
				KeyMessages: f.KeyMessages,
			}}
		} else {
			// Create topics from titles, put all items in the first topic
			for i, title := range f.Topics {
				t := Topic{Title: title}
				if i == 0 {
					t.Summary = f.Summary
					t.Decisions = f.Decisions
					t.ActionItems = f.ActionItems
					t.Situations = f.Situations
					t.KeyMessages = f.KeyMessages
				}
				topics = append(topics, t)
			}
		}
	}
	return DigestResult{
		Summary:        f.Summary,
		Topics:         topics,
		RunningSummary: f.RunningSummary,
	}
}
