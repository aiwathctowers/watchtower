package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage watchtower configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive configuration wizard",
	RunE:  runConfigInit,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value using dot-notation",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	RunE:  runConfigShow,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configShowCmd)
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	configPath := flagConfig

	fmt.Fprintln(cmd.OutOrStdout(), "Watchtower Configuration Wizard")
	fmt.Fprintln(cmd.OutOrStdout(), "================================")
	fmt.Fprintln(cmd.OutOrStdout())

	// Workspace name
	fmt.Fprint(cmd.OutOrStdout(), "Workspace name: ")
	workspace, _ := reader.ReadString('\n')
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return fmt.Errorf("workspace name is required")
	}
	if strings.ContainsAny(workspace, "/\\") || strings.Contains(workspace, "..") {
		return fmt.Errorf("workspace name contains invalid characters")
	}

	// Slack token
	fmt.Fprint(cmd.OutOrStdout(), "Slack token (xoxp-...): ")
	slackToken, _ := reader.ReadString('\n')
	slackToken = strings.TrimSpace(slackToken)
	if slackToken == "" {
		return fmt.Errorf("slack token is required")
	}

	// Anthropic API key
	fmt.Fprint(cmd.OutOrStdout(), "Anthropic API key (or press Enter to skip): ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	// Ensure config directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Write config file
	v := viper.New()
	v.Set("active_workspace", workspace)
	v.Set("workspaces."+workspace+".slack_token", slackToken)
	if apiKey != "" {
		v.Set("ai.api_key", apiKey)
	}
	v.Set("ai.model", "claude-sonnet-4-20250514")
	v.Set("ai.max_tokens", 4096)
	v.Set("ai.context_budget", 150000)
	v.Set("sync.workers", 5)
	v.Set("sync.initial_history_days", 30)
	v.Set("sync.poll_interval", "15m")
	v.Set("sync.sync_threads", true)
	v.Set("sync.sync_on_wake", true)

	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	// Restrict file permissions since it contains secrets (tokens, API keys)
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("setting config file permissions: %w", err)
	}

	// Create DB directory
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	dbDir := filepath.Join(home, ".local", "share", "watchtower", workspace)
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		return fmt.Errorf("creating database directory: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "Config written to: %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Database directory: %s\n", dbDir)
	fmt.Fprintln(cmd.OutOrStdout(), "Run 'watchtower sync' to start syncing your workspace.")

	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]
	configPath := flagConfig

	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	// Parse value to appropriate type so YAML stores typed values
	var typedValue interface{} = value
	if i, err := strconv.Atoi(value); err == nil {
		typedValue = i
	} else if b, err := strconv.ParseBool(value); err == nil {
		typedValue = b
	} else if f, err := strconv.ParseFloat(value, 64); err == nil {
		typedValue = f
	}
	v.Set(key, typedValue)
	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	// Ensure file permissions remain restrictive since config contains secrets
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("setting config file permissions: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", key, value)
	return nil
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	configPath := flagConfig

	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(cmd.OutOrStdout(), "No config file found. Run 'watchtower config init' to create one.")
			return nil
		}
		return fmt.Errorf("reading config: %w", err)
	}

	for _, key := range v.AllKeys() {
		val := fmt.Sprintf("%v", v.Get(key))
		if isSensitiveKey(key) {
			val = maskValue(val)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", key, val)
	}
	return nil
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "token") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "secret")
}

func maskValue(val string) string {
	return "****"
}
