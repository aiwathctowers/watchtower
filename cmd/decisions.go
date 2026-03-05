package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/spf13/cobra"
)

var decisionsFlagDays int

var decisionsCmd = &cobra.Command{
	Use:   "decisions",
	Short: "Show decisions extracted from Slack conversations",
	Long:  "Lists decisions found in AI-generated digests. Decisions are extracted automatically during digest generation.",
	RunE:  runDecisions,
}

func init() {
	rootCmd.AddCommand(decisionsCmd)
	decisionsCmd.Flags().IntVar(&decisionsFlagDays, "days", 7, "number of days to look back")
}

func runDecisions(cmd *cobra.Command, args []string) error {
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

	days := decisionsFlagDays
	if days <= 0 {
		days = 7
	}
	sinceUnix := float64(time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix())

	digests, err := database.GetDigests(db.DigestFilter{FromUnix: sinceUnix})
	if err != nil {
		return fmt.Errorf("querying digests: %w", err)
	}

	type decision struct {
		Text      string `json:"text"`
		By        string `json:"by"`
		MessageTS string `json:"message_ts"`
	}

	var allDecisions []struct {
		decision
		channelName string
		digestType  string
	}

	for _, d := range digests {
		var decisions []decision
		if err := json.Unmarshal([]byte(d.Decisions), &decisions); err != nil || len(decisions) == 0 {
			continue
		}

		channelName := d.ChannelID
		if d.ChannelID != "" {
			if ch, err := database.GetChannelByID(d.ChannelID); err == nil && ch != nil {
				channelName = "#" + ch.Name
			}
		} else {
			channelName = "(cross-channel)"
		}

		for _, dec := range decisions {
			allDecisions = append(allDecisions, struct {
				decision
				channelName string
				digestType  string
			}{dec, channelName, d.Type})
		}
	}

	if len(allDecisions) == 0 {
		fmt.Fprintf(out, "No decisions found in the last %d days.\n", days)
		fmt.Fprintln(out, "Decisions are extracted during digest generation. Run 'watchtower sync --daemon' to start.")
		return nil
	}

	fmt.Fprintf(out, "Decisions from the last %d days:\n\n", days)
	for i, d := range allDecisions {
		by := ""
		if d.By != "" {
			by = fmt.Sprintf(" (by %s)", d.By)
		}
		fmt.Fprintf(out, "%d. %s%s\n   %s\n\n", i+1, d.Text, by, d.channelName)
	}

	return nil
}
