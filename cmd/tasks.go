package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// tasksCmd is a deprecation stub. The command has been renamed to "targets".
var tasksCmd = &cobra.Command{
	Use:          "tasks",
	Short:        "(deprecated) renamed to 'targets'",
	Hidden:       false,
	SilenceUsage: true,
	// Accept any subcommand / args so they don't produce a spurious "unknown command" error.
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "`tasks` has been renamed to `targets`. See `watchtower targets --help`.")
		os.Exit(2)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tasksCmd)
}
