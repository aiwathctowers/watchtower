package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"

	"github.com/spf13/cobra"
)

var (
	tasksFlagStatus     string
	tasksFlagPriority   string
	tasksFlagOwnership  string
	tasksFlagAll        bool
	tasksFlagJSON       bool
	tasksFlagText       string
	tasksFlagIntent     string
	tasksFlagDue        string
	tasksFlagSourceType string
	tasksFlagSourceID   string
	tasksFlagTags       string
	tasksFlagBallOn     string
	tasksFlagBlocking   string
)

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Show personal action items",
	Long:  "Displays tasks — personal action items with priorities, due dates, and ownership.",
	RunE:  runTasks,
}

var tasksShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksShow,
}

var tasksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task",
	RunE:  runTasksCreate,
}

var tasksDoneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksDone,
}

var tasksDismissCmd = &cobra.Command{
	Use:   "dismiss <id>",
	Short: "Dismiss a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksDismiss,
}

var tasksSnoozeCmd = &cobra.Command{
	Use:   "snooze <id> <YYYY-MM-DDTHH:MM>",
	Short: "Snooze a task until a date+time",
	Args:  cobra.ExactArgs(2),
	RunE:  runTasksSnooze,
}

var tasksUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTasksUpdate,
}

var tasksGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate task details with AI (checklist, priority, due date)",
	Long:  "Uses AI to enrich a task description: breaks it into sub-items, suggests priority and due date. Outputs JSON to stdout.",
	RunE:  runTasksGenerate,
}

func init() {
	rootCmd.AddCommand(tasksCmd)
	tasksCmd.AddCommand(tasksShowCmd, tasksCreateCmd, tasksDoneCmd, tasksDismissCmd, tasksSnoozeCmd, tasksUpdateCmd, tasksGenerateCmd)

	tasksCmd.Flags().StringVar(&tasksFlagStatus, "status", "", "filter by status (todo, in_progress, blocked, done, dismissed, snoozed)")
	tasksCmd.Flags().StringVar(&tasksFlagPriority, "priority", "", "filter by priority (high, medium, low)")
	tasksCmd.Flags().StringVar(&tasksFlagOwnership, "ownership", "", "filter by ownership (mine, delegated, watching)")
	tasksCmd.Flags().BoolVar(&tasksFlagAll, "all", false, "include done and dismissed tasks")
	tasksCmd.Flags().BoolVar(&tasksFlagJSON, "json", false, "output as JSON")

	tasksCreateCmd.Flags().StringVar(&tasksFlagText, "text", "", "task text (required)")
	tasksCreateCmd.Flags().StringVar(&tasksFlagIntent, "intent", "", "task intent/context")
	tasksCreateCmd.Flags().StringVar(&tasksFlagPriority, "priority", "medium", "priority (high, medium, low)")
	tasksCreateCmd.Flags().StringVar(&tasksFlagOwnership, "ownership", "mine", "ownership (mine, delegated, watching)")
	tasksCreateCmd.Flags().StringVar(&tasksFlagDue, "due", "", "due date+time (YYYY-MM-DDTHH:MM)")
	tasksCreateCmd.Flags().StringVar(&tasksFlagSourceType, "source-type", "manual", "source type (track, digest, briefing, manual, chat)")
	tasksCreateCmd.Flags().StringVar(&tasksFlagSourceID, "source-id", "", "source entity ID")
	tasksCreateCmd.Flags().StringVar(&tasksFlagTags, "tags", "", "comma-separated tags")

	tasksUpdateCmd.Flags().StringVar(&tasksFlagText, "text", "", "new task text")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagIntent, "intent", "", "new intent")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagPriority, "priority", "", "new priority")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagStatus, "status", "", "new status")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagOwnership, "ownership", "", "new ownership")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagBallOn, "ball-on", "", "who has the ball")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagDue, "due", "", "new due date+time (YYYY-MM-DDTHH:MM)")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagBlocking, "blocking", "", "what this task blocks")
	tasksUpdateCmd.Flags().StringVar(&tasksFlagTags, "tags", "", "comma-separated tags")

	tasksGenerateCmd.Flags().StringVar(&tasksFlagText, "text", "", "task description (required)")
	tasksGenerateCmd.Flags().StringVar(&tasksFlagSourceType, "source-type", "", "source type for context (track, digest)")
	tasksGenerateCmd.Flags().StringVar(&tasksFlagSourceID, "source-id", "", "source entity ID for context")
}

func runTasks(cmd *cobra.Command, _ []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	f := db.TaskFilter{
		Status:      tasksFlagStatus,
		Priority:    tasksFlagPriority,
		Ownership:   tasksFlagOwnership,
		IncludeDone: tasksFlagAll,
	}

	items, err := database.GetTasks(f)
	if err != nil {
		return fmt.Errorf("querying tasks: %w", err)
	}

	if tasksFlagJSON {
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	active, overdue, err := database.GetTaskCounts()
	if err != nil {
		return fmt.Errorf("getting task counts: %w", err)
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No tasks found.")
		return nil
	}

	header := fmt.Sprintf("Tasks (%d active", active)
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

		if item.DueDate != "" {
			line += fmt.Sprintf("    due: %s", item.DueDate)
		}

		if item.Status != "todo" {
			line += fmt.Sprintf("  (%s)", item.Status)
		}

		fmt.Fprintln(out, line)
	}

	return nil
}

func runTasksShow(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid task ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	task, err := database.GetTaskByID(id)
	if err != nil {
		return fmt.Errorf("task #%d not found: %w", id, err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Task #%d: %s\n", task.ID, task.Text)
	fmt.Fprintf(out, "Status: %s | Priority: %s\n", task.Status, task.Priority)

	if task.Intent != "" {
		fmt.Fprintf(out, "Intent: %s\n", task.Intent)
	}
	if task.BallOn != "" {
		fmt.Fprintf(out, "Ball on: %s\n", task.BallOn)
	}
	if task.DueDate != "" {
		fmt.Fprintf(out, "Due: %s\n", task.DueDate)
	}
	if task.SnoozeUntil != "" {
		fmt.Fprintf(out, "Snoozed until: %s\n", task.SnoozeUntil)
	}
	if task.Blocking != "" {
		fmt.Fprintf(out, "Blocking: %s\n", task.Blocking)
	}

	// Tags
	var tags []string
	if json.Unmarshal([]byte(task.Tags), &tags) == nil && len(tags) > 0 {
		fmt.Fprintf(out, "Tags: %s\n", strings.Join(tags, ", "))
	}

	// Sub-items
	if task.SubItems != "" && task.SubItems != "[]" {
		type subItem struct {
			Text string `json:"text"`
			Done bool   `json:"done"`
		}
		var subs []subItem
		if json.Unmarshal([]byte(task.SubItems), &subs) == nil && len(subs) > 0 {
			fmt.Fprintf(out, "\nSub-items:\n")
			for _, s := range subs {
				check := "[ ]"
				if s.Done {
					check = "[x]"
				}
				fmt.Fprintf(out, "  %s %s\n", check, s.Text)
			}
		}
	}

	fmt.Fprintf(out, "\nSource: %s", task.SourceType)
	if task.SourceID != "" {
		fmt.Fprintf(out, " #%s", task.SourceID)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Created: %s | Updated: %s\n", task.CreatedAt, task.UpdatedAt)

	return nil
}

func runTasksCreate(cmd *cobra.Command, _ []string) error {
	if tasksFlagText == "" {
		return fmt.Errorf("--text is required")
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	task := db.Task{
		Text:       tasksFlagText,
		Intent:     tasksFlagIntent,
		Status:     "todo",
		Priority:   tasksFlagPriority,
		Ownership:  tasksFlagOwnership,
		DueDate:    tasksFlagDue,
		SourceType: tasksFlagSourceType,
		SourceID:   tasksFlagSourceID,
	}

	if tasksFlagTags != "" {
		parts := strings.Split(tasksFlagTags, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		tagsJSON, _ := json.Marshal(parts)
		task.Tags = string(tagsJSON)
	}

	id, err := database.CreateTask(task)
	if err != nil {
		return fmt.Errorf("creating task: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created task #%d\n", id)
	return nil
}

func runTasksDone(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid task ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.UpdateTaskStatus(id, "done"); err != nil {
		return fmt.Errorf("marking task done: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Task #%d marked as done\n", id)
	return nil
}

func runTasksDismiss(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid task ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.UpdateTaskStatus(id, "dismissed"); err != nil {
		return fmt.Errorf("dismissing task: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Task #%d dismissed\n", id)
	return nil
}

func runTasksSnooze(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid task ID %q: must be a positive integer", args[0])
	}
	snoozeDate := args[1]

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	task, err := database.GetTaskByID(id)
	if err != nil {
		return fmt.Errorf("task #%d not found: %w", id, err)
	}

	task.Status = "snoozed"
	task.SnoozeUntil = snoozeDate
	if err := database.UpdateTask(*task); err != nil {
		return fmt.Errorf("snoozing task: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Task #%d snoozed until %s\n", id, snoozeDate)
	return nil
}

func runTasksUpdate(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid task ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	task, err := database.GetTaskByID(id)
	if err != nil {
		return fmt.Errorf("task #%d not found: %w", id, err)
	}

	if cmd.Flags().Changed("text") {
		task.Text = tasksFlagText
	}
	if cmd.Flags().Changed("intent") {
		task.Intent = tasksFlagIntent
	}
	if cmd.Flags().Changed("priority") {
		task.Priority = tasksFlagPriority
	}
	if cmd.Flags().Changed("status") {
		task.Status = tasksFlagStatus
	}
	if cmd.Flags().Changed("ownership") {
		task.Ownership = tasksFlagOwnership
	}
	if cmd.Flags().Changed("ball-on") {
		task.BallOn = tasksFlagBallOn
	}
	if cmd.Flags().Changed("due") {
		task.DueDate = tasksFlagDue
	}
	if cmd.Flags().Changed("blocking") {
		task.Blocking = tasksFlagBlocking
	}
	if cmd.Flags().Changed("tags") {
		parts := strings.Split(tasksFlagTags, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		tagsJSON, _ := json.Marshal(parts)
		task.Tags = string(tagsJSON)
	}

	if err := database.UpdateTask(*task); err != nil {
		return fmt.Errorf("updating task: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Task #%d updated\n", id)
	return nil
}

func runTasksGenerate(cmd *cobra.Command, _ []string) error {
	if tasksFlagText == "" {
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

	// Build source context if provided.
	var sourceContext string
	if tasksFlagSourceType != "" && tasksFlagSourceID != "" {
		database, err := db.Open(cfg.DBPath())
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}
		sourceContext = loadSourceContext(database, tasksFlagSourceType, tasksFlagSourceID)
		database.Close()
	}

	// Build prompt.
	now := time.Now().Format("2006-01-02T15:04 (Monday)")
	promptTmpl := prompts.Defaults[prompts.TasksGenerate]
	systemPrompt := fmt.Sprintf(promptTmpl, now)

	userMessage := tasksFlagText
	if sourceContext != "" {
		userMessage += "\n\n=== SOURCE CONTEXT ===\n" + sourceContext
	}

	// Call AI.
	gen := digest.NewClaudeGenerator(digest.ModelSonnet, cfg.ClaudePath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, usage, _, err := gen.Generate(ctx, systemPrompt, userMessage, "")
	if err != nil {
		return fmt.Errorf("AI generation failed: %w", err)
	}

	// Record usage in pipeline_runs.
	database, err := db.Open(cfg.DBPath())
	if err == nil {
		model := digest.ModelSonnet
		runID, runErr := database.CreatePipelineRun("tasks", "cli", model)
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

	// Extract JSON from result (AI may wrap it in markdown code blocks).
	jsonStr := extractJSON(result)

	// Output raw JSON to stdout for the desktop app to parse.
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
	// Try to find ```json ... ``` block first.
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	// Try to find ``` ... ``` block.
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		// Skip optional language tag on same line.
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	// Try to find raw JSON object.
	if idx := strings.Index(s, "{"); idx >= 0 {
		if end := strings.LastIndex(s, "}"); end > idx {
			return s[idx : end+1]
		}
	}
	return s
}
