package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// MeetingPrepResult is the AI output for a single meeting.
type MeetingPrepResult struct {
	EventID         string                  `json:"event_id"`
	Title           string                  `json:"title"`
	StartTime       string                  `json:"start_time"`
	TalkingPoints   []TalkingPoint          `json:"talking_points"`
	OpenItems       []OpenItem              `json:"open_items"`
	PeopleNotes     []PersonNote            `json:"people_notes"`
	SuggestedPrep   []string                `json:"suggested_prep"`
	Recommendations []MeetingRecommendation `json:"recommendations"`
	ContextGaps     []string                `json:"context_gaps,omitempty"`
}

// MeetingRecommendation is a suggestion for improving the meeting.
type MeetingRecommendation struct {
	Text     string `json:"text"`
	Category string `json:"category"` // agenda, format, participants, followup, preparation
	Priority string `json:"priority"` // high, medium, low
}

// TalkingPoint is a topic to raise or discuss.
type TalkingPoint struct {
	Text       string `json:"text"`
	SourceType string `json:"source_type"` // track, digest, inbox, task
	SourceID   string `json:"source_id"`
	Priority   string `json:"priority"` // high, medium, low
}

// OpenItem is an unresolved item involving a meeting attendee.
type OpenItem struct {
	Text       string `json:"text"`
	Type       string `json:"type"` // track, inbox, task
	ID         string `json:"id"`
	PersonName string `json:"person_name"`
	PersonID   string `json:"person_id"`
}

// PersonNote is a communication tip for a meeting attendee.
type PersonNote struct {
	UserID           string `json:"user_id"`
	Name             string `json:"name"`
	CommunicationTip string `json:"communication_tip"`
	RecentContext    string `json:"recent_context"`
}

// Pipeline generates AI-powered meeting preparation.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store
}

// New creates a new meeting prep pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
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

// PrepareForEvent generates meeting prep for a single calendar event.
// userNotes is optional additional context provided by the user (agenda, goals, etc.).
func (p *Pipeline) PrepareForEvent(ctx context.Context, eventID string, userNotes string) (*MeetingPrepResult, error) {
	event, err := p.db.GetCalendarEventByID(eventID)
	if err != nil {
		return nil, fmt.Errorf("loading event: %w", err)
	}
	if event == nil {
		return nil, fmt.Errorf("event %q not found", eventID)
	}
	return p.prepareForEvent(ctx, *event, userNotes)
}

// PrepareForNext generates meeting prep for the next upcoming event with >1 attendee.
func (p *Pipeline) PrepareForNext(ctx context.Context, userNotes string) (*MeetingPrepResult, error) {
	now := time.Now().UTC()
	events, err := p.db.GetCalendarEvents(db.CalendarEventFilter{
		FromTime: now.Format(time.RFC3339),
		Limit:    20,
	})
	if err != nil {
		return nil, fmt.Errorf("querying upcoming events: %w", err)
	}

	for _, ev := range events {
		if ev.IsAllDay {
			continue
		}
		var attendees []attendeeEntry
		if err := json.Unmarshal([]byte(ev.Attendees), &attendees); err == nil && len(attendees) > 1 {
			return p.prepareForEvent(ctx, ev, userNotes)
		}
	}

	return nil, fmt.Errorf("no upcoming meetings with attendees found")
}

type attendeeEntry struct {
	Email          string `json:"email"`
	DisplayName    string `json:"display_name"`
	ResponseStatus string `json:"response_status"`
	SlackUserID    string `json:"slack_user_id"`
}

func (p *Pipeline) prepareForEvent(ctx context.Context, event db.CalendarEvent, userNotes string) (*MeetingPrepResult, error) {
	p.logger.Printf("meeting: generating prep for %q (ID: %s)", event.Title, event.ID)

	// Parse attendees.
	var attendees []attendeeEntry
	if err := json.Unmarshal([]byte(event.Attendees), &attendees); err != nil {
		attendees = nil
	}

	// Get current user info.
	currentUserID, _ := p.db.GetCurrentUserID()
	userName := "User"
	if currentUserID != "" {
		if user, err := p.db.GetUserByID(currentUserID); err == nil && user != nil {
			if user.DisplayName != "" {
				userName = user.DisplayName
			} else if user.RealName != "" {
				userName = user.RealName
			}
		}
	}

	// Format meeting time.
	start, _ := time.Parse(time.RFC3339, event.StartTime)
	end, _ := time.Parse(time.RFC3339, event.EndTime)
	start = start.Local()
	end = end.Local()
	meetingTime := fmt.Sprintf("%s-%s", start.Format("15:04"), end.Format("15:04"))

	// Gather attendee context.
	attendeesCtx := p.gatherAttendeesContext(attendees)

	// Gather shared context (tracks, digests involving attendees).
	sharedCtx := p.gatherSharedContext(attendees)

	// Jira context for attendees.
	var attendeeSlackIDs []string
	for _, a := range attendees {
		if a.SlackUserID != "" {
			attendeeSlackIDs = append(attendeeSlackIDs, a.SlackUserID)
		}
	}
	jiraCtx, _ := gatherJiraMeetingContext(p.db, p.cfg, attendeeSlackIDs)

	// User profile context.
	profileCtx := ""
	if currentUserID != "" {
		if profile, err := p.db.GetUserProfile(currentUserID); err == nil && profile != nil {
			profileCtx = formatProfile(profile)
		}
	}

	// Language directive.
	langDirective := ""
	if p.cfg.Digest.Language != "" {
		langDirective = fmt.Sprintf("Respond in %s", p.cfg.Digest.Language)
	}

	// Load prompt template.
	promptTmpl := p.loadPromptTemplate()

	// Meeting description/agenda.
	meetingDesc := "(no description or agenda provided)"
	if event.Description != "" {
		meetingDesc = event.Description
	}

	// User notes context.
	userNotesCtx := "(no user notes provided)"
	if userNotes != "" {
		userNotesCtx = userNotes
	}

	// Args: 1=userName, 2=title, 3=meetingTime, 4=language, 5=meetingDesc, 6=attendees, 7=sharedContext, 8=jiraContext, 9=profile, 10=userNotes
	systemPrompt := fmt.Sprintf(promptTmpl,
		userName,
		event.Title,
		meetingTime,
		langDirective,
		meetingDesc,
		attendeesCtx,
		sharedCtx,
		jiraCtx,
		profileCtx,
		userNotesCtx,
	)

	userMessage := fmt.Sprintf("Generate meeting prep for event %q (ID: %s) at %s on %s.",
		event.Title, event.ID, meetingTime, start.Format("2006-01-02"))

	aiResponse, _, _, err := p.generator.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		p.logger.Printf("meeting: error generating prep for %q: %v", event.Title, err)
		return nil, fmt.Errorf("AI generation: %w", err)
	}

	// Parse AI response.
	var result MeetingPrepResult
	cleaned := cleanJSON(aiResponse)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		p.logger.Printf("meeting: error parsing AI response for %q: %v", event.Title, err)
		return nil, fmt.Errorf("parsing AI response: %w (raw: %.500s)", err, aiResponse)
	}

	// Ensure event metadata is filled.
	result.EventID = event.ID
	result.Title = event.Title
	result.StartTime = event.StartTime

	p.logger.Printf("meeting: completed prep for %q (%d talking points, %d open items)",
		event.Title, len(result.TalkingPoints), len(result.OpenItems))

	return &result, nil
}

// gatherAttendeesContext builds the attendee section for the prompt.
func (p *Pipeline) gatherAttendeesContext(attendees []attendeeEntry) string {
	if len(attendees) == 0 {
		return "(no attendees)"
	}

	// Fetch inbox items and targets once, then filter per-attendee inside the loop.
	allInboxItems, _ := p.db.GetInboxItems(db.InboxFilter{Status: "pending", Limit: 5})
	allTargets, _ := p.db.GetTargets(db.TargetFilter{Limit: 50})

	// Time window for recent activity (last 7 days).
	now := time.Now()
	activityFrom := float64(now.Add(-7 * 24 * time.Hour).Unix())
	activityTo := float64(now.Unix())

	var sb strings.Builder
	for _, a := range attendees {
		name := a.DisplayName
		if name == "" {
			name = a.Email
		}

		sb.WriteString(fmt.Sprintf("### %s", name))
		if a.SlackUserID != "" {
			sb.WriteString(fmt.Sprintf(" (Slack: %s)", a.SlackUserID))
		}
		sb.WriteString("\n")

		if a.SlackUserID == "" {
			sb.WriteString("  No Slack profile linked.\n\n")
			continue
		}

		// People card.
		cards, err := p.db.GetPeopleCards(db.PeopleCardFilter{UserID: a.SlackUserID, Limit: 1})
		if err == nil && len(cards) > 0 {
			card := cards[0]
			if card.CommunicationGuide != "" {
				sb.WriteString(fmt.Sprintf("  Communication: %s\n", card.CommunicationGuide))
			}
			if card.DecisionStyle != "" {
				sb.WriteString(fmt.Sprintf("  Decision style: %s\n", card.DecisionStyle))
			}
			if card.Summary != "" {
				sb.WriteString(fmt.Sprintf("  Summary: %s\n", card.Summary))
			}
		}

		// Latest user analysis (communication style, decision role, highlights).
		if analysis, err := p.db.GetLatestUserAnalysis(a.SlackUserID); err == nil && analysis != nil {
			if analysis.CommunicationStyle != "" {
				sb.WriteString(fmt.Sprintf("  Communication style: %s\n", analysis.CommunicationStyle))
			}
			if analysis.DecisionRole != "" {
				sb.WriteString(fmt.Sprintf("  Decision role: %s\n", analysis.DecisionRole))
			}
			if analysis.Highlights != "" && analysis.Highlights != "[]" {
				sb.WriteString(fmt.Sprintf("  Recent highlights: %s\n", truncate(analysis.Highlights, 300)))
			}
			if analysis.Concerns != "" && analysis.Concerns != "[]" {
				sb.WriteString(fmt.Sprintf("  Concerns: %s\n", truncate(analysis.Concerns, 200)))
			}
			if analysis.Accomplishments != "" && analysis.Accomplishments != "[]" {
				sb.WriteString(fmt.Sprintf("  Accomplishments: %s\n", truncate(analysis.Accomplishments, 200)))
			}
		}

		// Activity stats (last 7 days).
		if stats, err := p.db.ComputeUserStats(a.SlackUserID, activityFrom, activityTo); err == nil && stats != nil && stats.MessageCount > 0 {
			sb.WriteString(fmt.Sprintf("  Activity (7d): %d msgs across %d channels, %d threads started, %d replies\n",
				stats.MessageCount, stats.ChannelsActive, stats.ThreadsInitiated, stats.ThreadsReplied))
			if stats.VolumeChangePct != 0 {
				sb.WriteString(fmt.Sprintf("  Volume change: %.0f%% vs previous week\n", stats.VolumeChangePct))
			}
		}

		// Recent situations from digests (last 7 days).
		if situations, err := p.db.GetSituationsForUser(a.SlackUserID, activityFrom, activityTo); err == nil && len(situations) > 0 {
			sb.WriteString("  Recent situations:\n")
			count := 0
			for _, cs := range situations {
				for _, sit := range cs.Situations {
					if count >= 5 {
						break
					}
					chName := cs.ChannelName
					if chName == "" {
						chName = cs.ChannelID
					}
					sb.WriteString(fmt.Sprintf("    - [#%s %s] %s", chName, sit.Type, sit.Topic))
					if sit.Outcome != "" {
						sb.WriteString(fmt.Sprintf(" → %s", truncate(sit.Outcome, 100)))
					}
					sb.WriteString("\n")
					count++
				}
				if count >= 5 {
					break
				}
			}
		}

		// Pending inbox items from this person.
		for _, item := range allInboxItems {
			if item.SenderUserID == a.SlackUserID {
				sb.WriteString(fmt.Sprintf("  [INBOX pending] %s\n", item.Snippet))
			}
		}

		// Targets where this person is ball_on.
		for _, t := range allTargets {
			if t.BallOn == a.SlackUserID {
				sb.WriteString(fmt.Sprintf("  [TARGET %s id=%d] %s\n", t.Priority, t.ID, t.Text))
			}
		}

		sb.WriteString("\n")
	}
	return sb.String()
}

// gatherSharedContext builds the shared context section for the prompt.
// It focuses on tracks and cross-attendee situations (involving 2+ attendees).
// Per-attendee situations are already covered in gatherAttendeesContext.
func (p *Pipeline) gatherSharedContext(attendees []attendeeEntry) string {
	var sb strings.Builder

	// Collect attendee user IDs for matching.
	attendeeIDs := make(map[string]bool)
	for _, a := range attendees {
		if a.SlackUserID != "" {
			attendeeIDs[a.SlackUserID] = true
		}
	}

	// Active tracks involving attendees.
	tracks, err := p.db.GetTracks(db.TrackFilter{Limit: 50})
	if err == nil {
		for _, t := range tracks {
			involves := false
			for uid := range attendeeIDs {
				if strings.Contains(t.Participants, uid) {
					involves = true
					break
				}
			}
			if involves {
				sb.WriteString(fmt.Sprintf("[TRACK id=%d priority=%s] %s\n", t.ID, t.Priority, t.Text))
				if t.Context != "" {
					sb.WriteString(fmt.Sprintf("  Context: %s\n", t.Context))
				}
				sb.WriteString("\n")
			}
		}
	}

	// Cross-attendee situations: only include situations where 2+ meeting attendees
	// are participants. This avoids duplicating per-attendee context and surfaces
	// shared dynamics that are directly relevant to the meeting.
	now := time.Now()
	cutoff := now.Add(-7 * 24 * time.Hour)
	type sitKey struct {
		channelID string
		topic     string
	}
	seenSituations := make(map[sitKey]bool)

	for uid := range attendeeIDs {
		situations, err := p.db.GetSituationsForUser(uid, float64(cutoff.Unix()), float64(now.Unix()))
		if err != nil {
			continue
		}
		for _, cs := range situations {
			chName := cs.ChannelName
			if chName == "" {
				chName = cs.ChannelID
			}
			for _, sit := range cs.Situations {
				// Count how many meeting attendees are in this situation.
				attendeeCount := 0
				for _, sp := range sit.Participants {
					if attendeeIDs[sp.UserID] {
						attendeeCount++
					}
				}
				if attendeeCount < 2 {
					continue
				}

				sk := sitKey{channelID: cs.ChannelID, topic: sit.Topic}
				if seenSituations[sk] {
					continue
				}
				seenSituations[sk] = true

				sb.WriteString(fmt.Sprintf("[SHARED SITUATION #%s type=%s] %s\n", chName, sit.Type, sit.Topic))
				if sit.Dynamic != "" {
					sb.WriteString(fmt.Sprintf("  Dynamic: %s\n", truncate(sit.Dynamic, 150)))
				}
				if sit.Outcome != "" {
					sb.WriteString(fmt.Sprintf("  Outcome: %s\n", truncate(sit.Outcome, 150)))
				}
				// List which attendees are involved.
				var names []string
				for _, sp := range sit.Participants {
					if attendeeIDs[sp.UserID] {
						names = append(names, sp.UserID+" ("+sp.Role+")")
					}
				}
				if len(names) > 0 {
					sb.WriteString(fmt.Sprintf("  Attendees involved: %s\n", strings.Join(names, ", ")))
				}
				sb.WriteString("\n")
			}
		}
	}

	if sb.Len() == 0 {
		return "(no shared context available)"
	}
	return sb.String()
}

func (p *Pipeline) loadPromptTemplate() string {
	if p.promptStore != nil {
		tmpl, _, err := p.promptStore.Get(prompts.MeetingPrep)
		if err == nil && tmpl != "" {
			return tmpl
		}
	}
	return prompts.Defaults[prompts.MeetingPrep]
}

func formatProfile(profile *db.UserProfile) string {
	if profile == nil {
		return ""
	}
	var parts []string
	if profile.Role != "" {
		parts = append(parts, "Role: "+profile.Role)
	}
	if profile.Team != "" {
		parts = append(parts, "Team: "+profile.Team)
	}
	if profile.Reports != "" {
		parts = append(parts, "Direct reports: "+profile.Reports)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// cleanJSON strips markdown fences from AI response if present.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
