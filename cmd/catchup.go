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
	catchupFlagSince       time.Duration
	catchupFlagWatchedOnly bool
	catchupFlagChannel     string
)

var catchupCmd = &cobra.Command{
	Use:   "catchup",
	Short: "Get a summary of what happened since you last checked",
	Long:  "Queries messages since your last catchup (or --since duration) and uses AI to provide a structured summary of activity.",
	RunE:  runCatchup,
}

func init() {
	rootCmd.AddCommand(catchupCmd)
	catchupCmd.Flags().DurationVar(&catchupFlagSince, "since", 0, "override checkpoint with explicit duration (e.g., 2h, 24h)")
	catchupCmd.Flags().BoolVar(&catchupFlagWatchedOnly, "watched-only", false, "only include watched channels and users")
	catchupCmd.Flags().StringVar(&catchupFlagChannel, "channel", "", "limit catchup to a specific channel")
}

func runCatchup(cmd *cobra.Command, args []string) error {
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

	// Determine the "since" time
	sinceTime, err := determineSinceTime(database, catchupFlagSince)
	if err != nil {
		return fmt.Errorf("determining catchup window: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Catching up since %s...\n\n", sinceTime.Format("2006-01-02 15:04 MST"))

	// Build a catchup query
	now := time.Now()
	pq := ai.ParsedQuery{
		RawText: "What happened since I was last here? Summarize activity by channel, highlight decisions, action items, and anything unusual.",
		TimeRange: &ai.TimeRange{
			From: sinceTime,
			To:   now,
		},
		Intent: ai.IntentCatchup,
	}

	if catchupFlagChannel != "" {
		pq.Channels = append(pq.Channels, catchupFlagChannel)
	}

	// Build context from DB
	ctxBuilder := ai.NewContextBuilder(database, cfg.AI.ContextBudget, ws.Domain)
	msgContext, err := ctxBuilder.Build(pq)
	if err != nil {
		return fmt.Errorf("building context: %w", err)
	}

	if strings.TrimSpace(msgContext) == "" {
		fmt.Fprintln(out, "No new activity found since your last catchup.")
		// Still update checkpoint
		if err := database.UpdateCheckpoint(now); err != nil {
			return fmt.Errorf("updating checkpoint: %w", err)
		}
		return nil
	}

	// Assemble prompt
	systemPrompt := ai.BuildSystemPrompt(ws.Name, ws.Domain)

	question := "What happened since I was last here? Give me a structured catchup summary."
	if catchupFlagWatchedOnly {
		question += " Focus only on watched channels and users."
	}
	if catchupFlagChannel != "" {
		question += fmt.Sprintf(" Focus on #%s.", catchupFlagChannel)
	}

	userMessage := ai.AssembleUserMessage("", msgContext, question)

	// Create AI client and query
	aiClient := ai.NewClient(cfg.AI.ApiKey, cfg.AI.Model, cfg.AI.MaxTokens)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	renderer := ai.NewResponseRenderer(database, ws.Domain)

	textCh, errCh := aiClient.Query(ctx, systemPrompt, userMessage)

	var fullResponse strings.Builder
	for chunk := range textCh {
		fullResponse.WriteString(chunk)
		fmt.Fprint(out, chunk)
	}

	if err := <-errCh; err != nil {
		return fmt.Errorf("ai query failed: %w", err)
	}

	// Render for sources
	rendered, err := renderer.Render(fullResponse.String())
	if err == nil {
		sources := ai.ExtractSourcesSection(rendered)
		if sources != "" {
			fmt.Fprintf(out, "\n\n%s", sources)
		}
	}

	// Update checkpoint to now
	if err := database.UpdateCheckpoint(now); err != nil {
		return fmt.Errorf("updating checkpoint: %w", err)
	}

	fmt.Fprintln(out)
	return nil
}

// determineSinceTime resolves the catchup start time from:
// 1. --since flag (explicit duration)
// 2. user_checkpoints.last_checked_at (previous catchup)
// 3. default: last 24 hours
func determineSinceTime(database *db.DB, sinceDuration time.Duration) (time.Time, error) {
	now := time.Now()

	// Explicit --since flag takes priority
	if sinceDuration > 0 {
		return now.Add(-sinceDuration), nil
	}

	// Check for saved checkpoint
	cp, err := database.GetCheckpoint()
	if err != nil {
		return time.Time{}, err
	}
	if cp != nil {
		t, err := time.Parse("2006-01-02T15:04:05Z", cp.LastCheckedAt)
		if err == nil {
			return t, nil
		}
	}

	// Default: last 24 hours
	return now.Add(-24 * time.Hour), nil
}
