package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"watchtower/internal/ai"
	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/ui"

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

	// Determine the "since" time
	sinceTime, err := database.DetermineSinceTime(catchupFlagSince)
	if err != nil {
		return fmt.Errorf("determining catchup window: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Catching up since %s...\n\n", sinceTime.Format("2006-01-02 15:04 MST"))

	now := time.Now()
	fromUnix := float64(sinceTime.Unix()) + float64(sinceTime.Nanosecond())/1e9
	toUnix := float64(now.Unix()) + float64(now.Nanosecond())/1e9

	// Quick check: are there any messages in the catchup window?
	msgCount, err := database.CountMessagesByTimeRange(fromUnix, toUnix)
	if err != nil {
		return fmt.Errorf("counting messages: %w", err)
	}
	if msgCount == 0 {
		fmt.Fprintln(out, "No new activity found since your last catchup.")
		if err := database.UpdateCheckpoint(now); err != nil {
			return fmt.Errorf("updating checkpoint: %w", err)
		}
		return nil
	}

	// Fast path: if digests are available for this period, show them directly
	if shown := showDigestCatchup(out, database, fromUnix); shown {
		if err := database.UpdateCheckpoint(now); err != nil {
			return fmt.Errorf("updating checkpoint: %w", err)
		}
		fmt.Fprintln(out)
		return nil
	}

	// Slow path: no digests available, use AI query on raw messages
	// Build time range for hints
	pq := ai.ParsedQuery{
		TimeRange: &ai.TimeRange{
			From: sinceTime,
			To:   now,
		},
	}

	// Assemble prompt with DB access
	dbPath := cfg.DBPath()
	systemPrompt := ai.BuildSystemPrompt(ws.Name, ws.Domain, dbPath, db.Schema)
	timeHints := ai.FormatTimeHints(pq)

	question := "What happened since I was last here? Give me a structured catchup summary."
	if catchupFlagWatchedOnly {
		question += " Focus only on watched channels and users."
	}
	if catchupFlagChannel != "" {
		question += fmt.Sprintf(" Focus on #%s.", catchupFlagChannel)
	}

	userMessage := ai.AssembleUserMessage(question, timeHints)

	// Create AI client and query
	aiClient := ai.NewClient(cfg.AI.Model, dbPath, cfg.ClaudePath)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	renderer := ai.NewResponseRenderer(database, ws.Domain)

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

	// Update checkpoint to now
	if err := database.UpdateCheckpoint(now); err != nil {
		return fmt.Errorf("updating checkpoint: %w", err)
	}

	fmt.Fprintln(out)
	return nil
}

// showDigestCatchup displays pre-built digests for the catchup period.
// Returns true if digests were shown, false if none were available.
func showDigestCatchup(out interface{ Write([]byte) (int, error) }, database *db.DB, fromUnix float64) bool {
	// Check for daily digest first
	dailyDigests, err := database.GetDigests(db.DigestFilter{
		Type:     "daily",
		FromUnix: fromUnix,
		Limit:    1,
	})
	if err == nil && len(dailyDigests) > 0 {
		d := dailyDigests[0]
		var buf strings.Builder
		fmt.Fprintln(&buf, d.Summary)
		printDigestDetails(&buf, d)
		fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
		return true
	}

	// Fall back to channel digests
	channelDigests, err := database.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: fromUnix,
	})
	if err != nil || len(channelDigests) == 0 {
		return false
	}

	var buf strings.Builder
	for _, d := range channelDigests {
		name := d.ChannelID
		if ch, err := database.GetChannelByID(d.ChannelID); err == nil && ch != nil {
			name = "#" + ch.Name
		}
		fmt.Fprintf(&buf, "**%s** (%d messages)\n%s\n\n", name, d.MessageCount, d.Summary)
		printDigestDetails(&buf, d)
	}
	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	return true
}

func printDigestDetails(out interface{ Write([]byte) (int, error) }, d db.Digest) {
	var decisions []struct {
		Text string `json:"text"`
		By   string `json:"by"`
	}
	if err := json.Unmarshal([]byte(d.Decisions), &decisions); err == nil && len(decisions) > 0 {
		fmt.Fprintln(out, "\n**Decisions:**")
		fmt.Fprintln(out)
		for _, dec := range decisions {
			if dec.By != "" {
				fmt.Fprintf(out, "- %s (by %s)\n", dec.Text, dec.By)
			} else {
				fmt.Fprintf(out, "- %s\n", dec.Text)
			}
		}
	}

	var actions []struct {
		Text     string `json:"text"`
		Assignee string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(d.ActionItems), &actions); err == nil && len(actions) > 0 {
		fmt.Fprintln(out, "\n**Action Items:**")
		fmt.Fprintln(out)
		for _, a := range actions {
			assignee := ""
			if a.Assignee != "" {
				assignee = " -> " + a.Assignee
			}
			fmt.Fprintf(out, "- %s%s\n", a.Text, assignee)
		}
	}
}
