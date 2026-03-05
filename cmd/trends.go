package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/ui"

	"github.com/spf13/cobra"
)

var trendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Show weekly trending topics and team pulse",
	Long:  "Displays weekly trends analysis from AI-generated digests including hot topics, key decisions, and outstanding action items.",
	RunE:  runTrends,
}

func init() {
	rootCmd.AddCommand(trendsCmd)
}

func runTrends(cmd *cobra.Command, args []string) error {
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

	// Look for the latest weekly digest
	weekAgo := float64(time.Now().Add(-8 * 24 * time.Hour).Unix())
	digests, err := database.GetDigests(db.DigestFilter{
		Type:     "weekly",
		FromUnix: weekAgo,
		Limit:    1,
	})
	if err != nil {
		return fmt.Errorf("querying weekly digests: %w", err)
	}

	if len(digests) == 0 {
		// Fallback: show aggregated topics from channel digests
		return showTopicsSummary(out, database)
	}

	d := digests[0]
	periodFrom := time.Unix(int64(d.PeriodFrom), 0)
	periodTo := time.Unix(int64(d.PeriodTo), 0)

	var buf strings.Builder
	fmt.Fprintf(&buf, "## Weekly Trends — %s to %s\n\n",
		periodFrom.Format("2006-01-02"), periodTo.Format("2006-01-02"))

	fmt.Fprintf(&buf, "%s\n\n", d.Summary)

	var topics []string
	if err := json.Unmarshal([]byte(d.Topics), &topics); err == nil && len(topics) > 0 {
		fmt.Fprintln(&buf, "**Trending Topics:**")
		fmt.Fprintln(&buf)
		for _, t := range topics {
			fmt.Fprintf(&buf, "- %s\n", t)
		}
		fmt.Fprintln(&buf)
	}

	var decisions []struct {
		Text string `json:"text"`
		By   string `json:"by"`
	}
	if err := json.Unmarshal([]byte(d.Decisions), &decisions); err == nil && len(decisions) > 0 {
		fmt.Fprintln(&buf, "**Key Decisions:**")
		fmt.Fprintln(&buf)
		for _, dec := range decisions {
			if dec.By != "" {
				fmt.Fprintf(&buf, "- %s (by %s)\n", dec.Text, dec.By)
			} else {
				fmt.Fprintf(&buf, "- %s\n", dec.Text)
			}
		}
		fmt.Fprintln(&buf)
	}

	var actions []struct {
		Text     string `json:"text"`
		Assignee string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(d.ActionItems), &actions); err == nil && len(actions) > 0 {
		fmt.Fprintln(&buf, "**Outstanding Actions:**")
		fmt.Fprintln(&buf)
		for _, a := range actions {
			assignee := ""
			if a.Assignee != "" {
				assignee = " -> " + a.Assignee
			}
			fmt.Fprintf(&buf, "- %s%s\n", a.Text, assignee)
		}
		fmt.Fprintln(&buf)
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))

	return nil
}

func showTopicsSummary(out interface{ Write([]byte) (int, error) }, database *db.DB) error {
	weekAgo := float64(time.Now().Add(-7 * 24 * time.Hour).Unix())
	digests, err := database.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: weekAgo,
	})
	if err != nil {
		return fmt.Errorf("querying channel digests: %w", err)
	}

	if len(digests) == 0 {
		fmt.Fprintln(out, "No trends data available. Run 'watchtower sync --daemon' to generate digests.")
		return nil
	}

	// Aggregate topics from channel digests
	topicCounts := make(map[string]int)
	for _, d := range digests {
		var topics []string
		if err := json.Unmarshal([]byte(d.Topics), &topics); err == nil {
			for _, t := range topics {
				topicCounts[t]++
			}
		}
	}

	if len(topicCounts) == 0 {
		fmt.Fprintln(out, "No topics extracted yet.")
		return nil
	}

	// Sort topics by frequency (descending)
	type topicEntry struct {
		name  string
		count int
	}
	sorted := make([]topicEntry, 0, len(topicCounts))
	for t, c := range topicCounts {
		sorted = append(sorted, topicEntry{t, c})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	var buf strings.Builder
	fmt.Fprintf(&buf, "## Trending Topics (last 7 days, from %d channel digests)\n\n", len(digests))
	for _, e := range sorted {
		fmt.Fprintf(&buf, "- %s (%d channels)\n", e.name, e.count)
	}
	fmt.Fprintln(&buf)

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))

	return nil
}
