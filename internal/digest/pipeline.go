// Package digest provides digest generation and pipeline for summarizing workspace conversations.
package digest

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
	"watchtower/internal/prompts"
)

// Usage holds token and cost metrics from an AI generation call.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
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
	Summary     string         `json:"summary"`
	Topics      []string       `json:"topics"`
	Decisions   []Decision     `json:"decisions"`
	ActionItems []ActionItem   `json:"action_items"`
	KeyMessages []string       `json:"key_messages"`
	Situations  []db.Situation `json:"situations"`
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

// ChainLinker runs the chains pipeline between channel digests and rollups.
// Defined as an interface to avoid import cycles (chains imports digest).
type ChainLinker interface {
	Run(ctx context.Context) (int, error)
	FormatActiveChainsForPrompt(ctx context.Context) (string, error)
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

	// ChainContext is injected by the daemon after chains pipeline runs.
	// If non-empty, it's prepended to the daily/weekly rollup prompt to make
	// rollups chain-aware (collapsing chained decisions instead of repeating them).
	ChainContext string

	// ChainLinker, if set, runs chains pipeline between channel digests and rollups.
	// Used by `digest generate` to replicate the daemon's phased pipeline.
	ChainLinker ChainLinker

	// caches populated during a run
	channelNames map[string]string
	userNames    map[string]string
	profile      *db.UserProfile // loaded once per Run, nil if not available
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

// Run executes the full digest pipeline: channel digests, then daily rollup.
// Returns the number of channel digests generated and total token usage.
func (p *Pipeline) Run(ctx context.Context) (int, *Usage, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil, nil
	}

	// Clean up duplicate daily/weekly rollups from before period_to normalization
	if removed, err := p.db.DeduplicateDailyDigests(); err != nil {
		p.logger.Printf("digest: warning: dedup cleanup failed: %v", err)
	} else if removed > 0 {
		p.logger.Printf("digest: cleaned up %d duplicate rollup digests", removed)
	}

	p.loadCaches()

	// On first run, process day-by-day for better quality.
	// Skip if SinceOverride is set (manual run with custom window).
	if p.SinceOverride == 0 && p.isFirstRun() {
		return p.runInitialDayByDay(ctx)
	}

	n, totalUsage, err := p.RunChannelDigests(ctx)
	if err != nil {
		return n, totalUsage, err
	}

	if ctx.Err() != nil {
		return n, totalUsage, ctx.Err()
	}

	p.runChainLinker(ctx)

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

	if removed, err := p.db.DeduplicateDailyDigests(); err != nil {
		p.logger.Printf("digest: warning: dedup cleanup failed: %v", err)
	} else if removed > 0 {
		p.logger.Printf("digest: cleaned up %d duplicate rollup digests", removed)
	}

	p.loadCaches()

	if p.SinceOverride == 0 && p.isFirstRun() {
		return p.runInitialDayByDay(ctx)
	}

	return p.RunChannelDigests(ctx)
}

// RunRollups runs daily and weekly rollups only (no channel digests).
// runChainLinker runs the chains pipeline (if configured) and injects chain context for rollups.
func (p *Pipeline) runChainLinker(ctx context.Context) {
	if p.ChainLinker == nil || ctx.Err() != nil {
		return
	}
	n, err := p.ChainLinker.Run(ctx)
	if err != nil {
		p.logger.Printf("digest: chains error: %v", err)
	} else if n > 0 {
		p.logger.Printf("digest: linked %d decision(s) to chains", n)
	}
	if chainCtx, err := p.ChainLinker.FormatActiveChainsForPrompt(ctx); err == nil && chainCtx != "" {
		p.ChainContext = chainCtx
	}
}

// Used by daemon after chains pipeline has linked decisions.
func (p *Pipeline) RunRollups(ctx context.Context) error {
	if !p.cfg.Digest.Enabled {
		return nil
	}

	p.loadCaches()

	if err := p.RunDailyRollup(ctx); err != nil {
		return fmt.Errorf("daily rollup: %w", err)
	}

	return nil
}

// isFirstRun returns true if no channel digests exist yet.
func (p *Pipeline) isFirstRun() bool {
	digests, err := p.db.GetDigests(db.DigestFilter{Type: "channel", Limit: 1})
	return err != nil || len(digests) == 0
}

// runInitialDayByDay processes the initial history window day-by-day,
// generating channel digests + daily rollup for each day, then weekly trends.
// This produces much better quality than one giant digest per channel.
func (p *Pipeline) runInitialDayByDay(ctx context.Context) (int, *Usage, error) {
	days := p.cfg.Sync.InitialHistoryDays
	if days <= 0 {
		days = config.DefaultInitialHistDays
	}

	now := time.Now()
	totalGenerated := 0
	totalUsage := &Usage{}

	p.logger.Printf("digest: first run — processing %d days individually", days)

	for d := days; d >= 1; d-- {
		if ctx.Err() != nil {
			return totalGenerated, totalUsage, ctx.Err()
		}

		dayDate := now.AddDate(0, 0, -d)
		dayStart := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day(), 0, 0, 0, 0, time.UTC)
		dayEnd := time.Date(dayDate.Year(), dayDate.Month(), dayDate.Day()+1, 0, 0, 0, 0, time.UTC)

		fromUnix := float64(dayStart.Unix())
		toUnix := float64(dayEnd.Unix())

		if p.OnProgress != nil {
			p.OnProgress(days-d, days, fmt.Sprintf("Day %d/%d (%s)...", days-d+1, days, dayStart.Format("2006-01-02")))
		}

		n, usage, err := p.runChannelDigestsForWindow(ctx, fromUnix, toUnix)
		if err != nil {
			p.logger.Printf("digest: day %s error: %v", dayStart.Format("2006-01-02"), err)
			continue
		}

		totalGenerated += n
		if usage != nil {
			totalUsage.InputTokens += usage.InputTokens
			totalUsage.OutputTokens += usage.OutputTokens
			totalUsage.CostUSD += usage.CostUSD
		}

		if n > 0 {
			if err := p.runDailyRollupForDate(ctx, dayStart); err != nil {
				p.logger.Printf("digest: daily rollup for %s error: %v", dayStart.Format("2006-01-02"), err)
			}
		}
	}

	// Also process today (partial day).
	if ctx.Err() == nil {
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		n, usage, err := p.runChannelDigestsForWindow(ctx, float64(todayStart.Unix()), float64(now.Unix()))
		if err != nil {
			p.logger.Printf("digest: today error: %v", err)
		} else {
			totalGenerated += n
			if usage != nil {
				totalUsage.InputTokens += usage.InputTokens
				totalUsage.OutputTokens += usage.OutputTokens
				totalUsage.CostUSD += usage.CostUSD
			}
			if n > 0 {
				if err := p.RunDailyRollup(ctx); err != nil {
					p.logger.Printf("digest: daily rollup error: %v", err)
				}
			}
		}
	}

	// Link decisions to chains (once, across all days).
	p.runChainLinker(ctx)

	// Generate weekly trends from all daily rollups.
	if ctx.Err() == nil {
		if err := p.RunWeeklyTrends(ctx); err != nil {
			p.logger.Printf("digest: weekly trends error: %v", err)
		}
	}

	p.logger.Printf("digest: initial day-by-day complete: %d digests across %d days", totalGenerated, days)
	return totalGenerated, totalUsage, nil
}

// RunChannelDigests generates digests for all channels with new messages
// since the last digest run. Returns the count and accumulated token usage.
// Channels are processed in parallel using digest.workers (default 5).
func (p *Pipeline) RunChannelDigests(ctx context.Context) (int, *Usage, error) {
	sinceUnix := p.SinceOverride
	if sinceUnix == 0 {
		sinceUnix = p.lastDigestTime()
	}
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
	workers := p.cfg.Digest.Workers
	if workers <= 0 {
		workers = 1
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
				if p.OnProgress != nil {
					p.OnProgress(c, total, fmt.Sprintf("#%s (%d msgs)", t.channelName, len(t.msgs)))
				}

				result, usage, pv, err := p.generateChannelDigest(ctx, t.channelName, t.msgs, sinceUnix, nowUnix)
				if err != nil {
					p.logger.Printf("digest: error generating digest for #%s: %v", t.channelName, err)
					errCount.Add(1)
					lastErrMu.Lock()
					lastErr = err
					lastErrMu.Unlock()
					completed.Add(1)
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
					completed.Add(1)
					continue
				}

				generated.Add(1)
				done := int(completed.Add(1))
				if p.OnProgress != nil {
					p.OnProgress(done, total, fmt.Sprintf("#%s done", t.channelName))
				}
				if usage != nil {
					totalInput.Add(int64(usage.InputTokens))
					totalOutput.Add(int64(usage.OutputTokens))
					totalCostU.Add(int64(usage.CostUSD * 1e6))
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
		// Include channel-level decisions so the rollup can consolidate them
		if d.Decisions != "" && d.Decisions != "[]" {
			fmt.Fprintf(&sb, "Decisions: %s\n", sanitizePromptValue(d.Decisions))
		}
		sb.WriteString("\n")
	}

	// Prepend chain context if available (decisions grouped into chains are shown
	// as chain updates rather than repeated individually).
	channelInput := sb.String()
	if p.ChainContext != "" {
		channelInput = p.ChainContext + "\n" + channelInput
	}

	dateStr := dayStart.Format("2006-01-02")
	tmpl, pv := p.getPrompt(prompts.DigestDaily, dailyRollupPrompt)
	prompt := fmt.Sprintf(tmpl, dateStr, p.formatProfileContext(), p.languageInstruction(), channelInput)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.daily"), "", prompt, "")
	if err != nil {
		return fmt.Errorf("generating daily rollup: %w", err)
	}

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
		if d.Decisions != "" && d.Decisions != "[]" {
			fmt.Fprintf(&sb, "Decisions: %s\n", sanitizePromptValue(d.Decisions))
		}
		sb.WriteString("\n")
	}

	fromStr := weekStart.Format("2006-01-02")
	toStr := now.Format("2006-01-02")
	tmpl, pv := p.getPrompt(prompts.DigestWeekly, weeklyTrendsPrompt)
	prompt := fmt.Sprintf(tmpl, now.Format("2006-01-02"), fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), sb.String())

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.weekly"), "", prompt, "")
	if err != nil {
		return fmt.Errorf("generating weekly trends: %w", err)
	}

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
		fmt.Fprintf(&sb, "### %s — %s (%d messages)\n%s\n\n", date, label, d.MessageCount, sanitizePromptValue(d.Summary))
	}

	fromStr := from.Format("2006-01-02")
	toStr := to.Format("2006-01-02")
	tmpl, _ := p.getPrompt(prompts.DigestPeriod, periodSummaryPrompt)
	prompt := fmt.Sprintf(tmpl, fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), sb.String())

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.period"), "", prompt, "")
	if err != nil {
		return nil, nil, fmt.Errorf("generating period summary: %w", err)
	}

	result, err := parseDigestResult(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing period summary: %w", err)
	}

	return result, usage, nil
}

// generateChannelDigest returns the parsed result, usage, prompt version, and error.
func (p *Pipeline) generateChannelDigest(ctx context.Context, channelName string, msgs []db.Message, from, to float64) (*DigestResult, *Usage, int, error) {
	// Sort messages chronologically (oldest first) for natural reading
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].TSUnix < msgs[j].TSUnix })

	formatted := p.formatMessages(msgs)
	if strings.TrimSpace(formatted) == "" {
		return nil, nil, 0, fmt.Errorf("no visible messages after filtering (all empty or deleted)")
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02 15:04")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02 15:04")

	tmpl, pv := p.getPrompt(prompts.DigestChannel, channelDigestPrompt)
	prompt := fmt.Sprintf(tmpl, channelName, fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), formatted)

	raw, usage, _, err := p.generator.Generate(WithSource(ctx, "digest.channel"), "", prompt, "")
	if err != nil {
		return nil, nil, 0, fmt.Errorf("claude call failed: %w", err)
	}

	result, err := parseDigestResult(raw)
	return result, usage, pv, err
}

func (p *Pipeline) storeDigest(channelID, digestType string, from, to float64, result *DigestResult, msgCount int, usage *Usage, promptVersion int) error {
	topics, _ := json.Marshal(result.Topics)
	decisions, _ := json.Marshal(result.Decisions)
	actionItems, _ := json.Marshal(result.ActionItems)
	situations, _ := json.Marshal(result.Situations)

	d := db.Digest{
		ChannelID:     channelID,
		Type:          digestType,
		PeriodFrom:    from,
		PeriodTo:      to,
		Summary:       result.Summary,
		Topics:        string(topics),
		Decisions:     string(decisions),
		ActionItems:   string(actionItems),
		PeopleSignals: "[]",
		Situations:    string(situations),
		MessageCount:  msgCount,
		Model:         p.cfg.Digest.Model,
		PromptVersion: promptVersion,
	}
	if usage != nil {
		d.InputTokens = usage.InputTokens
		d.OutputTokens = usage.OutputTokens
		d.CostUSD = usage.CostUSD
	}

	_, err := p.db.UpsertDigest(d)
	return err
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
		return nil, fmt.Errorf("parsing digest JSON: %w (raw: %.200s)", err, raw)
	}

	return &result, nil
}
