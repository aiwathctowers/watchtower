package cmd

import (
	"context"
	"fmt"
	"strconv"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/prompts"

	"github.com/spf13/cobra"
)

var promptsCmd = &cobra.Command{
	Use:   "prompts",
	Short: "Manage AI prompt templates",
	Long:  "View, edit, and manage prompt templates used by the AI pipelines. Supports versioning, history, rollback, and auto-tuning.",
}

var promptsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all prompt templates",
	RunE:  runPromptsList,
}

var promptsShowCmd = &cobra.Command{
	Use:   "show <prompt-id>",
	Short: "Show a prompt template",
	Args:  cobra.ExactArgs(1),
	RunE:  runPromptsShow,
}

var promptsHistoryCmd = &cobra.Command{
	Use:   "history <prompt-id>",
	Short: "Show version history for a prompt",
	Args:  cobra.ExactArgs(1),
	RunE:  runPromptsHistory,
}

var promptsResetCmd = &cobra.Command{
	Use:   "reset <prompt-id>",
	Short: "Reset a prompt to its built-in default",
	Args:  cobra.ExactArgs(1),
	RunE:  runPromptsReset,
}

var promptsRollbackCmd = &cobra.Command{
	Use:   "rollback <prompt-id> <version>",
	Short: "Rollback a prompt to a specific version",
	Args:  cobra.ExactArgs(2),
	RunE:  runPromptsRollback,
}

var tuneCmd = &cobra.Command{
	Use:   "tune [prompt-id]",
	Short: "Auto-suggest prompt improvements based on feedback",
	Long: `Analyzes user feedback and suggests improvements to prompt templates.
Without arguments, suggests improvements for all prompts with sufficient feedback.
With a prompt-id, suggests improvements for that specific prompt.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTune,
}

var tuneFlagApply bool
var tuneFlagInstructions string

func init() {
	rootCmd.AddCommand(promptsCmd)
	promptsCmd.AddCommand(promptsListCmd)
	promptsCmd.AddCommand(promptsShowCmd)
	promptsCmd.AddCommand(promptsHistoryCmd)
	promptsCmd.AddCommand(promptsResetCmd)
	promptsCmd.AddCommand(promptsRollbackCmd)

	rootCmd.AddCommand(tuneCmd)
	tuneCmd.Flags().BoolVar(&tuneFlagApply, "apply", false, "apply the suggested changes (default: dry-run showing diff)")
	tuneCmd.Flags().StringVar(&tuneFlagInstructions, "instructions", "", "manual instructions describing what to change in the prompt")
}

func runPromptsList(cmd *cobra.Command, args []string) error {
	_, store, closer, err := openPromptStore()
	if err != nil {
		return err
	}
	defer closer()

	all, err := store.GetAll()
	if err != nil {
		return fmt.Errorf("listing prompts: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Prompt Templates:")
	fmt.Fprintln(out, "")
	for _, p := range all {
		desc := prompts.Descriptions[p.ID]
		versionStr := "default"
		if p.Version > 0 {
			versionStr = fmt.Sprintf("v%d", p.Version)
		}
		lang := "auto"
		if p.Language != "" {
			lang = p.Language
		}
		fmt.Fprintf(out, "  %-25s  %s  lang=%s  %s\n", p.ID, versionStr, lang, desc)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Use 'watchtower prompts show <id>' to view a prompt.")
	return nil
}

func runPromptsShow(cmd *cobra.Command, args []string) error {
	_, store, closer, err := openPromptStore()
	if err != nil {
		return err
	}
	defer closer()

	id := args[0]
	tmpl, version, err := store.Get(id)
	if err != nil {
		return fmt.Errorf("loading prompt: %w", err)
	}

	out := cmd.OutOrStdout()
	desc := prompts.Descriptions[id]
	versionStr := "default (built-in)"
	if version > 0 {
		versionStr = fmt.Sprintf("v%d (customized)", version)
	}
	fmt.Fprintf(out, "Prompt: %s\n", id)
	fmt.Fprintf(out, "Description: %s\n", desc)
	fmt.Fprintf(out, "Version: %s\n", versionStr)
	fmt.Fprintf(out, "---\n%s\n---\n", tmpl)
	return nil
}

func runPromptsHistory(cmd *cobra.Command, args []string) error {
	_, store, closer, err := openPromptStore()
	if err != nil {
		return err
	}
	defer closer()

	history, err := store.History(args[0])
	if err != nil {
		return fmt.Errorf("loading history: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(history) == 0 {
		fmt.Fprintf(out, "No history for prompt %q. It's using the built-in default.\n", args[0])
		return nil
	}

	fmt.Fprintf(out, "Version history for %s:\n\n", args[0])
	for _, h := range history {
		ts := h.CreatedAt
		if len(ts) > 19 {
			ts = ts[:19]
		}
		fmt.Fprintf(out, "  v%-4d  %s  %s\n", h.Version, ts, h.Reason)
	}
	return nil
}

func runPromptsReset(cmd *cobra.Command, args []string) error {
	_, store, closer, err := openPromptStore()
	if err != nil {
		return err
	}
	defer closer()

	if err := store.Reset(args[0]); err != nil {
		return fmt.Errorf("resetting prompt: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Prompt %q reset to built-in default.\n", args[0])
	return nil
}

func runPromptsRollback(cmd *cobra.Command, args []string) error {
	_, store, closer, err := openPromptStore()
	if err != nil {
		return err
	}
	defer closer()

	version, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", args[1], err)
	}

	if err := store.Rollback(args[0], version); err != nil {
		return fmt.Errorf("rolling back: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Prompt %q rolled back to v%d.\n", args[0], version)
	return nil
}

func runTune(cmd *cobra.Command, args []string) error {
	cfg, store, closer, err := openPromptStore()
	if err != nil {
		return err
	}
	defer closer()

	// H3 fix: reuse the database from the store instead of opening a second connection.
	database := store.DB()

	gen := cliGenerator(cfg)

	// Track accumulated usage across all tune calls.
	var totalInTok, totalOutTok, totalAPITok int
	var totalCost float64

	// Wrap digest.Generator as prompts.TextGenerator, capturing usage.
	tuneGen := prompts.GenerateFunc(func(ctx context.Context, systemPrompt, userMessage string) (string, error) {
		raw, usage, _, err := gen.Generate(ctx, systemPrompt, userMessage, "")
		if usage != nil {
			totalInTok += usage.InputTokens
			totalOutTok += usage.OutputTokens
			totalCost += usage.CostUSD
			totalAPITok += usage.TotalAPITokens
		}
		return raw, err
	})

	_ = store.Seed()
	tuner := prompts.NewTuner(store, database, tuneGen)

	out := cmd.OutOrStdout()

	model := cfg.Digest.Model
	runID, _ := database.CreatePipelineRun("tune", "cli", model)
	var tuneErr error
	itemsFound := 0

	defer func() {
		errMsg := ""
		if tuneErr != nil {
			errMsg = tuneErr.Error()
		}
		if runID > 0 {
			_ = database.CompletePipelineRun(runID, itemsFound, totalInTok, totalOutTok, totalCost, totalAPITok, nil, nil, errMsg)
		}
	}()

	// Determine which prompts to tune
	var targetIDs []string
	if len(args) > 0 {
		targetIDs = []string{args[0]}
	} else {
		// Tune the 3 main prompts
		targetIDs = []string{prompts.DigestChannel, prompts.TracksExtract, prompts.PeopleReduce}
	}

	for _, id := range targetIDs {
		var result *prompts.TuneResult

		if tuneFlagInstructions != "" {
			fmt.Fprintf(out, "Manual tuning %s with custom instructions...\n", id)
			result, err = tuner.SuggestManual(cmd.Context(), id, tuneFlagInstructions)
		} else {
			fmt.Fprintf(out, "Analyzing feedback for %s...\n", id)
			result, err = tuner.Suggest(cmd.Context(), id)
		}
		if err != nil {
			fmt.Fprintf(out, "  Skipped: %v\n\n", err)
			continue
		}

		itemsFound++

		fmt.Fprintf(out, "\nSuggested changes for %s (v%d → v%d):\n", id, result.CurrentVersion, result.CurrentVersion+1)
		fmt.Fprintf(out, "  Explanation: %s\n", result.Explanation)
		for _, change := range result.Changes {
			fmt.Fprintf(out, "  - %s\n", change)
		}
		fmt.Fprintln(out, "")

		if tuneFlagApply {
			if err := tuner.Apply(result); err != nil {
				tuneErr = fmt.Errorf("applying tune for %s: %w", id, err)
				return tuneErr
			}
			fmt.Fprintf(out, "  Applied! New version saved.\n\n")
		} else {
			fmt.Fprintln(out, "  Run with --apply to save these changes.")
			fmt.Fprintln(out, "")
		}
	}

	// Auto-detect importance corrections
	if tuner.HasImportanceCorrections() {
		fmt.Fprintln(out, "Analyzing importance corrections...")

		result, err := tuner.SuggestImportance(cmd.Context())
		if err != nil {
			fmt.Fprintf(out, "  Skipped: %v\n\n", err)
		} else {
			itemsFound++

			fmt.Fprintf(out, "\nSuggested importance criteria changes for %s (v%d → v%d):\n",
				result.PromptID, result.CurrentVersion, result.CurrentVersion+1)
			fmt.Fprintf(out, "  Explanation: %s\n", result.Explanation)
			for _, change := range result.Changes {
				fmt.Fprintf(out, "  - %s\n", change)
			}
			fmt.Fprintln(out, "")

			if tuneFlagApply {
				if err := tuner.ApplyImportance(result); err != nil {
					tuneErr = fmt.Errorf("applying importance tune: %w", err)
					return tuneErr
				}
				fmt.Fprintf(out, "  Applied! Importance criteria updated, corrections cleared.\n\n")
			} else {
				fmt.Fprintln(out, "  Run with --apply to save these changes.")
				fmt.Fprintln(out, "")
			}
		}
	}

	return nil
}

// openPromptStore loads config, opens DB, seeds defaults, and returns the store.
func openPromptStore() (*config.Config, *prompts.Store, func(), error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid config: %w", err)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening database: %w", err)
	}

	store := prompts.New(database, nil)
	_ = store.Seed()

	return cfg, store, func() { database.Close() }, nil
}
