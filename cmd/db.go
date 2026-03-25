package cmd

import (
	"fmt"

	"watchtower/internal/prompts"

	"github.com/spf13/cobra"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply pending database migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := openDBFromConfig()
		if err != nil {
			return err
		}
		defer database.Close()

		// Seed any new prompt templates added since last run
		store := prompts.New(database, nil)
		_ = store.Seed()

		fmt.Println("Database migrations applied successfully.")
		return nil
	},
}

func init() {
	dbCmd.AddCommand(dbMigrateCmd)
	rootCmd.AddCommand(dbCmd)
}
