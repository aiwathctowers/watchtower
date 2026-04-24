package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"
	"watchtower/internal/prompts"
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
		targetsSnoozeCmd,
		targetsUpdateCmd,
		targetsGenerateCmd,
		targetsNoteCmd,
		targetsAIUpdateCmd,
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

func runTargetsExtract(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	rawText := targetsFlagExtractText
	sourceRef := targetsFlagExtractSourceRef

	if targetsFlagExtractFromInbox > 0 {
		item, err := database.GetInboxItemByID(targetsFlagExtractFromInbox)
		if err != nil {
			return fmt.Errorf("inbox item #%d not found: %w", targetsFlagExtractFromInbox, err)
		}
		rawText = item.RawText
		if rawText == "" {
			rawText = item.Snippet
		}
		if sourceRef == "" {
			sourceRef = fmt.Sprintf("inbox:%d", targetsFlagExtractFromInbox)
		}
	}

	if rawText == "" {
		return fmt.Errorf("--text or --from-inbox is required")
	}

	applyProviderOverride(cfg)
	gen := cliGenerator(cfg)
	// Construct resolver with local DB lookups; MCP client stays nil in V1.
	// URL enrichment (Slack/Jira permalinks) runs through the extract pipeline.
	// Timeout governed by config targets.extract.timeout_seconds (no outer wrap needed).
	resolver := targets.NewResolver(database, nil,
		time.Duration(cfg.Targets.Resolver.MCPTimeoutSeconds)*time.Second)
	pipe := targets.New(database, &cfg.Targets, gen, resolver, nil)

	// extract respects config targets.extract.timeout_seconds internally.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := pipe.Extract(ctx, targets.ExtractRequest{
		RawText:    rawText,
		EntryPoint: "cli",
		SourceRef:  sourceRef,
	})
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	out := cmd.OutOrStdout()

	if targetsFlagExtractJSON {
		jsonOut := struct {
			Extracted    []jsonProposedTarget `json:"extracted"`
			OmittedCount int                  `json:"omitted_count"`
			Notes        string               `json:"notes"`
		}{
			Extracted:    toJSONProposedTargets(result.Extracted),
			OmittedCount: result.OmittedCount,
			Notes:        result.Notes,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonOut)
	}

	if len(result.Extracted) == 0 {
		fmt.Fprintln(out, "No targets extracted.")
		return nil
	}

	if result.OmittedCount > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: AI omitted %d additional items (cap reached).\n", result.OmittedCount)
	}
	if result.Notes != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "AI notes: %s\n", result.Notes)
	}

	reader := bufio.NewReader(os.Stdin)
	var confirmed []targets.ProposedTarget

	fmt.Fprintf(out, "\nExtracted %d target(s):\n\n", len(result.Extracted))

	for i, pt := range result.Extracted {
		fmt.Fprintf(out, "[%d/%d] %s\n", i+1, len(result.Extracted), pt.Text)
		fmt.Fprintf(out, "      Level: %s | Priority: %s | Period: %s - %s\n",
			pt.Level, pt.Priority, pt.PeriodStart, pt.PeriodEnd)
		if pt.Intent != "" {
			fmt.Fprintf(out, "      Intent: %s\n", pt.Intent)
		}
		if pt.ParentID.Valid {
			fmt.Fprintf(out, "      Parent: #%d\n", pt.ParentID.Int64)
		}
		fmt.Fprintf(out, "      Create? [y/N]: ")

		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "y" || line == "yes" {
			confirmed = append(confirmed, pt)
		}
		fmt.Fprintln(out)
	}

	if len(confirmed) == 0 {
		fmt.Fprintln(out, "No targets created.")
		return nil
	}

	ids, err := pipe.CreateFromExtraction(ctx, confirmed, "extract", sourceRef)
	if err != nil {
		return fmt.Errorf("creating targets: %w", err)
	}

	fmt.Fprintf(out, "Created %d target(s):", len(ids))
	for _, id := range ids {
		fmt.Fprintf(out, " #%d", id)
	}
	fmt.Fprintln(out)
	return nil
}

// jsonProposedTarget is the JSON wire format for a proposed target (Swift Decodable).
// It is a cmd-layer adapter — do NOT merge into internal/targets.
type jsonProposedTarget struct {
	Text              string             `json:"text"`
	Intent            string             `json:"intent"`
	Level             string             `json:"level"`
	CustomLabel       string             `json:"custom_label"`
	PeriodStart       string             `json:"period_start"`
	PeriodEnd         string             `json:"period_end"`
	Priority          string             `json:"priority"`
	DueDate           string             `json:"due_date"`
	ParentID          *int64             `json:"parent_id"`
	AILevelConfidence *float64           `json:"ai_level_confidence"`
	SecondaryLinks    []jsonProposedLink `json:"secondary_links"`
}

type jsonProposedLink struct {
	TargetID    *int64   `json:"target_id"`
	ExternalRef string   `json:"external_ref"`
	Relation    string   `json:"relation"`
	Confidence  *float64 `json:"confidence"`
}

func toJSONProposedTargets(items []targets.ProposedTarget) []jsonProposedTarget {
	out := make([]jsonProposedTarget, 0, len(items))
	for _, pt := range items {
		j := jsonProposedTarget{
			Text:        pt.Text,
			Intent:      pt.Intent,
			Level:       pt.Level,
			CustomLabel: pt.CustomLabel,
			PeriodStart: pt.PeriodStart,
			PeriodEnd:   pt.PeriodEnd,
			Priority:    pt.Priority,
			DueDate:     pt.DueDate,
		}
		if pt.ParentID.Valid {
			pid := pt.ParentID.Int64
			j.ParentID = &pid
		}
		if pt.AILevelConfidence.Valid {
			c := pt.AILevelConfidence.Float64
			j.AILevelConfidence = &c
		}
		for _, l := range pt.SecondaryLinks {
			jl := jsonProposedLink{
				ExternalRef: l.ExternalRef,
				Relation:    l.Relation,
			}
			if l.TargetID.Valid {
				tid := l.TargetID.Int64
				jl.TargetID = &tid
			}
			if l.Confidence.Valid {
				c := l.Confidence.Float64
				jl.Confidence = &c
			}
			j.SecondaryLinks = append(j.SecondaryLinks, jl)
		}
		out = append(out, j)
	}
	return out
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

func runTargetsSuggestLinks(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	applyProviderOverride(cfg)
	gen := cliGenerator(cfg)
	pipe := targets.New(database, &cfg.Targets, gen, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := pipe.LinkExisting(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("suggest-links failed: %w", err)
	}

	if targetsFlagSuggestLinksJSON {
		jsonOut := struct {
			ParentID       *int64             `json:"parent_id"`
			SecondaryLinks []jsonProposedLink `json:"secondary_links"`
		}{
			SecondaryLinks: make([]jsonProposedLink, 0, len(result.SecondaryLinks)),
		}
		if result.ParentID.Valid {
			pid := result.ParentID.Int64
			jsonOut.ParentID = &pid
		}
		for _, l := range result.SecondaryLinks {
			jl := jsonProposedLink{
				ExternalRef: l.ExternalRef,
				Relation:    l.Relation,
			}
			if l.TargetID.Valid {
				tid := l.TargetID.Int64
				jl.TargetID = &tid
			}
			if l.Confidence.Valid {
				c := l.Confidence.Float64
				jl.Confidence = &c
			}
			jsonOut.SecondaryLinks = append(jsonOut.SecondaryLinks, jl)
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(jsonOut)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Suggested links for target #%d:\n\n", id)

	if result.ParentID.Valid {
		fmt.Fprintf(out, "  Parent: #%d\n", result.ParentID.Int64)
	}

	for _, l := range result.SecondaryLinks {
		if l.TargetID.Valid {
			fmt.Fprintf(out, "  -> target #%d (%s)\n", l.TargetID.Int64, l.Relation)
		} else {
			fmt.Fprintf(out, "  -> %s (%s)\n", l.ExternalRef, l.Relation)
		}
	}

	if !result.ParentID.Valid && len(result.SecondaryLinks) == 0 {
		fmt.Fprintln(out, "  No links suggested.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(out, "\nApply these links? [y/N]: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line != "y" && line != "yes" {
		fmt.Fprintln(out, "Aborted.")
		return nil
	}

	if result.ParentID.Valid {
		target, err := database.GetTargetByID(id)
		if err != nil {
			return fmt.Errorf("loading target: %w", err)
		}
		target.ParentID = sql.NullInt64{Int64: result.ParentID.Int64, Valid: true}
		if err := database.UpdateTarget(*target); err != nil {
			return fmt.Errorf("setting parent: %w", err)
		}
		fmt.Fprintf(out, "Parent set to #%d\n", result.ParentID.Int64)
	}

	for _, l := range result.SecondaryLinks {
		link := db.TargetLink{
			SourceTargetID: id,
			Relation:       l.Relation,
			CreatedBy:      "ai",
		}
		if l.TargetID.Valid {
			link.TargetTargetID = sql.NullInt64{Int64: l.TargetID.Int64, Valid: true}
		}
		if l.ExternalRef != "" {
			link.ExternalRef = l.ExternalRef
		}
		if l.Confidence.Valid {
			link.Confidence = sql.NullFloat64{Float64: l.Confidence.Float64, Valid: true}
		}
		linkID, err := database.CreateTargetLink(link)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not create link: %v\n", err)
			continue
		}
		fmt.Fprintf(out, "Link #%d created\n", linkID)
	}

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

func runTargetsGenerate(cmd *cobra.Command, _ []string) error {
	if targetsFlagText == "" {
		return fmt.Errorf("--text is required")
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	var sourceContext string
	if targetsFlagSourceType != "" && targetsFlagSourceID != "" {
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}
		sourceContext = loadSourceContext(database, targetsFlagSourceType, targetsFlagSourceID)
		database.Close()
	}

	now := time.Now().Format("2006-01-02T15:04 (Monday)")
	promptTmpl := prompts.Defaults[prompts.TasksGenerate]
	if promptDB, dbErr := db.Open(cfg.DBPath()); dbErr == nil {
		store := prompts.New(promptDB, nil)
		if tmpl, _, err := store.Get(prompts.TasksGenerate); err == nil && tmpl != "" {
			promptTmpl = tmpl
		}
		promptDB.Close()
	}
	systemPrompt := fmt.Sprintf(promptTmpl, now)

	userMessage := targetsFlagText
	if sourceContext != "" {
		userMessage += "\n\n=== SOURCE CONTEXT ===\n" + sourceContext
	}

	applyProviderOverride(cfg)
	gen := cliGenerator(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, usage, _, err := gen.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}

	database, err := db.Open(cfg.DBPath())
	if err == nil {
		model := cfg.AI.Provider + "/sonnet"
		runID, runErr := database.CreatePipelineRun("targets", "cli", model)
		if runErr == nil {
			inputTokens, outputTokens, totalAPI := 0, 0, 0
			if usage != nil {
				inputTokens = usage.InputTokens
				outputTokens = usage.OutputTokens
				totalAPI = usage.TotalAPITokens
			}
			_ = database.CompletePipelineRun(runID, 1, inputTokens, outputTokens, 0, totalAPI, nil, nil, "")
		}
		database.Close()
	}

	jsonStr := extractJSON(result)
	fmt.Fprintln(cmd.OutOrStdout(), jsonStr)
	return nil
}

// loadSourceContext retrieves context from the source entity for AI enrichment.
func loadSourceContext(database *db.DB, sourceType, sourceID string) string {
	switch sourceType {
	case "track":
		id, err := strconv.Atoi(sourceID)
		if err != nil {
			return ""
		}
		track, err := database.GetTrackByID(id)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("Track #%d: %s\n%s", track.ID, track.Text, track.Context)
	case "digest":
		id, err := strconv.Atoi(sourceID)
		if err != nil {
			return ""
		}
		d, err := database.GetDigestByID(id)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("Digest #%d: %s", d.ID, d.Summary)
	default:
		return ""
	}
}

// extractJSON finds and returns the first JSON object in the string,
// handling cases where AI wraps JSON in markdown code blocks.
func extractJSON(s string) string {
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "{"); idx >= 0 {
		if end := strings.LastIndex(s, "}"); end > idx {
			return s[idx : end+1]
		}
	}
	return s
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

func runTargetsAIUpdate(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	if targetsFlagInstruction == "" {
		return fmt.Errorf("--instruction is required")
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	target, err := database.GetTargetByID(id)
	if err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	targetContext := fmt.Sprintf("Title: %s\nIntent: %s\nPriority: %s\nDue: %s\nStatus: %s\nSub-items: %s\nNotes: %s",
		target.Text, target.Intent, target.Priority, target.DueDate, target.Status, target.SubItems, target.Notes)

	now := time.Now().Format("2006-01-02T15:04 (Monday)")
	store := prompts.New(database, nil)
	promptTmpl, _, _ := store.Get(prompts.TasksUpdate)
	if promptTmpl == "" {
		promptTmpl = prompts.Defaults[prompts.TasksUpdate]
	}
	systemPrompt := fmt.Sprintf(promptTmpl, now, targetContext)

	applyProviderOverride(cfg)
	gen := cliGenerator(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, usage, _, err := gen.Generate(ctx, systemPrompt, targetsFlagInstruction, "")
	if err != nil {
		return fmt.Errorf("AI update failed: %w", err)
	}

	model := cfg.AI.Provider + "/sonnet"
	runID, runErr := database.CreatePipelineRun("targets", "cli", model)
	if runErr == nil {
		inputTokens, outputTokens, totalAPI := 0, 0, 0
		if usage != nil {
			inputTokens = usage.InputTokens
			outputTokens = usage.OutputTokens
			totalAPI = usage.TotalAPITokens
		}
		_ = database.CompletePipelineRun(runID, 1, inputTokens, outputTokens, 0, totalAPI, nil, nil, "")
	}

	jsonStr := extractJSON(result)
	fmt.Fprintln(cmd.OutOrStdout(), jsonStr)
	return nil
}
