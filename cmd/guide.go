package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var guideCmd = &cobra.Command{
	Use:        "guide",
	Short:      "Deprecated: use 'watchtower people' instead",
	Long:       "The guide command has been merged into 'watchtower people'. Use 'watchtower people' for unified people cards with both analysis and coaching.",
	Deprecated: "use 'watchtower people' instead",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "The 'guide' command has been replaced by 'people'.")
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'watchtower people' to see unified people cards.")
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'watchtower people generate' to generate new cards.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(guideCmd)
}
