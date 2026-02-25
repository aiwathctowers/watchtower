package ai

import (
	"fmt"
	"strings"
	"time"
)

const systemPromptTemplate = `You are Watchtower, an AI assistant that helps analyze Slack workspace activity for the "%s" workspace (domain: %s.slack.com).

Current time: %s

Guidelines:
- Be concise and direct in your responses.
- When referencing specific messages, include Slack permalinks so the user can jump to the original conversation.
- Use the user's language and tone — if they ask casually, respond casually.
- When summarizing activity, organize by channel or topic, not chronologically.
- Highlight important items: decisions made, action items, questions left unanswered, and anything unusual.
- If you don't have enough context to answer confidently, say so rather than guessing.
- Format your response using markdown for readability.`

// BuildSystemPrompt generates the system prompt with workspace context.
func BuildSystemPrompt(workspaceName, domain string) string {
	now := time.Now().Format("2006-01-02 15:04 MST")
	return fmt.Sprintf(systemPromptTemplate, workspaceName, domain, now)
}

// AssembleUserMessage combines the workspace summary, message context, and
// user question into a single prompt for the AI.
func AssembleUserMessage(summary, context, question string) string {
	var b strings.Builder

	if summary != "" {
		b.WriteString("=== Workspace Summary ===\n")
		b.WriteString(summary)
		b.WriteString("\n\n")
	}

	if context != "" {
		b.WriteString("=== Message Context ===\n")
		b.WriteString(context)
		b.WriteString("\n\n")
	}

	b.WriteString("=== Question ===\n")
	b.WriteString(question)

	return b.String()
}
