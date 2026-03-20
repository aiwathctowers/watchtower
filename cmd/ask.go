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
	askFlagModel   string
	askFlagChannel string
	askFlagSince   time.Duration
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
	askCmd.Flags().StringVar(&askFlagModel, "model", "", "override AI model (e.g., claude-sonnet-4-6)")
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
	if err := cfg.ValidateWorkspace(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
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

	// Parse the query for time hints
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

	// Assemble prompt with DB access
	dbPath := cfg.DBPath()
	systemPrompt := ai.BuildSystemPrompt(ws.Name, ws.Domain, ws.ID, dbPath, db.Schema)

	// Inject digest context if available
	if digestCtx := buildDigestContext(database); digestCtx != "" {
		systemPrompt += "\n\n=== RECENT DIGEST SUMMARIES ===\n" +
			"Below are pre-analyzed summaries of recent activity. Use these as background knowledge. " +
			"For detailed questions, query the database for raw messages.\n\n" + digestCtx
	}

	timeHints := ai.FormatTimeHints(pq)
	userMessage := ai.AssembleUserMessage(question, timeHints)

	// Determine model
	model := cfg.AI.Model
	if askFlagModel != "" {
		model = askFlagModel
	}

	// Create AI client
	aiClient := ai.NewClient(model, dbPath, cfg.ClaudePath)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()
	renderer := ai.NewResponseRenderer(database, ws.Domain, ws.ID)

	resp, err := aiClient.QuerySync(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return fmt.Errorf("ai query failed: %w", err)
	}
	rendered, err := renderer.Render(resp)
	if err != nil {
		fmt.Fprint(out, resp)
	} else {
		fmt.Fprint(out, rendered)
	}

	return nil
}
