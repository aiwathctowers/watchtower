package meeting

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

// MeetingPrepResult is the AI output for a single meeting.
type MeetingPrepResult struct {
	EventID       string         `json:"event_id"`
	Title         string         `json:"title"`
	StartTime     string         `json:"start_time"`
	TalkingPoints []TalkingPoint `json:"talking_points"`
	OpenItems     []OpenItem     `json:"open_items"`
	PeopleNotes   []PersonNote   `json:"people_notes"`
	SuggestedPrep []string       `json:"suggested_prep"`
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
func (p *Pipeline) PrepareForEvent(ctx context.Context, eventID string) (*MeetingPrepResult, error) {
	event, err := p.db.GetCalendarEventByID(eventID)
	if err != nil {
		return nil, fmt.Errorf("loading event: %w", err)
	}
	if event == nil {
		return nil, fmt.Errorf("event %q not found", eventID)
	}
	return p.prepareForEvent(ctx, *event)
}

// PrepareForNext generates meeting prep for the next upcoming event with >1 attendee.
func (p *Pipeline) PrepareForNext(ctx context.Context) (*MeetingPrepResult, error) {
	now := time.Now().UTC()
	events, err := p.db.GetCalendarEvents(db.CalendarEventFilter{
		FromTime: now.Format(time.RFC3339),
		Limit:    20,
	})
	if err != nil {
		return nil, fmt.Errorf("querying upcoming events: %w", err)
	}

	for _, ev := range events {
		var attendees []attendeeEntry
		if err := json.Unmarshal([]byte(ev.Attendees), &attendees); err == nil && len(attendees) > 1 {
			return p.prepareForEvent(ctx, ev)
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

func (p *Pipeline) prepareForEvent(ctx context.Context, event db.CalendarEvent) (*MeetingPrepResult, error) {
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

	systemPrompt := fmt.Sprintf(promptTmpl,
		userName,
		event.Title,
		meetingTime,
		langDirective,
		attendeesCtx,
		sharedCtx,
		profileCtx,
	)

	userMessage := fmt.Sprintf("Generate meeting prep for event %q (ID: %s) at %s on %s.",
		event.Title, event.ID, meetingTime, start.Format("2006-01-02"))

	aiResponse, _, _, err := p.generator.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return nil, fmt.Errorf("AI generation: %w", err)
	}

	// Parse AI response.
	var result MeetingPrepResult
	cleaned := cleanJSON(aiResponse)
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing AI response: %w (raw: %.500s)", err, aiResponse)
	}

	// Ensure event metadata is filled.
	result.EventID = event.ID
	result.Title = event.Title
	result.StartTime = event.StartTime

	return &result, nil
}

// gatherAttendeesContext builds the attendee section for the prompt.
func (p *Pipeline) gatherAttendeesContext(attendees []attendeeEntry) string {
	if len(attendees) == 0 {
		return "(no attendees)"
	}

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

		// Pending inbox items from this person.
		inboxItems, err := p.db.GetInboxItems(db.InboxFilter{Status: "pending", Limit: 5})
		if err == nil {
			for _, item := range inboxItems {
				if item.SenderUserID == a.SlackUserID {
					sb.WriteString(fmt.Sprintf("  [INBOX pending] %s\n", item.Snippet))
				}
			}
		}

		// Tasks where this person is ball_on.
		tasks, err := p.db.GetTasks(db.TaskFilter{Limit: 50})
		if err == nil {
			for _, t := range tasks {
				if t.BallOn == a.SlackUserID {
					sb.WriteString(fmt.Sprintf("  [TASK %s id=%d] %s\n", t.Priority, t.ID, t.Text))
				}
			}
		}

		sb.WriteString("\n")
	}
	return sb.String()
}

// gatherSharedContext builds the shared context section for the prompt.
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
			// Check if any attendee appears in participants JSON.
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

	// Recent digests (last 24h) — brief summaries.
	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)
	digests, err := p.db.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: float64(cutoff.Unix()),
		Limit:    20,
	})
	if err == nil {
		for _, d := range digests {
			if d.Summary != "" {
				sb.WriteString(fmt.Sprintf("[DIGEST #%s] %s\n", d.ChannelID, truncate(d.Summary, 200)))
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
