package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/ui"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	digestFlagChannel        string
	digestFlagDays           int
	digestGenFlagSince       int
	digestGenFlagProgressJSON bool
)

var digestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Show AI-generated digests of channel activity",
	Long:  "Displays pre-generated summaries of Slack activity. Digests are created automatically after each sync when the daemon is running.",
	RunE:  runDigest,
}

var digestGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate digests from existing synced data",
	Long:  "Runs the digest pipeline on already-synced messages without requiring a new sync. Useful for generating digests from an existing database.",
	RunE:  runDigestGenerate,
}

var digestStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show digest generation statistics and token usage",
	RunE:  runDigestStats,
}

var digestSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Generate an AI summary across digests for a time period",
	Long:  "Aggregates existing digests within a date range and produces a single comprehensive summary via AI.",
	RunE:  runDigestSummary,
}

var (
	digestStatsFlagDays    int
	digestSummaryFlagFrom  string
	digestSummaryFlagTo    string
	digestSummaryFlagDays  int
	digestSummaryFlagHours int
)

func init() {
	rootCmd.AddCommand(digestCmd)
	digestCmd.AddCommand(digestGenerateCmd)
	digestCmd.AddCommand(digestStatsCmd)
	digestCmd.AddCommand(digestSummaryCmd)
	digestCmd.Flags().StringVar(&digestFlagChannel, "channel", "", "show digest for a specific channel")
	digestCmd.Flags().IntVar(&digestFlagDays, "days", 1, "number of days to show")
	digestGenerateCmd.Flags().IntVar(&digestGenFlagSince, "since", 1, "generate digests for the last N days")
	digestGenerateCmd.Flags().BoolVar(&digestGenFlagProgressJSON, "progress-json", false, "output progress as JSON lines")
	digestStatsCmd.Flags().IntVar(&digestStatsFlagDays, "days", 7, "number of days to look back")
	digestSummaryCmd.Flags().StringVar(&digestSummaryFlagFrom, "from", "", "start date (YYYY-MM-DD)")
	digestSummaryCmd.Flags().StringVar(&digestSummaryFlagTo, "to", "", "end date (YYYY-MM-DD), default: today")
	digestSummaryCmd.Flags().IntVar(&digestSummaryFlagDays, "days", 0, "summarize last N days")
	digestSummaryCmd.Flags().IntVar(&digestSummaryFlagHours, "hours", 0, "summarize last N hours")
}

func runDigest(cmd *cobra.Command, args []string) error {
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

	days := digestFlagDays
	if days <= 0 {
		days = 1
	}
	if days > 3650 {
		days = 3650 // clamp to prevent time.Duration overflow
	}
	sinceUnix := float64(time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix())

	filter := db.DigestFilter{
		FromUnix: sinceUnix,
	}

	if digestFlagChannel != "" {
		ch, err := database.GetChannelByName(digestFlagChannel)
		if err != nil {
			return fmt.Errorf("looking up channel: %w", err)
		}
		if ch == nil {
			return fmt.Errorf("channel #%s not found", digestFlagChannel)
		}
		filter.ChannelID = ch.ID
		filter.Type = "channel"
	}

	digests, err := database.GetDigests(filter)
	if err != nil {
		return fmt.Errorf("querying digests: %w", err)
	}

	if len(digests) == 0 {
		fmt.Fprintln(out, "No digests available. Run 'watchtower sync --daemon' to generate digests automatically.")
		return nil
	}

	var buf strings.Builder
	for _, d := range digests {
		printDigest(&buf, d, database)
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))

	return nil
}

func printDigest(w io.Writer, d db.Digest, database *db.DB) {
	periodFrom := time.Unix(int64(d.PeriodFrom), 0)
	periodTo := time.Unix(int64(d.PeriodTo), 0)

	// Header
	switch d.Type {
	case "channel":
		name := d.ChannelID
		if ch, err := database.GetChannelByID(d.ChannelID); err == nil && ch != nil {
			name = "#" + ch.Name
		}
		fmt.Fprintf(w, "## %s — Channel Digest\n", name)
	case "daily":
		fmt.Fprintf(w, "## Daily Digest — %s\n", periodFrom.Format("2006-01-02"))
	case "weekly":
		fmt.Fprintf(w, "## Weekly Trends — %s to %s\n",
			periodFrom.Format("2006-01-02"), periodTo.Format("2006-01-02"))
	}

	fmt.Fprintf(w, "%s to %s | %s messages | model: %s\n\n",
		periodFrom.Format("15:04"), periodTo.Format("15:04"),
		humanize.Comma(int64(d.MessageCount)), d.Model)

	// Summary
	fmt.Fprintf(w, "%s\n\n", d.Summary)

	// Topics
	var topics []string
	if err := json.Unmarshal([]byte(d.Topics), &topics); err == nil && len(topics) > 0 {
		fmt.Fprintf(w, "**Topics:** %s\n\n", joinTopics(topics))
	}

	// Decisions
	var decisions []struct {
		Text string `json:"text"`
		By   string `json:"by"`
	}
	if err := json.Unmarshal([]byte(d.Decisions), &decisions); err == nil && len(decisions) > 0 {
		fmt.Fprintln(w, "**Decisions:**")
		fmt.Fprintln(w)
		for _, dec := range decisions {
			if dec.By != "" {
				fmt.Fprintf(w, "- %s (by %s)\n", dec.Text, dec.By)
			} else {
				fmt.Fprintf(w, "- %s\n", dec.Text)
			}
		}
		fmt.Fprintln(w)
	}

	// Action items
	var actions []struct {
		Text     string `json:"text"`
		Assignee string `json:"assignee"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal([]byte(d.ActionItems), &actions); err == nil && len(actions) > 0 {
		fmt.Fprintln(w, "**Action Items:**")
		fmt.Fprintln(w)
		for _, a := range actions {
			assignee := ""
			if a.Assignee != "" {
				assignee = " -> " + a.Assignee
			}
			fmt.Fprintf(w, "- [%s] %s%s\n", a.Status, a.Text, assignee)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "---")
}

func runDigestGenerate(cmd *cobra.Command, args []string) error {
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

	// Force digest enabled for this run regardless of config.
	cfg.Digest.Enabled = true
	if cfg.Digest.Model == "" {
		cfg.Digest.Model = config.DefaultDigestModel
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	days := digestGenFlagSince
	if days <= 0 {
		days = 1
	}
	sinceUnix := float64(time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix())

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	gen := digest.NewClaudeGenerator(cfg.Digest.Model)
	pipe := digest.New(database, cfg, gen, logger)
	pipe.SinceOverride = sinceUnix

	if digestGenFlagProgressJSON {
		type pj struct {
			Pipeline     string  `json:"pipeline"`
			Done         int     `json:"done"`
			Total        int     `json:"total"`
			Status       string  `json:"status,omitempty"`
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			CostUSD      float64 `json:"cost_usd"`
			Error        string  `json:"error,omitempty"`
			Finished     bool    `json:"finished"`
			ItemsFound   int     `json:"items_found"`
		}
		emit := func(p pj) { data, _ := json.Marshal(p); fmt.Fprintln(out, string(data)) }

		pipe.OnProgress = func(done, total int, status string) {
			emit(pj{Pipeline: "digest", Done: done, Total: total, Status: status})
		}
		n, usage, err := pipe.Run(cmd.Context())
		final := pj{Pipeline: "digest", Finished: true, ItemsFound: n}
		if usage != nil {
			final.InputTokens = usage.InputTokens
			final.OutputTokens = usage.OutputTokens
			final.CostUSD = usage.CostUSD
		}
		if err != nil {
			final.Error = err.Error()
		}
		emit(final)
		return nil
	}

	spinner := ui.NewSpinner(out, fmt.Sprintf("Generating digests for the last %d day(s) using %s...", days, cfg.Digest.Model))

	n, usage, err := pipe.Run(cmd.Context())
	if err != nil {
		spinner.Stop("failed")
		return fmt.Errorf("digest pipeline: %w", err)
	}

	if n == 0 {
		spinner.Stop("No channels with enough messages to generate digests")
	} else {
		msg := fmt.Sprintf("Generated %d channel digest(s).", n)
		if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
			msg = fmt.Sprintf("Generated %d channel digest(s). Tokens: %s in + %s out | $%.4f",
				n,
				humanize.Comma(int64(usage.InputTokens)),
				humanize.Comma(int64(usage.OutputTokens)),
				usage.CostUSD)
		}
		spinner.Stop(msg)
		fmt.Fprintln(out, "Run 'watchtower digest' to view them.")
	}

	return nil
}

func runDigestStats(cmd *cobra.Command, args []string) error {
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

	days := digestStatsFlagDays
	if days <= 0 {
		days = 7
	}
	sinceUnix := float64(time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix())

	// Per-type stats
	for _, dtype := range []string{"channel", "daily", "weekly"} {
		stats, err := database.GetDigestStats(db.DigestFilter{Type: dtype, FromUnix: sinceUnix})
		if err != nil {
			return fmt.Errorf("querying %s stats: %w", dtype, err)
		}
		if stats.TotalDigests == 0 {
			continue
		}
		fmt.Fprintf(out, "%-8s  %d digests | %s messages | %s in + %s out tokens | $%.4f\n",
			dtype,
			stats.TotalDigests,
			humanize.Comma(int64(stats.TotalMessages)),
			humanize.Comma(int64(stats.InputTokens)),
			humanize.Comma(int64(stats.OutputTokens)),
			stats.CostUSD,
		)
	}

	// Totals
	total, err := database.GetDigestStats(db.DigestFilter{FromUnix: sinceUnix})
	if err != nil {
		return fmt.Errorf("querying total stats: %w", err)
	}

	if total.TotalDigests == 0 {
		fmt.Fprintf(out, "No digests generated in the last %d days.\n", days)
		return nil
	}

	fmt.Fprintf(out, "\nTotal (%d days): %d digests | %s messages | %s in + %s out tokens | $%.4f\n",
		days,
		total.TotalDigests,
		humanize.Comma(int64(total.TotalMessages)),
		humanize.Comma(int64(total.InputTokens)),
		humanize.Comma(int64(total.OutputTokens)),
		total.CostUSD,
	)

	// All-time stats
	allTime, err := database.GetDigestStats(db.DigestFilter{})
	if err != nil {
		return fmt.Errorf("querying all-time stats: %w", err)
	}
	if allTime.CostUSD > 0 {
		fmt.Fprintf(out, "All time: %d digests | $%.4f\n", allTime.TotalDigests, allTime.CostUSD)
	}

	return nil
}

func runDigestSummary(cmd *cobra.Command, args []string) error {
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

	if cfg.Digest.Model == "" {
		cfg.Digest.Model = config.DefaultDigestModel
	}

	// Parse time range
	var from, to time.Time
	now := time.Now().UTC()

	if digestSummaryFlagHours > 0 {
		from = now.Add(-time.Duration(digestSummaryFlagHours) * time.Hour)
		to = now
	} else if digestSummaryFlagDays > 0 {
		from = now.AddDate(0, 0, -digestSummaryFlagDays)
		to = now
	} else if digestSummaryFlagFrom != "" {
		from, err = time.Parse("2006-01-02", digestSummaryFlagFrom)
		if err != nil {
			return fmt.Errorf("invalid --from date: %w", err)
		}
		if digestSummaryFlagTo != "" {
			to, err = time.Parse("2006-01-02", digestSummaryFlagTo)
			if err != nil {
				return fmt.Errorf("invalid --to date: %w", err)
			}
			// Include the full end day (exclusive upper bound via next midnight)
			to = to.AddDate(0, 0, 1).Add(-time.Millisecond)
		} else {
			to = now
		}
	} else {
		return fmt.Errorf("specify --hours, --days, or --from (and optionally --to)")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Generating summary for %s to %s using %s...\n\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"), cfg.Digest.Model)

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	gen := digest.NewClaudeGenerator(cfg.Digest.Model)
	pipe := digest.New(database, cfg, gen, logger)

	result, usage, err := pipe.RunPeriodSummary(cmd.Context(), from, to)
	if err != nil {
		return err
	}

	// Print summary
	var buf strings.Builder
	fmt.Fprintf(&buf, "## Summary: %s to %s\n\n", from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Fprintf(&buf, "%s\n\n", result.Summary)

	if len(result.Topics) > 0 {
		fmt.Fprintf(&buf, "**Topics:** %s\n\n", joinTopics(result.Topics))
	}

	if len(result.Decisions) > 0 {
		fmt.Fprintln(&buf, "**Decisions:**")
		fmt.Fprintln(&buf)
		for _, dec := range result.Decisions {
			if dec.By != "" {
				fmt.Fprintf(&buf, "- %s (by %s)\n", dec.Text, dec.By)
			} else {
				fmt.Fprintf(&buf, "- %s\n", dec.Text)
			}
		}
		fmt.Fprintln(&buf)
	}

	if len(result.ActionItems) > 0 {
		fmt.Fprintln(&buf, "**Action Items:**")
		fmt.Fprintln(&buf)
		for _, a := range result.ActionItems {
			assignee := ""
			if a.Assignee != "" {
				assignee = " -> " + a.Assignee
			}
			fmt.Fprintf(&buf, "- [%s] %s%s\n", a.Status, a.Text, assignee)
		}
		fmt.Fprintln(&buf)
	}

	if usage != nil {
		fmt.Fprintf(&buf, "---\nTokens: %s in + %s out | $%.4f\n",
			humanize.Comma(int64(usage.InputTokens)),
			humanize.Comma(int64(usage.OutputTokens)),
			usage.CostUSD)
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))

	return nil
}

func joinTopics(topics []string) string {
	return strings.Join(topics, ", ")
}
