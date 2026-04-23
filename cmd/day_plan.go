package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/dayplan"

	"github.com/spf13/cobra"
)

var dayPlanCmd = &cobra.Command{
	Use:   "day-plan",
	Short: "Manage personalized daily plans",
	RunE:  runDayPlanShow,
}

var dayPlanShowCmd = &cobra.Command{
	Use:   "show [date]",
	Short: "Show day plan for a date (default: today)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDayPlanShow,
}

var dayPlanListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent day plans",
	RunE:  runDayPlanList,
}

var dayPlanResetCmd = &cobra.Command{
	Use:   "reset [date]",
	Short: "Delete day plan (tasks unaffected)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDayPlanReset,
}

var dayPlanCheckConflictsCmd = &cobra.Command{
	Use:   "check-conflicts [date]",
	Short: "Run conflict detection for a date (default: today)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDayPlanCheckConflicts,
}

var dayPlanGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate (or regenerate) the day plan for a date",
	RunE:  runDayPlanGenerate,
}

// newDayPlanPipelineFactory is the seam tests override to inject a mock generator.
var newDayPlanPipelineFactory = func(database *db.DB, cfg *config.Config, logger *log.Logger) (*dayplan.Pipeline, error) {
	gen := cliGenerator(cfg)
	return dayplan.New(database, cfg, gen, logger), nil
}

func init() {
	rootCmd.AddCommand(dayPlanCmd)
	dayPlanCmd.AddCommand(dayPlanShowCmd, dayPlanListCmd, dayPlanResetCmd, dayPlanCheckConflictsCmd, dayPlanGenerateCmd)
	dayPlanListCmd.Flags().Int("limit", 7, "max plans to show")

	dayPlanGenerateCmd.Flags().String("date", "", "date to generate plan for (YYYY-MM-DD, default: today)")
	dayPlanGenerateCmd.Flags().Bool("force", false, "regenerate even if a plan already exists")
	dayPlanGenerateCmd.Flags().String("feedback", "", "feedback text (implies --force)")
	dayPlanGenerateCmd.Flags().Bool("json", false, "output as JSON instead of human-readable text")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func dayPlanDate(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return time.Now().Format("2006-01-02")
}

// ── handlers ──────────────────────────────────────────────────────────────────

func runDayPlanShow(cmd *cobra.Command, args []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	userID, err := database.GetCurrentUserID()
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set. Run 'watchtower sync' first.")
		return nil
	}

	date := dayPlanDate(args)

	plan, err := database.GetDayPlan(userID, date)
	if err != nil {
		return fmt.Errorf("loading day plan: %w", err)
	}
	if plan == nil {
		fmt.Fprintf(out, "No day plan for %s.\n", date)
		return nil
	}

	items, err := database.GetDayPlanItems(plan.ID)
	if err != nil {
		return fmt.Errorf("loading day plan items: %w", err)
	}

	fmt.Fprint(out, formatDayPlanShow(plan, items))
	_ = database.MarkDayPlanRead(plan.ID)
	return nil
}

func runDayPlanList(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 7
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	userID, err := database.GetCurrentUserID()
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set. Run 'watchtower sync' first.")
		return nil
	}

	plans, err := database.ListDayPlans(userID, limit)
	if err != nil {
		return fmt.Errorf("listing day plans: %w", err)
	}

	if len(plans) == 0 {
		fmt.Fprintln(out, "No day plans found.")
		return nil
	}

	fmt.Fprintf(out, "%-12s  %-7s  %-6s  %-11s  %s\n", "DATE", "ITEMS", "DONE", "CONFLICTS", "GENERATED")
	for _, p := range plans {
		items, err := database.GetDayPlanItems(p.ID)
		if err != nil {
			items = nil
		}
		total := len(items)
		done := 0
		for _, it := range items {
			if it.Status == "done" {
				done++
			}
		}
		conflicts := "no"
		if p.HasConflicts {
			conflicts = "yes"
		}
		genTime := p.GeneratedAt.Local().Format("15:04")
		doneStr := fmt.Sprintf("%d/%d", done, total)
		fmt.Fprintf(out, "%-12s  %-7d  %-6s  %-11s  %s\n",
			p.PlanDate, total, doneStr, conflicts, genTime)
	}
	return nil
}

func runDayPlanReset(cmd *cobra.Command, args []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	userID, err := database.GetCurrentUserID()
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set. Run 'watchtower sync' first.")
		return nil
	}

	date := dayPlanDate(args)

	plan, err := database.GetDayPlan(userID, date)
	if err != nil {
		return fmt.Errorf("looking up day plan: %w", err)
	}
	if plan == nil {
		fmt.Fprintf(out, "No day plan for %s.\n", date)
		return nil
	}

	_, err = database.Exec(`DELETE FROM day_plans WHERE id = ?`, plan.ID)
	if err != nil {
		return fmt.Errorf("deleting day plan: %w", err)
	}

	fmt.Fprintf(out, "Deleted day plan for %s (id=%d). Tasks unaffected.\n", date, plan.ID)
	return nil
}

func runDayPlanCheckConflicts(cmd *cobra.Command, args []string) error {
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
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set. Run 'watchtower sync' first.")
		return nil
	}

	date := dayPlanDate(args)

	plan, err := database.GetDayPlan(userID, date)
	if err != nil {
		return fmt.Errorf("looking up day plan: %w", err)
	}
	if plan == nil {
		fmt.Fprintf(out, "No day plan for %s.\n", date)
		return nil
	}

	logger := log.New(cmd.ErrOrStderr(), "", 0)
	pipe := dayplan.New(database, cfg, nil, logger)

	if err := pipe.SyncCalendarItemsForDate(cmd.Context(), userID, date); err != nil {
		return fmt.Errorf("syncing calendar items: %w", err)
	}
	if err := pipe.DetectConflicts(cmd.Context(), userID, date); err != nil {
		return fmt.Errorf("detecting conflicts: %w", err)
	}

	// Re-fetch to get updated conflict state.
	plan, err = database.GetDayPlan(userID, date)
	if err != nil {
		return fmt.Errorf("reloading day plan: %w", err)
	}

	if plan.HasConflicts {
		fmt.Fprintf(out, "Conflicts: %s\n", plan.ConflictSummary.String)
	} else {
		fmt.Fprintln(out, "No conflicts.")
	}
	return nil
}

func runDayPlanGenerate(cmd *cobra.Command, _ []string) error {
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

	// Force day_plan enabled so CLI generate always works.
	cfg.DayPlan.Enabled = true

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	userID, err := database.GetCurrentUserID()
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set. Run 'watchtower sync' first.")
		return nil
	}

	// Parse flags.
	dateFlag, _ := cmd.Flags().GetString("date")
	forceFlag, _ := cmd.Flags().GetBool("force")
	feedbackFlag, _ := cmd.Flags().GetString("feedback")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	date := dateFlag
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	pipe, err := newDayPlanPipelineFactory(database, cfg, logger)
	if err != nil {
		return fmt.Errorf("creating day-plan pipeline: %w", err)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	plan, err := pipe.Run(ctx, dayplan.RunOptions{
		UserID:   userID,
		Date:     date,
		Force:    forceFlag || feedbackFlag != "",
		Feedback: feedbackFlag,
	})
	if err != nil {
		return fmt.Errorf("generating day plan: %w", err)
	}
	if plan == nil {
		fmt.Fprintln(out, "Day plan generation is disabled in config.")
		return nil
	}

	items, err := database.GetDayPlanItems(plan.ID)
	if err != nil {
		return fmt.Errorf("loading day plan items: %w", err)
	}

	if jsonFlag {
		payload := struct {
			Plan  *db.DayPlan      `json:"plan"`
			Items []db.DayPlanItem `json:"items"`
		}{Plan: plan, Items: items}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Fprint(out, formatDayPlanShow(plan, items))
	return nil
}

// ── formatting ────────────────────────────────────────────────────────────────

// formatDayPlanShow renders a human-friendly view of a day plan.
func formatDayPlanShow(p *db.DayPlan, items []db.DayPlanItem) string {
	var sb strings.Builder

	// Count done items.
	done := 0
	for _, it := range items {
		if it.Status == "done" {
			done++
		}
	}

	// Header.
	sb.WriteString(fmt.Sprintf("Day Plan — %s   [%d done / %d total]\n", p.PlanDate, done, len(items)))

	// Conflict warning.
	if p.HasConflicts && p.ConflictSummary.Valid && p.ConflictSummary.String != "" {
		sb.WriteString(fmt.Sprintf("Conflicts: %s\n", p.ConflictSummary.String))
	}
	sb.WriteString("\n")

	// Separate timeblocks and backlog.
	var timeblocks, backlog []db.DayPlanItem
	for _, it := range items {
		if it.Kind == db.DayPlanItemKindTimeblock {
			timeblocks = append(timeblocks, it)
		} else {
			backlog = append(backlog, it)
		}
	}

	if len(timeblocks) > 0 {
		sb.WriteString("TIMELINE\n")
		for _, it := range timeblocks {
			timeRange := "         "
			if it.StartTime.Valid && it.EndTime.Valid {
				start := it.StartTime.Time.Local().Format("15:04")
				end := it.EndTime.Time.Local().Format("15:04")
				timeRange = fmt.Sprintf("%s–%s", start, end)
			}
			sourceTag := fmt.Sprintf("[%s]", it.SourceType)
			sb.WriteString(fmt.Sprintf("  %-11s  %-10s  %s   [%s]\n",
				timeRange, sourceTag, it.Title, it.Status))
		}
		sb.WriteString("\n")
	}

	if len(backlog) > 0 {
		sb.WriteString("BACKLOG (if time permits)\n")
		for _, it := range backlog {
			priority := ""
			if it.Priority.Valid && it.Priority.String != "" {
				priority = fmt.Sprintf(" [%s]", it.Priority.String)
			}
			sourceTag := fmt.Sprintf("[%s]", it.SourceType)
			sb.WriteString(fmt.Sprintf("  • %s%s %s   [%s]\n",
				sourceTag, priority, it.Title, it.Status))
		}
		sb.WriteString("\n")
	}

	// Footer.
	genTime := p.GeneratedAt.Local().Format("15:04")
	footer := fmt.Sprintf("Generated %s", genTime)
	if p.RegenerateCount > 0 {
		footer += fmt.Sprintf("  |  Regenerated %d time(s)", p.RegenerateCount)
		// Show last feedback if any.
		if p.FeedbackHistory != "" && p.FeedbackHistory != "[]" {
			var feedbacks []string
			// Simple extraction: find first quoted string.
			if idx := strings.Index(p.FeedbackHistory, `"`); idx >= 0 {
				rest := p.FeedbackHistory[idx+1:]
				if end := strings.Index(rest, `"`); end >= 0 {
					feedbacks = append(feedbacks, rest[:end])
				}
			}
			if len(feedbacks) > 0 {
				footer += fmt.Sprintf(`  |  Last feedback: "%s"`, feedbacks[0])
			}
		}
	}
	sb.WriteString(footer + "\n")

	return sb.String()
}
