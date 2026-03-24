package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/tracks"
	"watchtower/internal/ui"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	tracksFlagStatus          string
	tracksFlagPriority        string
	tracksFlagChannel         string
	tracksFlagOwnership       string
	tracksGenFlagSince        int
	tracksGenFlagProgressJSON bool
	tracksSnoozeFlagUntil     string
	tracksSnoozeFlagHours     int
)

var tracksCmd = &cobra.Command{
	Use:   "tracks",
	Short: "Show tracks assigned to you across all channels",
	Long:  "Displays AI-extracted tracks directed at the current Slack user. Tracks are generated automatically in daemon mode or manually via 'tracks generate'.",
	RunE:  runTracks,
}

var tracksGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Extract tracks from existing synced messages",
	Long:  "Runs the tracks pipeline on already-synced messages to find tasks directed at you.",
	RunE:  runTracksGenerate,
}

var tracksAcceptCmd = &cobra.Command{
	Use:   "accept <id>",
	Short: "Accept an inbox item — moves it to active",
	Args:  cobra.ExactArgs(1),
	RunE:  runTracksAccept,
}

var tracksDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a track as done",
	Args:  cobra.ExactArgs(1),
	RunE:  runTracksStatusChange("done"),
}

var tracksDismissCmd = &cobra.Command{
	Use:   "dismiss <id>",
	Short: "Dismiss a track",
	Args:  cobra.ExactArgs(1),
	RunE:  runTracksStatusChange("dismissed"),
}

var tracksSnoozeCmd = &cobra.Command{
	Use:   "snooze <id>",
	Short: "Snooze a track until a specific time",
	Long: `Snooze a track. It will return to its previous status (inbox or active) when the time arrives.

Presets: --until tomorrow, --until next-week, --until monday
Date:    --until 2026-03-15
Hours:   --hours 4`,
	Args: cobra.ExactArgs(1),
	RunE: runTracksSnooze,
}

// actionsCmd is a hidden deprecated alias for tracksCmd.
var actionsCmd = &cobra.Command{
	Use:        "actions",
	Short:      "Deprecated: use 'watchtower tracks' instead",
	Hidden:     true,
	Deprecated: "use 'watchtower tracks' instead",
	RunE:       runTracks,
}

func init() {
	rootCmd.AddCommand(tracksCmd)
	rootCmd.AddCommand(actionsCmd)
	tracksCmd.AddCommand(tracksGenerateCmd)
	tracksCmd.AddCommand(tracksAcceptCmd)
	tracksCmd.AddCommand(tracksDoneCmd)
	tracksCmd.AddCommand(tracksDismissCmd)
	tracksCmd.AddCommand(tracksSnoozeCmd)
	// Register same subcommands under the deprecated alias.
	actionsSnoozeCmd := &cobra.Command{Use: "snooze", Hidden: true, Deprecated: "use 'watchtower tracks snooze'", Args: cobra.ExactArgs(1), RunE: runTracksSnooze}
	actionsSnoozeCmd.Flags().StringVar(&tracksSnoozeFlagUntil, "until", "", "when to unsnooze (tomorrow, next-week, monday, or YYYY-MM-DD)")
	actionsSnoozeCmd.Flags().IntVar(&tracksSnoozeFlagHours, "hours", 0, "snooze for N hours")
	actionsCmd.AddCommand(
		&cobra.Command{Use: "generate", Hidden: true, Deprecated: "use 'watchtower tracks generate'", RunE: runTracksGenerate},
		&cobra.Command{Use: "accept", Hidden: true, Deprecated: "use 'watchtower tracks accept'", Args: cobra.ExactArgs(1), RunE: runTracksAccept},
		&cobra.Command{Use: "done", Hidden: true, Deprecated: "use 'watchtower tracks done'", Args: cobra.ExactArgs(1), RunE: runTracksStatusChange("done")},
		&cobra.Command{Use: "dismiss", Hidden: true, Deprecated: "use 'watchtower tracks dismiss'", Args: cobra.ExactArgs(1), RunE: runTracksStatusChange("dismissed")},
		actionsSnoozeCmd,
	)
	tracksCmd.Flags().StringVar(&tracksFlagStatus, "status", "", "filter by status (inbox, active, done, dismissed, snoozed, all)")
	tracksCmd.Flags().StringVar(&tracksFlagPriority, "priority", "", "filter by priority (high, medium, low)")
	tracksCmd.Flags().StringVar(&tracksFlagChannel, "channel", "", "filter by channel name")
	tracksCmd.Flags().StringVar(&tracksFlagOwnership, "ownership", "", "filter by ownership (mine, delegated, watching)")
	tracksGenerateCmd.Flags().IntVar(&tracksGenFlagSince, "since", 1, "look back N days for messages")
	tracksGenerateCmd.Flags().BoolVar(&tracksGenFlagProgressJSON, "progress-json", false, "output progress as JSON lines")
	tracksSnoozeCmd.Flags().StringVar(&tracksSnoozeFlagUntil, "until", "", "when to unsnooze (tomorrow, next-week, monday, or YYYY-MM-DD)")
	tracksSnoozeCmd.Flags().IntVar(&tracksSnoozeFlagHours, "hours", 0, "snooze for N hours")
}

func runTracks(cmd *cobra.Command, args []string) error {
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

	userID, err := database.GetCurrentUserID()
	if err != nil {
		return fmt.Errorf("getting current user: %w", err)
	}
	if userID == "" {
		fmt.Fprintln(out, "Current user not set. Run 'watchtower sync' first to identify your Slack account.")
		return nil
	}

	// Validate flag values
	validStatuses := map[string]bool{"inbox": true, "active": true, "done": true, "dismissed": true, "snoozed": true, "all": true, "": true}
	if !validStatuses[tracksFlagStatus] {
		return fmt.Errorf("invalid --status %q: must be one of inbox, active, done, dismissed, snoozed, all", tracksFlagStatus)
	}
	validPriorities := map[string]bool{"high": true, "medium": true, "low": true, "": true}
	if !validPriorities[tracksFlagPriority] {
		return fmt.Errorf("invalid --priority %q: must be one of high, medium, low", tracksFlagPriority)
	}
	validOwnerships := map[string]bool{"mine": true, "delegated": true, "watching": true, "": true}
	if !validOwnerships[tracksFlagOwnership] {
		return fmt.Errorf("invalid --ownership %q: must be one of mine, delegated, watching", tracksFlagOwnership)
	}

	channelIDFilter := ""
	if tracksFlagChannel != "" {
		ch, err := database.GetChannelByName(tracksFlagChannel)
		if err != nil {
			return fmt.Errorf("looking up channel: %w", err)
		}
		if ch == nil {
			return fmt.Errorf("channel #%s not found", tracksFlagChannel)
		}
		channelIDFilter = ch.ID
	}

	// Default: show inbox + active. With --status flag: show that specific status.
	var items []db.Track
	if tracksFlagStatus == "" {
		// Fetch inbox and active separately so we can group them.
		for _, st := range []string{"inbox", "active"} {
			f := db.TrackFilter{
				AssigneeUserID: userID,
				Status:         st,
				Priority:       tracksFlagPriority,
				ChannelID:      channelIDFilter,
				Ownership:      tracksFlagOwnership,
			}
			batch, err := database.GetTracks(f)
			if err != nil {
				return fmt.Errorf("querying tracks: %w", err)
			}
			items = append(items, batch...)
		}
	} else {
		f := db.TrackFilter{
			AssigneeUserID: userID,
			Priority:       tracksFlagPriority,
			ChannelID:      channelIDFilter,
			Ownership:      tracksFlagOwnership,
		}
		if tracksFlagStatus != "all" {
			f.Status = tracksFlagStatus
		}
		var err error
		items, err = database.GetTracks(f)
		if err != nil {
			return fmt.Errorf("querying tracks: %w", err)
		}
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No tracks found. Run 'watchtower tracks generate' to extract them from synced messages.")
		return nil
	}

	var buf strings.Builder
	// Split into inbox and active for grouped display
	var inbox, active, other []db.Track
	for _, item := range items {
		switch item.Status {
		case "inbox":
			inbox = append(inbox, item)
		case "active":
			active = append(active, item)
		default:
			other = append(other, item)
		}
	}

	if len(inbox) > 0 {
		fmt.Fprintf(&buf, "## Inbox (%d)\n\n", len(inbox))
		printTracks(&buf, inbox, database)
	}
	if len(active) > 0 {
		fmt.Fprintf(&buf, "## Active (%d)\n\n", len(active))
		printTracks(&buf, active, database)
	}
	if len(other) > 0 {
		fmt.Fprintf(&buf, "## Other (%d)\n\n", len(other))
		printTracks(&buf, other, database)
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	return nil
}

func printTracks(w io.Writer, items []db.Track, database *db.DB) {
	priorityIcon := map[string]string{
		"high":   "🔴",
		"medium": "🟡",
		"low":    "🟢",
	}

	categoryLabel := map[string]string{
		"code_review":     "review",
		"decision_needed": "decision",
		"info_request":    "info",
		"task":            "task",
		"approval":        "approval",
		"follow_up":       "follow-up",
		"bug_fix":         "bug",
		"discussion":      "discuss",
	}

	// Pre-fetch channel names to avoid N+1 queries.
	channelNameMap := make(map[string]string)
	for _, item := range items {
		if item.SourceChannelName == "" && item.ChannelID != "" {
			channelNameMap[item.ChannelID] = "" // mark for lookup
		}
	}
	if len(channelNameMap) > 0 {
		for chID := range channelNameMap {
			if ch, err := database.GetChannelByID(chID); err == nil && ch != nil {
				name := ch.Name
				if name == "" && (ch.Type == "dm" || ch.Type == "im") && ch.DMUserID.Valid && ch.DMUserID.String != "" {
					if u, err := database.GetUserByID(ch.DMUserID.String); err == nil && u != nil {
						uname := u.DisplayName
						if uname == "" {
							uname = u.Name
						}
						if uname != "" {
							name = "DM: " + uname
						}
					}
				}
				if name == "" {
					name = chID
				}
				channelNameMap[chID] = name
			}
		}
	}

	for _, item := range items {
		icon := priorityIcon[item.Priority]
		if icon == "" {
			icon = "🟡"
		}

		updateBadge := ""
		if item.HasUpdates {
			updateBadge = " 🔔"
		}

		channelName := item.SourceChannelName
		if channelName == "" && item.ChannelID != "" {
			channelName = channelNameMap[item.ChannelID]
		}

		status := ""
		if item.Status != "inbox" && item.Status != "active" {
			status = fmt.Sprintf(" [%s]", item.Status)
		}

		catBadge := ""
		if label, ok := categoryLabel[item.Category]; ok {
			catBadge = fmt.Sprintf(" `%s`", label)
		}

		ownershipBadge := ""
		switch item.Ownership {
		case "delegated":
			ownershipBadge = " 📋"
		case "watching":
			ownershipBadge = " 👁"
		}

		fmt.Fprintf(w, "%s #%d **%s**%s%s%s%s\n", icon, item.ID, item.Text, catBadge, ownershipBadge, status, updateBadge)

		// Meta line: channel, requester, tags
		var meta []string
		if channelName != "" {
			meta = append(meta, "#"+channelName)
		}
		if item.RequesterName != "" {
			meta = append(meta, "from: "+item.RequesterName)
		}
		if item.Tags != "" && item.Tags != "[]" {
			meta = append(meta, "tags: "+item.Tags)
		}
		if len(meta) > 0 {
			fmt.Fprintf(w, "   %s\n", strings.Join(meta, " | "))
		}

		if item.Context != "" {
			fmt.Fprintf(w, "   %s\n", item.Context)
		}
		if item.Blocking != "" {
			fmt.Fprintf(w, "   ⚠ Blocking: %s\n", item.Blocking)
		}
		if item.DecisionSummary != "" {
			fmt.Fprintf(w, "   Decision: %s\n", item.DecisionSummary)
		}

		// Footer line: due date, snooze, age
		var footer []string
		if item.DueDate > 0 {
			due := time.Unix(int64(item.DueDate), 0)
			footer = append(footer, "due: "+due.Format("2006-01-02"))
		}
		if item.SnoozeUntil > 0 && item.Status == "snoozed" {
			snoozeTime := time.Unix(int64(item.SnoozeUntil), 0)
			footer = append(footer, "snoozed until "+snoozeTime.Format("2006-01-02 15:04"))
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z", item.CreatedAt); err == nil {
			footer = append(footer, humanize.Time(t))
		}
		if len(footer) > 0 {
			fmt.Fprintf(w, "   %s\n", strings.Join(footer, " | "))
		}
		fmt.Fprintln(w)
	}
}

func runTracksStatusChange(newStatus string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id, err := parseTrackID(args[0])
		if err != nil {
			return err
		}

		database, err := openTracksDB()
		if err != nil {
			return err
		}
		defer database.Close()

		if err := verifyTrackOwnership(database, id); err != nil {
			return err
		}

		if err := database.UpdateTrackStatus(id, newStatus); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Track #%d marked as %s\n", id, newStatus)
		return nil
	}
}

func runTracksAccept(cmd *cobra.Command, args []string) error {
	id, err := parseTrackID(args[0])
	if err != nil {
		return err
	}

	database, err := openTracksDB()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := verifyTrackOwnership(database, id); err != nil {
		return err
	}

	if err := database.AcceptTrack(id); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Track #%d accepted (inbox → active)\n", id)
	return nil
}

func runTracksSnooze(cmd *cobra.Command, args []string) error {
	id, err := parseTrackID(args[0])
	if err != nil {
		return err
	}

	until, err := parseSnoozeUntil(tracksSnoozeFlagUntil, tracksSnoozeFlagHours)
	if err != nil {
		return err
	}

	database, err := openTracksDB()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := verifyTrackOwnership(database, id); err != nil {
		return err
	}

	if err := database.SnoozeTrack(id, float64(until.Unix())); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Track #%d snoozed until %s\n", id, until.Format("2006-01-02 15:04"))
	return nil
}

// verifyTrackOwnership checks that the track belongs to the current user.
func verifyTrackOwnership(database *db.DB, trackID int) error {
	currentUserID, err := database.GetCurrentUserID()
	if err != nil {
		return fmt.Errorf("getting current user: %w", err)
	}
	assignee, err := database.GetTrackAssignee(trackID)
	if err != nil {
		return fmt.Errorf("track #%d not found: %w", trackID, err)
	}
	if currentUserID != "" && assignee != currentUserID {
		return fmt.Errorf("track #%d belongs to a different user", trackID)
	}
	return nil
}

// parseSnoozeUntil parses --until and --hours flags into a concrete time.
func parseSnoozeUntil(until string, hours int) (time.Time, error) {
	if hours > 0 && until != "" {
		return time.Time{}, fmt.Errorf("specify either --until or --hours, not both")
	}

	now := time.Now()

	if hours > 0 {
		return now.Add(time.Duration(hours) * time.Hour), nil
	}

	if until == "" {
		return time.Time{}, fmt.Errorf("specify --until (tomorrow, next-week, monday, YYYY-MM-DD) or --hours N")
	}

	switch strings.ToLower(until) {
	case "tomorrow":
		t := now.AddDate(0, 0, 1)
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location()), nil
	case "next-week":
		daysUntilMonday := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		t := now.AddDate(0, 0, daysUntilMonday)
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location()), nil
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		target := dayOfWeek(until)
		current := now.Weekday()
		days := (int(target) - int(current) + 7) % 7
		if days == 0 {
			days = 7
		}
		t := now.AddDate(0, 0, days)
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location()), nil
	default:
		// Try YYYY-MM-DD
		t, err := time.ParseInLocation("2006-01-02", until, now.Location())
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --until value %q: use tomorrow, next-week, a weekday name, or YYYY-MM-DD", until)
		}
		snooze := time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location())
		if !snooze.After(now) {
			return time.Time{}, fmt.Errorf("snooze date %s is in the past", until)
		}
		return snooze, nil
	}
}

func dayOfWeek(name string) time.Weekday {
	switch strings.ToLower(name) {
	case "sunday":
		return time.Sunday
	case "monday":
		return time.Monday
	case "tuesday":
		return time.Tuesday
	case "wednesday":
		return time.Wednesday
	case "thursday":
		return time.Thursday
	case "friday":
		return time.Friday
	case "saturday":
		return time.Saturday
	}
	return time.Monday // fallback
}

func parseTrackID(s string) (int, error) {
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid track ID %q: %w", s, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("track ID must be a positive integer, got %d", id)
	}
	return id, nil
}

func openTracksDB() (*db.DB, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return db.Open(cfg.DBPath())
}

func runTracksGenerate(cmd *cobra.Command, args []string) error {
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

	// Force digest enabled for this run.
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

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	days := tracksGenFlagSince
	if days < 0 {
		return fmt.Errorf("--since must be a positive number of days, got %d", days)
	}
	if days == 0 {
		days = 1
	}

	userID, err := resolveCurrentUser(cmd, cfg, database)
	if err != nil {
		return err
	}

	gen, savePool := cliPooledGenerator(cfg, logger)
	defer savePool()
	pipe := tracks.New(database, cfg, gen, logger)

	// When --since is not explicitly passed, use pipe.Run() which detects
	// first run and processes all initial_history_days day-by-day.
	sinceExplicit := cmd.Flags().Changed("since")

	if days > 3650 {
		days = 3650 // clamp to prevent time.Duration overflow
	}

	if tracksGenFlagProgressJSON {
		type pj struct {
			Pipeline         string  `json:"pipeline"`
			Done             int     `json:"done"`
			Total            int     `json:"total"`
			Status           string  `json:"status,omitempty"`
			InputTokens      int     `json:"input_tokens"`
			OutputTokens     int     `json:"output_tokens"`
			CostUSD          float64 `json:"cost_usd"`
			Error            string  `json:"error,omitempty"`
			Finished         bool    `json:"finished"`
			ItemsFound       int     `json:"items_found"`
			MessageCount     int     `json:"message_count,omitempty"`
			PeriodFrom       string  `json:"period_from,omitempty"`
			PeriodTo         string  `json:"period_to,omitempty"`
			StepDurationSec  float64 `json:"step_duration_seconds,omitempty"`
			StepInputTokens  int     `json:"step_input_tokens,omitempty"`
			StepOutputTokens int     `json:"step_output_tokens,omitempty"`
			StepCostUSD      float64 `json:"step_cost_usd,omitempty"`
			TotalAPITokens   int     `json:"total_api_tokens,omitempty"`
		}
		emit := func(p pj) { data, _ := json.Marshal(p); fmt.Fprintln(out, string(data)) }

		runID, _ := database.CreatePipelineRun("tracks", "cli")

		pipe.OnProgress = func(done, total int, status string) {
			inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
			p := pj{Pipeline: "tracks", Done: done, Total: total, Status: status, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost, TotalAPITokens: totalAPI}
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
			p.StepCostUSD = pipe.LastStepCostUSD
			emit(p)

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
					CostUSD: p.StepCostUSD, TotalAPITokens: totalAPI,
					MessageCount: pipe.LastStepMessageCount,
					PeriodFrom:   pFrom, PeriodTo: pTo,
					DurationSeconds: p.StepDurationSec,
				})
			}
		}

		var n int
		if sinceExplicit {
			now := time.Now().UTC()
			to := float64(now.Unix())
			from := float64(now.Add(-time.Duration(days) * 24 * time.Hour).Unix())
			n, err = pipe.RunForWindow(cmd.Context(), userID, from, to)
		} else {
			n, err = pipe.Run(cmd.Context())
		}
		inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
		final := pj{Pipeline: "tracks", Finished: true, ItemsFound: n, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost, TotalAPITokens: totalAPI}
		if pipe.LastStepMessageCount > 0 {
			final.MessageCount = pipe.LastStepMessageCount
			final.PeriodFrom = pipe.LastStepPeriodFrom.Format(time.RFC3339)
			final.PeriodTo = pipe.LastStepPeriodTo.Format(time.RFC3339)
		}
		if err != nil {
			final.Error = err.Error()
		}
		emit(final)

		if runID > 0 {
			var errMsg string
			if err != nil {
				errMsg = err.Error()
			}
			_ = database.CompletePipelineRun(runID, n, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
		}
		return nil
	}

	runID, _ := database.CreatePipelineRun("tracks", "cli")

	var n int
	if sinceExplicit {
		now := time.Now().UTC()
		to := float64(now.Unix())
		from := float64(now.Add(-time.Duration(days) * 24 * time.Hour).Unix())

		spinner := ui.NewSpinner(out, fmt.Sprintf("Extracting tracks for the last %d day(s) using %s...", days, cfg.Digest.Model))
		pipe.OnProgress = func(done, total int, status string) {
			spinner.UpdateProgress(done, total, status)
		}
		n, err = pipe.RunForWindow(cmd.Context(), userID, from, to)
		if err != nil {
			spinner.Stop("failed")
			if runID > 0 {
				_ = database.CompletePipelineRun(runID, 0, 0, 0, 0, 0, nil, nil, err.Error())
			}
			return fmt.Errorf("tracks pipeline: %w", err)
		}
		if n == 0 {
			spinner.Stop("No tracks found")
		} else {
			spinner.Stop(fmt.Sprintf("Found %d track(s). Run 'watchtower tracks' to view them.", n))
		}
		if runID > 0 {
			inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
			_ = database.CompletePipelineRun(runID, n, inTok, outTok, cost, totalAPI, nil, nil, "")
		}
		return nil
	}

	spinner := ui.NewSpinner(out, fmt.Sprintf("Extracting tracks using %s...", cfg.Digest.Model))
	pipe.OnProgress = func(done, total int, status string) {
		spinner.UpdateProgress(done, total, status)
	}

	n, err = pipe.Run(cmd.Context())
	if err != nil {
		spinner.Stop("failed")
		if runID > 0 {
			_ = database.CompletePipelineRun(runID, 0, 0, 0, 0, 0, nil, nil, err.Error())
		}
		return fmt.Errorf("tracks pipeline: %w", err)
	}

	if runID > 0 {
		inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
		_ = database.CompletePipelineRun(runID, n, inTok, outTok, cost, totalAPI, nil, nil, "")
	}

	if n == 0 {
		spinner.Stop("No tracks found")
	} else {
		spinner.Stop(fmt.Sprintf("Found %d track(s). Run 'watchtower tracks' to view them.", n))
	}

	return nil
}

// resolveCurrentUser returns the current Slack user ID, calling auth.test if needed.
func resolveCurrentUser(cmd *cobra.Command, cfg *config.Config, database *db.DB) (string, error) {
	userID, err := database.GetCurrentUserID()
	if err != nil {
		return "", fmt.Errorf("getting current user: %w", err)
	}
	if userID != "" {
		return userID, nil
	}

	// Not cached — call auth.test directly
	ws, err := cfg.GetActiveWorkspace()
	if err != nil {
		return "", fmt.Errorf("getting workspace config: %w", err)
	}
	client := watchtowerslack.NewClient(ws.SlackToken)
	authResp, err := client.AuthTest(cmd.Context())
	if err != nil {
		return "", fmt.Errorf("identifying current user (auth.test): %w", err)
	}
	// Cache for future use.
	if err := database.SetCurrentUserID(authResp.UserID); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not cache current user ID: %v\n", err)
	}
	return authResp.UserID, nil
}
