package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"watchtower/internal/config"

	"github.com/spf13/cobra"
)

var (
	logsFlagFollow bool
	logsFlagLines  int
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View sync log output",
	Long:  "Display recent sync log entries from the watchtower.log file.",
	RunE:  runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolVarP(&logsFlagFollow, "follow", "f", false, "follow log output (tail -f)")
	logsCmd.Flags().IntVarP(&logsFlagLines, "lines", "n", 50, "number of lines to show")
}

func runLogs(cmd *cobra.Command, args []string) error {
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

	logPath := syncLogFilePath(cfg)

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return fmt.Errorf("no log file found at %s; run 'watchtower sync' first to generate logs", logPath)
	}

	if err := printLastLines(cmd.OutOrStdout(), logPath, logsFlagLines); err != nil {
		return err
	}

	if logsFlagFollow {
		return followLog(cmd, logPath)
	}
	return nil
}

// printLastLines prints the last n lines of the file.
func printLastLines(w io.Writer, path string, n int) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	// Read all lines (simple approach — log files are typically small).
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading log file: %w", err)
	}

	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}
	for _, line := range lines[start:] {
		fmt.Fprintln(w, line)
	}
	return nil
}

// followLog tails the log file, printing new lines as they appear.
func followLog(cmd *cobra.Command, path string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer f.Close()

	// Seek to end of file.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seeking log file: %w", err)
	}

	w := cmd.OutOrStdout()
	reader := bufio.NewReader(f)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					fmt.Fprint(w, line)
				}
				if err != nil {
					break
				}
			}
		}
	}
}
