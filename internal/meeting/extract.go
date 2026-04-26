package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"watchtower/internal/prompts"
)

// ExtractedTopic is a single discussion topic parsed from user-pasted text.
type ExtractedTopic struct {
	Text     string `json:"text"`
	Priority string `json:"priority"` // high|medium|low (optional hint)
}

// ExtractTopicsResult is the AI output for discussion-topic extraction.
type ExtractTopicsResult struct {
	Topics []ExtractedTopic `json:"topics"`
	Notes  string           `json:"notes"`
}

// ExtractDiscussionTopics splits a raw blob of text (recap, pasted notes, rambling
// status update) into discrete discussion topics suitable for seeding the
// meeting_notes Discussion Topics section.
//
// eventTitle is optional context; passing "" is fine.
func (p *Pipeline) ExtractDiscussionTopics(
	ctx context.Context,
	text string,
	eventTitle string,
) (*ExtractTopicsResult, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return &ExtractTopicsResult{Topics: []ExtractedTopic{}}, nil
	}

	langDirective := ""
	if p.cfg != nil && p.cfg.Digest.Language != "" {
		langDirective = fmt.Sprintf("Respond in %s", p.cfg.Digest.Language)
	}

	titleCtx := "(no event title)"
	if eventTitle != "" {
		titleCtx = eventTitle
	}

	tmpl := p.loadExtractTopicsPrompt()

	// Template args: 1=eventTitle, 2=langDirective, 3=rawText
	systemPrompt := fmt.Sprintf(tmpl, titleCtx, langDirective, trimmed)
	userMessage := "Extract discussion topics from the raw text."

	aiResponse, _, _, err := p.generator.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return nil, fmt.Errorf("AI generation: %w", err)
	}

	cleaned := cleanJSON(aiResponse)
	var result ExtractTopicsResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing AI response: %w (raw: %.300s)", err, aiResponse)
	}

	// Defensive: normalize priorities and drop empty topics.
	cleanedTopics := make([]ExtractedTopic, 0, len(result.Topics))
	for _, t := range result.Topics {
		txt := strings.TrimSpace(t.Text)
		if txt == "" {
			continue
		}
		pr := strings.ToLower(strings.TrimSpace(t.Priority))
		switch pr {
		case "high", "medium", "low":
		default:
			pr = ""
		}
		cleanedTopics = append(cleanedTopics, ExtractedTopic{Text: txt, Priority: pr})
	}
	result.Topics = cleanedTopics
	return &result, nil
}

func (p *Pipeline) loadExtractTopicsPrompt() string {
	if p.promptStore != nil {
		tmpl, _, err := p.promptStore.Get(prompts.MeetingExtractTopics)
		if err == nil && tmpl != "" {
			return tmpl
		}
	}
	if tmpl, ok := prompts.Defaults[prompts.MeetingExtractTopics]; ok && tmpl != "" {
		return tmpl
	}
	return defaultExtractTopicsPromptFallback
}

// defaultExtractTopicsPromptFallback is used when the prompts package is not
// yet migrated (defensive — should not normally be reached).
const defaultExtractTopicsPromptFallback = `You split a raw blob of meeting-prep text into atomic discussion topics.

=== MEETING TITLE ===
%s
=== /MEETING TITLE ===

%s

=== RAW TEXT ===
%s
=== /RAW TEXT ===

Return ONLY a JSON object (no markdown fences, no commentary) matching:

{
  "topics": [
    {"text": "string (<=200 chars, imperative where possible)", "priority": "high|medium|low|"}
  ],
  "notes": "optional short note about what was skipped or merged"
}

Rules:
- Produce 1-15 atomic topics. Merge near-duplicates. Skip pure recap unless it flags action.
- Each topic is a single idea that can be discussed independently.
- Strip markdown syntax (**bold**, numbered lists, emojis) from topic text.
- priority is optional. Use "" when unclear. Use "high" only for blockers or explicit urgency signals.
- Return empty topics array if the text has no actionable content.`
