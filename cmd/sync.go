package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/daemon"
	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"
	"watchtower/internal/sync"

	"github.com/spf13/cobra"
)

var (
	syncFlagFull     bool
	syncFlagDaemon   bool
	syncFlagChannels []string
	syncFlagWorkers  int
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync Slack workspace data to local database",
	Long:  "Fetches workspace metadata, messages, and threads from Slack and stores them in the local SQLite database.",
	RunE:  runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolVar(&syncFlagFull, "full", false, "re-fetch all history within the initial history window")
	syncCmd.Flags().BoolVar(&syncFlagDaemon, "daemon", false, "run in daemon mode with periodic syncing")
	syncCmd.Flags().StringSliceVar(&syncFlagChannels, "channels", nil, "limit sync to specific channel names or IDs")
	syncCmd.Flags().IntVar(&syncFlagWorkers, "workers", 0, "number of concurrent sync workers (0 = use config default)")
}

func runSync(cmd *cobra.Command, args []string) error {
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

	// Configure logger: discard unless verbose
	if !flagVerbose {
		orch.SetLogger(log.New(io.Discard, "", 0))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Daemon mode: run periodic syncs until interrupted
	if syncFlagDaemon {
		d := daemon.New(orch, cfg)
		if !flagVerbose {
			d.SetLogger(log.New(io.Discard, "", 0))
		}
		return d.Run(ctx)
	}

	opts := sync.SyncOptions{
		Full:     syncFlagFull,
		Channels: syncFlagChannels,
		Workers:  syncFlagWorkers,
	}

	out := cmd.OutOrStdout()

	// Start progress display in background
	done := make(chan error, 1)
	go func() {
		done <- orch.Run(ctx, opts)
	}()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			// Final render
			printProgress(out, orch.Progress(), cfg.ActiveWorkspace)
			if err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}
			snap := orch.Progress().Snapshot()
			fmt.Fprintf(out, "\nSync complete: %d messages, %d threads synced.\n",
				snap.MessagesFetched, snap.ThreadsFetched)
			return nil
		case <-ticker.C:
			printProgress(out, orch.Progress(), cfg.ActiveWorkspace)
		}
	}
}

func printProgress(w io.Writer, p *sync.Progress, workspace string) {
	if f, ok := w.(*os.File); ok && isTerminal(f.Fd()) {
		fmt.Fprint(w, "\033[2J\033[H") // clear screen, move cursor to top
	}
	fmt.Fprintln(w, p.Render(workspace))
}

func isTerminal(fd uintptr) bool {
	// Check if the file descriptor refers to a terminal by attempting
	// a tcgetattr-equivalent: os.File.Stat() on a terminal has a
	// Mode() that includes os.ModeCharDevice.
	fi, err := os.NewFile(fd, "").Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
