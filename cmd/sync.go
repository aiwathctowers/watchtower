package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"encoding/json"
	"watchtower/internal/actionitems"
	"watchtower/internal/analysis"
	"watchtower/internal/config"
	"watchtower/internal/daemon"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/sync"
	"watchtower/internal/ui"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	syncFlagFull         bool
	syncFlagDaemon       bool
	syncFlagDetach       bool
	syncFlagStop         bool
	syncFlagChannels     []string
	syncFlagWorkers      int
	syncFlagSkipDMs      bool
	syncFlagDays         int
	syncFlagProgressJSON bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync Slack workspace data to local database",
	Long:  "Fetches workspace metadata, messages, and threads from Slack and stores them in the local SQLite database.",
	RunE:  runSync,
}

var syncStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop a running detached daemon",
	RunE:  runSyncStopCmd,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(syncStopCmd)
	syncCmd.Flags().BoolVar(&syncFlagFull, "full", false, "re-fetch all history within the initial history window")
	syncCmd.Flags().BoolVar(&syncFlagDaemon, "daemon", false, "run in daemon mode with periodic syncing")
	syncCmd.Flags().BoolVar(&syncFlagDetach, "detach", false, "start daemon in the background (requires --daemon)")
	syncCmd.Flags().BoolVar(&syncFlagStop, "stop", false, "stop a running detached daemon")
	syncCmd.Flags().StringSliceVar(&syncFlagChannels, "channels", nil, "limit sync to specific channel names or IDs")
	syncCmd.Flags().IntVar(&syncFlagWorkers, "workers", 0, "number of concurrent sync workers (0 = use config default)")
	syncCmd.Flags().BoolVar(&syncFlagSkipDMs, "skip-dms", false, "skip syncing DMs and group DMs")
	syncCmd.Flags().BoolVar(&syncFlagProgressJSON, "progress-json", false, "output progress as JSON lines to stdout")
	syncCmd.Flags().IntVar(&syncFlagDays, "days", 0, "override initial_history_days for this run")
}

func runSyncStopCmd(cmd *cobra.Command, args []string) error {
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
	return runSyncStop(cfg)
}

func pidFilePath(cfg *config.Config) string {
	return filepath.Join(cfg.WorkspaceDir(), "daemon.pid")
}

func logFilePath(cfg *config.Config) string {
	return filepath.Join(cfg.WorkspaceDir(), "daemon.log")
}

func syncLogFilePath(cfg *config.Config) string {
	return filepath.Join(cfg.WorkspaceDir(), "watchtower.log")
}

func syncResultPath(cfg *config.Config) string {
	return filepath.Join(cfg.WorkspaceDir(), "last_sync.json")
}

func runSyncStop(cfg *config.Config) error {
	pidPath := pidFilePath(cfg)
	pid, err := daemon.FindProcess(pidPath)
	if err != nil {
		return fmt.Errorf("reading pid file: %w", err)
	}
	if pid == 0 {
		fmt.Println("No daemon is running.")
		return nil
	}

	fmt.Printf("Stopping daemon (PID %d)...\n", pid)
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	// Poll until process exits (10s timeout).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			daemon.RemovePID(pidPath)
			fmt.Println("Daemon stopped.")
			return nil //nolint:nilerr // err means process exited, which is the desired outcome
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("daemon (PID %d) did not exit within 10 seconds", pid)
}

func runSyncDetach(cfg *config.Config) error {
	pidPath := pidFilePath(cfg)
	pid, err := daemon.FindProcess(pidPath)
	if err != nil {
		return fmt.Errorf("checking existing daemon: %w", err)
	}
	if pid != 0 {
		return fmt.Errorf("daemon already running (PID %d)", pid)
	}

	logPath := logFilePath(cfg)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	// Re-exec ourselves with the detach env var set.
	child := exec.Command(exe, os.Args[1:]...)
	child.Env = append(os.Environ(), daemon.DetachEnvKey+"=1")
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := child.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	fmt.Printf("Daemon started (PID %d)\n", child.Process.Pid)
	fmt.Printf("Log: %s\n", logPath)
	return nil
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}

	// --stop only needs workspace validation (no Slack token).
	if syncFlagStop {
		if err := cfg.ValidateWorkspace(); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}
		return runSyncStop(cfg)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// --detach re-execs the process in the background.
	// Skip if we're already the detached child (env key is set).
	if syncFlagDetach && os.Getenv(daemon.DetachEnvKey) != "1" {
		if !syncFlagDaemon {
			return fmt.Errorf("--detach requires --daemon")
		}
		return runSyncDetach(cfg)
	}

	// Acquire exclusive lock to prevent concurrent syncs.
	lockPath := filepath.Join(cfg.WorkspaceDir(), "sync.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another sync is already running (lock: %s)", lockPath)
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	ws, err := cfg.GetActiveWorkspace()
	if err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	slackClient := watchtowerslack.NewClient(ws.SlackToken)
	orch := sync.NewOrchestrator(database, slackClient, cfg)

	// Always write logs to watchtower.log; also to stderr when verbose or detached.
	syncLog := syncLogFilePath(cfg)
	if err := os.MkdirAll(filepath.Dir(syncLog), 0o755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	logFile, err := os.OpenFile(syncLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer logFile.Close()

	var logWriter io.Writer = logFile
	isDetachedChild := os.Getenv(daemon.DetachEnvKey) == "1"
	if flagVerbose || isDetachedChild {
		logWriter = io.MultiWriter(logFile, os.Stderr)
	}
	logger := log.New(logWriter, "", log.LstdFlags)
	orch.SetLogger(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Daemon mode: run periodic syncs until interrupted
	if syncFlagDaemon {
		d := daemon.New(orch, cfg)
		d.SetLogger(logger)
		d.SetPIDPath(pidFilePath(cfg))
		if cfg.Digest.Enabled {
			gen := digest.NewClaudeGenerator(cfg.Digest.Model, cfg.ClaudePath)
			pipe := digest.New(database, cfg, gen, logger)
			d.SetDigestPipeline(pipe)
			d.SetAnalysisPipeline(analysis.New(database, cfg, gen, logger))
			d.SetActionItemsPipeline(actionitems.New(database, cfg, gen, logger))
		}
		return d.Run(ctx)
	}

	// Override initial_history_days if --days specified
	if syncFlagDays > 0 {
		cfg.Sync.InitialHistoryDays = syncFlagDays
	}

	opts := sync.SyncOptions{
		Full:     syncFlagFull,
		Channels: syncFlagChannels,
		Workers:  syncFlagWorkers,
		SkipDMs:  syncFlagSkipDMs,
	}

	out := cmd.OutOrStdout()

	// In verbose mode: just run sync, logs go to stderr
	if flagVerbose {
		syncErr := orch.Run(ctx, opts)
		snap := orch.Progress().Snapshot()
		if err := sync.WriteSyncResult(syncResultPath(cfg), sync.ResultFromSnapshot(snap, syncErr)); err != nil {
			logger.Printf("warning: failed to write sync result: %v", err)
		}
		if syncErr != nil {
			return fmt.Errorf("sync failed: %w", syncErr)
		}
		elapsed := time.Since(snap.StartTime).Round(time.Second)
		fmt.Fprintf(out, "Sync complete in %s: %d messages, %d threads synced.\n",
			elapsed, snap.MessagesFetched, snap.ThreadsFetched)
		runPostSyncPipelines(ctx, database, cfg, logger)
		return nil
	}

	// Normal mode: progress display in background
	progressLines.Store(0)
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("sync panicked: %v\n%s", r, debug.Stack())
			}
		}()
		done <- orch.Run(ctx, opts)
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case syncErr := <-done:
			snap := orch.Progress().Snapshot()
			if syncFlagProgressJSON {
				printProgressJSON(out, snap, syncErr)
			} else {
				printProgress(out, orch.Progress(), cfg.ActiveWorkspace)
			}
			if wErr := sync.WriteSyncResult(syncResultPath(cfg), sync.ResultFromSnapshot(snap, syncErr)); wErr != nil {
				logger.Printf("warning: failed to write sync result: %v", wErr)
			}
			if syncErr != nil {
				return fmt.Errorf("sync failed: %w", syncErr)
			}
			if !syncFlagProgressJSON {
				runPostSyncPipelines(ctx, database, cfg, logger)
			}
			return nil
		case <-ticker.C:
			snap := orch.Progress().Snapshot()
			if syncFlagProgressJSON {
				printProgressJSON(out, snap, nil)
			} else {
				printProgress(out, orch.Progress(), cfg.ActiveWorkspace)
			}
		}
	}
}

var progressLines atomic.Int32

func printProgress(w io.Writer, p *sync.Progress, workspace string) {
	if !flagVerbose {
		if f, ok := w.(*os.File); ok && isTerminal(f) {
			// Move cursor up to overwrite previous output
			if lines := progressLines.Load(); lines > 0 {
				fmt.Fprintf(w, "\033[%dA\033[J", lines)
			}
		}
	}
	output := p.Render(workspace)
	fmt.Fprintln(w, output)
	progressLines.Store(int32(strings.Count(output, "\n") + 1)) //nolint:gosec // safe conversion within expected range
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

type progressJSON struct {
	Phase               string  `json:"phase"`
	ElapsedSec          float64 `json:"elapsed_sec"`
	UsersTotal          int     `json:"users_total"`
	UsersDone           int     `json:"users_done"`
	ChannelsTotal       int     `json:"channels_total"`
	ChannelsDone        int     `json:"channels_done"`
	DiscoveryPages      int     `json:"discovery_pages"`
	DiscoveryTotalPages int     `json:"discovery_total_pages"`
	DiscoveryChannels   int     `json:"discovery_channels"`
	DiscoveryUsers      int     `json:"discovery_users"`
	UserProfilesTotal   int     `json:"user_profiles_total"`
	UserProfilesDone    int     `json:"user_profiles_done"`
	MsgChannelsTotal    int     `json:"msg_channels_total"`
	MsgChannelsDone     int     `json:"msg_channels_done"`
	MessagesFetched     int     `json:"messages_fetched"`
	ThreadsTotal        int     `json:"threads_total"`
	ThreadsDone         int     `json:"threads_done"`
	ThreadsFetched      int     `json:"threads_fetched"`
	Error               string  `json:"error,omitempty"`
}

func runPostSyncPipelines(ctx context.Context, database *db.DB, cfg *config.Config, logger *log.Logger) {
	if !cfg.Digest.Enabled {
		return
	}

	out := os.Stdout
	gen := digest.NewClaudeGenerator(cfg.Digest.Model, cfg.ClaudePath)

	// Digests
	fmt.Fprintln(out)
	digestSpinner := ui.NewSpinner(out, "Generating digests...")
	pipe := digest.New(database, cfg, gen, logger)
	pipe.OnProgress = func(done, total int, status string) {
		digestSpinner.UpdateProgress(done, total, status)
	}
	n, usage, err := pipe.Run(ctx)
	if err != nil {
		digestSpinner.Stop(fmt.Sprintf("Digest error: %v", err))
	} else if n > 0 {
		if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
			digestSpinner.Stop(fmt.Sprintf("Generated %d digest(s) (%d+%d tokens, $%.4f)",
				n, usage.InputTokens, usage.OutputTokens, usage.CostUSD))
		} else {
			digestSpinner.Stop(fmt.Sprintf("Generated %d digest(s)", n))
		}
	} else {
		digestSpinner.Stop("No new digests needed")
	}

	// People analysis (independent, runs before action items)
	analysisSpinner := ui.NewSpinner(out, "Running people analysis...")
	analysisPipe := analysis.New(database, cfg, gen, logger)
	analysisPipe.OnProgress = func(done, total int, status string) {
		analysisSpinner.UpdateProgress(done, total, status)
	}
	pn, err := analysisPipe.Run(ctx)
	if err != nil {
		analysisSpinner.Stop(fmt.Sprintf("People analysis error: %v", err))
	} else if pn > 0 {
		analysisSpinner.Stop(fmt.Sprintf("Analyzed %d user(s)", pn))
	} else {
		analysisSpinner.Stop("No new analyses needed")
	}

	// Action items (depends on digests for related_digest_ids)
	actionSpinner := ui.NewSpinner(out, "Extracting action items...")
	actionPipe := actionitems.New(database, cfg, gen, logger)
	actionPipe.OnProgress = func(done, total int, status string) {
		actionSpinner.UpdateProgress(done, total, status)
	}
	an, err := actionPipe.Run(ctx)
	if err != nil {
		actionSpinner.Stop(fmt.Sprintf("Action items error: %v", err))
	} else if an > 0 {
		actionSpinner.Stop(fmt.Sprintf("Extracted %d action item(s)", an))
	} else {
		actionSpinner.Stop("No new action items")
	}
}

func printProgressJSON(w io.Writer, snap sync.Snapshot, syncErr error) {
	p := progressJSON{
		Phase:               snap.Phase.String(),
		ElapsedSec:          time.Since(snap.StartTime).Seconds(),
		UsersTotal:          snap.UsersTotal,
		UsersDone:           snap.UsersDone,
		ChannelsTotal:       snap.ChannelsTotal,
		ChannelsDone:        snap.ChannelsDone,
		DiscoveryPages:      snap.DiscoveryPages,
		DiscoveryTotalPages: snap.DiscoveryTotalPages,
		DiscoveryChannels:   snap.DiscoveryChannels,
		DiscoveryUsers:      snap.DiscoveryUsers,
		UserProfilesTotal:   snap.UserProfilesTotal,
		UserProfilesDone:    snap.UserProfilesDone,
		MsgChannelsTotal:    snap.MsgChannelsTotal,
		MsgChannelsDone:     snap.MsgChannelsDone,
		MessagesFetched:     snap.MessagesFetched,
		ThreadsTotal:        snap.ThreadsTotal,
		ThreadsDone:         snap.ThreadsDone,
		ThreadsFetched:      snap.ThreadsFetched,
	}
	if syncErr != nil {
		p.Error = syncErr.Error()
	}
	data, _ := json.Marshal(p)
	fmt.Fprintln(w, string(data))
}
