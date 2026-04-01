package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"watchtower/internal/ai"
)

// Client wraps the Codex CLI for AI queries (ask/chat).
type Client struct {
	model    string
	dbPath   string // path to SQLite database for MCP server
	codexCmd string // path to codex binary
}

// NewClient creates a new AI client that invokes the Codex CLI.
// dbPath is the path to the SQLite database; when non-empty, an MCP SQLite
// server is configured via .codex/config.toml.
// codexPath is an optional explicit path to the codex binary; pass "" for default lookup.
func NewClient(model, dbPath, codexPath string) *Client {
	return &Client{
		model:    model,
		dbPath:   dbPath,
		codexCmd: FindBinary(codexPath),
	}
}

// buildArgs constructs the CLI arguments for a codex exec call.
// workDir is an optional working directory to pass via --cd.
func (c *Client) buildArgs(systemPrompt, userMessage, workDir string) []string {
	args := []string{
		"exec",
		"--model", c.model,
		"--json",
		"--ephemeral",
		"-c", "approval_policy=never",
		"-c", "sandbox_mode=read-only",
	}
	if workDir != "" {
		args = append(args, "--cd", workDir)
	}
	if systemPrompt != "" {
		args = append(args, "-c", fmt.Sprintf("developer_instructions=%s", systemPrompt))
	}
	args = append(args, userMessage)
	return args
}

// Query sends a streaming request via the Codex CLI and returns channels
// for text chunks, errors, and the session ID (always empty for Codex).
func (c *Client) Query(ctx context.Context, systemPrompt, userMessage, _ string) (<-chan string, <-chan error, <-chan string) {
	textCh := make(chan string, 64)
	errCh := make(chan error, 1)
	sidCh := make(chan string, 1)

	go func() {
		defer close(textCh)
		defer close(errCh)
		defer close(sidCh)

		// Set up MCP config if database path is provided.
		var workDir string
		if c.dbPath != "" {
			tmpDir, mcpErr := mcpWorkDir(c.dbPath)
			if mcpErr != nil {
				errCh <- mcpErr
				return
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()
			workDir = tmpDir
		}

		args := c.buildArgs(systemPrompt, userMessage, workDir)

		cmd := exec.CommandContext(ctx, c.codexCmd, args...)
		cmd.Cancel = func() error {
			return cmd.Process.Signal(os.Interrupt)
		}
		cmd.WaitDelay = 5 * time.Second

		// Build clean environment with enriched PATH.
		var env []string
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "PATH=") {
				continue
			}
			env = append(env, e)
		}
		cmd.Env = append(env, "PATH="+RichPATH())

		var stderrBuf strings.Builder
		cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: 64 * 1024}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errCh <- fmt.Errorf("creating stdout pipe: %w", err)
			return
		}

		if err := cmd.Start(); err != nil {
			errCh <- classifyError(err, "", c.codexCmd)
			return
		}

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var event CodexEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			if event.Error != nil {
				_ = cmd.Wait()
				errCh <- fmt.Errorf("codex error: %s", event.Error.Message)
				return
			}

			// Stream agent_message content as it arrives.
			if event.Item != nil && event.Item.Type == "agent_message" && event.Item.Content != "" {
				select {
				case textCh <- event.Item.Content:
				case <-ctx.Done():
					_ = cmd.Wait()
					errCh <- ctx.Err()
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			_ = cmd.Wait()
			errCh <- fmt.Errorf("reading codex output: %w", err)
			return
		}

		if err := cmd.Wait(); err != nil {
			errCh <- classifyError(err, stderrBuf.String(), c.codexCmd)
		}

		// Codex doesn't support session resumption — emit empty session ID.
		sidCh <- ""
	}()

	return textCh, errCh, sidCh
}

// QuerySync sends a non-streaming request via the Codex CLI and returns
// the full response text and token usage.
func (c *Client) QuerySync(ctx context.Context, systemPrompt, userMessage, _ string) (string, *ai.Usage, error) {
	// Set up MCP config if database path is provided.
	var workDir string
	if c.dbPath != "" {
		tmpDir, mcpErr := mcpWorkDir(c.dbPath)
		if mcpErr != nil {
			return "", nil, mcpErr
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()
		workDir = tmpDir
	}

	args := c.buildArgs(systemPrompt, userMessage, workDir)

	cmd := exec.CommandContext(ctx, c.codexCmd, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	// Build clean environment with enriched PATH.
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, "PATH="+RichPATH())

	var stderrBuf strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: 64 * 1024}

	output, err := cmd.Output()
	if err != nil {
		return "", nil, classifyError(err, stderrBuf.String(), c.codexCmd)
	}

	result, codexUsage, parseErr := parseJSONLOutput(output)
	if parseErr != nil {
		return "", nil, parseErr
	}

	usage := &ai.Usage{
		InputTokens:  codexUsage.InputTokens,
		OutputTokens: codexUsage.OutputTokens,
	}

	return result, usage, nil
}
