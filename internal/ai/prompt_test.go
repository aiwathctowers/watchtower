package ai

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"watchtower/internal/db"
)

func TestBuildSystemPrompt_ContainsWorkspaceInfo(t *testing.T) {
	prompt := BuildSystemPrompt("my-company", "my-company", "/path/to/db.sqlite", "CREATE TABLE test;")

	assert.Contains(t, prompt, `"my-company"`)
	assert.Contains(t, prompt, "my-company.slack.com")
	assert.Contains(t, prompt, "Current time:")
	assert.Contains(t, prompt, "You are Watchtower")
}

func TestBuildSystemPrompt_ContainsDBAccess(t *testing.T) {
	prompt := BuildSystemPrompt("test-ws", "test-ws", "/tmp/watchtower.db", "CREATE TABLE messages;")

	assert.Contains(t, prompt, "/tmp/watchtower.db")
	assert.Contains(t, prompt, "sqlite3")
	assert.Contains(t, prompt, "CREATE TABLE messages;")
}

func TestBuildSystemPrompt_ContainsGuidelines(t *testing.T) {
	prompt := BuildSystemPrompt("test-ws", "test-ws", "/tmp/db", "schema")

	assert.Contains(t, prompt, "concise")
	assert.Contains(t, prompt, "permalink")
	assert.Contains(t, prompt, "markdown")
}

func TestBuildSystemPrompt_ContainsQueryPatterns(t *testing.T) {
	prompt := BuildSystemPrompt("test-ws", "test-ws", "/tmp/db", "schema")

	assert.Contains(t, prompt, "QUERY PATTERNS")
	assert.Contains(t, prompt, "messages_fts MATCH")
	assert.Contains(t, prompt, "ts_unix")
}

func TestBuildSystemPrompt_MustQueryDB(t *testing.T) {
	prompt := BuildSystemPrompt("test-ws", "test-ws", "/tmp/db", "schema")

	assert.Contains(t, prompt, "MUST query the database")
	assert.Contains(t, prompt, "read_query")
	assert.Contains(t, prompt, "MCP tools")
}

func TestBuildSystemPrompt_SanitizesInputs(t *testing.T) {
	prompt := BuildSystemPrompt("my company!", "my domain<>", "/tmp/db", "schema")

	assert.Contains(t, prompt, "mycompany")
	assert.Contains(t, prompt, "mydomain")
	assert.NotContains(t, prompt, "!")
	assert.NotContains(t, prompt, "<>")
}

func TestBuildSystemPrompt_EmptyInputsGetDefaults(t *testing.T) {
	prompt := BuildSystemPrompt("", "", "/tmp/db", "schema")

	assert.Contains(t, prompt, "unknown")
}

func TestAssembleUserMessage_QuestionOnly(t *testing.T) {
	msg := AssembleUserMessage("What's up?", "")

	assert.Equal(t, "What's up?", msg)
}

func TestAssembleUserMessage_WithTimeHints(t *testing.T) {
	msg := AssembleUserMessage("What happened?", "Time range: 2025-02-26 10:00 UTC to 2025-02-26 14:00 UTC (ts_unix BETWEEN 1740564000 AND 1740578400)")

	assert.Contains(t, msg, "What happened?")
	assert.Contains(t, msg, "ts_unix BETWEEN")
}

func TestFormatTimeHints_WithTimeRange(t *testing.T) {
	from := time.Date(2025, 2, 26, 10, 0, 0, 0, time.UTC)
	to := time.Date(2025, 2, 26, 14, 0, 0, 0, time.UTC)

	pq := ParsedQuery{
		TimeRange: &TimeRange{From: from, To: to},
	}

	hints := FormatTimeHints(pq)

	assert.Contains(t, hints, "2025-02-26 10:00 UTC")
	assert.Contains(t, hints, "2025-02-26 14:00 UTC")
	assert.Contains(t, hints, "ts_unix BETWEEN")
	assert.Contains(t, hints, "1740564000")
}

func TestFormatTimeHints_NoTimeRange(t *testing.T) {
	pq := ParsedQuery{}
	hints := FormatTimeHints(pq)
	assert.Empty(t, hints)
}

func TestDBSchemaNotEmpty(t *testing.T) {
	assert.NotEmpty(t, db.Schema)
	assert.Contains(t, db.Schema, "CREATE TABLE")
	assert.Contains(t, db.Schema, "messages")
	assert.Contains(t, db.Schema, "channels")
	assert.Contains(t, db.Schema, "users")
	assert.Contains(t, db.Schema, "messages_fts")
}
