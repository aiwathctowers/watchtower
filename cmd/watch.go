package cmd

import (
	"fmt"
	"strings"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/spf13/cobra"
)

var watchFlagPriority string

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Manage the watch list for channels and users",
	Long:  "Add, remove, or list channels and users on the watch list. Watched items receive higher sync priority and appear prominently in catchup reports.",
}

var watchAddCmd = &cobra.Command{
	Use:   "add <target>",
	Short: "Add a channel or user to the watch list",
	Long:  "Add a #channel or @user to the watch list. Targets are resolved by name against the synced database.",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatchAdd,
}

var watchRemoveCmd = &cobra.Command{
	Use:   "remove <target>",
	Short: "Remove a channel or user from the watch list",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatchRemove,
}

var watchListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all watched channels and users",
	RunE:  runWatchList,
}

func init() {
	rootCmd.AddCommand(watchCmd)
	watchCmd.AddCommand(watchAddCmd)
	watchCmd.AddCommand(watchRemoveCmd)
	watchCmd.AddCommand(watchListCmd)

	watchAddCmd.Flags().StringVar(&watchFlagPriority, "priority", "normal", "watch priority: high, normal, or low")
}

// resolveTarget parses a target string like "#channel-name" or "@username"
// and resolves it to an entity type, ID, and display name using the database.
func resolveTarget(database *db.DB, target string) (entityType, entityID, entityName string, err error) {
	if strings.HasPrefix(target, "#") {
		name := strings.TrimPrefix(target, "#")
		ch, err := database.GetChannelByName(name)
		if err != nil {
			return "", "", "", fmt.Errorf("looking up channel: %w", err)
		}
		if ch == nil {
			return "", "", "", fmt.Errorf("channel %q not found in database (run sync first?)", name)
		}
		return "channel", ch.ID, ch.Name, nil
	}

	if strings.HasPrefix(target, "@") {
		name := strings.TrimPrefix(target, "@")
		u, err := database.GetUserByName(name)
		if err != nil {
			return "", "", "", fmt.Errorf("looking up user: %w", err)
		}
		if u == nil {
			return "", "", "", fmt.Errorf("user %q not found in database (run sync first?)", name)
		}
		return "user", u.ID, u.Name, nil
	}

	// Try channel first, then user
	ch, err := database.GetChannelByName(target)
	if err != nil {
		return "", "", "", fmt.Errorf("looking up channel: %w", err)
	}
	if ch != nil {
		return "channel", ch.ID, ch.Name, nil
	}

	u, err := database.GetUserByName(target)
	if err != nil {
		return "", "", "", fmt.Errorf("looking up user: %w", err)
	}
	if u != nil {
		return "user", u.ID, u.Name, nil
	}

	return "", "", "", fmt.Errorf("%q not found as a channel or user in database (run sync first?)", target)
}

func openDBFromConfig() (*db.DB, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return nil, err
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	return database, nil
}

func runWatchAdd(cmd *cobra.Command, args []string) error {
	priority := strings.ToLower(watchFlagPriority)
	switch priority {
	case "high", "normal", "low":
		// valid
	default:
		return fmt.Errorf("invalid priority %q: must be high, normal, or low", watchFlagPriority)
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, args[0])
	if err != nil {
		return err
	}

	if err := database.AddWatch(entityType, entityID, entityName, priority); err != nil {
		return fmt.Errorf("adding watch: %w", err)
	}

	prefix := "#"
	if entityType == "user" {
		prefix = "@"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Watching %s%s (priority: %s)\n", prefix, entityName, priority)
	return nil
}

func runWatchRemove(cmd *cobra.Command, args []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	entityType, entityID, entityName, err := resolveTarget(database, args[0])
	if err != nil {
		return err
	}

	if err := database.RemoveWatch(entityType, entityID); err != nil {
		return fmt.Errorf("removing watch: %w", err)
	}

	prefix := "#"
	if entityType == "user" {
		prefix = "@"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed %s%s from watch list\n", prefix, entityName)
	return nil
}

func runWatchList(cmd *cobra.Command, args []string) error {
	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	items, err := database.GetWatchList()
	if err != nil {
		return fmt.Errorf("getting watch list: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(items) == 0 {
		fmt.Fprintln(out, "No watched channels or users.")
		return nil
	}

	for _, item := range items {
		prefix := "#"
		if item.EntityType == "user" {
			prefix = "@"
		}
		fmt.Fprintf(out, "%s%-20s  %s  %s\n", prefix, item.EntityName, item.EntityType, item.Priority)
	}
	return nil
}
