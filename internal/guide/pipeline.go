// Package guide provides the People Card pipeline — a unified REDUCE phase that
// synthesizes people_signals from channel digests into per-user cards combining
// analysis and coaching advice.
package guide

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	DefaultWindowDays  = 7
	DefaultMinMessages = 3
	DefaultWorkers     = 10
)

// PeopleCardResult is the AI output for a unified people card.
type PeopleCardResult struct {
	Summary            string   `json:"summary"`
	CommunicationStyle string   `json:"communication_style"`
	DecisionRole       string   `json:"decision_role"`
	RedFlags           []string `json:"red_flags"`
	Highlights         []string `json:"highlights"`
	Accomplishments    []string `json:"accomplishments"`
	HowToCommunicate   string   `json:"how_to_communicate"`
	DecisionStyle      string   `json:"decision_style"`
	Tactics            []string `json:"tactics"`
}

// TeamSummaryResult is the AI output for team-level summary.
type TeamSummaryResult struct {
	Summary   string   `json:"summary"`
	Attention []string `json:"attention"`
	Tips      []string `json:"tips"`
}

// TeamNorms holds average stats across all users.
type TeamNorms struct {
	AvgMessages     float64
	AvgChannels     float64
	AvgMsgLength    float64
	AvgThreadsStart float64
	TotalUsers      int
}

// SignalNorms holds signal type frequency across the team.
type SignalNorms struct {
	TypeCounts map[string]int
	TotalUsers int
}

// ProgressFunc is called during generation to report progress.
type ProgressFunc func(completed, totalUsers int, status string)

// Pipeline generates and stores unified people cards.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	OnProgress      ProgressFunc
	ForceRegenerate bool
	Workers         int

	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64
	totalCostMicro    atomic.Int64

	channelNames map[string]string
	userNames    map[string]string
	profile      *db.UserProfile
}

// New creates a new people card pipeline.
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
func (p *Pipeline) AccumulatedUsage() (int, int, float64) {
	return int(p.totalInputTokens.Load()), int(p.totalOutputTokens.Load()), float64(p.totalCostMicro.Load()) / 1e6
}

// Run executes the people card pipeline for the current 7-day window.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	if !p.cfg.Digest.Enabled {
		return 0, nil
	}

	now := time.Now()
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	to := float64(endOfDay.Unix())
	from := float64(endOfDay.AddDate(0, 0, -DefaultWindowDays).Unix())

	return p.RunForWindow(ctx, from, to)
}

// RunForWindow executes the people card pipeline for a specific time window.
func (p *Pipeline) RunForWindow(ctx context.Context, from, to float64) (int, error) {
	p.loadCaches()

	if !p.ForceRegenerate {
		existing, err := p.db.GetPeopleCardsForWindow(from, to)
		if err != nil {
			return 0, fmt.Errorf("checking existing people cards: %w", err)
		}
		if len(existing) > 0 {
			p.logger.Printf("people: window already has %d cards, skipping", len(existing))
			return 0, nil
		}
	}

	p.progress(0, 0, "Computing user statistics...")

	allStats, err := p.db.ComputeAllUserStats(from, to, DefaultMinMessages)
	if err != nil {
		return 0, fmt.Errorf("computing user stats: %w", err)
	}
	if len(allStats) == 0 {
		p.progress(0, 0, "No active users with enough messages")
		p.logger.Println("people: no active users with enough messages")
		return 0, nil
	}

	// Load all signals for team norms
	allSignals, err := p.db.GetAllPeopleSignals(from, to)
	if err != nil {
		p.logger.Printf("people: warning: failed to load signals: %v", err)
		allSignals = make(map[string][]db.ChannelSignals)
	}

	teamNorms := computeTeamNorms(allStats)
	signalNorms := computeSignalNorms(allSignals)

	p.logger.Printf("people: team norms: %d users, %.0f avg msgs, %d signal types",
		teamNorms.TotalUsers, teamNorms.AvgMessages, len(signalNorms.TypeCounts))

	totalUsers := len(allStats)
	workers := p.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	if workers > totalUsers {
		workers = totalUsers
	}

	p.progress(0, totalUsers, fmt.Sprintf("Generating people cards for %d users (%d workers)...", totalUsers, workers))
	p.logger.Printf("people: generating cards for %d users with %d workers", totalUsers, workers)

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

				userSignals := allSignals[stats.UserID]
				if err := p.processUser(ctx, stats, from, to, userSignals, teamNorms, signalNorms); err != nil {
					p.logger.Printf("people: error for @%s: %v", userName, err)
				}
				newVal := int(completed.Add(1))
				p.progress(newVal, totalUsers, fmt.Sprintf("@%s done", userName))
			}
		}()
	}

	wg.Wait()

	total := int(completed.Load())
	p.progress(total, totalUsers, fmt.Sprintf("Complete: %d people cards generated", total))
	p.logger.Printf("people: completed %d user cards", total)

	if total > 0 {
		p.progress(total, totalUsers, "Generating team summary...")
		if err := p.generateTeamSummary(ctx, from, to); err != nil {
			p.logger.Printf("people: team summary error: %v", err)
		}
	}

	return total, nil
}

func (p *Pipeline) processUser(ctx context.Context, stats db.UserStats, from, to float64,
	userSignals []db.ChannelSignals, teamNorms *TeamNorms, signalNorms *SignalNorms) error {

	signalsBlock := p.formatSignals(userSignals)
	statsBlock := p.formatStats(stats)
	normsBlock := p.formatTeamNorms(teamNorms, signalNorms)
	relCtx := p.relationshipContext(stats.UserID)

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")

	tmpl, pv := p.getPrompt(prompts.PeopleReduce, defaultPeopleReducePrompt)
	prompt := fmt.Sprintf(tmpl,
		p.userName(stats.UserID), fromStr, toStr,
		p.formatProfileContext(),
		relCtx,
		p.languageInstruction(),
		signalsBlock,
		statsBlock,
		normsBlock,
	)

	raw, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "people.reduce"), "", prompt, "")
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
	}

	result, err := parsePeopleCardResult(raw)
	if err != nil {
		return fmt.Errorf("parsing result: %w", err)
	}

	redFlags, _ := json.Marshal(result.RedFlags)
	highlights, _ := json.Marshal(result.Highlights)
	accomplishments, _ := json.Marshal(result.Accomplishments)
	tactics, _ := json.Marshal(result.Tactics)

	card := db.PeopleCard{
		UserID:              stats.UserID,
		PeriodFrom:          from,
		PeriodTo:            to,
		MessageCount:        stats.MessageCount,
		ChannelsActive:      stats.ChannelsActive,
		ThreadsInitiated:    stats.ThreadsInitiated,
		ThreadsReplied:      stats.ThreadsReplied,
		AvgMessageLength:    stats.AvgMessageLength,
		ActiveHoursJSON:     stats.ActiveHoursJSON,
		VolumeChangePct:     stats.VolumeChangePct,
		Summary:             result.Summary,
		CommunicationStyle:  result.CommunicationStyle,
		DecisionRole:        result.DecisionRole,
		RedFlags:            string(redFlags),
		Highlights:          string(highlights),
		Accomplishments:     string(accomplishments),
		HowToCommunicate:    result.HowToCommunicate,
		DecisionStyle:       result.DecisionStyle,
		Tactics:             string(tactics),
		RelationshipContext: relCtx,
		Model:               p.cfg.Digest.Model,
		PromptVersion:       pv,
	}
	if usage != nil {
		card.InputTokens = usage.InputTokens
		card.OutputTokens = usage.OutputTokens
		card.CostUSD = usage.CostUSD
	}

	_, err = p.db.UpsertPeopleCard(card)
	return err
}

func (p *Pipeline) generateTeamSummary(ctx context.Context, from, to float64) error {
	cards, err := p.db.GetPeopleCardsForWindow(from, to)
	if err != nil {
		return fmt.Errorf("fetching cards for summary: %w", err)
	}
	if len(cards) == 0 {
		return nil
	}

	var sb strings.Builder
	for _, c := range cards {
		userName := p.userName(c.UserID)
		fmt.Fprintf(&sb, "=== @%s ===\n", userName)
		fmt.Fprintf(&sb, "Style: %s | Decision role: %s\n", c.CommunicationStyle, c.DecisionRole)
		fmt.Fprintf(&sb, "Messages: %d | Channels: %d | Volume: %+.0f%%\n",
			c.MessageCount, c.ChannelsActive, c.VolumeChangePct)
		fmt.Fprintf(&sb, "Summary: %s\n", sanitize(c.Summary))
		if c.RedFlags != "" && c.RedFlags != "[]" {
			fmt.Fprintf(&sb, "Red flags: %s\n", sanitize(c.RedFlags))
		}
		if c.Highlights != "" && c.Highlights != "[]" {
			fmt.Fprintf(&sb, "Highlights: %s\n", sanitize(c.Highlights))
		}
		fmt.Fprintln(&sb)
	}

	fromStr := time.Unix(int64(from), 0).Local().Format("2006-01-02")
	toStr := time.Unix(int64(to), 0).Local().Format("2006-01-02")
	tmpl, pv := p.getPrompt(prompts.PeopleTeam, defaultPeopleTeamPrompt)
	prompt := fmt.Sprintf(tmpl, fromStr, toStr, p.formatProfileContext(), p.languageInstruction(), sb.String())

	raw, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "people.team"), "", prompt, "")
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}

	if usage != nil {
		p.totalInputTokens.Add(int64(usage.InputTokens))
		p.totalOutputTokens.Add(int64(usage.OutputTokens))
		p.totalCostMicro.Add(int64(usage.CostUSD * 1e6))
	}

	result, err := parseTeamSummaryResult(raw)
	if err != nil {
		return fmt.Errorf("parsing team summary: %w", err)
	}

	attention, _ := json.Marshal(result.Attention)
	tips, _ := json.Marshal(result.Tips)
	s := db.PeopleCardSummary{
		PeriodFrom:    from,
		PeriodTo:      to,
		Summary:       result.Summary,
		Attention:     string(attention),
		Tips:          string(tips),
		Model:         p.cfg.Digest.Model,
		PromptVersion: pv,
	}
	if usage != nil {
		s.InputTokens = usage.InputTokens
		s.OutputTokens = usage.OutputTokens
		s.CostUSD = usage.CostUSD
	}

	return p.db.UpsertPeopleCardSummary(s)
}

func (p *Pipeline) formatSignals(channelSignals []db.ChannelSignals) string {
	if len(channelSignals) == 0 {
		return "(No signals observed for this user in the current period.)"
	}
	var sb strings.Builder
	for _, cs := range channelSignals {
		fmt.Fprintf(&sb, "#%s:\n", cs.ChannelName)
		for _, sig := range cs.Signals {
			detail := sanitize(sig.Detail)
			if sig.EvidenceTS != "" {
				fmt.Fprintf(&sb, "  - [%s] %s (ts: %s)\n", sig.Type, detail, sig.EvidenceTS)
			} else {
				fmt.Fprintf(&sb, "  - [%s] %s\n", sig.Type, detail)
			}
		}
	}
	return sb.String()
}

func (p *Pipeline) formatStats(stats db.UserStats) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Messages: %d\n", stats.MessageCount)
	fmt.Fprintf(&sb, "Channels active: %d\n", stats.ChannelsActive)
	if stats.ThreadsInitiated > 0 || stats.ThreadsReplied > 0 {
		fmt.Fprintf(&sb, "Threads started: %d, replied: %d\n", stats.ThreadsInitiated, stats.ThreadsReplied)
	}
	fmt.Fprintf(&sb, "Avg message length: %.0f chars\n", stats.AvgMessageLength)
	fmt.Fprintf(&sb, "Volume change vs previous period: %+.0f%%\n", stats.VolumeChangePct)
	fmt.Fprintf(&sb, "Active hours (UTC): %s\n", stats.ActiveHoursJSON)
	return sb.String()
}

func (p *Pipeline) formatTeamNorms(tn *TeamNorms, sn *SignalNorms) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Team averages (%d people): %.0f msgs/person, %.0f channels, %.0f char avg msg, %.0f threads started\n",
		tn.TotalUsers, tn.AvgMessages, tn.AvgChannels, tn.AvgMsgLength, tn.AvgThreadsStart)
	if len(sn.TypeCounts) > 0 {
		sb.WriteString("Signal distribution: ")
		parts := make([]string, 0, len(sn.TypeCounts))
		for sigType, count := range sn.TypeCounts {
			parts = append(parts, fmt.Sprintf("%s %d/%d", sigType, count, sn.TotalUsers))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("\n")
	}
	return sb.String()
}

func computeTeamNorms(allStats []db.UserStats) *TeamNorms {
	tn := &TeamNorms{TotalUsers: len(allStats)}
	if len(allStats) == 0 {
		return tn
	}
	for _, s := range allStats {
		tn.AvgMessages += float64(s.MessageCount)
		tn.AvgChannels += float64(s.ChannelsActive)
		tn.AvgMsgLength += s.AvgMessageLength
		tn.AvgThreadsStart += float64(s.ThreadsInitiated)
	}
	n := float64(len(allStats))
	tn.AvgMessages /= n
	tn.AvgChannels /= n
	tn.AvgMsgLength /= n
	tn.AvgThreadsStart /= n
	return tn
}

func computeSignalNorms(allSignals map[string][]db.ChannelSignals) *SignalNorms {
	sn := &SignalNorms{
		TypeCounts: make(map[string]int),
		TotalUsers: len(allSignals),
	}
	// Count users who have each signal type (not total signal count)
	for _, channelSignals := range allSignals {
		userTypes := make(map[string]bool)
		for _, cs := range channelSignals {
			for _, sig := range cs.Signals {
				userTypes[sig.Type] = true
			}
		}
		for sigType := range userTypes {
			sn.TypeCounts[sigType]++
		}
	}
	return sn
}

// relationshipContext determines the relationship between the current user and
// the analyzed user based on the user profile (reports, peers, manager).
func (p *Pipeline) relationshipContext(targetUserID string) string {
	if p.profile == nil {
		return ""
	}

	targetName := p.userName(targetUserID)

	if p.profile.Reports != "" && p.profile.Reports != "[]" {
		if strings.Contains(p.profile.Reports, targetUserID) || strings.Contains(p.profile.Reports, targetName) {
			return "RELATIONSHIP: This person is YOUR DIRECT REPORT. Tailor advice for a manager->report dynamic: be more directive, suggest setting expectations, checking in, accountability."
		}
	}
	if p.profile.Manager != "" {
		if strings.Contains(p.profile.Manager, targetUserID) || strings.Contains(p.profile.Manager, targetName) {
			return "RELATIONSHIP: This person is YOUR MANAGER. Tailor advice for a report->manager dynamic: be tactical, suggest batching requests, sending summaries, managing up."
		}
	}
	if p.profile.Peers != "" && p.profile.Peers != "[]" {
		if strings.Contains(p.profile.Peers, targetUserID) || strings.Contains(p.profile.Peers, targetName) {
			return "RELATIONSHIP: This person is YOUR PEER. Tailor advice for peer collaboration: shared goals, mutual alignment, cross-team coordination."
		}
	}

	return ""
}

func (p *Pipeline) getPrompt(id, fallback string) (string, int) {
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
	tmpl := fallback
	roleInstr := prompts.GetRoleInstruction(role)
	if roleInstr != "" {
		tmpl = roleInstr + "\n\n" + tmpl
	}
	return tmpl, 0
}

func (p *Pipeline) loadCaches() {
	p.channelNames = make(map[string]string)
	p.userNames = make(map[string]string)

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
			p.channelNames[ch.ID] = ch.Name
		}
	}
}

func (p *Pipeline) formatProfileContext() string {
	if p.profile == nil || p.profile.CustomPromptContext == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("=== VIEWER PROFILE CONTEXT ===\n")
	sb.WriteString(sanitize(p.profile.CustomPromptContext))
	sb.WriteString("\n\nCOACHING PERSONALIZATION:\n")
	sb.WriteString("- Tailor communication advice to the viewer's role and responsibilities\n")
	if p.profile.Reports != "" && p.profile.Reports != "[]" {
		sb.WriteString(fmt.Sprintf("\nVIEWER'S REPORTS: %s — coaching for managing these people\n", sanitize(p.profile.Reports)))
	}
	if p.profile.Peers != "" && p.profile.Peers != "[]" {
		sb.WriteString(fmt.Sprintf("\nVIEWER'S PEERS: %s — coaching for peer collaboration\n", sanitize(p.profile.Peers)))
	}
	if p.profile.Manager != "" {
		sb.WriteString(fmt.Sprintf("\nVIEWER'S MANAGER: %s — coaching for managing up\n", sanitize(p.profile.Manager)))
	}
	return sb.String()
}

func (p *Pipeline) languageInstruction() string {
	if lang := p.cfg.Digest.Language; lang != "" && !strings.EqualFold(lang, "English") {
		return fmt.Sprintf("IMPORTANT: Write ALL text values in %s. Do NOT use English for any text content.", lang)
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

// Fallback prompt consts — used when prompt store has no entry.
var (
	defaultPeopleReducePrompt = prompts.Defaults[prompts.PeopleReduce]
	defaultPeopleTeamPrompt   = prompts.Defaults[prompts.PeopleTeam]
)

func sanitize(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "```", "` ` `")
	text = strings.ReplaceAll(text, "===", "= = =")
	text = strings.ReplaceAll(text, "---", "- - -")
	return text
}

func parsePeopleCardResult(raw string) (*PeopleCardResult, error) {
	cleaned := extractJSON(raw)
	var result PeopleCardResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing people card JSON: %w (raw: %.200s)", err, raw)
	}
	return &result, nil
}

func parseTeamSummaryResult(raw string) (*TeamSummaryResult, error) {
	cleaned := extractJSON(raw)
	var result TeamSummaryResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing team summary JSON: %w (raw: %.200s)", err, raw)
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
