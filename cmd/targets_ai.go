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
	"watchtower/internal/prompts"
	"watchtower/internal/targets"

	"github.com/spf13/cobra"
)

// This file hosts the AI-driven target subcommands and the helpers they
// share: extract / suggest-links / generate / ai-update / promote-subitem.
// Each one runs an AI pipeline over a target context (raw text, an existing
// target, or a sub-item) and either prints structured output for Swift or
// confirms with the user. Cobra command vars and init() wiring stay in
// cmd/targets.go alongside the lifecycle commands.

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
	Text              string                `json:"text"`
	Intent            string                `json:"intent"`
	Level             string                `json:"level"`
	CustomLabel       string                `json:"custom_label"`
	PeriodStart       string                `json:"period_start"`
	PeriodEnd         string                `json:"period_end"`
	Priority          string                `json:"priority"`
	DueDate           string                `json:"due_date"`
	ParentID          *int64                `json:"parent_id"`
	AILevelConfidence *float64              `json:"ai_level_confidence"`
	SecondaryLinks    []jsonProposedLink    `json:"secondary_links"`
	SubItems          []jsonProposedSubItem `json:"sub_items"`
}

type jsonProposedLink struct {
	TargetID    *int64   `json:"target_id"`
	ExternalRef string   `json:"external_ref"`
	Relation    string   `json:"relation"`
	Confidence  *float64 `json:"confidence"`
}

type jsonProposedSubItem struct {
	Text string `json:"text"`
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
		for _, s := range pt.SubItems {
			j.SubItems = append(j.SubItems, jsonProposedSubItem{Text: s.Text})
		}
		out = append(out, j)
	}
	return out
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

func runTargetsPromoteSubItem(cmd *cobra.Command, args []string) error {
	parentID, err := strconv.Atoi(args[0])
	if err != nil || parentID <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil || idx < 0 {
		return fmt.Errorf("invalid sub-item index %q: must be a non-negative integer", args[1])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	overrides := db.PromoteOverrides{}
	if cmd.Flags().Changed("text") {
		v := targetsFlagPromoteText
		overrides.Text = &v
	}
	if cmd.Flags().Changed("intent") {
		v := targetsFlagPromoteIntent
		overrides.Intent = &v
	}
	if cmd.Flags().Changed("level") {
		v := targetsFlagPromoteLevel
		overrides.Level = &v
	}
	if cmd.Flags().Changed("priority") {
		v := targetsFlagPromotePriority
		overrides.Priority = &v
	}
	if cmd.Flags().Changed("ownership") {
		v := targetsFlagPromoteOwnership
		overrides.Ownership = &v
	}
	if cmd.Flags().Changed("due") {
		v := targetsFlagPromoteDue
		overrides.DueDate = &v
	}
	if cmd.Flags().Changed("period-start") {
		v := targetsFlagPromotePeriodStart
		overrides.PeriodStart = &v
	}
	if cmd.Flags().Changed("period-end") {
		v := targetsFlagPromotePeriodEnd
		overrides.PeriodEnd = &v
	}
	if cmd.Flags().Changed("tags") {
		// Comma-separated input → JSON array. Empty input → empty array (clears tags).
		parts := []string{}
		if trimmed := strings.TrimSpace(targetsFlagPromoteTags); trimmed != "" {
			for _, p := range strings.Split(targetsFlagPromoteTags, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					parts = append(parts, p)
				}
			}
		}
		buf, err := json.Marshal(parts)
		if err != nil {
			return fmt.Errorf("encoding tags: %w", err)
		}
		s := string(buf)
		overrides.Tags = &s
	}

	childID, err := database.PromoteSubItemToChild(int64(parentID), idx, overrides)
	if err != nil {
		return fmt.Errorf("promoting sub-item: %w", err)
	}

	if targetsFlagPromoteJSON {
		child, err := database.GetTargetByID(int(childID))
		if err != nil {
			return fmt.Errorf("loading new child target: %w", err)
		}
		payload := map[string]any{
			"id":           child.ID,
			"text":         child.Text,
			"level":        child.Level,
			"priority":     child.Priority,
			"status":       child.Status,
			"due_date":     child.DueDate,
			"period_start": child.PeriodStart,
			"period_end":   child.PeriodEnd,
			"parent_id":    parentID,
			"source_type":  child.SourceType,
			"source_id":    child.SourceID,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Promoted sub-item #%d of target #%d to new child target #%d\n",
		idx, parentID, childID)
	return nil
}
