package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/spf13/cobra"
)

var (
	askFlagModel    string
	askFlagNoStream bool
	askFlagChannel  string
	askFlagSince    time.Duration
)

var askCmd = &cobra.Command{
	Use:   `ask "<question>"`,
	Short: "Ask a question about your Slack workspace",
	Long:  "Uses AI to analyze synced Slack data and answer your question with context from messages, channels, and users.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runAsk,
}

func init() {
	rootCmd.AddCommand(askCmd)
	askCmd.Flags().StringVar(&askFlagModel, "model", "", "override AI model (e.g., claude-sonnet-4-20250514)")
	askCmd.Flags().BoolVar(&askFlagNoStream, "no-stream", false, "wait for full response instead of streaming")
	askCmd.Flags().StringVar(&askFlagChannel, "channel", "", "limit context to a specific channel")
	askCmd.Flags().DurationVar(&askFlagSince, "since", 0, "limit context to messages since this duration ago (e.g., 2h, 24h)")
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if cfg.AI.ApiKey == "" {
		return fmt.Errorf("Anthropic API key not configured — set ANTHROPIC_API_KEY or config ai.api_key")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	ws, err := database.GetWorkspace()
	if err != nil {
		return fmt.Errorf("getting workspace: %w", err)
	}
	if ws == nil {
		return fmt.Errorf("no workspace data found — run 'watchtower sync' first")
	}

	// Parse the query
	pq := ai.Parse(question)

	// Apply CLI flag overrides
	if askFlagChannel != "" {
		pq.Channels = append(pq.Channels, askFlagChannel)
	}
	if askFlagSince > 0 {
		now := time.Now()
		pq.TimeRange = &ai.TimeRange{
			From: now.Add(-askFlagSince),
			To:   now,
		}
	}

	// Build context from DB
	ctxBuilder := ai.NewContextBuilder(database, cfg.AI.ContextBudget, ws.Domain)
	msgContext, err := ctxBuilder.Build(pq)
	if err != nil {
		return fmt.Errorf("building context: %w", err)
	}

	// Assemble prompt
	systemPrompt := ai.BuildSystemPrompt(ws.Name, ws.Domain)
	userMessage := ai.AssembleUserMessage("", msgContext, question)

	// Determine model
	model := cfg.AI.Model
	if askFlagModel != "" {
		model = askFlagModel
	}

	// Create AI client
	aiClient := ai.NewClient(cfg.AI.ApiKey, model, cfg.AI.MaxTokens)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()
	renderer := ai.NewResponseRenderer(database, ws.Domain)

	if askFlagNoStream {
		resp, err := aiClient.QuerySync(ctx, systemPrompt, userMessage)
		if err != nil {
			return fmt.Errorf("ai query failed: %w", err)
		}
		rendered, err := renderer.Render(resp)
		if err != nil {
			return fmt.Errorf("rendering response: %w", err)
		}
		fmt.Fprint(out, rendered)
		return nil
	}

	// Streaming mode
	textCh, errCh := aiClient.Query(ctx, systemPrompt, userMessage)

	var fullResponse strings.Builder
	for chunk := range textCh {
		fullResponse.WriteString(chunk)
		fmt.Fprint(out, chunk)
	}

	if err := <-errCh; err != nil {
		return fmt.Errorf("ai query failed: %w", err)
	}

	// Render final response for sources section
	rendered, err := renderer.Render(fullResponse.String())
	if err == nil {
		// Extract just the sources section (after the main content)
		sources := extractSourcesSection(rendered)
		if sources != "" {
			fmt.Fprintf(out, "\n\n%s", sources)
		}
	}

	return nil
}

// extractSourcesSection returns the "Sources:" section from rendered output, if present.
func extractSourcesSection(rendered string) string {
	idx := strings.LastIndex(rendered, "Sources:\n")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(rendered[idx:])
}
