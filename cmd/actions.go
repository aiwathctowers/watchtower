package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/actionitems"
	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/ui"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	actionsFlagStatus          string
	actionsFlagPriority        string
	actionsFlagChannel         string
	actionsGenFlagSince        int
	actionsGenFlagProgressJSON bool
	actionsSnoozeFlagUntil     string
	actionsSnoozeFlagHours     int
)

var actionsCmd = &cobra.Command{
	Use:   "actions",
	Short: "Show action items assigned to you across all channels",
	Long:  "Displays AI-extracted action items directed at the current Slack user. Items are generated automatically in daemon mode or manually via 'actions generate'.",
	RunE:  runActions,
}

var actionsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Extract action items from existing synced messages",
	Long:  "Runs the action items pipeline on already-synced messages to find tasks directed at you.",
	RunE:  runActionsGenerate,
}

var actionsAcceptCmd = &cobra.Command{
	Use:   "accept <id>",
	Short: "Accept an inbox item — moves it to active",
	Args:  cobra.ExactArgs(1),
	RunE:  runActionsAccept,
}

var actionsDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark an action item as done",
	Args:  cobra.ExactArgs(1),
	RunE:  runActionsStatusChange("done"),
}

var actionsDismissCmd = &cobra.Command{
	Use:   "dismiss <id>",
	Short: "Dismiss an action item",
	Args:  cobra.ExactArgs(1),
	RunE:  runActionsStatusChange("dismissed"),
}

var actionsSnoozCmd = &cobra.Command{
	Use:   "snooze <id>",
	Short: "Snooze an action item until a specific time",
	Long: `Snooze an action item. It will return to its previous status (inbox or active) when the time arrives.

Presets: --until tomorrow, --until next-week, --until monday
Date:    --until 2026-03-15
Hours:   --hours 4`,
	Args: cobra.ExactArgs(1),
	RunE: runActionsSnooze,
}

func init() {
	rootCmd.AddCommand(actionsCmd)
	actionsCmd.AddCommand(actionsGenerateCmd)
	actionsCmd.AddCommand(actionsAcceptCmd)
	actionsCmd.AddCommand(actionsDoneCmd)
	actionsCmd.AddCommand(actionsDismissCmd)
	actionsCmd.AddCommand(actionsSnoozCmd)
	actionsCmd.Flags().StringVar(&actionsFlagStatus, "status", "", "filter by status (inbox, active, done, dismissed, snoozed, all)")
	actionsCmd.Flags().StringVar(&actionsFlagPriority, "priority", "", "filter by priority (high, medium, low)")
	actionsCmd.Flags().StringVar(&actionsFlagChannel, "channel", "", "filter by channel name")
	actionsGenerateCmd.Flags().IntVar(&actionsGenFlagSince, "since", 1, "look back N days for messages")
	actionsGenerateCmd.Flags().BoolVar(&actionsGenFlagProgressJSON, "progress-json", false, "output progress as JSON lines")
	actionsSnoozCmd.Flags().StringVar(&actionsSnoozeFlagUntil, "until", "", "when to unsnooze (tomorrow, next-week, monday, or YYYY-MM-DD)")
	actionsSnoozCmd.Flags().IntVar(&actionsSnoozeFlagHours, "hours", 0, "snooze for N hours")
}

func runActions(cmd *cobra.Command, args []string) error {
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
	if !validStatuses[actionsFlagStatus] {
		return fmt.Errorf("invalid --status %q: must be one of inbox, active, done, dismissed, snoozed, all", actionsFlagStatus)
	}
	validPriorities := map[string]bool{"high": true, "medium": true, "low": true, "": true}
	if !validPriorities[actionsFlagPriority] {
		return fmt.Errorf("invalid --priority %q: must be one of high, medium, low", actionsFlagPriority)
	}

	if actionsFlagChannel != "" {
		ch, err := database.GetChannelByName(actionsFlagChannel)
		if err != nil {
			return fmt.Errorf("looking up channel: %w", err)
		}
		if ch == nil {
			return fmt.Errorf("channel #%s not found", actionsFlagChannel)
		}
		actionsFlagChannel = ch.ID // reuse var for channel ID
	}

	// Default: show inbox + active. With --status flag: show that specific status.
	var items []db.ActionItem
	if actionsFlagStatus == "" {
		// Fetch inbox and active separately so we can group them.
		for _, st := range []string{"inbox", "active"} {
			f := db.ActionItemFilter{
				AssigneeUserID: userID,
				Status:         st,
				Priority:       actionsFlagPriority,
				ChannelID:      actionsFlagChannel,
			}
			batch, err := database.GetActionItems(f)
			if err != nil {
				return fmt.Errorf("querying action items: %w", err)
			}
			items = append(items, batch...)
		}
	} else {
		f := db.ActionItemFilter{
			AssigneeUserID: userID,
			Priority:       actionsFlagPriority,
			ChannelID:      actionsFlagChannel,
		}
		if actionsFlagStatus != "all" {
			f.Status = actionsFlagStatus
		}
		var err error
		items, err = database.GetActionItems(f)
		if err != nil {
			return fmt.Errorf("querying action items: %w", err)
		}
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No action items found. Run 'watchtower actions generate' to extract them from synced messages.")
		return nil
	}

	var buf strings.Builder
	// Split into inbox and active for grouped display
	var inbox, active, other []db.ActionItem
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
		printActionItems(&buf, inbox, database)
	}
	if len(active) > 0 {
		fmt.Fprintf(&buf, "## Active (%d)\n\n", len(active))
		printActionItems(&buf, active, database)
	}
	if len(other) > 0 {
		fmt.Fprintf(&buf, "## Other (%d)\n\n", len(other))
		printActionItems(&buf, other, database)
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	return nil
}

func printActionItems(w io.Writer, items []db.ActionItem, database *db.DB) {
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
			if ch, err := database.GetChannelByID(item.ChannelID); err == nil && ch != nil {
				channelName = ch.Name
			}
		}

		status := ""
		if item.Status != "inbox" && item.Status != "active" {
			status = fmt.Sprintf(" [%s]", item.Status)
		}

		catBadge := ""
		if label, ok := categoryLabel[item.Category]; ok {
			catBadge = fmt.Sprintf(" `%s`", label)
		}

		fmt.Fprintf(w, "%s #%d **%s**%s%s%s\n", icon, item.ID, item.Text, catBadge, status, updateBadge)

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

func runActionsStatusChange(newStatus string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id, err := parseActionItemID(args[0])
		if err != nil {
			return err
		}

		database, err := openActionItemsDB()
		if err != nil {
			return err
		}
		defer database.Close()

		if err := database.UpdateActionItemStatus(id, newStatus); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Action item #%d marked as %s\n", id, newStatus)
		return nil
	}
}

func runActionsAccept(cmd *cobra.Command, args []string) error {
	id, err := parseActionItemID(args[0])
	if err != nil {
		return err
	}

	database, err := openActionItemsDB()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.AcceptActionItem(id); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Action item #%d accepted (inbox → active)\n", id)
	return nil
}

func runActionsSnooze(cmd *cobra.Command, args []string) error {
	id, err := parseActionItemID(args[0])
	if err != nil {
		return err
	}

	until, err := parseSnoozeUntil(actionsSnoozeFlagUntil, actionsSnoozeFlagHours)
	if err != nil {
		return err
	}

	database, err := openActionItemsDB()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.SnoozeActionItem(id, float64(until.Unix())); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Action item #%d snoozed until %s\n", id, until.Format("2006-01-02 15:04"))
	return nil
}

// parseSnoozeUntil parses --until and --hours flags into a concrete time.
func parseSnoozeUntil(until string, hours int) (time.Time, error) {
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
		daysUntilMonday := (8 - int(now.Weekday())) % 7
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
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location()), nil
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

func parseActionItemID(s string) (int, error) {
	var id int
	if _, err := fmt.Sscan(s, &id); err != nil {
		return 0, fmt.Errorf("invalid action item ID %q: %w", s, err)
	}
	return id, nil
}

func openActionItemsDB() (*db.DB, error) {
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

func runActionsGenerate(cmd *cobra.Command, args []string) error {
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

	days := actionsGenFlagSince
	if days <= 0 {
		days = 1
	}

	userID, err := resolveCurrentUser(cmd, cfg, database)
	if err != nil {
		return err
	}

	gen := digest.NewClaudeGenerator(cfg.Digest.Model)
	pipe := actionitems.New(database, cfg, gen, logger)

	if days > 3650 {
		days = 3650 // clamp to prevent time.Duration overflow
	}

	now := time.Now().UTC()
	to := float64(now.Unix())
	from := float64(now.Add(-time.Duration(days) * 24 * time.Hour).Unix())

	if actionsGenFlagProgressJSON {
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
			emit(pj{Pipeline: "actions", Done: done, Total: total, Status: status})
		}
		n, err := pipe.RunForWindow(cmd.Context(), userID, from, to)
		inTok, outTok, cost := pipe.AccumulatedUsage()
		final := pj{Pipeline: "actions", Finished: true, ItemsFound: n, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost}
		if err != nil {
			final.Error = err.Error()
		}
		emit(final)
		return nil
	}

	spinner := ui.NewSpinner(out, fmt.Sprintf("Extracting action items for the last %d day(s) using %s...", days, cfg.Digest.Model))
	pipe.OnProgress = func(done, total int, status string) {
		spinner.UpdateProgress(done, total, status)
	}

	n, err := pipe.RunForWindow(cmd.Context(), userID, from, to)
	if err != nil {
		spinner.Stop("failed")
		return fmt.Errorf("action items pipeline: %w", err)
	}

	if n == 0 {
		spinner.Stop("No action items found")
	} else {
		spinner.Stop(fmt.Sprintf("Found %d action item(s). Run 'watchtower actions' to view them.", n))
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
	// Cache for future use
	_ = database.SetCurrentUserID(authResp.UserID)
	return authResp.UserID, nil
}
