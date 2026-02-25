package cmd

import (
	"fmt"
	"os"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workspace sync status and statistics",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	dbPath := cfg.DBPath()
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	ws, err := database.GetWorkspace()
	if err != nil {
		return fmt.Errorf("getting workspace: %w", err)
	}

	stats, err := database.GetStats()
	if err != nil {
		return fmt.Errorf("getting stats: %w", err)
	}

	lastSync, err := database.LastSyncTime()
	if err != nil {
		return fmt.Errorf("getting last sync time: %w", err)
	}

	// Workspace line
	if ws != nil {
		fmt.Fprintf(out, "Workspace: %s (%s)\n", ws.Name, ws.ID)
	} else {
		fmt.Fprintf(out, "Workspace: %s (not yet synced)\n", cfg.ActiveWorkspace)
	}

	// Database line
	dbSize := dbFileSize(dbPath)
	fmt.Fprintf(out, "Database:  %s (%s)\n", dbPath, humanize.IBytes(uint64(dbSize)))

	// Last sync line
	if lastSync != "" {
		t, err := time.Parse("2006-01-02T15:04:05Z", lastSync)
		if err == nil {
			fmt.Fprintf(out, "Last sync: %s (%s)\n", lastSync, humanize.Time(t))
		} else {
			fmt.Fprintf(out, "Last sync: %s\n", lastSync)
		}
	} else {
		fmt.Fprintln(out, "Last sync: never")
	}

	// Summary line
	watchedStr := ""
	if stats.WatchedCount > 0 {
		watchedStr = fmt.Sprintf(" (%s watched)", humanize.Comma(int64(stats.WatchedCount)))
	}
	fmt.Fprintf(out, "Channels: %s%s | Users: %s | Messages: %s | Threads: %s\n",
		humanize.Comma(int64(stats.ChannelCount)),
		watchedStr,
		humanize.Comma(int64(stats.UserCount)),
		humanize.Comma(int64(stats.MessageCount)),
		humanize.Comma(int64(stats.ThreadCount)),
	)

	return nil
}

func dbFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
