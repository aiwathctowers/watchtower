// Package briefing provides the daily briefing generation pipeline.
package briefing

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// BriefingResult is the structured output from the AI.
type BriefingResult struct {
	Attention    []AttentionItem    `json:"attention"`
	YourDay      []YourDayItem      `json:"your_day"`
	WhatHappened []WhatHappenedItem `json:"what_happened"`
	TeamPulse    []TeamPulseItem    `json:"team_pulse"`
	Coaching     []CoachingItem     `json:"coaching"`
}

// AttentionItem is something requiring the user's immediate focus.
type AttentionItem struct {
	Text          string `json:"text"`
	SourceType    string `json:"source_type"` // track, digest, people, target
	SourceID      string `json:"source_id"`
	Priority      string `json:"priority"` // high, medium
	Reason        string `json:"reason"`
	SuggestTarget bool   `json:"suggest_target,omitempty"`
}

// YourDayItem is a track/target for the user's day.
type YourDayItem struct {
	Text      string `json:"text"`
	TrackID   int    `json:"track_id,omitempty"`
	TargetID  int    `json:"target_id,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
	Priority  string `json:"priority"`
	Status    string `json:"status"`
	Ownership string `json:"ownership"`
}

// WhatHappenedItem is a notable event from digests.
type WhatHappenedItem struct {
	Text        string `json:"text"`
	DigestID    int    `json:"digest_id,omitempty"`
	ChannelName string `json:"channel_name"`
	ItemType    string `json:"item_type"` // decision, summary, topic
	Importance  string `json:"importance"`
}

// TeamPulseItem is a people signal.
type TeamPulseItem struct {
	Text       string `json:"text"`
	UserID     string `json:"user_id"`
	SignalType string `json:"signal_type"` // volume_drop, volume_spike, new_red_flag, highlight, conflict
	Detail     string `json:"detail"`
}

// CoachingItem is a communication/process recommendation.
type CoachingItem struct {
	Text          string `json:"text"`
	RelatedUserID string `json:"related_user_id,omitempty"`
	Category      string `json:"category"` // communication, delegation, conflict, process
}

// Pipeline generates daily personalized briefings.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store

	// Accumulated usage from the last Run call.
	lastInputTokens    int
	lastOutputTokens   int
	lastCostUSD        float64 // Deprecated: always 0
	lastTotalAPITokens int
}

// AccumulatedUsage returns the token usage from the last Run call.
// Returns (inputTokens, outputTokens, costUSD, totalAPITokens).
func (p *Pipeline) AccumulatedUsage() (int, int, float64, int) {
	return p.lastInputTokens, p.lastOutputTokens, 0, p.lastTotalAPITokens
}

// New creates a new briefing pipeline.
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

// Run generates a daily briefing for today.
// Returns the briefing ID (>0 if generated) and any error.
func (p *Pipeline) Run(ctx context.Context) (int, error) {
	today := time.Now().Format("2006-01-02")
	return p.RunForDate(ctx, today)
}

// RunForDate generates a briefing for the given date (YYYY-MM-DD).
func (p *Pipeline) RunForDate(ctx context.Context, date string) (int, error) {
	if !p.cfg.Briefing.Enabled {
		return 0, nil
	}

	currentUserID, err := p.db.GetCurrentUserID()
	if err != nil {
		return 0, fmt.Errorf("getting current user: %w", err)
	}
	if currentUserID == "" {
		p.logger.Println("briefing: no current user set, skipping")
		return 0, nil
	}

	// Deduplication check.
	existing, err := p.db.GetBriefing(currentUserID, date)
	if err != nil {
		return 0, fmt.Errorf("checking existing briefing: %w", err)
	}
	if existing != nil {
		p.logger.Printf("briefing: already exists for %s on %s, skipping", currentUserID, date)
		return existing.ID, nil
	}

	// Load user profile.
	profile, err := p.db.GetUserProfile(currentUserID)
	if err != nil {
		p.logger.Printf("briefing: could not load user profile: %v", err)
	}

	// Gather data in parallel-friendly sections.
	targetsCtx, hasRealTargets := p.gatherTargets()
	tracksCtx, hasRealTracks := p.gatherTracks()
	inboxCtx, hasRealInbox := p.gatherInbox()
	calendarCtx := p.gatherCalendar()
	digestsCtx := p.gatherDigests(date)
	dailyDigestCtx := p.gatherLatestDailyDigest()
	peopleCardsCtx := p.gatherPeopleCards()
	peopleSummaryCtx := p.gatherPeopleSummary()
	profileCtx := formatUserProfile(profile)
	jiraCtx := p.gatherJiraContext(currentUserID)

	// Check we have some data (suggestion text alone doesn't count).
	hasData := digestsCtx != "" || dailyDigestCtx != "" || hasRealTracks || hasRealTargets || hasRealInbox
	if !hasData {
		p.logger.Println("briefing: no digests or tracks available, skipping")
		return 0, nil
	}

	// Determine user role.
	role := ""
	if profile != nil {
		role = profile.Role
	}

	// Get workspace ID.
	workspaceID := ""
	if ws, err := p.db.GetWorkspace(); err == nil && ws != nil {
		workspaceID = ws.ID
	}

	// Build prompt.
	userName := p.userName(currentUserID)
	promptTmpl, promptVersion := p.getPrompt(prompts.BriefingDaily, role)
	langDirective := fmt.Sprintf("Respond in %s", p.cfg.Digest.Language)

	systemPrompt := fmt.Sprintf(promptTmpl,
		userName, date, role,
		langDirective,
		targetsCtx,
		inboxCtx,
		calendarCtx,
		tracksCtx,
		digestsCtx,
		dailyDigestCtx,
		peopleCardsCtx,
		peopleSummaryCtx,
		profileCtx,
		jiraCtx,
	)

	// Generate.
	p.logger.Printf("briefing: generating for %s on %s", userName, date)

	response, usage, _, err := p.generator.Generate(digest.WithSource(ctx, "briefing.daily"), systemPrompt, "Generate the daily briefing.", "")
	if err != nil {
		return 0, fmt.Errorf("generating briefing: %w", err)
	}

	// Parse result.
	result, err := parseBriefingResult(response)
	if err != nil {
		return 0, fmt.Errorf("parsing briefing response: %w", err)
	}

	// Serialize JSON sections.
	attentionJSON, _ := json.Marshal(result.Attention)
	yourDayJSON, _ := json.Marshal(result.YourDay)
	whatHappenedJSON, _ := json.Marshal(result.WhatHappened)
	teamPulseJSON, _ := json.Marshal(result.TeamPulse)
	coachingJSON, _ := json.Marshal(result.Coaching)

	var inTok, outTok, totalAPI int
	if usage != nil {
		inTok = usage.InputTokens
		outTok = usage.OutputTokens
		totalAPI = usage.TotalAPITokens
	}
	p.lastInputTokens = inTok
	p.lastOutputTokens = outTok
	p.lastCostUSD = 0
	p.lastTotalAPITokens = totalAPI

	briefing := db.Briefing{
		WorkspaceID:   workspaceID,
		UserID:        currentUserID,
		Date:          date,
		Role:          role,
		Attention:     string(attentionJSON),
		YourDay:       string(yourDayJSON),
		WhatHappened:  string(whatHappenedJSON),
		TeamPulse:     string(teamPulseJSON),
		Coaching:      string(coachingJSON),
		Model:         usage.Model,
		InputTokens:   inTok,
		OutputTokens:  outTok,
		CostUSD:       0,
		PromptVersion: promptVersion,
	}

	id, err := p.db.UpsertBriefing(briefing)
	if err != nil {
		return 0, fmt.Errorf("storing briefing: %w", err)
	}

	p.logger.Printf("briefing: generated (id=%d, %d+%d tokens)",
		id, inTok, outTok)

	return int(id), nil
}

func (p *Pipeline) getPrompt(id, role string) (string, int) {
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

// gatherTargets loads active targets for the briefing.
// Returns the formatted context string and whether real targets were found.
func (p *Pipeline) gatherTargets() (string, bool) {
	targets, err := p.db.GetTargetsForBriefing()
	if err != nil {
		p.logger.Printf("briefing: error loading targets: %v", err)
		return "", false
	}

	if len(targets) == 0 {
		return "(No active targets.)\n", false
	}

	today := time.Now().Format("2006-01-02")
	var sb strings.Builder
	for _, t := range targets {
		overdue := ""
		if t.DueDate != "" && t.DueDate < today {
			overdue = " OVERDUE"
		}
		sb.WriteString(fmt.Sprintf("- [target_id=%d level=%s %s%s] %s\n", t.ID, t.Level, t.Priority, overdue, t.Text))
		if t.Intent != "" {
			sb.WriteString(fmt.Sprintf("  Why: %s\n", t.Intent))
		}
		if t.DueDate != "" {
			sb.WriteString(fmt.Sprintf("  Due: %s\n", t.DueDate))
		}
		if t.Status != "todo" {
			sb.WriteString(fmt.Sprintf("  Status: %s\n", t.Status))
		}
		if t.Blocking != "" {
			sb.WriteString(fmt.Sprintf("  Blocking: %s\n", t.Blocking))
		}
	}
	return sb.String(), true
}

// gatherTracks loads active tracks.
// Returns the formatted context string and whether real tracks were found.
func (p *Pipeline) gatherTracks() (string, bool) {
	tracks, err := p.db.GetTracks(db.TrackFilter{Limit: 30})
	if err != nil {
		p.logger.Printf("briefing: error loading tracks: %v", err)
		return "", false
	}

	if len(tracks) == 0 {
		return "(No active tracks yet.)\n", false
	}

	var sb strings.Builder
	for _, t := range tracks {
		sb.WriteString(fmt.Sprintf("- [id=%d %s %s] %s\n", t.ID, t.Priority, t.Ownership, t.Text))
		if t.Context != "" {
			ctx := t.Context
			if len(ctx) > 200 {
				ctx = ctx[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("  Context: %s\n", ctx))
		}
		if t.Participants != "" && t.Participants != "[]" {
			participants := t.Participants
			if len(participants) > 150 {
				participants = participants[:150] + "...]"
			}
			sb.WriteString(fmt.Sprintf("  Participants: %s\n", participants))
		}
	}
	return sb.String(), true
}

// gatherInbox loads pending inbox items for the briefing.
// Returns the formatted context string and whether real items were found.
func (p *Pipeline) gatherInbox() (string, bool) {
	items, err := p.db.GetInboxItemsForBriefing()
	if err != nil {
		p.logger.Printf("briefing: error loading inbox: %v", err)
		return "", false
	}

	if len(items) == 0 {
		return "(No pending inbox items.)\n", false
	}

	var sb strings.Builder
	for _, item := range items {
		typeLabel := "@mention"
		if item.TriggerType == "dm" {
			typeLabel = "DM"
		}
		sb.WriteString(fmt.Sprintf("- [inbox_id=%d %s %s] from %s: %s\n",
			item.ID, item.Priority, typeLabel, item.SenderUserID, item.Snippet))
		if item.AIReason != "" {
			sb.WriteString(fmt.Sprintf("  Reason: %s\n", item.AIReason))
		}
	}
	return sb.String(), true
}

// gatherDigests loads channel digests for the last 24 hours.
func (p *Pipeline) gatherDigests(date string) string {
	// Parse date to get window.
	t, err := time.ParseInLocation("2006-01-02", date, time.Now().Location())
	if err != nil {
		p.logger.Printf("briefing: bad date %q: %v", date, err)
		return ""
	}
	from := float64(t.AddDate(0, 0, -1).Unix())
	to := float64(t.Add(24 * time.Hour).Unix())

	digests, err := p.db.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: from,
		ToUnix:   to,
		Limit:    50,
	})
	if err != nil {
		p.logger.Printf("briefing: error loading digests: %v", err)
		return ""
	}
	if len(digests) == 0 {
		return ""
	}

	// Filter out digests from muted channels.
	mutedIDs, mutedErr := p.db.GetMutedChannelIDs()
	if mutedErr != nil {
		p.logger.Printf("briefing: warning: failed to load muted channels: %v", mutedErr)
	} else if len(mutedIDs) > 0 {
		muted := make(map[string]bool, len(mutedIDs))
		for _, id := range mutedIDs {
			muted[id] = true
		}
		var filtered []db.Digest
		for _, d := range digests {
			if !muted[d.ChannelID] {
				filtered = append(filtered, d)
			}
		}
		if skipped := len(digests) - len(filtered); skipped > 0 {
			p.logger.Printf("briefing: skipped %d digest(s) from muted channels", skipped)
		}
		digests = filtered
		if len(digests) == 0 {
			return ""
		}
	}

	// Batch-load topics for all digests.
	digestIDs := make([]int, len(digests))
	for i, d := range digests {
		digestIDs[i] = d.ID
	}
	allTopics, err := p.db.GetDigestTopicsByDigestIDs(digestIDs)
	if err != nil {
		p.logger.Printf("briefing: error loading digest topics: %v", err)
		allTopics = nil
	}
	topicsByDigest := make(map[int][]db.DigestTopic)
	for _, t := range allTopics {
		topicsByDigest[t.DigestID] = append(topicsByDigest[t.DigestID], t)
	}

	var sb strings.Builder
	for _, d := range digests {
		channelName := d.ChannelID
		sb.WriteString(fmt.Sprintf("--- [digest_id=%d] #%s (msgs: %d) ---\n", d.ID, channelName, d.MessageCount))

		topics := topicsByDigest[d.ID]
		if len(topics) > 0 {
			// Topic-structured format.
			for _, t := range topics {
				sb.WriteString(fmt.Sprintf("  Topic: %s\n", t.Title))
				sb.WriteString(fmt.Sprintf("    %s\n", t.Summary))
				if t.Decisions != "" && t.Decisions != "[]" {
					sb.WriteString(fmt.Sprintf("    Decisions: %s\n", t.Decisions))
				}
				if t.ActionItems != "" && t.ActionItems != "[]" {
					sb.WriteString(fmt.Sprintf("    Action items: %s\n", t.ActionItems))
				}
			}
		} else {
			// Fallback to old flat format.
			sb.WriteString(d.Summary + "\n")
			if d.Decisions != "" && d.Decisions != "[]" {
				sb.WriteString("Decisions: " + d.Decisions + "\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// gatherLatestDailyDigest loads the most recent daily rollup.
func (p *Pipeline) gatherLatestDailyDigest() string {
	d, err := p.db.GetLatestDigest("", "daily")
	if err != nil || d == nil {
		return ""
	}
	return fmt.Sprintf("--- DAILY ROLLUP (digest_id=%d) ---\n%s\n", d.ID, d.Summary)
}

// gatherPeopleCards loads the latest people cards.
func (p *Pipeline) gatherPeopleCards() string {
	cards, err := p.db.GetPeopleCards(db.PeopleCardFilter{Limit: 20})
	if err != nil {
		p.logger.Printf("briefing: error loading people cards: %v", err)
		return ""
	}
	if len(cards) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, c := range cards {
		if c.Status == "insufficient_data" {
			continue
		}
		sb.WriteString(fmt.Sprintf("@%s: %s", c.UserID, c.Summary))
		if c.RedFlags != "" && c.RedFlags != "[]" {
			sb.WriteString(fmt.Sprintf(" [flags: %s]", c.RedFlags))
		}
		if c.Highlights != "" && c.Highlights != "[]" {
			sb.WriteString(fmt.Sprintf(" [highlights: %s]", c.Highlights))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// gatherPeopleSummary loads the latest team summary.
func (p *Pipeline) gatherPeopleSummary() string {
	s, err := p.db.GetLatestPeopleCardSummary()
	if err != nil || s == nil {
		return ""
	}
	return fmt.Sprintf("Team summary: %s\nAttention: %s\nTips: %s\n", s.Summary, s.Attention, s.Tips)
}

// gatherCalendar loads today's calendar events for the briefing.
func (p *Pipeline) gatherCalendar() string {
	today := time.Now().Local().Format("2006-01-02")
	events, err := p.db.GetCalendarEventsForDate(today)
	if err != nil || len(events) == 0 {
		return ""
	}

	var buf strings.Builder
	for _, e := range events {
		buf.WriteString(formatCalendarEvent(e, p.db))
	}
	return buf.String()
}

// formatCalendarEvent formats a calendar event for the briefing prompt.
func formatCalendarEvent(e db.CalendarEvent, database *db.DB) string {
	start, _ := time.Parse(time.RFC3339, e.StartTime)
	end, _ := time.Parse(time.RFC3339, e.EndTime)
	start = start.Local()
	end = end.Local()

	var timeStr string
	if e.IsAllDay {
		timeStr = "All day"
	} else {
		timeStr = fmt.Sprintf("%s-%s", start.Format("15:04"), end.Format("15:04"))
	}

	var attendeeNames []string
	var attendees []struct {
		Email          string `json:"email"`
		DisplayName    string `json:"display_name"`
		ResponseStatus string `json:"response_status"`
		SlackUserID    string `json:"slack_user_id"`
	}
	if err := json.Unmarshal([]byte(e.Attendees), &attendees); err == nil {
		for _, a := range attendees {
			name := a.DisplayName
			if a.SlackUserID != "" {
				if u, err := database.GetUserByID(a.SlackUserID); err == nil && u != nil {
					displayName := u.DisplayName
					if displayName == "" {
						displayName = u.RealName
					}
					if displayName == "" {
						displayName = u.Name
					}
					name = "@" + displayName
				}
			}
			if name == "" {
				name = a.Email
			}
			attendeeNames = append(attendeeNames, name)
		}
	}

	line := fmt.Sprintf("- %s %q", timeStr, e.Title)
	if len(attendeeNames) > 0 {
		line += " — " + strings.Join(attendeeNames, ", ")
	}
	line += "\n"
	return line
}

func formatUserProfile(profile *db.UserProfile) string {
	if profile == nil {
		return ""
	}

	var sb strings.Builder
	if profile.Role != "" {
		sb.WriteString(fmt.Sprintf("Role: %s\n", profile.Role))
	}
	if profile.Team != "" {
		sb.WriteString(fmt.Sprintf("Team: %s\n", profile.Team))
	}
	if profile.Responsibilities != "" && profile.Responsibilities != "[]" {
		sb.WriteString(fmt.Sprintf("Responsibilities: %s\n", profile.Responsibilities))
	}
	if profile.Reports != "" && profile.Reports != "[]" {
		sb.WriteString(fmt.Sprintf("Reports: %s\n", profile.Reports))
	}
	if profile.PainPoints != "" && profile.PainPoints != "[]" {
		sb.WriteString(fmt.Sprintf("Pain points: %s\n", profile.PainPoints))
	}
	if profile.TrackFocus != "" && profile.TrackFocus != "[]" {
		sb.WriteString(fmt.Sprintf("Focus areas: %s\n", profile.TrackFocus))
	}
	return sb.String()
}

func (p *Pipeline) userName(userID string) string {
	var name string
	err := p.db.QueryRow(`SELECT COALESCE(NULLIF(display_name,''), NULLIF(real_name,''), name) FROM users WHERE id = ?`, userID).Scan(&name)
	if err != nil || name == "" {
		return userID
	}
	return name
}

func parseBriefingResult(response string) (*BriefingResult, error) {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		lines := strings.SplitN(response, "\n", 2)
		if len(lines) == 2 {
			response = lines[1]
		}
		if idx := strings.LastIndex(response, "```"); idx >= 0 {
			response = response[:idx]
		}
		response = strings.TrimSpace(response)
	}

	var result BriefingResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w (response: %.200s)", err, response)
	}
	return &result, nil
}
