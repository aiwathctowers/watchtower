package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/inbox"

	"github.com/spf13/cobra"
)

const syncStalenessThreshold = 10 * time.Minute

var (
	inboxFlagPriority        string
	inboxFlagType            string
	inboxFlagAll             bool
	inboxFlagJSON            bool
	inboxGenFlagProgressJSON bool
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Show messages awaiting your response",
	Long:  "Displays inbox items — @mentions and DMs where you haven't replied yet.",
	RunE:  runInbox,
}

var inboxShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show inbox item details",
	Args:  cobra.ExactArgs(1),
	RunE:  runInboxShow,
}

var inboxResolveCmd = &cobra.Command{
	Use:   "resolve <id>",
	Short: "Mark an inbox item as resolved",
	Args:  cobra.ExactArgs(1),
	RunE:  runInboxResolve,
}

var inboxDismissCmd = &cobra.Command{
	Use:   "dismiss <id>",
	Short: "Dismiss an inbox item",
	Args:  cobra.ExactArgs(1),
	RunE:  runInboxDismiss,
}

var inboxSnoozeCmd = &cobra.Command{
	Use:   "snooze <id> <duration>",
	Short: "Snooze an inbox item (e.g. 1d, 3d, 1w)",
	Args:  cobra.ExactArgs(2),
	RunE:  runInboxSnooze,
}

var inboxGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run inbox detection pipeline",
	RunE:  runInboxGenerate,
}

var inboxTaskCmd = &cobra.Command{
	Use:   "task <id>",
	Short: "Create a task from an inbox item",
	Args:  cobra.ExactArgs(1),
	RunE:  runInboxTask,
}

func init() {
	rootCmd.AddCommand(inboxCmd)
	inboxCmd.AddCommand(inboxShowCmd, inboxResolveCmd, inboxDismissCmd, inboxSnoozeCmd, inboxGenerateCmd, inboxTaskCmd)

	inboxCmd.Flags().StringVar(&inboxFlagPriority, "priority", "", "filter by priority (high, medium, low)")
	inboxCmd.Flags().StringVar(&inboxFlagType, "type", "", "filter by trigger type (mention, dm)")
	inboxCmd.Flags().BoolVar(&inboxFlagAll, "all", false, "include resolved and dismissed items")
	inboxCmd.Flags().BoolVar(&inboxFlagJSON, "json", false, "output as JSON")
	inboxGenerateCmd.Flags().BoolVar(&inboxGenFlagProgressJSON, "progress-json", false, "output progress as JSON lines")
}

func runInbox(cmd *cobra.Command, _ []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	f := db.InboxFilter{
		Priority:        inboxFlagPriority,
		TriggerType:     inboxFlagType,
		IncludeResolved: inboxFlagAll,
	}

	items, err := database.GetInboxItems(f)
	if err != nil {
		return fmt.Errorf("querying inbox: %w", err)
	}

	if inboxFlagJSON {
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	pending, unread, err := database.GetInboxCounts()
	if err != nil {
		return fmt.Errorf("getting inbox counts: %w", err)
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No inbox items found.")
		return nil
	}

	header := fmt.Sprintf("Inbox (%d pending", pending)
	if unread > 0 {
		header += fmt.Sprintf(", %d unread", unread)
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

		typeLabel := "@"
		if item.TriggerType == "dm" {
			typeLabel = "DM"
		}

		snippet := item.Snippet
		if len(snippet) > 80 {
			snippet = snippet[:80] + "..."
		}

		line := fmt.Sprintf(" %s  %s  [#%d] %s", pLabel, typeLabel, item.ID, snippet)

		if item.Status != "pending" {
			line += fmt.Sprintf("  (%s)", item.Status)
		}

		fmt.Fprintln(out, line)
	}

	return nil
}

func runInboxShow(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid inbox item ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	item, err := database.GetInboxItemByID(id)
	if err != nil {
		return fmt.Errorf("inbox item #%d not found: %w", id, err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Inbox Item #%d\n", item.ID)
	fmt.Fprintf(out, "Status: %s | Priority: %s | Type: %s\n", item.Status, item.Priority, item.TriggerType)
	fmt.Fprintf(out, "Channel: %s | Sender: %s\n", item.ChannelID, item.SenderUserID)

	if item.Snippet != "" {
		fmt.Fprintf(out, "\n%s\n", item.Snippet)
	}
	if item.Context != "" {
		fmt.Fprintf(out, "\n--- Context ---\n%s\n", item.Context)
	}
	if item.AIReason != "" {
		fmt.Fprintf(out, "\nAI Reason: %s\n", item.AIReason)
	}
	if item.ResolvedReason != "" {
		fmt.Fprintf(out, "Resolved: %s\n", item.ResolvedReason)
	}
	if item.Permalink != "" {
		fmt.Fprintf(out, "Link: %s\n", item.Permalink)
	}
	if item.SnoozeUntil != "" {
		fmt.Fprintf(out, "Snoozed until: %s\n", item.SnoozeUntil)
	}
	if item.TaskID != nil {
		fmt.Fprintf(out, "Task: #%d\n", *item.TaskID)
	}

	fmt.Fprintf(out, "Created: %s | Updated: %s\n", item.CreatedAt, item.UpdatedAt)

	return nil
}

func runInboxResolve(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid inbox item ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.ResolveInboxItem(id, "Manually resolved"); err != nil {
		return fmt.Errorf("resolving inbox item: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Inbox item #%d resolved\n", id)
	return nil
}

func runInboxDismiss(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid inbox item ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.DismissInboxItem(id); err != nil {
		return fmt.Errorf("dismissing inbox item: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Inbox item #%d dismissed\n", id)
	return nil
}

func runInboxSnooze(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid inbox item ID %q: must be a positive integer", args[0])
	}

	until, err := parseDuration(args[1])
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", args[1], err)
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.SnoozeInboxItem(id, until); err != nil {
		return fmt.Errorf("snoozing inbox item: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Inbox item #%d snoozed until %s\n", id, until)
	return nil
}

// parseDuration converts a human-readable duration to a YYYY-MM-DD date.
// Supported formats: 1d, 3d, 1w, 2w.
func parseDuration(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 {
		return "", fmt.Errorf("duration too short")
	}

	unit := s[len(s)-1]
	num, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || num <= 0 {
		return "", fmt.Errorf("invalid number in duration")
	}

	now := time.Now()
	switch unit {
	case 'd':
		return now.AddDate(0, 0, num).Format("2006-01-02"), nil
	case 'w':
		return now.AddDate(0, 0, num*7).Format("2006-01-02"), nil
	default:
		return "", fmt.Errorf("unknown unit %q (use d or w)", string(unit))
	}
}

func runInboxGenerate(cmd *cobra.Command, _ []string) error {
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

	if err := validateModel(cfg); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	logger := log.New(cmd.ErrOrStderr(), "[inbox] ", log.LstdFlags)
	out := cmd.OutOrStdout()

	// Ensure messages are fresh — run sync if last sync was >10 min ago.
	if needsSync(database, logger) {
		var onProgress func(string)
		if inboxGenFlagProgressJSON {
			onProgress = func(line string) {
				// Parse sync progress JSON and relay as inbox pipeline status
				var sp struct {
					Phase               string `json:"phase"`
					DiscoveryPages      int    `json:"discovery_pages"`
					DiscoveryTotalPages int    `json:"discovery_total_pages"`
					MessagesFetched     int    `json:"messages_fetched"`
					SearchAfter         string `json:"search_after"`
				}
				if json.Unmarshal([]byte(line), &sp) != nil {
					return
				}

				status := "Syncing messages..."
				if sp.SearchAfter != "" {
					status = fmt.Sprintf("Sync: от %s", sp.SearchAfter)
				}
				if sp.DiscoveryPages > 0 {
					pages := fmt.Sprintf("стр. %d", sp.DiscoveryPages)
					if sp.DiscoveryTotalPages > 0 {
						pages = fmt.Sprintf("стр. %d/%d", sp.DiscoveryPages, sp.DiscoveryTotalPages)
					}
					msgs := fmt.Sprintf("%d сообщ.", sp.MessagesFetched)
					if sp.SearchAfter != "" {
						status = fmt.Sprintf("Sync: от %s (%s, %s)", sp.SearchAfter, pages, msgs)
					} else {
						status = fmt.Sprintf("Sync: %s, %s", pages, msgs)
					}
				}

				data, _ := json.Marshal(map[string]any{
					"pipeline": "inbox", "done": 0, "total": 0,
					"status": status, "finished": false,
					"input_tokens": 0, "output_tokens": 0, "cost_usd": 0,
				})
				fmt.Fprintln(out, string(data))
			}
		}
		database.Close() // release DB lock for sync subprocess
		if err := runQuickSync(cmd, logger, onProgress); err != nil {
			logger.Printf("inbox: pre-sync failed (continuing with stale data): %v", err)
		}
		database, err = db.Open(cfg.DBPath())
		if err != nil {
			return fmt.Errorf("reopening database after sync: %w", err)
		}
		defer database.Close()
	}

	gen, cleanupPool := cliPooledGenerator(cfg, logger)
	defer cleanupPool()
	pipe := inbox.New(database, cfg, gen, logger)

	if inboxGenFlagProgressJSON {
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
			StepDurationSec  float64 `json:"step_duration_seconds,omitempty"`
			StepInputTokens  int     `json:"step_input_tokens,omitempty"`
			StepOutputTokens int     `json:"step_output_tokens,omitempty"`
			StepCostUSD      float64 `json:"step_cost_usd,omitempty"`
			TotalAPITokens   int     `json:"total_api_tokens,omitempty"`
		}
		emit := func(p pj) { data, _ := json.Marshal(p); fmt.Fprintln(out, string(data)) }

		runID, _ := database.CreatePipelineRun("inbox", "cli", "auto")
		lastTotal := 4 // default, updated dynamically

		pipe.OnProgress = func(done, total int, status string) {
			lastTotal = total
			inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
			p := pj{
				Pipeline:       "inbox",
				Done:           done,
				Total:          total,
				Status:         status,
				InputTokens:    inTok,
				OutputTokens:   outTok,
				CostUSD:        cost,
				TotalAPITokens: totalAPI,
			}
			if pipe.LastStepDurationSeconds > 0 {
				p.StepDurationSec = pipe.LastStepDurationSeconds
				p.StepInputTokens = pipe.LastStepInputTokens
				p.StepOutputTokens = pipe.LastStepOutputTokens
				p.StepCostUSD = pipe.LastStepCostUSD
			}
			emit(p)

			// Log step to DB.
			if runID > 0 && p.StepDurationSec > 0 {
				_ = database.InsertPipelineStep(db.PipelineStep{
					RunID: runID, Step: done, Total: total, Status: status,
					InputTokens:     p.StepInputTokens,
					OutputTokens:    p.StepOutputTokens,
					CostUSD:         p.StepCostUSD,
					TotalAPITokens:  totalAPI,
					DurationSeconds: p.StepDurationSec,
				})
			}
		}

		created, resolved, err := pipe.Run(cmd.Context())
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}

		inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
		emit(pj{
			Pipeline:       "inbox",
			Done:           lastTotal,
			Total:          lastTotal,
			Finished:       true,
			ItemsFound:     created + resolved,
			InputTokens:    inTok,
			OutputTokens:   outTok,
			CostUSD:        cost,
			TotalAPITokens: totalAPI,
			Error:          errMsg,
		})

		if runID > 0 {
			_ = database.CompletePipelineRun(runID, created+resolved, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
		}

		if err != nil {
			return fmt.Errorf("inbox pipeline: %w", err)
		}
		return nil
	}

	runID, _ := database.CreatePipelineRun("inbox", "cli", "auto")

	created, resolved, err := pipe.Run(cmd.Context())
	inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	if runID > 0 {
		_ = database.CompletePipelineRun(runID, created+resolved, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
	}
	if err != nil {
		return fmt.Errorf("inbox pipeline: %w", err)
	}

	fmt.Fprintf(out, "Inbox: %d new items detected, %d resolved\n", created, resolved)
	return nil
}

func runInboxTask(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid inbox item ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	item, err := database.GetInboxItemByID(id)
	if err != nil {
		return fmt.Errorf("inbox item #%d not found: %w", id, err)
	}

	task := db.Task{
		Text:       item.Snippet,
		Status:     "todo",
		Priority:   item.Priority,
		Ownership:  "mine",
		SourceType: "inbox",
		SourceID:   strconv.Itoa(item.ID),
	}

	taskID, err := database.CreateTask(task)
	if err != nil {
		return fmt.Errorf("creating task: %w", err)
	}

	if err := database.LinkInboxTask(id, int(taskID)); err != nil {
		return fmt.Errorf("linking task: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created task #%d from inbox item #%d\n", taskID, id)
	return nil
}

// needsSync checks if workspace.synced_at is older than syncStalenessThreshold.
func needsSync(database *db.DB, logger *log.Logger) bool {
	var syncedAt string
	err := database.QueryRow(`SELECT COALESCE(synced_at, '') FROM workspace LIMIT 1`).Scan(&syncedAt)
	if err != nil || syncedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, syncedAt)
	if err != nil {
		// Try alternate format
		t, err = time.Parse("2006-01-02T15:04:05Z", syncedAt)
		if err != nil {
			logger.Printf("inbox: cannot parse synced_at %q: %v", syncedAt, err)
			return true
		}
	}
	return time.Since(t) > syncStalenessThreshold
}

// runQuickSync runs `watchtower sync` as a subprocess to refresh messages.
// If onProgress is non-nil, --progress-json is added and each stdout line
// is forwarded to the callback for real-time progress relay.
func runQuickSync(cmd *cobra.Command, logger *log.Logger, onProgress func(string)) error {
	logger.Println("inbox: messages stale, running quick sync...")
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}
	args := []string{"sync"}
	if flagConfig != "" {
		args = append(args, "--config", flagConfig)
	}
	if flagWorkspace != "" {
		args = append(args, "--workspace", flagWorkspace)
	}
	if onProgress != nil {
		args = append(args, "--progress-json")
	}
	syncProc := exec.CommandContext(cmd.Context(), exe, args...)
	syncProc.Stderr = cmd.ErrOrStderr()

	if onProgress == nil {
		return syncProc.Run()
	}

	stdout, err := syncProc.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	if err := syncProc.Start(); err != nil {
		return fmt.Errorf("starting sync: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		onProgress(scanner.Text())
	}

	return syncProc.Wait()
}
