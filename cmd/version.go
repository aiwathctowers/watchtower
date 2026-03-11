package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "0.2.0"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "watchtower %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
