package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"watchtower/internal/config"

	"github.com/spf13/cobra"
)

var (
	aiFlagModel        string
	aiFlagSessionID    string
	aiFlagSystemPrompt string
	aiFlagDBPath       string
	aiFlagAllowedTools string
)

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "AI provider interface (used by desktop app)",
}

var aiQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Stream an AI query through the configured provider",
	Long:  "Sends a prompt to the configured AI provider (Claude or Codex) and streams the response as JSON lines. Intended for programmatic use by the desktop app.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAIQuery,
}

var aiTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test AI provider connectivity",
	Long:  "Verifies that the configured AI provider is available and responds. Outputs JSON with status.",
	RunE:  runAITest,
}

// aiStreamEvent is a JSON line emitted by `watchtower ai query`.
// Maps 1:1 to Swift StreamEvent enum.
type aiStreamEvent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

func init() {
	rootCmd.AddCommand(aiCmd)
	aiCmd.AddCommand(aiQueryCmd)
	aiCmd.AddCommand(aiTestCmd)

	aiQueryCmd.Flags().StringVar(&aiFlagModel, "model", "", "override AI model")
	aiQueryCmd.Flags().StringVar(&aiFlagSessionID, "session-id", "", "resume session (Claude only)")
	aiQueryCmd.Flags().StringVar(&aiFlagSystemPrompt, "system-prompt", "", "system prompt")
	aiQueryCmd.Flags().StringVar(&aiFlagDBPath, "db-path", "", "SQLite database path for MCP (overrides default)")
	aiQueryCmd.Flags().StringVar(&aiFlagAllowedTools, "allowed-tools", "", "additional allowed tools (comma-separated)")
}

func runAIQuery(_ *cobra.Command, args []string) error {
	prompt := args[0]
	enc := json.NewEncoder(os.Stdout)

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return emitError(enc, fmt.Sprintf("loading config: %v", err))
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	applyProviderOverride(cfg)
	if err := cfg.ValidateWorkspace(); err != nil {
		return emitError(enc, fmt.Sprintf("invalid config: %v", err))
	}

	// Determine database path
	dbPath := aiFlagDBPath
	if dbPath == "" {
		dbPath = cfg.DBPath()
	}

	// Determine model
	model := cfg.AI.Model
	if aiFlagModel != "" {
		model = aiFlagModel
	}
	origModel := cfg.AI.Model
	cfg.AI.Model = model
	aiClient := newAIClient(cfg, dbPath)
	cfg.AI.Model = origModel

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	systemPrompt := aiFlagSystemPrompt
	textCh, errCh, sidCh := aiClient.Query(ctx, systemPrompt, prompt, aiFlagSessionID)

	// Drain text channel (main stream)
	for text := range textCh {
		_ = enc.Encode(aiStreamEvent{Type: "text", Text: text})
	}

	// Drain session ID (at most one value)
	for sid := range sidCh {
		if sid != "" {
			_ = enc.Encode(aiStreamEvent{Type: "session_id", SessionID: sid})
		}
	}

	// Check for errors
	for err := range errCh {
		if err != nil {
			_ = enc.Encode(aiStreamEvent{Type: "error", Error: err.Error()})
		}
	}

	_ = enc.Encode(aiStreamEvent{Type: "done"})
	return nil
}

func runAITest(_ *cobra.Command, _ []string) error {
	enc := json.NewEncoder(os.Stdout)

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return enc.Encode(map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("loading config: %v", err),
		})
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	applyProviderOverride(cfg)

	model := cfg.AI.Model
	if aiFlagModel != "" {
		model = aiFlagModel
	}

	// Quick connectivity check — ask for a minimal response
	origModel := cfg.AI.Model
	cfg.AI.Model = model
	aiClient := newAIClient(cfg, "")
	cfg.AI.Model = origModel

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	_, _, err = aiClient.QuerySync(ctx, "", "respond with exactly: OK", "")
	if err != nil {
		return enc.Encode(map[string]any{
			"ok":       false,
			"error":    err.Error(),
			"provider": cfg.AI.Provider,
			"model":    model,
		})
	}

	return enc.Encode(map[string]any{
		"ok":       true,
		"provider": cfg.AI.Provider,
		"model":    model,
	})
}

func emitError(enc *json.Encoder, msg string) error {
	_ = enc.Encode(aiStreamEvent{Type: "error", Error: msg})
	_ = enc.Encode(aiStreamEvent{Type: "done"})
	return nil
}
