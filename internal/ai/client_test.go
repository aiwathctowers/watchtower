package ai

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeMockClaude creates a shell script that mimics the claude CLI for testing.
func writeMockClaude(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755)
	require.NoError(t, err)
	return path
}

func TestNewClient_DefaultClaudeCmd(t *testing.T) {
	c := NewClient("model", "", "")
	assert.Contains(t, c.claudeCmd, "claude")
	assert.Equal(t, "model", c.model)
}

func TestBuildArgs(t *testing.T) {
	c := NewClient("claude-sonnet-4-6", "", "")
	args := c.buildArgs("system prompt", "user message", "text", "")

	assert.Contains(t, args, "-p")
	assert.Contains(t, args, "user message")
	assert.Contains(t, args, "--system-prompt")
	assert.Contains(t, args, "system prompt")
	assert.Contains(t, args, "--output-format")
	assert.Contains(t, args, "text")
	assert.Contains(t, args, "--model")
	assert.Contains(t, args, "claude-sonnet-4-6")
	assert.Contains(t, args, "--allowedTools")
	assert.Contains(t, args, "mcp__sqlite__*,Bash(sqlite3*)")
	assert.Contains(t, args, "--disallowedTools")
	assert.Contains(t, args, "Edit,Write,NotebookEdit")
	assert.NotContains(t, args, "--resume")
}

func TestBuildArgs_WithDBPath(t *testing.T) {
	c := NewClient("claude-sonnet-4-6", "/tmp/test.db", "")
	args := c.buildArgs("system prompt", "user message", "text", "")

	assert.Contains(t, args, "--mcp-config")
	// Find the mcp-config value and verify it contains the DB path
	for i, a := range args {
		if a == "--mcp-config" && i+1 < len(args) {
			assert.Contains(t, args[i+1], "/tmp/test.db")
			assert.Contains(t, args[i+1], "mcpServers")
			assert.Contains(t, args[i+1], "sqlite")
		}
	}
}

func TestBuildArgs_WithoutDBPath(t *testing.T) {
	c := NewClient("claude-sonnet-4-6", "", "")
	args := c.buildArgs("system prompt", "user message", "text", "")

	assert.NotContains(t, args, "--mcp-config")
}

func TestBuildArgs_WithSessionID(t *testing.T) {
	c := NewClient("claude-sonnet-4-6", "", "")
	args := c.buildArgs("system prompt", "user message", "stream-json", "session-123")

	assert.Contains(t, args, "--resume")
	assert.Contains(t, args, "session-123")
	assert.NotContains(t, args, "--system-prompt")
}

func TestQuerySync_Success(t *testing.T) {
	mockPath := writeMockClaude(t, `echo "Hello from Claude"`)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	result, err := c.QuerySync(context.Background(), "system", "hello", "")
	require.NoError(t, err)
	assert.Equal(t, "Hello from Claude", result)
}

func TestQuerySync_TrimsTrailingNewlines(t *testing.T) {
	mockPath := writeMockClaude(t, `printf "response\n\n"`)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	result, err := c.QuerySync(context.Background(), "system", "hello", "")
	require.NoError(t, err)
	assert.Equal(t, "response", result)
}

func TestQuerySync_ExitError(t *testing.T) {
	mockPath := writeMockClaude(t, `echo "something went wrong" >&2; exit 1`)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	_, err := c.QuerySync(context.Background(), "system", "hello", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude CLI failed")
	assert.Contains(t, err.Error(), "something went wrong")
}

func TestQuerySync_ContextCancellation(t *testing.T) {
	mockPath := writeMockClaude(t, `sleep 10; echo "too late"`)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := c.QuerySync(ctx, "system", "hello", "")
	require.Error(t, err)
}

func TestQuery_StreamingSuccess(t *testing.T) {
	// Mock script outputs stream-json events
	script := `
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello "}]}}\n'
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"world!"}]}}\n'
printf '{"type":"result","subtype":"success","result":"Hello world!","session_id":"sess-abc"}\n'
`
	mockPath := writeMockClaude(t, script)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, sidCh := c.Query(context.Background(), "system", "hello", "")

	var result strings.Builder
	for chunk := range textCh {
		result.WriteString(chunk)
	}

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, "Hello world!", result.String())

	sid := <-sidCh
	assert.Equal(t, "sess-abc", sid)
}

func TestQuery_StreamingIgnoresNonTextEvents(t *testing.T) {
	script := `
printf '{"type":"system","subtype":"init","session_id":"test"}\n'
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"response"}]}}\n'
printf '{"type":"result","subtype":"success","result":"response","session_id":"sess-xyz"}\n'
`
	mockPath := writeMockClaude(t, script)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, _ := c.Query(context.Background(), "system", "hello", "")

	var result strings.Builder
	for chunk := range textCh {
		result.WriteString(chunk)
	}

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, "response", result.String())
}

func TestQuery_StreamingError(t *testing.T) {
	mockPath := writeMockClaude(t, `echo "error occurred" >&2; exit 1`)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, _ := c.Query(context.Background(), "system", "hello", "")

	for range textCh {
	}

	err := <-errCh
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude CLI failed")
}

func TestQuery_ContextCancellation(t *testing.T) {
	mockPath := writeMockClaude(t, `sleep 10`)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	textCh, errCh, _ := c.Query(ctx, "system", "hello", "")

	for range textCh {
	}

	err := <-errCh
	if err != nil {
		// Either context error, kill error, or pipe read error is acceptable
		msg := err.Error()
		assert.True(t, strings.Contains(msg, "context") ||
			strings.Contains(msg, "signal") ||
			strings.Contains(msg, "killed") ||
			strings.Contains(msg, "claude CLI") ||
			strings.Contains(msg, "reading claude output"),
			"unexpected error: %s", msg)
	}
}

func TestQuery_SessionIDFromResultEvent(t *testing.T) {
	script := `
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}\n'
printf '{"type":"result","subtype":"success","result":"hi","session_id":"new-session-42"}\n'
`
	mockPath := writeMockClaude(t, script)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, sidCh := c.Query(context.Background(), "system", "hello", "")

	for range textCh {
	}
	require.NoError(t, <-errCh)

	sid := <-sidCh
	assert.Equal(t, "new-session-42", sid)
}

func TestQuery_NoSessionIDWhenMissing(t *testing.T) {
	script := `
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}\n'
printf '{"type":"result","subtype":"success","result":"hi"}\n'
`
	mockPath := writeMockClaude(t, script)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, sidCh := c.Query(context.Background(), "system", "hello", "")

	for range textCh {
	}
	require.NoError(t, <-errCh)

	// Channel should be closed with no value
	sid, ok := <-sidCh
	assert.False(t, ok)
	assert.Empty(t, sid)
}

func TestClassifyError_NotFound(t *testing.T) {
	err := classifyError(&exec.Error{Name: "claude", Err: exec.ErrNotFound}, "")
	assert.Contains(t, err.Error(), "claude CLI not found")
}

func TestClassifyError_ExitError(t *testing.T) {
	err := classifyError(&exec.ExitError{}, "auth failed")
	assert.Contains(t, err.Error(), "claude CLI failed")
	assert.Contains(t, err.Error(), "auth failed")
}

func TestStreamEvent_ExtractText(t *testing.T) {
	tests := []struct {
		name     string
		event    streamEvent
		expected string
	}{
		{"assistant message", streamEvent{Type: "assistant", Message: &streamMessage{Content: []streamContent{{Type: "text", Text: "hello"}}}}, "hello"},
		{"result event", streamEvent{Type: "result", Subtype: "success", Result: "full"}, ""},
		{"system event", streamEvent{Type: "system", Subtype: "init"}, ""},
		{"assistant no message", streamEvent{Type: "assistant"}, ""},
		{"assistant empty content", streamEvent{Type: "assistant", Message: &streamMessage{}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.extractText())
		})
	}
}

func TestQuery_IgnoresMalformedJSON(t *testing.T) {
	script := `
printf 'not json\n'
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"valid"}]}}\n'
printf '{broken json\n'
`
	mockPath := writeMockClaude(t, script)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, _ := c.Query(context.Background(), "system", "hello", "")

	var result strings.Builder
	for chunk := range textCh {
		result.WriteString(chunk)
	}

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, "valid", result.String())
}

func TestQuery_SkipsEmptyLines(t *testing.T) {
	script := `
printf '\n'
printf '{"type":"assistant","message":{"content":[{"type":"text","text":"data"}]}}\n'
printf '\n'
`
	mockPath := writeMockClaude(t, script)

	c := NewClient("test-model", "", "")
	c.claudeCmd = mockPath

	textCh, errCh, _ := c.Query(context.Background(), "system", "hello", "")

	var result strings.Builder
	for chunk := range textCh {
		result.WriteString(chunk)
	}

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, "data", result.String())
}
