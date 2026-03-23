package analysis

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

const (
	// DefaultWindowDays is the default sliding window size.
	DefaultWindowDays = 7

	// DefaultMinMessages is the minimum messages to analyze a user.
	DefaultMinMessages = 3

	// DefaultWorkers is the default number of parallel workers.
	DefaultWorkers = 10
)

// UserResult is the AI analysis for a single user.
type UserResult struct {
	Summary            string   `json:"summary"`
	CommunicationStyle string   `json:"communication_style"`
	DecisionRole       string   `json:"decision_role"`
	StyleDetails       string   `json:"style_details"`
	RedFlags           []string `json:"red_flags"`
	Highlights         []string `json:"highlights"`
	Recommendations    []string `json:"recommendations"`
	Concerns           []string `json:"concerns"`
	Accomplishments    []string `json:"accomplishments"`
}

// PeriodSummaryResult is the AI analysis for a whole team period.
type PeriodSummaryResult struct {
	Summary   string   `json:"summary"`
	Attention []string `json:"attention"`
}

// ProgressFunc is called during analysis to report progress.
type ProgressFunc func(completed, totalUsers int, status string)

// Pipeline generates and stores user communication analyses.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	// OnProgress is called to report progress during analysis.
	OnProgress ProgressFunc

	// ForceRegenerate skips the "window already exists" check.
	ForceRegenerate bool

	// Workers is the number of parallel workers. 0 means DefaultWorkers.
	Workers int

	// Accumulated token usage across all Generate calls (thread-safe).
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalCostMicro    atomic.Int64 // cost * 1e6 for atomic ops

	// caches populated during a run
	channelNames map[string]string
	userNames    map[string]string
}

// New creates a new analysis pipeline.
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

// Run executes the people analysis pipeline for the current 7-day window.
// Returns the number of users analyzed.
// Not safe for concurrent calls — Pipeline must be used by a single goroutine.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	// loadCaches is called inside RunForWindow, no need to call it here.

	now := time.Now()
	to := float64(now.Unix())
	from := float64(now.AddDate(0, 0, -DefaultWindowDays).Unix())

	return p.RunForWindow(ctx, from, to)
}

// RunForWindow executes analysis for a specific time window.
func (p *Pipeline) RunForWindow(ctx context.Context, from, to float64) (int, error) {
	p.loadCaches()

	// Check if this window already has analyses
	if !p.ForceRegenerate {
		existing, err := p.db.GetUserAnalysesForWindow(from, to)
		if err != nil {
			return 0, fmt.Errorf("checking existing analyses: %w", err)
		}
		if len(existing) > 0 {
			p.logger.Printf("analysis: window already has %d analyses, skipping", len(existing))
			return 0, nil
		}
	}

	p.progress(0, 0, "Computing user statistics...")

	// Compute stats for all active users
	allStats, err := p.db.ComputeAllUserStats(from, to, DefaultMinMessages)
	if err != nil {
		return 0, fmt.Errorf("computing user stats: %w", err)
	}

	if len(allStats) == 0 {
		p.progress(0, 0, "No active users with enough messages")
		p.logger.Println("analysis: no active users with enough messages")
		return 0, nil
	}

	// Detect if thread data is available (search sync doesn't populate it)
	hasThreadData := false
	for _, s := range allStats {
		if s.ThreadsInitiated > 0 || s.ThreadsReplied > 0 {
			hasThreadData = true
			break
		}
	}

	totalUsers := len(allStats)
	workers := p.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	if workers > totalUsers {
		workers = totalUsers
	}

	p.progress(0, totalUsers, fmt.Sprintf("Analyzing %d users (%d workers)...", totalUsers, workers))
	p.logger.Printf("analysis: analyzing %d users with %d workers", totalUsers, workers)

	// Worker pool
	var completed atomic.Int32
	tasks := make(chan db.UserStats, totalUsers)
	var wg sync.WaitGroup

	for _, s := range allStats {
		tasks <- s
	}
	close(tasks)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for stats := range tasks {
				if ctx.Err() != nil {
					return
				}

				userName := p.userName(stats.UserID)
				c := int(completed.Load())
				p.progress(c, totalUsers, fmt.Sprintf("@%s (%d msgs)...", userName, stats.MessageCount))

				err := p.processUser(ctx, stats, from, to, hasThreadData)
				if err != nil {
					p.logger.Printf("analysis: error for @%s: %v", userName, err)
				}
				newVal := int(completed.Add(1))
				p.progress(newVal, totalUsers, fmt.Sprintf("@%s done", userName))
			}
		}()
	}

	wg.Wait()

	total := int(completed.Load())
	p.progress(total, totalUsers, fmt.Sprintf("Complete: %d users analyzed", total))
	p.logger.Printf("analysis: completed %d user analyses", total)

	// Generate period summary across all users
	if total > 0 {
		p.progress(total, totalUsers, "Generating team summary...")
		if err := p.generatePeriodSummary(ctx, from, to); err != nil {
			p.logger.Printf("analysis: period summary error: %v", err)
		}
	}

	return total, nil
}

func (p *Pipeline) processUser(ctx context.Context, stats db.UserStats, from, to float64, hasThreadData bool) error {
	// Get ALL messages for this user in the window (excluding DMs)
	msgs, err := p.db.GetMessages(db.MessageOpts{
		UserID:     stats.UserID,
		FromUnix:   from,
		ToUnix:     to,
		Limit:      5000,
		ExcludeDMs: true,
	})
	if err != nil {
		return fmt.Errorf("getting messages: %w", err)
	}

	// Sort chronologically
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].TSUnix < msgs[j].TSUnix })

	// Format user block
	userBlock := p.formatUser(stats, msgs)

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")

	langInstr := p.languageInstruction()
	if !hasThreadData {
		langInstr += "\n- IMPORTANT: Thread data (threads started / threads replied) is NOT available in this dataset. Do NOT penalize users for lack of thread participation. Do NOT mention threads in red flags or concerns."
	}

	tmpl, _ := p.getPrompt(prompts.AnalysisUser, singleUserPrompt)
	prompt := fmt.Sprintf(tmpl, p.userName(stats.UserID), fromStr, toStr, langInstr, userBlock)

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
	}

	result, err := parseSingleResult(raw)
	if err != nil {
		return fmt.Errorf("parsing result: %w", err)
	}

	redFlags, _ := json.Marshal(result.RedFlags)
	highlights, _ := json.Marshal(result.Highlights)
	recommendations, _ := json.Marshal(result.Recommendations)
	concerns, _ := json.Marshal(result.Concerns)
	accomplishments, _ := json.Marshal(result.Accomplishments)

	a := db.UserAnalysis{
		UserID:             stats.UserID,
		PeriodFrom:         from,
		PeriodTo:           to,
		MessageCount:       stats.MessageCount,
		ChannelsActive:     stats.ChannelsActive,
		ThreadsInitiated:   stats.ThreadsInitiated,
		ThreadsReplied:     stats.ThreadsReplied,
		AvgMessageLength:   stats.AvgMessageLength,
		ActiveHoursJSON:    stats.ActiveHoursJSON,
		VolumeChangePct:    stats.VolumeChangePct,
		Summary:            result.Summary,
		CommunicationStyle: result.CommunicationStyle,
		DecisionRole:       result.DecisionRole,
		RedFlags:           string(redFlags),
		Highlights:         string(highlights),
		StyleDetails:       result.StyleDetails,
		Recommendations:    string(recommendations),
		Concerns:           string(concerns),
		Accomplishments:    string(accomplishments),
		Model:              p.cfg.Digest.Model,
	}
	if usage != nil {
		a.InputTokens = usage.InputTokens
		a.OutputTokens = usage.OutputTokens
		a.CostUSD = usage.CostUSD
	}

	_, err = p.db.UpsertUserAnalysis(a)
	return err
}

func (p *Pipeline) formatUser(stats db.UserStats, msgs []db.Message) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "User ID: %s\n", stats.UserID)
	fmt.Fprintf(&sb, "Stats: %d messages, %d channels", stats.MessageCount, stats.ChannelsActive)
	if stats.ThreadsInitiated > 0 || stats.ThreadsReplied > 0 {
		fmt.Fprintf(&sb, ", %d threads started, %d threads replied", stats.ThreadsInitiated, stats.ThreadsReplied)
	}
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "Avg message length: %.0f chars, Volume change vs previous period: %+.0f%%\n",
		stats.AvgMessageLength, stats.VolumeChangePct)
	fmt.Fprintf(&sb, "Active hours (UTC): %s\n\n", stats.ActiveHoursJSON)

	sb.WriteString("Messages:\n")
	for _, m := range msgs {
		if m.Text == "" || m.IsDeleted {
			continue
		}
		channelName := p.channelName(m.ChannelID)
		ts := time.Unix(int64(m.TSUnix), 0).Local().Format("2006-01-02 15:04")
		text := sanitize(m.Text)
		threadMarker := ""
		if m.ThreadTS.Valid {
			threadMarker = " [thread reply]"
		} else if m.ReplyCount > 0 {
			threadMarker = fmt.Sprintf(" [%d replies]", m.ReplyCount)
		}
		fmt.Fprintf(&sb, "  [%s #%s] %s%s\n", ts, channelName, text, threadMarker)
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
		return fmt.Sprintf("IMPORTANT: Write ALL text values (summary, communication_style, decision_role, red_flags, highlights, recommendations, concerns, accomplishments) in %s. Do NOT use English for any text content.", lang)
	}
	if lang := p.cfg.Digest.Language; lang != "" {
		return "Write all text values in English"
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

func (p *Pipeline) progress(completed, total int, status string) {
	if p.OnProgress != nil {
		p.OnProgress(completed, total, status)
	}
}

func (p *Pipeline) generatePeriodSummary(ctx context.Context, from, to float64) error {
	analyses, err := p.db.GetUserAnalysesForWindow(from, to)
	if err != nil {
		return fmt.Errorf("fetching analyses for summary: %w", err)
	}
	if len(analyses) == 0 {
		return nil
	}

	// Build team analyses block. Sanitize AI-generated values to prevent
	// prompt injection via prior AI output being re-embedded here.
	var sb strings.Builder
	for _, a := range analyses {
		userName := p.userName(a.UserID)
		fmt.Fprintf(&sb, "=== @%s ===\n", userName)
		fmt.Fprintf(&sb, "Style: %s | Role: %s | Messages: %d | Channels: %d | Volume: %+.0f%%\n",
			sanitize(a.CommunicationStyle), sanitize(a.DecisionRole), a.MessageCount, a.ChannelsActive, a.VolumeChangePct)
		fmt.Fprintf(&sb, "Summary: %s\n", sanitize(a.Summary))
		if a.StyleDetails != "" {
			fmt.Fprintf(&sb, "Style details: %s\n", sanitize(a.StyleDetails))
		}
		if a.RedFlags != "[]" && a.RedFlags != "" {
			fmt.Fprintf(&sb, "Red flags: %s\n", sanitize(a.RedFlags))
		}
		if a.Highlights != "[]" && a.Highlights != "" {
			fmt.Fprintf(&sb, "Highlights: %s\n", sanitize(a.Highlights))
		}
		if a.Concerns != "[]" && a.Concerns != "" {
			fmt.Fprintf(&sb, "Concerns: %s\n", sanitize(a.Concerns))
		}
		fmt.Fprintln(&sb)
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")
	tmpl, _ := p.getPrompt(prompts.AnalysisPeriod, periodSummaryPrompt)
	prompt := fmt.Sprintf(tmpl, fromStr, toStr, p.languageInstruction(), sb.String())

	raw, usage, err := p.generator.Generate(ctx, "", prompt)
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
	}

	result, err := parsePeriodSummaryResult(raw)
	if err != nil {
		return fmt.Errorf("parsing period summary: %w", err)
	}

	attention, _ := json.Marshal(result.Attention)
	s := db.PeriodSummary{
		PeriodFrom: from,
		PeriodTo:   to,
		Summary:    result.Summary,
		Attention:  string(attention),
		Model:      p.cfg.Digest.Model,
	}
	if usage != nil {
		s.InputTokens = usage.InputTokens
		s.OutputTokens = usage.OutputTokens
		s.CostUSD = usage.CostUSD
	}

	return p.db.UpsertPeriodSummary(s)
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

func parseSingleResult(raw string) (*UserResult, error) {
	cleaned := extractJSON(raw)
	var result UserResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing analysis JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}

func parsePeriodSummaryResult(raw string) (*PeriodSummaryResult, error) {
	cleaned := extractJSON(raw)
	var result PeriodSummaryResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing period summary JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}

func extractJSON(raw string) string {
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
