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
	"watchtower/internal/tracks"
	"watchtower/internal/ui"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	digestFlagChannel         string
	digestFlagDays            int
	digestGenFlagSince        int
	digestGenFlagProgressJSON bool
	digestGenFlagChannelsOnly bool
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

var digestResetContextCmd = &cobra.Command{
	Use:   "reset-context [channel]",
	Short: "Reset running context (channel memory) for digests",
	Long:  "Clears the running summary used for incremental context in digest generation. Without arguments, resets all channels. With a channel name, resets only that channel.",
	RunE:  runDigestResetContext,
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
	digestCmd.AddCommand(digestResetContextCmd)
	digestCmd.Flags().StringVar(&digestFlagChannel, "channel", "", "show digest for a specific channel")
	digestCmd.Flags().IntVar(&digestFlagDays, "days", 1, "number of days to show")
	digestGenerateCmd.Flags().IntVar(&digestGenFlagSince, "since", 1, "generate digests for the last N days")
	digestGenerateCmd.Flags().BoolVar(&digestGenFlagProgressJSON, "progress-json", false, "output progress as JSON lines")
	digestGenerateCmd.Flags().BoolVar(&digestGenFlagChannelsOnly, "channels-only", false, "generate channel digests only (no tracks/rollups)")
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

	// Try topic-structured data first
	topics, _ := database.GetDigestTopics(d.ID)
	if len(topics) > 0 {
		printDigestTopics(w, topics)
	} else {
		// Fallback to old flat fields for legacy digests
		printDigestLegacy(w, d)
	}

	fmt.Fprintln(w, "---")
}

// printDigestTopics renders structured topics with nested decisions and action items.
func printDigestTopics(w io.Writer, topics []db.DigestTopic) {
	for _, t := range topics {
		fmt.Fprintf(w, "### %s\n\n", t.Title)
		if t.Summary != "" {
			fmt.Fprintf(w, "%s\n\n", t.Summary)
		}

		var decisions []struct {
			Text string `json:"text"`
			By   string `json:"by"`
		}
		if err := json.Unmarshal([]byte(t.Decisions), &decisions); err == nil && len(decisions) > 0 {
			for _, dec := range decisions {
				if dec.By != "" {
					fmt.Fprintf(w, "- **Decision:** %s (by %s)\n", dec.Text, dec.By)
				} else {
					fmt.Fprintf(w, "- **Decision:** %s\n", dec.Text)
				}
			}
		}

		var actions []struct {
			Text     string `json:"text"`
			Assignee string `json:"assignee"`
			Status   string `json:"status"`
		}
		if err := json.Unmarshal([]byte(t.ActionItems), &actions); err == nil && len(actions) > 0 {
			for _, a := range actions {
				assignee := ""
				if a.Assignee != "" {
					assignee = " -> " + a.Assignee
				}
				fmt.Fprintf(w, "- [%s] %s%s\n", a.Status, a.Text, assignee)
			}
		}
		fmt.Fprintln(w)
	}
}

// printDigestLegacy renders old-style flat digest fields (topics as string array, flat decisions/actions).
func printDigestLegacy(w io.Writer, d db.Digest) {
	var topicNames []string
	if err := json.Unmarshal([]byte(d.Topics), &topicNames); err == nil && len(topicNames) > 0 {
		fmt.Fprintf(w, "**Topics:** %s\n\n", joinTopics(topicNames))
	}

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
}

func runDigestGenerate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	applyProviderOverride(cfg)
	if err := cfg.ValidateWorkspace(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Force digest enabled for this run regardless of config.
	cfg.Digest.Enabled = true
	if err := validateModel(cfg); err != nil {
		return err
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

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	gen, savePool := cliPooledGenerator(cfg, logger)
	defer savePool()
	pipe := digest.New(database, cfg, gen, logger)
	if !digestGenFlagChannelsOnly {
		pipe.TrackLinker = tracks.New(database, cfg, gen, logger)
	}

	// Only set SinceOverride when --since was explicitly passed.
	// Without it, Run() uses isFirstRun() → runInitialDayByDay() for full history.
	if cmd.Flags().Changed("since") {
		sinceUnix := float64(time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix())
		pipe.SinceOverride = sinceUnix
	}

	if digestGenFlagProgressJSON {
		type pj struct {
			Pipeline         string  `json:"pipeline"`
			Done             int     `json:"done"`
			Total            int     `json:"total"`
			Status           string  `json:"status,omitempty"`
			InputTokens      int     `json:"input_tokens"`
			OutputTokens     int     `json:"output_tokens"`
			Error            string  `json:"error,omitempty"`
			Finished         bool    `json:"finished"`
			ItemsFound       int     `json:"items_found"`
			MessageCount     int     `json:"message_count,omitempty"`
			PeriodFrom       string  `json:"period_from,omitempty"`
			PeriodTo         string  `json:"period_to,omitempty"`
			StepDurationSec  float64 `json:"step_duration_seconds,omitempty"`
			StepInputTokens  int     `json:"step_input_tokens,omitempty"`
			StepOutputTokens int     `json:"step_output_tokens,omitempty"`
			TotalAPITokens   int     `json:"total_api_tokens,omitempty"`
		}
		emit := func(p pj) { data, _ := json.Marshal(p); fmt.Fprintln(out, string(data)) }

		runID, _ := database.CreatePipelineRun("digests", "cli", "auto")

		pipe.OnProgress = func(done, total int, status string) {
			inTok, outTok, _, totalAPI := pipe.AccumulatedUsage()
			p := pj{Pipeline: "digest", Done: done, Total: total, Status: status, InputTokens: inTok, OutputTokens: outTok, TotalAPITokens: totalAPI}
			if pipe.LastStepMessageCount > 0 {
				p.MessageCount = pipe.LastStepMessageCount
				p.PeriodFrom = pipe.LastStepPeriodFrom.Format(time.RFC3339)
				p.PeriodTo = pipe.LastStepPeriodTo.Format(time.RFC3339)
			}
			if pipe.LastStepDurationSeconds > 0 {
				p.StepDurationSec = pipe.LastStepDurationSeconds
			}
			p.StepInputTokens = pipe.LastStepInputTokens
			p.StepOutputTokens = pipe.LastStepOutputTokens
			emit(p)

			// Log step to DB
			if runID > 0 && p.StepDurationSec > 0 {
				var pFrom, pTo *float64
				if pipe.LastStepMessageCount > 0 {
					f := float64(pipe.LastStepPeriodFrom.Unix())
					t := float64(pipe.LastStepPeriodTo.Unix())
					pFrom, pTo = &f, &t
				}
				_ = database.InsertPipelineStep(db.PipelineStep{
					RunID: runID, Step: done, Total: total, Status: status,
					InputTokens: p.StepInputTokens, OutputTokens: p.StepOutputTokens,
					CostUSD: 0, TotalAPITokens: totalAPI,
					MessageCount: pipe.LastStepMessageCount,
					PeriodFrom:   pFrom, PeriodTo: pTo,
					DurationSeconds: p.StepDurationSec,
				})
			}
		}
		var n int
		var usage *digest.Usage
		var err error
		if digestGenFlagChannelsOnly {
			n, usage, err = pipe.RunChannelDigestsOnly(cmd.Context())
		} else {
			n, usage, err = pipe.Run(cmd.Context())
		}

		// Auto-mark digests as read based on Slack read cursors
		// (important for onboarding where digest generate runs standalone).
		if markDigests, markTracks, markErr := database.AutoMarkReadFromSlack(); markErr != nil {
			logger.Printf("warning: auto-mark read failed: %v", markErr)
		} else if markDigests > 0 || markTracks > 0 {
			logger.Printf("auto-marked %d digests, %d tracks as read (based on Slack read state)", markDigests, markTracks)
		}

		inTok, outTok, _, totalAPI := pipe.AccumulatedUsage()
		final := pj{Pipeline: "digest", Finished: true, ItemsFound: n, InputTokens: inTok, OutputTokens: outTok, TotalAPITokens: totalAPI}
		if pipe.LastStepMessageCount > 0 {
			final.MessageCount = pipe.LastStepMessageCount
			final.PeriodFrom = pipe.LastStepPeriodFrom.Format(time.RFC3339)
			final.PeriodTo = pipe.LastStepPeriodTo.Format(time.RFC3339)
		}
		if err != nil {
			final.Error = err.Error()
		}
		emit(final)

		// Complete run in DB
		if runID > 0 {
			var errMsg string
			if err != nil {
				errMsg = err.Error()
			}
			inTok, outTok, cost, totalAPI := 0, 0, 0.0, 0
			if usage != nil {
				inTok, outTok, cost, totalAPI = usage.InputTokens, usage.OutputTokens, usage.CostUSD, usage.TotalAPITokens
			}
			_ = database.CompletePipelineRun(runID, n, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
		}
		return nil
	}

	spinner := ui.NewSpinner(out, fmt.Sprintf("Generating digests for the last %d day(s)...", days))

	runID, _ := database.CreatePipelineRun("digests", "cli", "auto")

	var n int
	var usage *digest.Usage
	if digestGenFlagChannelsOnly {
		n, usage, err = pipe.RunChannelDigestsOnly(cmd.Context())
	} else {
		n, usage, err = pipe.Run(cmd.Context())
	}
	if err != nil {
		spinner.Stop("failed")
		if runID > 0 {
			_ = database.CompletePipelineRun(runID, 0, 0, 0, 0, 0, nil, nil, err.Error())
		}
		return fmt.Errorf("digest pipeline: %w", err)
	}

	// Auto-mark digests as read based on Slack read cursors.
	if markDigests, _, markErr := database.AutoMarkReadFromSlack(); markErr != nil {
		logger.Printf("warning: auto-mark read failed: %v", markErr)
	} else if markDigests > 0 {
		logger.Printf("auto-marked %d digests as read (based on Slack read state)", markDigests)
	}

	// Complete run in DB
	if runID > 0 {
		inTok, outTok, cost, totalAPI := 0, 0, 0.0, 0
		if usage != nil {
			inTok, outTok, cost, totalAPI = usage.InputTokens, usage.OutputTokens, usage.CostUSD, usage.TotalAPITokens
		}
		_ = database.CompletePipelineRun(runID, n, inTok, outTok, cost, totalAPI, nil, nil, "")
	}

	if n == 0 {
		spinner.Stop("No channels with enough messages to generate digests")
	} else {
		msg := fmt.Sprintf("Generated %d channel digest(s).", n)
		if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
			msg = fmt.Sprintf("Generated %d channel digest(s). Tokens: %s in + %s out",
				n,
				humanize.Comma(int64(usage.InputTokens)),
				humanize.Comma(int64(usage.OutputTokens)))
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
		fmt.Fprintf(out, "%-8s  %d digests | %s messages | %s in + %s out tokens\n",
			dtype,
			stats.TotalDigests,
			humanize.Comma(int64(stats.TotalMessages)),
			humanize.Comma(int64(stats.InputTokens)),
			humanize.Comma(int64(stats.OutputTokens)),
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

	fmt.Fprintf(out, "\nTotal (%d days): %d digests | %s messages | %s in + %s out tokens\n",
		days,
		total.TotalDigests,
		humanize.Comma(int64(total.TotalMessages)),
		humanize.Comma(int64(total.InputTokens)),
		humanize.Comma(int64(total.OutputTokens)),
	)

	// All-time stats
	allTime, err := database.GetDigestStats(db.DigestFilter{})
	if err != nil {
		return fmt.Errorf("querying all-time stats: %w", err)
	}
	if allTime.TotalDigests > 0 {
		fmt.Fprintf(out, "All time: %d digests\n", allTime.TotalDigests)
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
	applyProviderOverride(cfg)
	if err := cfg.ValidateWorkspace(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
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

	fmt.Fprintf(out, "Generating summary for %s to %s...\n\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"))

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	gen, savePool := cliPooledGenerator(cfg, logger)
	defer savePool()
	pipe := digest.New(database, cfg, gen, logger)

	runID, _ := database.CreatePipelineRun("digest-summary", "cli", "auto")

	result, usage, err := pipe.RunPeriodSummary(cmd.Context(), from, to)

	// Complete pipeline run regardless of outcome.
	{
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		inTok, outTok, cost, totalAPI := 0, 0, 0.0, 0
		if usage != nil {
			inTok, outTok, cost, totalAPI = usage.InputTokens, usage.OutputTokens, usage.CostUSD, usage.TotalAPITokens
		}
		fromUnix := float64(from.Unix())
		toUnix := float64(to.Unix())
		if runID > 0 {
			_ = database.CompletePipelineRun(runID, 1, inTok, outTok, cost, totalAPI, &fromUnix, &toUnix, errMsg)
		}
	}

	if err != nil {
		return err
	}

	// Print summary
	var buf strings.Builder
	fmt.Fprintf(&buf, "## Summary: %s to %s\n\n", from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Fprintf(&buf, "%s\n\n", result.Summary)

	for _, t := range result.Topics {
		fmt.Fprintf(&buf, "### %s\n\n", t.Title)
		if t.Summary != "" {
			fmt.Fprintf(&buf, "%s\n\n", t.Summary)
		}
		for _, dec := range t.Decisions {
			if dec.By != "" {
				fmt.Fprintf(&buf, "- **Decision:** %s (by %s)\n", dec.Text, dec.By)
			} else {
				fmt.Fprintf(&buf, "- **Decision:** %s\n", dec.Text)
			}
		}
		for _, a := range t.ActionItems {
			assignee := ""
			if a.Assignee != "" {
				assignee = " -> " + a.Assignee
			}
			fmt.Fprintf(&buf, "- [%s] %s%s\n", a.Status, a.Text, assignee)
		}
		fmt.Fprintln(&buf)
	}

	if usage != nil {
		fmt.Fprintf(&buf, "---\nTokens: %s in + %s out\n",
			humanize.Comma(int64(usage.InputTokens)),
			humanize.Comma(int64(usage.OutputTokens)))
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))

	return nil
}

func joinTopics(topics []string) string {
	return strings.Join(topics, ", ")
}

func runDigestResetContext(cmd *cobra.Command, args []string) error {
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

	var channelID string
	if len(args) > 0 {
		channelName := strings.TrimPrefix(args[0], "#")
		ch, err := database.GetChannelByName(channelName)
		if err != nil {
			return fmt.Errorf("channel %q not found: %w", channelName, err)
		}
		channelID = ch.ID
	}

	affected, err := database.ResetRunningSummary(channelID, "")
	if err != nil {
		return fmt.Errorf("resetting running context: %w", err)
	}

	if channelID != "" {
		fmt.Fprintf(out, "Reset running context for channel %s (%d digests updated).\n", args[0], affected)
	} else {
		fmt.Fprintf(out, "Reset running context for all channels (%d digests updated).\n", affected)
	}

	return nil
}
