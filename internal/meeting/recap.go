package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"watchtower/internal/prompts"
)

// RecapResult is the AI output for a meeting recap.
type RecapResult struct {
	Summary       string   `json:"summary"`
	KeyDecisions  []string `json:"key_decisions"`
	ActionItems   []string `json:"action_items"`
	OpenQuestions []string `json:"open_questions"`
}

// GenerateRecap takes the raw text the user pasted and returns a structured
// recap. The pipeline does NOT persist — the CLI caller writes the result
// (this keeps the pipeline mockable without DB writes).
func (p *Pipeline) GenerateRecap(
	ctx context.Context,
	eventID, sourceText string,
) (*RecapResult, error) {
	trimmed := strings.TrimSpace(sourceText)
	if trimmed == "" {
		return nil, fmt.Errorf("source text is required")
	}

	// Event metadata (non-fatal if missing — CLI may pass an event ID for an
	// event that hasn't synced yet; we still produce a recap with placeholders).
	title, startTime, endTime, attendees, description := "(no event)", "", "", "", ""
	if p.db != nil {
		if ev, err := p.db.GetCalendarEventByID(eventID); err == nil && ev != nil {
			title = ev.Title
			startTime = ev.StartTime
			endTime = ev.EndTime
			attendees = ev.Attendees // JSON or comma-list — pass through verbatim
			description = ev.Description
		}
	}

	// Existing meeting_notes (pre-meeting topics + freeform notes) for context.
	topicsBlock, notesBlock := "(none)", "(none)"
	if p.db != nil {
		if notes, err := p.db.GetMeetingNotesForEvent(eventID); err == nil {
			var qs, ns []string
			for _, n := range notes {
				line := "- " + strings.TrimSpace(n.Text)
				if n.Type == "question" {
					qs = append(qs, line)
				} else if n.Type == "note" {
					ns = append(ns, line)
				}
			}
			if len(qs) > 0 {
				topicsBlock = strings.Join(qs, "\n")
			}
			if len(ns) > 0 {
				notesBlock = strings.Join(ns, "\n")
			}
		}
	}

	langDirective := ""
	if p.cfg != nil && p.cfg.Digest.Language != "" {
		langDirective = fmt.Sprintf("Respond in %s.", p.cfg.Digest.Language)
	}

	tmpl := p.loadRecapPrompt()
	systemPrompt := fmt.Sprintf(
		tmpl,
		title, startTime, endTime, attendees, description,
		topicsBlock, notesBlock, trimmed, langDirective,
	)
	userMessage := "Generate a recap from the meeting notes."

	aiResponse, _, _, err := p.generator.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return nil, fmt.Errorf("AI generation: %w", err)
	}

	cleaned := cleanJSON(aiResponse) // reuse helper from pipeline.go (same package)
	var raw RecapResult
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("parsing AI response: %w (raw: %.300s)", err, aiResponse)
	}

	raw.Summary = strings.TrimSpace(raw.Summary)
	raw.KeyDecisions = trimNonEmpty(raw.KeyDecisions)
	raw.ActionItems = trimNonEmpty(raw.ActionItems)
	raw.OpenQuestions = trimNonEmpty(raw.OpenQuestions)

	return &raw, nil
}

// trimNonEmpty trims whitespace from each string and drops empty entries.
func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (p *Pipeline) loadRecapPrompt() string {
	if p.promptStore != nil {
		if tmpl, _, err := p.promptStore.Get(prompts.MeetingRecap); err == nil && tmpl != "" {
			return tmpl
		}
	}
	if tmpl, ok := prompts.Defaults[prompts.MeetingRecap]; ok && tmpl != "" {
		return tmpl
	}
	return defaultRecapPromptFallback
}

const defaultRecapPromptFallback = `Recap the meeting. Event: %s (%s-%s, attendees: %s, description: %s). Topics: %s. Notes: %s. Raw: %s. %s
Return JSON: {"summary":"","key_decisions":[],"action_items":[],"open_questions":[]}`
