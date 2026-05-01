package cmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/db"
	"watchtower/internal/jira"
	"watchtower/internal/targets"

	"github.com/spf13/cobra"
)

var (
	targetsFlagStatus     string
	targetsFlagPriority   string
	targetsFlagOwnership  string
	targetsFlagAll        bool
	targetsFlagJSON       bool
	targetsFlagText       string
	targetsFlagIntent     string
	targetsFlagDue        string
	targetsFlagSourceType string
	targetsFlagSourceID   string
	targetsFlagTags       string
	targetsFlagBallOn     string
	targetsFlagBlocking   string
	targetsFlagSource     string
	targetsFlagLevel      string
	// targetsFlagPeriod removed — period filtering returns in V2 (DB filter not yet wired)
	targetsFlagPeriodStart string
	targetsFlagPeriodEnd   string
	targetsFlagParent      int
	targetsFlagInstruction string

	// link subcommand flags
	targetsFlagLinkParent   int
	targetsFlagLinkTo       int
	targetsFlagLinkRelation string
	targetsFlagLinkExternal string

	// extract subcommand flags
	targetsFlagExtractText      string
	targetsFlagExtractSourceRef string
	targetsFlagExtractFromInbox int
	targetsFlagExtractJSON      bool

	// suggest-links subcommand flags
	targetsFlagSuggestLinksJSON bool

	// delete subcommand flags
	targetsFlagDeleteJSON bool

	// promote-subitem subcommand flags (dedicated, NOT shared with create/update,
	// to avoid cross-subcommand state leaks between command invocations and tests).
	targetsFlagPromoteJSON        bool
	targetsFlagPromoteText        string
	targetsFlagPromoteIntent      string
	targetsFlagPromoteLevel       string
	targetsFlagPromotePriority    string
	targetsFlagPromoteOwnership   string
	targetsFlagPromoteDue         string
	targetsFlagPromotePeriodStart string
	targetsFlagPromotePeriodEnd   string
	targetsFlagPromoteTags        string
)

var targetsCmd = &cobra.Command{
	Use:   "targets",
	Short: "Manage hierarchical goals (targets) with levels and relationships",
	RunE:  runTargetsList,
}

var targetsShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show target details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsShow,
}

var targetsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new target",
	RunE:  runTargetsCreate,
}

var targetsExtractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract targets from text using AI",
	Long:  "Run AI extraction on the provided text, preview each proposed target, and confirm before inserting.",
	RunE:  runTargetsExtract,
}

var targetsLinkCmd = &cobra.Command{
	Use:   "link <id>",
	Short: "Add a link from a target to a parent, another target, or an external reference",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsLink,
}

var targetsUnlinkCmd = &cobra.Command{
	Use:   "unlink <link-id>",
	Short: "Remove a target link by its link ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsUnlink,
}

var targetsSuggestLinksCmd = &cobra.Command{
	Use:   "suggest-links <id>",
	Short: "Use AI to propose parent and secondary links for a target",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsSuggestLinks,
}

var targetsDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a target as done",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsDone,
}

var targetsDismissCmd = &cobra.Command{
	Use:   "dismiss <id>",
	Short: "Dismiss a target",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsDismiss,
}

var targetsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a target permanently",
	Long:  "Removes a target by ID. Children orphan to the root; linked rows cascade. This cannot be undone.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsDelete,
}

var targetsSnoozeCmd = &cobra.Command{
	Use:   "snooze <id> <date>",
	Short: "Snooze a target until a date",
	Args:  cobra.ExactArgs(2),
	RunE:  runTargetsSnooze,
}

var targetsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a target",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsUpdate,
}

var targetsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate target details with AI (checklist, priority, due date)",
	Long:  "Uses AI to enrich a target description: breaks it into sub-items, suggests priority and due date. Outputs JSON to stdout.",
	RunE:  runTargetsGenerate,
}

var targetsNoteCmd = &cobra.Command{
	Use:   "note",
	Short: "Manage target notes",
}

var targetsNoteAddCmd = &cobra.Command{
	Use:   "add <id> <text>",
	Short: "Add a note to a target",
	Args:  cobra.ExactArgs(2),
	RunE:  runTargetsNoteAdd,
}

var targetsNoteListCmd = &cobra.Command{
	Use:   "list <id>",
	Short: "List notes for a target",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsNoteList,
}

var targetsAIUpdateCmd = &cobra.Command{
	Use:   "ai-update <id>",
	Short: "Update a target using AI based on your instruction",
	Long:  "Reads current target state, sends it with your instruction to AI, and outputs the updated target as JSON to stdout. The caller is responsible for applying the changes.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsAIUpdate,
}

var targetsPromoteSubItemCmd = &cobra.Command{
	Use:   "promote-subitem <target-id> <sub-item-index>",
	Short: "Convert a sub-item into a standalone child target",
	Long: "Promotes the sub-item at the given index of the target to a new child target with parent_id set. " +
		"The sub-item is removed from the parent's checklist and parent.progress is recomputed. " +
		"Field defaults: text and due_date come from the sub-item itself (due_date falls back to the parent " +
		"when the sub-item has none); intent, level, priority, ownership, period and tags come from the parent. " +
		"ball_on is inherited from the parent; blocking and snooze_until are cleared. " +
		"Status mirrors the sub-item's done flag (done sub-item -> done child) so parent progress stays stable. " +
		"Any flag overrides the corresponding default.",
	Args: cobra.ExactArgs(2),
	RunE: runTargetsPromoteSubItem,
}

func init() {
	rootCmd.AddCommand(targetsCmd)
	targetsCmd.AddCommand(
		targetsShowCmd,
		targetsCreateCmd,
		targetsExtractCmd,
		targetsLinkCmd,
		targetsUnlinkCmd,
		targetsSuggestLinksCmd,
		targetsDoneCmd,
		targetsDismissCmd,
		targetsDeleteCmd,
		targetsSnoozeCmd,
		targetsUpdateCmd,
		targetsGenerateCmd,
		targetsNoteCmd,
		targetsAIUpdateCmd,
		targetsPromoteSubItemCmd,
	)
	targetsNoteCmd.AddCommand(targetsNoteAddCmd, targetsNoteListCmd)

	// targets (list) flags
	targetsCmd.Flags().StringVar(&targetsFlagStatus, "status", "", "filter by status (todo, in_progress, blocked, done, dismissed, snoozed)")
	targetsCmd.Flags().StringVar(&targetsFlagPriority, "priority", "", "filter by priority (high, medium, low)")
	targetsCmd.Flags().StringVar(&targetsFlagOwnership, "ownership", "", "filter by ownership (mine, delegated, watching)")
	targetsCmd.Flags().BoolVar(&targetsFlagAll, "all", false, "include done and dismissed targets")
	targetsCmd.Flags().BoolVar(&targetsFlagJSON, "json", false, "output as JSON")
	targetsCmd.Flags().StringVar(&targetsFlagSource, "source", "", "filter by source (all, jira, slack, manual, track, digest, inbox)")
	targetsCmd.Flags().StringVar(&targetsFlagLevel, "level", "", "filter by level (quarter, month, week, day, custom)")
	// --period flag removed; period filtering not yet wired to DB query (V2)

	// create flags
	targetsCreateCmd.Flags().StringVar(&targetsFlagText, "text", "", "target text (required)")
	targetsCreateCmd.Flags().StringVar(&targetsFlagIntent, "intent", "", "target intent/context")
	targetsCreateCmd.Flags().StringVar(&targetsFlagPriority, "priority", "medium", "priority (high, medium, low)")
	targetsCreateCmd.Flags().StringVar(&targetsFlagOwnership, "ownership", "mine", "ownership (mine, delegated, watching)")
	targetsCreateCmd.Flags().StringVar(&targetsFlagLevel, "level", "day", "level (quarter, month, week, day, custom)")
	targetsCreateCmd.Flags().IntVar(&targetsFlagParent, "parent", 0, "parent target ID")
	targetsCreateCmd.Flags().StringVar(&targetsFlagPeriodStart, "period-start", "", "period start date (YYYY-MM-DD)")
	targetsCreateCmd.Flags().StringVar(&targetsFlagPeriodEnd, "period-end", "", "period end date (YYYY-MM-DD)")
	targetsCreateCmd.Flags().StringVar(&targetsFlagDue, "due", "", "due date+time (YYYY-MM-DDTHH:MM)")
	targetsCreateCmd.Flags().StringVar(&targetsFlagSourceType, "source-type", "manual", "source type")
	targetsCreateCmd.Flags().StringVar(&targetsFlagSourceID, "source-id", "", "source entity ID")
	targetsCreateCmd.Flags().StringVar(&targetsFlagTags, "tags", "", "comma-separated tags")

	// extract flags
	targetsExtractCmd.Flags().StringVar(&targetsFlagExtractText, "text", "", "raw text to extract targets from")
	targetsExtractCmd.Flags().StringVar(&targetsFlagExtractSourceRef, "source-ref", "", "source reference (e.g. slack:C123:ts, inbox:42)")
	targetsExtractCmd.Flags().IntVar(&targetsFlagExtractFromInbox, "from-inbox", 0, "load raw text from inbox item with this ID")
	targetsExtractCmd.Flags().BoolVar(&targetsFlagExtractJSON, "json", false, "output extracted targets as JSON (non-interactive; caller is responsible for persistence)")

	// suggest-links flags
	targetsSuggestLinksCmd.Flags().BoolVar(&targetsFlagSuggestLinksJSON, "json", false, "output suggested links as JSON (non-interactive; caller is responsible for persistence)")

	// delete flags
	targetsDeleteCmd.Flags().BoolVar(&targetsFlagDeleteJSON, "json", false, "output result as JSON")

	// link flags
	targetsLinkCmd.Flags().IntVar(&targetsFlagLinkParent, "parent", 0, "set parent target ID")
	targetsLinkCmd.Flags().IntVar(&targetsFlagLinkTo, "to", 0, "target ID to link to")
	targetsLinkCmd.Flags().StringVar(&targetsFlagLinkRelation, "relation", "", "relation type (contributes_to, blocks, related, duplicates)")
	targetsLinkCmd.Flags().StringVar(&targetsFlagLinkExternal, "external", "", "external ref (e.g. jira:PROJ-123)")

	// update flags
	targetsUpdateCmd.Flags().StringVar(&targetsFlagText, "text", "", "new target text")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagIntent, "intent", "", "new intent")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagPriority, "priority", "", "new priority")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagStatus, "status", "", "new status")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagOwnership, "ownership", "", "new ownership")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagBallOn, "ball-on", "", "who has the ball")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagDue, "due", "", "new due date+time (YYYY-MM-DDTHH:MM)")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagBlocking, "blocking", "", "what this target blocks")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagTags, "tags", "", "comma-separated tags")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagLevel, "level", "", "new level (quarter, month, week, day, custom)")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagPeriodStart, "period-start", "", "new period start date (YYYY-MM-DD)")
	targetsUpdateCmd.Flags().StringVar(&targetsFlagPeriodEnd, "period-end", "", "new period end date (YYYY-MM-DD)")
	targetsUpdateCmd.Flags().IntVar(&targetsFlagParent, "parent", 0, "new parent target ID (0 = clear)")

	// generate flags
	targetsGenerateCmd.Flags().StringVar(&targetsFlagText, "text", "", "target description (required)")
	targetsGenerateCmd.Flags().StringVar(&targetsFlagSourceType, "source-type", "", "source type for context (track, digest)")
	targetsGenerateCmd.Flags().StringVar(&targetsFlagSourceID, "source-id", "", "source entity ID for context")

	// ai-update flags
	targetsAIUpdateCmd.Flags().StringVar(&targetsFlagInstruction, "instruction", "", "what to change (required)")
	_ = targetsAIUpdateCmd.MarkFlagRequired("instruction")

	// promote-subitem flags — every flag is optional; presence (Changed) toggles the override.
	// Bound to dedicated targetsFlagPromote* vars so they never collide with
	// create/update defaults (Conventional Commits scope: promote-subitem).
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromoteText, "text", "", "override the child target text (default: sub-item text)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromoteIntent, "intent", "", "override the child intent (default: parent intent)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromoteLevel, "level", "", "override the child level (quarter, month, week, day, custom)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromotePriority, "priority", "", "override the child priority (high, medium, low)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromoteOwnership, "ownership", "", "override the child ownership (mine, delegated, watching)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromoteDue, "due", "", "override the child due date (default: sub-item.due_date if set, else parent.due_date; YYYY-MM-DDTHH:MM)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromotePeriodStart, "period-start", "", "override the child period start (YYYY-MM-DD)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromotePeriodEnd, "period-end", "", "override the child period end (YYYY-MM-DD)")
	targetsPromoteSubItemCmd.Flags().StringVar(&targetsFlagPromoteTags, "tags", "", "override comma-separated tags (default: parent tags; pass empty string to clear)")
	targetsPromoteSubItemCmd.Flags().BoolVar(&targetsFlagPromoteJSON, "json", false, "output the new child target as JSON")
}

func runTargetsList(cmd *cobra.Command, _ []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	sourceFilter := targetsFlagSource
	if sourceFilter == "all" {
		sourceFilter = ""
	}

	// Period filtering not yet wired to TargetFilter (V2); --period flag removed.
	f := db.TargetFilter{
		Status:      targetsFlagStatus,
		Priority:    targetsFlagPriority,
		Ownership:   targetsFlagOwnership,
		Level:       targetsFlagLevel,
		SourceType:  sourceFilter,
		IncludeDone: targetsFlagAll,
	}

	items, err := database.GetTargets(f)
	if err != nil {
		return fmt.Errorf("querying targets: %w", err)
	}

	if targetsFlagJSON {
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	active, overdue, err := database.GetTargetCounts()
	if err != nil {
		return fmt.Errorf("getting target counts: %w", err)
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No targets found.")
		return nil
	}

	header := fmt.Sprintf("Targets (%d active", active)
	if overdue > 0 {
		header += fmt.Sprintf(", %d overdue", overdue)
	}
	header += ")\n"
	fmt.Fprintln(out, header)

	for _, item := range items {
		pLabel := strings.ToUpper(item.Priority)
		switch item.Priority {
		case "high":
			pLabel = "HIGH"
		case "medium":
			pLabel = "MED "
		case "low":
			pLabel = "LOW "
		}

		line := fmt.Sprintf(" %s  [#%d] %s", pLabel, item.ID, item.Text)

		if item.SourceType == "jira" && item.SourceID != "" {
			issue, err := database.GetJiraIssueByKey(item.SourceID)
			if err == nil && issue != nil {
				line += "  " + jira.FormatJiraBadge(*issue)
			} else {
				line += fmt.Sprintf("  [%s]", item.SourceID)
			}
		}

		if item.Level != "" && item.Level != "day" {
			line += fmt.Sprintf("  (%s)", item.Level)
		}

		if item.DueDate != "" {
			line += fmt.Sprintf("    due: %s", item.DueDate)
		}

		if item.Status != "todo" {
			line += fmt.Sprintf("  [%s]", item.Status)
		}

		fmt.Fprintln(out, line)
	}

	return nil
}

func runTargetsShow(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	target, err := database.GetTargetByID(id)
	if err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Target #%d: %s\n", target.ID, target.Text)
	fmt.Fprintf(out, "Status: %s | Priority: %s | Level: %s\n", target.Status, target.Priority, target.Level)

	if target.Intent != "" {
		fmt.Fprintf(out, "Intent: %s\n", target.Intent)
	}
	if target.BallOn != "" {
		fmt.Fprintf(out, "Ball on: %s\n", target.BallOn)
	}
	if target.DueDate != "" {
		fmt.Fprintf(out, "Due: %s\n", target.DueDate)
	}
	if target.SnoozeUntil != "" {
		fmt.Fprintf(out, "Snoozed until: %s\n", target.SnoozeUntil)
	}
	if target.Blocking != "" {
		fmt.Fprintf(out, "Blocking: %s\n", target.Blocking)
	}
	if target.PeriodStart != "" {
		fmt.Fprintf(out, "Period: %s - %s\n", target.PeriodStart, target.PeriodEnd)
	}
	if target.ParentID.Valid {
		fmt.Fprintf(out, "Parent: #%d\n", target.ParentID.Int64)
	}

	var tags []string
	if json.Unmarshal([]byte(target.Tags), &tags) == nil && len(tags) > 0 {
		fmt.Fprintf(out, "Tags: %s\n", strings.Join(tags, ", "))
	}

	if target.SubItems != "" && target.SubItems != "[]" {
		type subItem struct {
			Text    string `json:"text"`
			Done    bool   `json:"done"`
			DueDate string `json:"due_date,omitempty"`
		}
		var subs []subItem
		if json.Unmarshal([]byte(target.SubItems), &subs) == nil && len(subs) > 0 {
			fmt.Fprintf(out, "\nSub-items:\n")
			for _, s := range subs {
				check := "[ ]"
				if s.Done {
					check = "[x]"
				}
				line := fmt.Sprintf("  %s %s", check, s.Text)
				if s.DueDate != "" {
					line += fmt.Sprintf(" (due %s)", s.DueDate)
				}
				fmt.Fprintln(out, line)
			}
		}
	}

	if target.Notes != "" && target.Notes != "[]" {
		var notes []db.TargetNote
		if json.Unmarshal([]byte(target.Notes), &notes) == nil && len(notes) > 0 {
			fmt.Fprintf(out, "\nNotes:\n")
			for _, n := range notes {
				ts := n.CreatedAt
				if len(ts) > 16 {
					ts = ts[:16]
				}
				fmt.Fprintf(out, "  [%s] %s\n", ts, n.Text)
			}
		}
	}

	links, err := database.GetLinksForTarget(int64(target.ID), "both")
	if err == nil && len(links) > 0 {
		fmt.Fprintf(out, "\nLinks:\n")
		for _, l := range links {
			if l.TargetTargetID.Valid {
				if l.TargetTargetID.Int64 == int64(target.ID) {
					fmt.Fprintf(out, "  [link #%d] <- target #%d (%s)\n", l.ID, l.SourceTargetID, l.Relation)
				} else {
					fmt.Fprintf(out, "  [link #%d] -> target #%d (%s)\n", l.ID, l.TargetTargetID.Int64, l.Relation)
				}
			} else {
				fmt.Fprintf(out, "  [link #%d] -> %s (%s)\n", l.ID, l.ExternalRef, l.Relation)
			}
		}
	}

	fmt.Fprintf(out, "\nSource: %s", target.SourceType)
	if target.SourceID != "" {
		fmt.Fprintf(out, " #%s", target.SourceID)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Created: %s | Updated: %s\n", target.CreatedAt, target.UpdatedAt)

	return nil
}

func runTargetsCreate(cmd *cobra.Command, _ []string) error {
	if targetsFlagText == "" {
		return fmt.Errorf("--text is required")
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	today := time.Now().Format("2006-01-02")
	periodStart := targetsFlagPeriodStart
	if periodStart == "" {
		periodStart = today
	}
	periodEnd := targetsFlagPeriodEnd
	if periodEnd == "" {
		periodEnd = today
	}

	level := targetsFlagLevel
	if level == "" {
		level = "day"
	}

	target := db.Target{
		Text:        targetsFlagText,
		Intent:      targetsFlagIntent,
		Level:       level,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Status:      "todo",
		Priority:    targetsFlagPriority,
		Ownership:   targetsFlagOwnership,
		DueDate:     targetsFlagDue,
		SourceType:  targetsFlagSourceType,
		SourceID:    targetsFlagSourceID,
	}

	if targetsFlagParent > 0 {
		target.ParentID = sql.NullInt64{Int64: int64(targetsFlagParent), Valid: true}
	}

	if targetsFlagTags != "" {
		parts := strings.Split(targetsFlagTags, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		tagsJSON, _ := json.Marshal(parts)
		target.Tags = string(tagsJSON)
	}

	id, err := database.CreateTarget(target)
	if err != nil {
		return fmt.Errorf("creating target: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created target #%d\n", id)
	return nil
}

func runTargetsLink(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	// --parent: update parent_id on the target itself
	if targetsFlagLinkParent > 0 {
		target, err := database.GetTargetByID(id)
		if err != nil {
			return fmt.Errorf("target #%d not found: %w", id, err)
		}
		target.ParentID = sql.NullInt64{Int64: int64(targetsFlagLinkParent), Valid: true}
		if err := database.UpdateTarget(*target); err != nil {
			return fmt.Errorf("updating parent: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Target #%d parent set to #%d\n", id, targetsFlagLinkParent)
		return nil
	}

	// --to or --external: create a target_link record
	if targetsFlagLinkTo == 0 && targetsFlagLinkExternal == "" {
		return fmt.Errorf("one of --parent, --to, or --external is required")
	}
	if targetsFlagLinkRelation == "" {
		return fmt.Errorf("--relation is required (contributes_to, blocks, related, duplicates)")
	}

	link := db.TargetLink{
		SourceTargetID: id,
		Relation:       targetsFlagLinkRelation,
		CreatedBy:      "user",
	}

	if targetsFlagLinkTo > 0 {
		link.TargetTargetID = sql.NullInt64{Int64: int64(targetsFlagLinkTo), Valid: true}
	}
	if targetsFlagLinkExternal != "" {
		if !targets.IsValidExternalRef(targetsFlagLinkExternal) {
			return fmt.Errorf("invalid --external ref %q: must start with 'jira:' or 'slack:'", targetsFlagLinkExternal)
		}
		link.ExternalRef = targetsFlagLinkExternal
	}

	linkID, err := database.CreateTargetLink(link)
	if err != nil {
		return fmt.Errorf("creating link: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Link #%d created\n", linkID)
	return nil
}

func runTargetsUnlink(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid link ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.DeleteTargetLink(id); err != nil {
		return fmt.Errorf("deleting link #%d: %w", id, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Link #%d removed\n", id)
	return nil
}

func runTargetsDone(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.UpdateTargetStatus(id, "done"); err != nil {
		return fmt.Errorf("marking target done: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Target #%d marked as done\n", id)
	return nil
}

func runTargetsDismiss(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.UpdateTargetStatus(id, "dismissed"); err != nil {
		return fmt.Errorf("dismissing target: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Target #%d dismissed\n", id)
	return nil
}

func runTargetsDelete(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if _, err := database.GetTargetByID(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("target #%d not found", id)
		}
		return fmt.Errorf("looking up target #%d: %w", id, err)
	}

	if err := database.DeleteTarget(id); err != nil {
		return fmt.Errorf("deleting target #%d: %w", id, err)
	}

	if targetsFlagDeleteJSON {
		payload := map[string]any{"id": id, "removed": true}
		enc := json.NewEncoder(cmd.OutOrStdout())
		if err := enc.Encode(payload); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Target #%d removed\n", id)
	return nil
}

func runTargetsSnooze(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}
	snoozeDate := args[1]

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	target, err := database.GetTargetByID(id)
	if err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	target.Status = "snoozed"
	target.SnoozeUntil = snoozeDate
	if err := database.UpdateTarget(*target); err != nil {
		return fmt.Errorf("snoozing target: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Target #%d snoozed until %s\n", id, snoozeDate)
	return nil
}

func runTargetsUpdate(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	target, err := database.GetTargetByID(id)
	if err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	if cmd.Flags().Changed("text") {
		target.Text = targetsFlagText
	}
	if cmd.Flags().Changed("intent") {
		target.Intent = targetsFlagIntent
	}
	if cmd.Flags().Changed("priority") {
		target.Priority = targetsFlagPriority
	}
	if cmd.Flags().Changed("status") {
		target.Status = targetsFlagStatus
	}
	if cmd.Flags().Changed("ownership") {
		target.Ownership = targetsFlagOwnership
	}
	if cmd.Flags().Changed("ball-on") {
		target.BallOn = targetsFlagBallOn
	}
	if cmd.Flags().Changed("due") {
		target.DueDate = targetsFlagDue
	}
	if cmd.Flags().Changed("blocking") {
		target.Blocking = targetsFlagBlocking
	}
	if cmd.Flags().Changed("level") {
		target.Level = targetsFlagLevel
	}
	if cmd.Flags().Changed("period-start") {
		target.PeriodStart = targetsFlagPeriodStart
	}
	if cmd.Flags().Changed("period-end") {
		target.PeriodEnd = targetsFlagPeriodEnd
	}
	if cmd.Flags().Changed("parent") {
		if targetsFlagParent == 0 {
			target.ParentID = sql.NullInt64{}
		} else {
			target.ParentID = sql.NullInt64{Int64: int64(targetsFlagParent), Valid: true}
		}
	}
	if cmd.Flags().Changed("tags") {
		parts := strings.Split(targetsFlagTags, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		tagsJSON, _ := json.Marshal(parts)
		target.Tags = string(tagsJSON)
	}

	if err := database.UpdateTarget(*target); err != nil {
		return fmt.Errorf("updating target: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Target #%d updated\n", id)
	return nil
}

func runTargetsNoteAdd(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	text := args[1]
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("note text cannot be empty")
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	target, err := database.GetTargetByID(id)
	if err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	var notes []db.TargetNote
	if target.Notes != "" && target.Notes != "[]" {
		_ = json.Unmarshal([]byte(target.Notes), &notes)
	}
	notes = append(notes, db.TargetNote{
		Text:      text,
		CreatedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	notesJSON, _ := json.Marshal(notes)
	target.Notes = string(notesJSON)

	if err := database.UpdateTarget(*target); err != nil {
		return fmt.Errorf("adding note: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Note added to target #%d\n", id)
	return nil
}

func runTargetsNoteList(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	target, err := database.GetTargetByID(id)
	if err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	if target.Notes == "" || target.Notes == "[]" {
		fmt.Fprintf(cmd.OutOrStdout(), "No notes for target #%d\n", id)
		return nil
	}
	var notes []db.TargetNote
	if err := json.Unmarshal([]byte(target.Notes), &notes); err != nil {
		return fmt.Errorf("parsing notes: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Notes for target #%d (%d):\n\n", id, len(notes))
	for _, n := range notes {
		ts := n.CreatedAt
		if len(ts) > 16 {
			ts = ts[:16]
		}
		fmt.Fprintf(out, "  [%s] %s\n", ts, n.Text)
	}
	return nil
}
