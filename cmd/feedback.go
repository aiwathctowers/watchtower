package cmd

import (
	"fmt"
	"strconv"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/spf13/cobra"
)

var feedbackFlagComment string

var feedbackCmd = &cobra.Command{
	Use:   "feedback <good|bad> <type> <id>",
	Short: "Rate AI-generated content (digests, tracks, decisions)",
	Long: `Provide feedback on AI-generated content to improve prompt quality.

Types: digest, track, decision
Rating: good (+1) or bad (-1)

Examples:
  watchtower feedback good digest 42
  watchtower feedback bad track 7
  watchtower feedback bad decision 42:0 -m "not a real decision"`,
	Args: cobra.ExactArgs(3),
	RunE: runFeedback,
}

var feedbackStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show feedback statistics",
	RunE:  runFeedbackStats,
}

func init() {
	rootCmd.AddCommand(feedbackCmd)
	feedbackCmd.AddCommand(feedbackStatsCmd)
	feedbackCmd.Flags().StringVarP(&feedbackFlagComment, "message", "m", "", "optional comment explaining the rating")
}

func runFeedback(cmd *cobra.Command, args []string) error {
	ratingStr := args[0]
	entityType := args[1]
	entityID := args[2]

	var rating int
	switch ratingStr {
	case "good", "+1", "1":
		rating = 1
	case "bad", "-1":
		rating = -1
	default:
		return fmt.Errorf("invalid rating %q — use 'good' or 'bad'", ratingStr)
	}

	validTypes := map[string]bool{"digest": true, "track": true, "decision": true, "user_analysis": true}
	if !validTypes[entityType] {
		return fmt.Errorf("invalid type %q — use digest, track, decision, or user_analysis", entityType)
	}

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

	id, err := database.AddFeedback(db.Feedback{
		EntityType: entityType,
		EntityID:   entityID,
		Rating:     rating,
		Comment:    feedbackFlagComment,
	})
	if err != nil {
		return fmt.Errorf("saving feedback: %w", err)
	}

	icon := "👍"
	if rating < 0 {
		icon = "👎"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Feedback #%d recorded for %s %s\n", icon, id, entityType, entityID)
	return nil
}

func runFeedbackStats(cmd *cobra.Command, args []string) error {
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

	out := cmd.OutOrStdout()

	stats, err := database.GetFeedbackStats()
	if err != nil {
		return fmt.Errorf("querying stats: %w", err)
	}

	if len(stats) == 0 {
		fmt.Fprintln(out, "No feedback recorded yet.")
		fmt.Fprintln(out, "Use 'watchtower feedback good|bad <type> <id>' to rate AI content.")
		return nil
	}

	fmt.Fprintln(out, "Feedback Statistics:")
	fmt.Fprintln(out, "")
	for _, s := range stats {
		pct := 0
		if s.Total > 0 {
			pct = s.Positive * 100 / s.Total
		}
		fmt.Fprintf(out, "  %-15s  👍 %-4s  👎 %-4s  (%d%% positive)\n",
			s.EntityType,
			strconv.Itoa(s.Positive),
			strconv.Itoa(s.Negative),
			pct,
		)
	}
	fmt.Fprintln(out, "")
	return nil
}
