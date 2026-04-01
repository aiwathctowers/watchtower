package codex

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

	"watchtower/internal/digest"
)

// sessionSourceKey is the context key used by digest.WithSource.
// We re-declare it here so we can read the source label from context
// for model routing, matching the pattern in digest/generator.go.
type sessionSourceKey struct{}

// CodexGenerator implements digest.Generator by calling the Codex CLI.
type CodexGenerator struct {
	model     string
	codexPath string
}

// NewCodexGenerator creates a generator that uses the Codex CLI.
// codexPath is an optional explicit path to the codex binary; pass "" for auto-detection.
func NewCodexGenerator(model, codexPath string) *CodexGenerator {
	return &CodexGenerator{model: model, codexPath: codexPath}
}

// Generate calls Codex CLI with the given prompt and returns the response text,
// token usage statistics, and an empty session ID (Codex uses --ephemeral).
func (g *CodexGenerator) Generate(ctx context.Context, systemPrompt, userMessage, _ string) (string, *digest.Usage, string, error) {
	model := g.model
	if s, ok := ctx.Value(sessionSourceKey{}).(string); ok && s != "" {
		model = ModelForSource(s)
	}

	codexBin := FindBinary(g.codexPath)

	args := []string{
		"exec",
		"--model", model,
		"--json",
		"--ephemeral",
		"--skip-git-repo-check",
		"-c", "approval_policy=never",
		"-c", "sandbox_mode=read-only",
	}
	if systemPrompt != "" {
		args = append(args, "-c", fmt.Sprintf("developer_instructions=%s", systemPrompt))
	}
	args = append(args, userMessage)

	cmd := exec.CommandContext(ctx, codexBin, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = os.TempDir()

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
		return "", nil, "", classifyError(err, stderrBuf.String(), codexBin)
	}

	// Parse JSONL output — find last item.completed with agent_message
	result, usage, parseErr := parseJSONLOutput(output)
	if parseErr != nil {
		return "", nil, "", parseErr
	}

	if strings.TrimSpace(result) == "" {
		return "", nil, "", fmt.Errorf("codex returned empty result")
	}

	digestUsage := &digest.Usage{
		Model:        model,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	}

	return result, digestUsage, "", nil
}

// parseJSONLOutput parses Codex JSONL output and extracts the final agent_message
// content and accumulated usage.
func parseJSONLOutput(output []byte) (string, *CodexUsage, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastContent string
	totalUsage := &CodexUsage{}

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
			return "", nil, fmt.Errorf("codex error: %s", event.Error.Message)
		}

		if event.Usage != nil {
			totalUsage.InputTokens += event.Usage.InputTokens
			totalUsage.OutputTokens += event.Usage.OutputTokens
		}

		if event.Type == "item.completed" && event.Item != nil && event.Item.Type == "agent_message" {
			lastContent = event.Item.MessageText()
		}
	}

	if lastContent == "" {
		return "", nil, fmt.Errorf("no agent_message found in codex output")
	}

	return lastContent, totalUsage, nil
}

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
	return total, nil
}

// classifyError wraps CLI errors with user-friendly messages.
func classifyError(err error, stderr, codexBin string) error {
	if execErr, ok := err.(*exec.Error); ok {
		if execErr.Err == exec.ErrNotFound {
			return fmt.Errorf("codex CLI not found at %q — install Codex CLI first", codexBin)
		}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderrMsg := strings.TrimSpace(stderr)
		if stderrMsg == "" {
			stderrMsg = strings.TrimSpace(string(exitErr.Stderr))
		}
		if stderrMsg != "" {
			return fmt.Errorf("codex CLI failed (exit %d): %s", exitErr.ExitCode(), stderrMsg)
		}
		return fmt.Errorf("codex CLI failed with exit code %d", exitErr.ExitCode())
	}
	return fmt.Errorf("codex CLI error: %w", err)
}
