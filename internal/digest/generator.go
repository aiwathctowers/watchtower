package digest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"watchtower/internal/claude"
)

// limitedWriter wraps a writer and stops writing after limit bytes.
type limitedWriter struct {
	w       io.Writer
	limit   int
	written int
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written >= lw.limit {
		return len(p), nil
	}
	total := len(p)
	remaining := lw.limit - lw.written
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.written += n
	if err != nil {
		return n, err
	}
	// Report full length consumed to avoid short-write errors from callers.
	return total, nil
}

// ClaudeGenerator implements Generator by calling the Claude Code CLI.
type ClaudeGenerator struct {
	model      string
	claudePath string // optional override from config (claude_path)
}

// NewClaudeGenerator creates a generator that uses the Claude CLI.
// claudePath is an optional explicit path to the claude binary; pass "" for auto-detection.
func NewClaudeGenerator(model, claudePath string) *ClaudeGenerator {
	return &ClaudeGenerator{model: model, claudePath: claudePath}
}

// ValidateModel sends a minimal request to verify the configured model is valid.
// Returns nil if the model works, or an error describing the problem.
func (g *ClaudeGenerator) ValidateModel() error {
	claudeBin := claude.FindBinary(g.claudePath)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, claudeBin,
		"-p", "reply ok",
		"--output-format", "json",
		"--model", g.model,
		"--no-session-persistence",
		"--tools", "",
		"--max-tokens", "10",
	)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = os.TempDir()

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("model validation failed for %q: %w", g.model, err)
	}

	resp, err := parseCLIOutput(output)
	if err != nil {
		return fmt.Errorf("model validation: unexpected response for %q: %w", g.model, err)
	}
	if resp.IsError {
		return fmt.Errorf("model %q is not available: %s", g.model, resp.Result)
	}
	return nil
}

// cliUsage is the nested usage object in the Claude CLI response.
type cliUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// cliResponse is the JSON structure returned by `claude --output-format json`.
type cliResponse struct {
	Type       string   `json:"type"`
	Result     string   `json:"result"`
	CostUSD    float64  `json:"total_cost_usd"`
	DurationMS int      `json:"duration_ms"`
	NumTurns   int      `json:"num_turns"`
	IsError    bool     `json:"is_error"`
	SessionID  string   `json:"session_id"`
	Usage      cliUsage `json:"usage"`
}

// parseCLIOutput handles both output formats from the Claude CLI:
//   - Single JSON object: {"result": "...", ...}
//   - Streaming JSON array: [{"type":"system",...}, ..., {"type":"result","result":"...",...}]
func parseCLIOutput(output []byte) (*cliResponse, error) {
	trimmed := bytes.TrimSpace(output)

	// Try single JSON object first (legacy format)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var resp cliResponse
		if err := json.Unmarshal(trimmed, &resp); err == nil {
			return &resp, nil
		}
	}

	// Try JSON array (streaming format) — find the "result" event
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var events []cliResponse
		if err := json.Unmarshal(trimmed, &events); err != nil {
			return nil, fmt.Errorf("parsing claude CLI output array: %w", err)
		}
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Type == "result" {
				return &events[i], nil
			}
		}
		return nil, fmt.Errorf("no result event found in claude CLI streaming output (%d events)", len(events))
	}

	return nil, fmt.Errorf("unexpected claude CLI output format: %.200s", string(trimmed))
}

// Generate calls Claude CLI with the given prompt and returns the response text,
// token usage statistics, and the session ID for reuse.
// Each call creates a fresh session with --no-session-persistence to avoid
// disk clutter. The sessionID parameter is accepted for interface compatibility
// but ignored — session reuse via --resume is not supported by the current CLI.
func (g *ClaudeGenerator) Generate(ctx context.Context, systemPrompt, userMessage, sessionID string) (string, *Usage, string, error) {
	args := []string{
		"-p", userMessage,
		"--output-format", "json",
		"--model", g.model,
		"--no-session-persistence",
		"--tools", "",
	}

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	claudeBin := claude.FindBinary(g.claudePath)
	cmd := exec.CommandContext(ctx, claudeBin, args...)
	// Send SIGINT first for graceful shutdown; SIGKILL after 5s.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second
	// Run from ~/.config/watchtower as a stable working directory.
	configDir, _ := os.UserHomeDir()
	if configDir != "" {
		configDir = filepath.Join(configDir, ".config", "watchtower")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			configDir = os.TempDir()
		}
		cmd.Dir = configDir
	} else {
		cmd.Dir = os.TempDir()
	}
	// Build a clean environment:
	// - Enrich PATH so `#!/usr/bin/env node` resolves from macOS .app bundles.
	// - Remove CLAUDECODE to avoid "nested session" detection when launched
	//   from a parent process that is itself a Claude Code session.
	richPATH := "PATH=" + claude.RichPATH()
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			continue
		}
		if strings.HasPrefix(e, "PATH=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, richPATH)

	var stderrBuf strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderrBuf, limit: 64 * 1024}

	output, err := cmd.Output()
	if err != nil {
		if execErr, ok := err.(*exec.Error); ok {
			if execErr.Err == exec.ErrNotFound {
				return "", nil, "", fmt.Errorf("claude CLI not found at %q (PATH=%s) — install Claude Code first", claudeBin, os.Getenv("PATH"))
			}
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderrMsg := strings.TrimSpace(stderrBuf.String())
			if stderrMsg == "" {
				stderrMsg = strings.TrimSpace(string(exitErr.Stderr))
			}
			// Include any stdout output for debugging
			stdoutMsg := strings.TrimSpace(string(output))
			if stderrMsg == "" && stdoutMsg != "" {
				stderrMsg = stdoutMsg
			}
			if stderrMsg != "" {
				return "", nil, "", fmt.Errorf("claude CLI failed (exit %d): %s", exitErr.ExitCode(), stderrMsg)
			}
			return "", nil, "", fmt.Errorf("claude CLI failed with exit code %d", exitErr.ExitCode())
		}
		return "", nil, "", fmt.Errorf("claude CLI error: %w", err)
	}

	resp, err := parseCLIOutput(output)
	if err != nil {
		return "", nil, "", err
	}

	if resp.IsError {
		return "", nil, "", fmt.Errorf("claude returned error: %s", resp.Result)
	}

	if strings.TrimSpace(resp.Result) == "" {
		return "", nil, "", fmt.Errorf("claude returned empty result (turns=%d, tokens=%d+%d)", resp.NumTurns, resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}

	// Estimate our prompt size in tokens (~4 chars per token).
	promptTokens := (len(systemPrompt) + len(userMessage)) / 4
	// Total API tokens = everything the API processed (our content + CLI overhead).
	totalAPI := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
	usage := &Usage{
		InputTokens:    promptTokens,
		OutputTokens:   resp.Usage.OutputTokens,
		CostUSD:        resp.CostUSD,
		TotalAPITokens: totalAPI,
	}

	return resp.Result, usage, resp.SessionID, nil
}
