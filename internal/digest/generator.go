package digest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ClaudeGenerator implements Generator by calling the Claude Code CLI.
type ClaudeGenerator struct {
	model string
}

// NewClaudeGenerator creates a generator that uses the Claude CLI.
func NewClaudeGenerator(model string) *ClaudeGenerator {
	return &ClaudeGenerator{model: model}
}

// cliResponse is the JSON structure returned by `claude --output-format json`.
type cliResponse struct {
	Type               string  `json:"type"`
	Result             string  `json:"result"`
	CostUSD            float64 `json:"total_cost_usd"`
	DurationMS         int     `json:"duration_ms"`
	NumTurns           int     `json:"num_turns"`
	IsError            bool    `json:"is_error"`
	SessionID          string  `json:"session_id"`
	InputTokens        int     `json:"input_tokens"`
	OutputTokens       int     `json:"output_tokens"`
	CacheReadTokens    int     `json:"cache_read_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
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

// Generate calls Claude CLI with the given prompt and returns the response text
// along with token usage statistics.
func (g *ClaudeGenerator) Generate(ctx context.Context, systemPrompt, userMessage string) (string, *Usage, error) {
	args := []string{
		"-p", userMessage,
		"--output-format", "json",
		"--model", g.model,
	}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	// Run from a temp dir so the CLI doesn't load project-specific settings.
	cmd.Dir = os.TempDir()

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	output, err := cmd.Output()
	if err != nil {
		if execErr, ok := err.(*exec.Error); ok {
			if execErr.Err == exec.ErrNotFound {
				return "", nil, fmt.Errorf("claude CLI not found — install Claude Code first")
			}
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderrMsg := strings.TrimSpace(stderrBuf.String())
			if stderrMsg == "" {
				stderrMsg = strings.TrimSpace(string(exitErr.Stderr))
			}
			if stderrMsg != "" {
				return "", nil, fmt.Errorf("claude CLI failed (exit %d): %s", exitErr.ExitCode(), stderrMsg)
			}
			return "", nil, fmt.Errorf("claude CLI failed with exit code %d", exitErr.ExitCode())
		}
		return "", nil, fmt.Errorf("claude CLI error: %w", err)
	}

	resp, err := parseCLIOutput(output)
	if err != nil {
		return "", nil, err
	}

	if resp.IsError {
		return "", nil, fmt.Errorf("claude returned error: %s", resp.Result)
	}

	if strings.TrimSpace(resp.Result) == "" {
		return "", nil, fmt.Errorf("claude returned empty result (turns=%d, tokens=%d+%d)", resp.NumTurns, resp.InputTokens, resp.OutputTokens)
	}

	usage := &Usage{
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      resp.CostUSD,
	}

	return resp.Result, usage, nil
}
