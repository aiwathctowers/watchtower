package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagWorkspace string
	flagConfig    string
	flagVerbose   bool
)

var rootCmd = &cobra.Command{
	Use:   "watchtower",
	Short: "Slack workspace intelligence tool",
	Long:  "Watchtower syncs a Slack workspace into a local SQLite database and provides an AI-powered interface for analysis via the Claude API.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "workspace name to use")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", defaultConfigPath(), "path to config file")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "enable verbose output")
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.config/watchtower/config.yaml"
}
