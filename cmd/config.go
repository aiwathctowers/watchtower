package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/config"

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

	// Type-aware parsing: booleans (exact "true"/"false" only), then
	// durations (so "15m" or "900s" are stored correctly), then integers,
	// then fall back to string.
	var typedValue interface{} = value
	if value == "true" {
		typedValue = true
	} else if value == "false" {
		typedValue = false
	} else if _, err := time.ParseDuration(value); err == nil {
		// Store duration strings as-is (e.g., "15m", "900s") so viper
		// can parse them correctly into time.Duration on read.
		typedValue = value
	} else if i, err := strconv.Atoi(value); err == nil {
		typedValue = i
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
	// Check if config file exists before loading
	if _, err := os.Stat(flagConfig); os.IsNotExist(err) {
		fmt.Fprintln(cmd.OutOrStdout(), "No config file found. Run 'watchtower config init' to create one.")
		return nil
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "active_workspace: %s\n", cfg.ActiveWorkspace)
	fmt.Fprintf(out, "ai.model: %s\n", cfg.AI.Model)
	fmt.Fprintf(out, "ai.max_tokens: %d\n", cfg.AI.MaxTokens)
	fmt.Fprintf(out, "ai.context_budget: %d\n", cfg.AI.ContextBudget)
	if cfg.AI.ApiKey != "" {
		fmt.Fprintf(out, "ai.api_key: %s\n", maskValue(cfg.AI.ApiKey))
	}
	fmt.Fprintf(out, "sync.workers: %d\n", cfg.Sync.Workers)
	fmt.Fprintf(out, "sync.initial_history_days: %d\n", cfg.Sync.InitialHistoryDays)
	fmt.Fprintf(out, "sync.poll_interval: %s\n", cfg.Sync.PollInterval)
	fmt.Fprintf(out, "sync.sync_threads: %t\n", cfg.Sync.SyncThreads)
	fmt.Fprintf(out, "sync.sync_on_wake: %t\n", cfg.Sync.SyncOnWake)
	for name, ws := range cfg.Workspaces {
		if ws.SlackToken != "" {
			fmt.Fprintf(out, "workspaces.%s.slack_token: %s\n", name, maskValue(ws.SlackToken))
		}
	}
	return nil
}

func maskValue(val string) string {
	return "****"
}
