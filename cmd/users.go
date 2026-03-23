package cmd

import (
	"fmt"

	"watchtower/internal/db"

	"github.com/spf13/cobra"
)

var usersFlagActive bool

var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "List synced users",
	Long:  "List all users synced to the local database with display name, email, and bot status.",
	RunE:  runUsers,
}

func init() {
	rootCmd.AddCommand(usersCmd)
	usersCmd.Flags().BoolVar(&usersFlagActive, "active", false, "exclude deleted and bot users")
}

func runUsers(cmd *cobra.Command, args []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	filter := db.UserFilter{}
	if usersFlagActive {
		filter.ExcludeBots = true
		filter.ExcludeDeleted = true
	}

	users, err := database.GetUsers(filter)
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(users) == 0 {
		fmt.Fprintln(out, "No users found. Run sync first?")
		return nil
	}

	for _, u := range users {
		flags := ""
		if u.IsBot {
			flags = " [bot]"
		}
		if u.IsDeleted {
			flags += " [deleted]"
		}

		displayName := u.DisplayName
		if displayName == "" {
			displayName = u.RealName
		}

		email := u.Email
		if email == "" {
			email = "-"
		}

		fmt.Fprintf(out, "%-25s  %-30s  %s%s\n",
			u.Name,
			displayName,
			email,
			flags,
		)
	}

	fmt.Fprintf(out, "\n%d users total\n", len(users))
	return nil
}
