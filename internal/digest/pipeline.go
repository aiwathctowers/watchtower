package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
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
	Text      string `json:"text"`
	By        string `json:"by"`
	MessageTS string `json:"message_ts"`
}

// ActionItem represents an action item extracted from messages.
type ActionItem struct {
	Text     string `json:"text"`
	Assignee string `json:"assignee"`
	Status   string `json:"status"`
}

// Pipeline generates and stores AI digests for Slack channels.
type Pipeline struct {
	db        *db.DB
	cfg       *config.Config
	generator Generator
	logger    *log.Logger

	// SinceOverride, if non-zero, overrides the automatic "since last digest"
	// window. Used by `digest generate --since` to force a custom time range.
	SinceOverride float64

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

// Run executes the full digest pipeline: channel digests, then daily rollup.
// Returns the number of channel digests generated.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	p.loadCaches()

	n, err := p.RunChannelDigests(ctx)
	if err != nil {
		return n, err
	}

	if err := p.RunDailyRollup(ctx); err != nil {
		p.logger.Printf("digest: daily rollup error: %v", err)
	}

	return n, nil
}

// RunChannelDigests generates digests for all channels with new messages
// since the last digest run.
func (p *Pipeline) RunChannelDigests(ctx context.Context) (int, error) {
	sinceUnix := p.SinceOverride
	if sinceUnix == 0 {
		sinceUnix = p.lastDigestTime()
	}
	nowUnix := float64(time.Now().Unix())

	channels, err := p.db.ChannelsWithNewMessages(sinceUnix)
	if err != nil {
		return 0, fmt.Errorf("finding channels with new messages: %w", err)
	}

	if len(channels) == 0 {
		p.logger.Println("digest: no channels with new messages")
		return 0, nil
	}

	generated := 0
	for _, channelID := range channels {
		if ctx.Err() != nil {
			return generated, ctx.Err()
		}

		msgs, err := p.db.GetMessagesByTimeRange(channelID, sinceUnix, nowUnix)
		if err != nil {
			p.logger.Printf("digest: error getting messages for %s: %v", channelID, err)
			continue
		}

		if len(msgs) < p.cfg.Digest.MinMessages {
			continue
		}

		channelName := p.channelName(channelID)

		result, usage, err := p.generateChannelDigest(ctx, channelName, msgs, sinceUnix, nowUnix)
		if err != nil {
			p.logger.Printf("digest: error generating digest for #%s: %v", channelName, err)
			continue
		}

		if err := p.storeDigest(channelID, "channel", sinceUnix, nowUnix, result, len(msgs), usage); err != nil {
			p.logger.Printf("digest: error storing digest for #%s: %v", channelName, err)
			continue
		}

		generated++
		p.logger.Printf("digest: generated for #%s (%d messages)", channelName, len(msgs))
	}

	return generated, nil
}

// RunDailyRollup generates a cross-channel daily digest from today's channel digests.
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
		fmt.Fprintf(&sb, "### #%s (%d messages)\n%s\n\n", name, d.MessageCount, d.Summary)
	}

	dateStr := dayStart.Format("2006-01-02")
	prompt := fmt.Sprintf(dailyRollupPrompt, dateStr, p.languageInstruction(), sb.String())

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return fmt.Errorf("generating daily rollup: %w", err)
	}

	result, err := parseDigestResult(raw)
	if err != nil {
		return fmt.Errorf("parsing daily rollup: %w", err)
	}

	fromUnix := float64(dayStart.Unix())
	toUnix := float64(now.Unix())
	totalMsgs := 0
	for _, d := range channelDigests {
		totalMsgs += d.MessageCount
	}

	return p.storeDigest("", "daily", fromUnix, toUnix, result, totalMsgs, usage)
}

// RunWeeklyTrends generates a weekly trends digest from daily rollups.
func (p *Pipeline) RunWeeklyTrends(ctx context.Context) error {
	now := time.Now().UTC()
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
		date := time.Unix(int64(d.PeriodFrom), 0).UTC().Format("2006-01-02")
		fmt.Fprintf(&sb, "### %s (%d messages)\n%s\n\n", date, d.MessageCount, d.Summary)
	}

	fromStr := weekStart.Format("2006-01-02")
	toStr := now.Format("2006-01-02")
	prompt := fmt.Sprintf(weeklyTrendsPrompt, now.Format("2006-01-02"), fromStr, toStr, p.languageInstruction(), sb.String())

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return fmt.Errorf("generating weekly trends: %w", err)
	}

	result, err := parseDigestResult(raw)
	if err != nil {
		return fmt.Errorf("parsing weekly trends: %w", err)
	}

	fromUnix := float64(weekStart.Unix())
	toUnix := float64(now.Unix())
	totalMsgs := 0
	for _, d := range dailies {
		totalMsgs += d.MessageCount
	}

	return p.storeDigest("", "weekly", fromUnix, toUnix, result, totalMsgs, usage)
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
		date := time.Unix(int64(d.PeriodFrom), 0).UTC().Format("2006-01-02")
		fmt.Fprintf(&sb, "### %s — %s (%d messages)\n%s\n\n", date, label, d.MessageCount, d.Summary)
	}

	fromStr := from.Format("2006-01-02")
	toStr := to.Format("2006-01-02")
	prompt := fmt.Sprintf(periodSummaryPrompt, fromStr, toStr, p.languageInstruction(), sb.String())

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

func (p *Pipeline) generateChannelDigest(ctx context.Context, channelName string, msgs []db.Message, from, to float64) (*DigestResult, *Usage, error) {
	// Sort messages chronologically (oldest first) for natural reading
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].TSUnix < msgs[j].TSUnix })

	formatted := p.formatMessages(msgs)
	if strings.TrimSpace(formatted) == "" {
		return nil, nil, fmt.Errorf("no visible messages after filtering (all empty or deleted)")
	}

	fromStr := time.Unix(int64(from), 0).UTC().Format("2006-01-02 15:04")
	toStr := time.Unix(int64(to), 0).UTC().Format("2006-01-02 15:04")

	prompt := fmt.Sprintf(channelDigestPrompt, channelName, fromStr, toStr, p.languageInstruction(), formatted)

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return nil, nil, fmt.Errorf("claude call failed: %w", err)
	}

	result, err := parseDigestResult(raw)
	return result, usage, err
}

func (p *Pipeline) storeDigest(channelID, digestType string, from, to float64, result *DigestResult, msgCount int, usage *Usage) error {
	topics, _ := json.Marshal(result.Topics)
	decisions, _ := json.Marshal(result.Decisions)
	actionItems, _ := json.Marshal(result.ActionItems)

	d := db.Digest{
		ChannelID:    channelID,
		Type:         digestType,
		PeriodFrom:   from,
		PeriodTo:     to,
		Summary:      result.Summary,
		Topics:       string(topics),
		Decisions:    string(decisions),
		ActionItems:  string(actionItems),
		MessageCount: msgCount,
		Model:        p.cfg.Digest.Model,
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
		ts := time.Unix(int64(m.TSUnix), 0).UTC().Format("15:04")
		// Sanitize message text to prevent prompt injection via delimiter spoofing.
		text := strings.ReplaceAll(m.Text, "=== MESSAGES ===", "")
		text = strings.ReplaceAll(text, "=== CHANNEL DIGESTS ===", "")
		text = strings.ReplaceAll(text, "=== DAILY DIGESTS ===", "")
		fmt.Fprintf(&sb, "[%s] @%s: %s\n", ts, userName, text)
	}
	return sb.String()
}

func (p *Pipeline) loadCaches() {
	p.channelNames = make(map[string]string)
	p.userNames = make(map[string]string)

	channels, err := p.db.GetChannels(db.ChannelFilter{})
	if err != nil {
		p.logger.Printf("warning: failed to load channel names: %v", err)
	} else {
		for _, ch := range channels {
			p.channelNames[ch.ID] = ch.Name
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
}

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" {
		return fmt.Sprintf("Write ALL text values in %s", lang)
	}
	return "Write in the language most commonly used in the messages"
}

func (p *Pipeline) channelName(id string) string {
	if p.channelNames != nil {
		if name, ok := p.channelNames[id]; ok {
			return name
		}
	}
	return id
}

func (p *Pipeline) userName(id string) string {
	if p.userNames != nil {
		if name, ok := p.userNames[id]; ok {
			return name
		}
	}
	return id
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
