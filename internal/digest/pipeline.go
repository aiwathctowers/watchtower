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
type Generator interface {
	Generate(ctx context.Context, systemPrompt, userMessage string) (string, *Usage, error)
}

// DigestResult is the structured output from Claude for a digest.
type DigestResult struct {
	Summary     string       `json:"summary"`
	Topics      []string     `json:"topics"`
	Decisions   []Decision   `json:"decisions"`
	ActionItems []ActionItem `json:"action_items"`
	KeyMessages []string     `json:"key_messages"`
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

// Pipeline generates and stores AI digests for Slack channels.
// ProgressFunc is called during digest generation to report progress.
type ProgressFunc func(done, total int, status string)

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

	// caches populated during a run
	channelNames map[string]string
	userNames    map[string]string
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
func (p *Pipeline) getPrompt(id, fallback string) (string, int) {
	if p.promptStore != nil {
		tmpl, version, err := p.promptStore.Get(id)
		if err == nil {
			return tmpl, version
		}
	}
	return fallback, 0
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

	n, totalUsage, err := p.RunChannelDigests(ctx)
	if err != nil {
		return n, totalUsage, err
	}

	if ctx.Err() != nil {
		return n, totalUsage, ctx.Err()
	}

	if err := p.RunDailyRollup(ctx); err != nil {
		p.logger.Printf("digest: daily rollup error: %v", err)
	}

	return n, totalUsage, nil
}

// RunChannelDigests generates digests for all channels with new messages
// since the last digest run. Returns the count and accumulated token usage.
// Channels are processed in parallel using digest.workers (default 5).
func (p *Pipeline) RunChannelDigests(ctx context.Context) (int, *Usage, error) {
	if p.OnProgress != nil {
		p.OnProgress(0, 0, "Finding channels with new messages...")
	}

	sinceUnix := p.SinceOverride
	if sinceUnix == 0 {
		sinceUnix = p.lastDigestTime()
	}
	nowUnix := float64(time.Now().Unix())

	channels, err := p.db.ChannelsWithNewMessages(sinceUnix)
	if err != nil {
		return 0, nil, fmt.Errorf("finding channels with new messages: %w", err)
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
		if len(msgs) < p.cfg.Digest.MinMessages {
			continue
		}
		tasks = append(tasks, channelTask{
			channelID:   channelID,
			channelName: p.channelName(channelID),
			msgs:        msgs,
		})
	}

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

	channelDigests, err := p.db.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: float64(dayStart.Unix()),
	})
	if err != nil {
		return fmt.Errorf("getting today's channel digests: %w", err)
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

	dateStr := dayStart.Format("2006-01-02")
	tmpl, pv := p.getPrompt(prompts.DigestDaily, dailyRollupPrompt)
	prompt := fmt.Sprintf(tmpl, dateStr, p.languageInstruction(), sb.String())

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return fmt.Errorf("generating daily rollup: %w", err)
	}

	result, err := parseDigestResult(raw)
	if err != nil {
		return fmt.Errorf("parsing daily rollup: %w", err)
	}

	fromUnix := float64(dayStart.Unix())
	// Use end-of-day as period_to so multiple runs on the same day
	// upsert into the same row instead of creating duplicates.
	dayEnd := dayStart.Add(24*time.Hour - time.Second)
	toUnix := float64(dayEnd.Unix())
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
	prompt := fmt.Sprintf(tmpl, now.Format("2006-01-02"), fromStr, toStr, p.languageInstruction(), sb.String())

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
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
	prompt := fmt.Sprintf(tmpl, fromStr, toStr, p.languageInstruction(), sb.String())

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
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
	prompt := fmt.Sprintf(tmpl, channelName, fromStr, toStr, p.languageInstruction(), formatted)

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
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

	d := db.Digest{
		ChannelID:     channelID,
		Type:          digestType,
		PeriodFrom:    from,
		PeriodTo:      to,
		Summary:       result.Summary,
		Topics:        string(topics),
		Decisions:     string(decisions),
		ActionItems:   string(actionItems),
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
		return digests[0].PeriodTo
	}
	// Default: 24 hours ago
	return float64(time.Now().Add(-24 * time.Hour).Unix())
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
		fmt.Fprintf(&sb, "[%s] @%s: %s\n", ts, userName, text)
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
