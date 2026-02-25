package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSystemPrompt_ContainsWorkspaceInfo(t *testing.T) {
	prompt := BuildSystemPrompt("my-company", "my-company")

	assert.Contains(t, prompt, `"my-company" workspace`)
	assert.Contains(t, prompt, "my-company.slack.com")
	assert.Contains(t, prompt, "Current time:")
	assert.Contains(t, prompt, "You are Watchtower")
}

func TestBuildSystemPrompt_ContainsGuidelines(t *testing.T) {
	prompt := BuildSystemPrompt("test-ws", "test-ws")

	assert.Contains(t, prompt, "Be concise")
	assert.Contains(t, prompt, "permalinks")
	assert.Contains(t, prompt, "markdown")
}

func TestAssembleUserMessage_ContextAndQuestion(t *testing.T) {
	msg := AssembleUserMessage(
		"#general | 2025-02-24 14:30 | @alice: hello",
		"What happened today?",
	)

	assert.Contains(t, msg, "=== Message Context ===")
	assert.Contains(t, msg, "#general | 2025-02-24 14:30 | @alice: hello")
	assert.Contains(t, msg, "=== Question ===")
	assert.Contains(t, msg, "What happened today?")

	// Verify ordering: context before question
	contextIdx := strings.Index(msg, "Message Context")
	questionIdx := strings.Index(msg, "Question")
	assert.Less(t, contextIdx, questionIdx)
}

func TestAssembleUserMessage_NoContext(t *testing.T) {
	msg := AssembleUserMessage("", "question?")

	assert.NotContains(t, msg, "Message Context")
	assert.Contains(t, msg, "=== Question ===")
}

func TestAssembleUserMessage_QuestionOnly(t *testing.T) {
	msg := AssembleUserMessage("", "What's up?")

	assert.NotContains(t, msg, "Message Context")
	assert.Contains(t, msg, "=== Question ===")
	assert.Contains(t, msg, "What's up?")
}
