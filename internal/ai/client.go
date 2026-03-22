package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"watchtower/internal/claude"
)

// Client wraps the Claude Code CLI for AI queries.
type Client struct {
	model     string
	dbPath    string // path to SQLite database for MCP server
	claudeCmd string // path to claude binary, default "claude"
}

// NewClient creates a new AI client that invokes the Claude Code CLI.
// dbPath is the path to the SQLite database; when non-empty, an MCP SQLite
// server is attached so the AI can query the database directly.
// claudePath is an optional explicit path to the claude binary; pass "" for default PATH lookup.
func NewClient(model, dbPath, claudePath string) *Client {
	return &Client{
		model:     model,
		dbPath:    dbPath,
		claudeCmd: claude.FindBinary(claudePath),
	}
}

// buildArgs constructs the common CLI arguments.
// When sessionID is non-empty, --resume is used instead of --system-prompt
// (the system prompt is already baked into the existing session).
func (c *Client) buildArgs(systemPrompt, userMessage, outputFormat, sessionID string) []string {
	args := []string{
		"-p", userMessage,
		"--output-format", outputFormat,
		"--model", c.model,
		"--allowedTools", "mcp__sqlite__*,Bash(sqlite3*)",
		"--disallowedTools", "Edit,Write,NotebookEdit",
	}
	if c.dbPath != "" {
		mcpConfig := c.buildMCPConfig()
		args = append(args, "--mcp-config", mcpConfig)
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--system-prompt", systemPrompt)
	}
	return args
}

// buildMCPConfig generates a JSON string for the SQLite MCP server config.
func (c *Client) buildMCPConfig() string {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"sqlite": map[string]any{
				"command": "npx",
				"args":    []string{"-y", "@anthropic-ai/mcp-server-sqlite", c.dbPath},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// Query sends a streaming request via the Claude Code CLI and returns channels
// for text chunks, errors, and the session ID. The sessionIDCh receives at most
// one value — the session ID from the "result" event — enabling multi-turn
// conversations via --resume. Pass a non-empty sessionID to resume an existing session.
func (c *Client) Query(ctx context.Context, systemPrompt, userMessage, sessionID string) (<-chan string, <-chan error, <-chan string) {
	textCh := make(chan string, 64)
	errCh := make(chan error, 1)
	sidCh := make(chan string, 1)

	go func() {
		defer close(textCh)
		defer close(errCh)
		defer close(sidCh)

		args := c.buildArgs(systemPrompt, userMessage, "stream-json", sessionID)
		cmd := exec.CommandContext(ctx, c.claudeCmd, args...)
		// Send SIGINT first for graceful shutdown; SIGKILL after 5s.
		cmd.Cancel = func() error {
			return cmd.Process.Signal(os.Interrupt)
		}
		cmd.WaitDelay = 5 * time.Second
		cmd.Env = append(os.Environ(), "PATH="+claude.RichPATH())

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errCh <- fmt.Errorf("creating stdout pipe: %w", err)
			return
		}

		// Cap stderr to 64KB to prevent unbounded memory growth.
		var stderrBuf strings.Builder
		cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: 64 * 1024}

		if err := cmd.Start(); err != nil {
			errCh <- classifyError(err, "")
			return
		}

		scanner := bufio.NewScanner(stdout)
		// Allow up to 1MB lines for large context responses
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var event streamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			// Capture session ID from result event
			if event.Type == "result" && event.SessionID != "" {
				sidCh <- event.SessionID
			}

			text := event.extractText()
			if text == "" {
				continue
			}

			select {
			case textCh <- text:
			case <-ctx.Done():
				// CommandContext handles killing the process; just reap it.
				_ = cmd.Wait()
				errCh <- ctx.Err()
				return
			}
		}

		if err := scanner.Err(); err != nil {
			_ = cmd.Wait()
			errCh <- fmt.Errorf("reading claude output: %w", err)
			return
		}

		if err := cmd.Wait(); err != nil {
			errCh <- classifyError(err, stderrBuf.String())
		}
	}()

	return textCh, errCh, sidCh
}

// QuerySync sends a non-streaming request via the Claude Code CLI and returns
// the full response text. Pass a non-empty sessionID to resume an existing session.
func (c *Client) QuerySync(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, error) {
	args := c.buildArgs(systemPrompt, userMessage, "text", sessionID)
	cmd := exec.CommandContext(ctx, c.claudeCmd, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Env = append(os.Environ(), "PATH="+claude.RichPATH())

	var stderrBuf strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: 64 * 1024}

	output, err := cmd.Output()
	if err != nil {
		return "", classifyError(err, stderrBuf.String())
	}

	return strings.TrimRight(string(output), "\n"), nil
}

// streamEvent represents a JSON event from Claude Code CLI stream-json output.
type streamEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype"`
	SessionID string         `json:"session_id"`
	Message   *streamMessage `json:"message"`
	Result    string         `json:"result"`
}

type streamMessage struct {
	Content []streamContent `json:"content"`
}

type streamContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// extractText returns the text content from a stream event, if any.
func (e *streamEvent) extractText() string {
	// Current format: {"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}
	if e.Type == "assistant" && e.Message != nil {
		var sb strings.Builder
		for _, c := range e.Message.Content {
			if c.Type == "text" {
				sb.WriteString(c.Text)
			}
		}
		return sb.String()
	}
	// Note: "result" events contain the full response but we skip them
	// to avoid duplicating text already streamed via "assistant" events.
	return ""
}

// limitedWriter wraps an io.Writer and stops writing after limit bytes.
type limitedWriter struct {
	w       io.Writer
	limit   int
	written int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	remaining := lw.limit - lw.written
	if remaining <= 0 {
		return len(p), nil // silently discard
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.written += n
	return n, err
}

// classifyError wraps CLI errors with user-friendly messages.
func classifyError(err error, stderr string) error {
	// Check if claude binary is not found
	if execErr, ok := err.(*exec.Error); ok {
		if execErr.Err == exec.ErrNotFound {
			return fmt.Errorf("claude CLI not found — install Claude Code first: https://docs.anthropic.com/en/docs/claude-code")
		}
	}

	// Check exit error for details
	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		stderrMsg := strings.TrimSpace(stderr)
		if stderrMsg == "" {
			stderrMsg = strings.TrimSpace(string(exitErr.Stderr))
		}

		if stderrMsg != "" {
			return fmt.Errorf("claude CLI failed (exit %d): %s", code, stderrMsg)
		}
		return fmt.Errorf("claude CLI failed with exit code %d", code)
	}

	return fmt.Errorf("claude CLI error: %w", err)
}
