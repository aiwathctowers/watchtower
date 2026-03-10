package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"watchtower/internal/auth"
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
	out := cmd.OutOrStdout()
	configPath := flagConfig

	fmt.Fprintln(out, "Watchtower Configuration Wizard")
	fmt.Fprintln(out, "================================")
	fmt.Fprintln(out)

	reader := bufio.NewReader(cmd.InOrStdin())
	var workspace, slackToken string

	// OAuth credentials: use built-in defaults, allow env var override.
	clientID := os.Getenv("WATCHTOWER_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("WATCHTOWER_OAUTH_CLIENT_SECRET")
	if clientID == "" {
		clientID = auth.DefaultClientID
	}
	if clientSecret == "" {
		clientSecret = auth.DefaultClientSecret
	}

	fmt.Fprintln(out, "How do you want to authenticate?")
	fmt.Fprintln(out, "  [1] OAuth via browser (recommended)")
	fmt.Fprintln(out, "  [2] Paste a Slack token manually")
	fmt.Fprint(out, "Choice [1]: ")
	choice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading auth choice: %w", err)
	}
	choice = strings.TrimSpace(choice)
	if choice == "" {
		choice = "1"
	}

	if choice == "1" {
		result, err := auth.Login(cmd.Context(), auth.OAuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		}, out)
		if err != nil {
			return fmt.Errorf("oauth login: %w", err)
		}

		slackToken = result.AccessToken
		workspace = sanitizeWorkspaceName(result.TeamName)
		if workspace == "" {
			workspace = result.TeamID
		}

		if result.ExpiresIn > 0 {
			fmt.Fprintf(out, "\nWarning: token expires in %d seconds. Token rotation is not yet supported.\n", result.ExpiresIn)
		}

		fmt.Fprintf(out, "Authenticated to workspace %q (team: %s, user: %s)\n\n", workspace, result.TeamID, result.UserID)
	} else {
		// Manual flow
		fmt.Fprint(out, "Workspace name: ")
		var err error
		workspace, err = reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading workspace name: %w", err)
		}
		workspace = strings.TrimSpace(workspace)
		if workspace == "" {
			return fmt.Errorf("workspace name is required")
		}
		if !config.ValidWorkspaceRe.MatchString(workspace) {
			return fmt.Errorf("workspace name contains invalid characters (only letters, numbers, hyphens, dots, underscores allowed)")
		}

		fmt.Fprint(out, "Slack token (xoxb-... or xoxp-...): ")
		slackToken, err = reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading slack token: %w", err)
		}
		slackToken = strings.TrimSpace(slackToken)
		if slackToken == "" {
			return fmt.Errorf("slack token is required")
		}
	}

	// Ensure config directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Write config file
	v := viper.New()
	v.Set("active_workspace", workspace)
	v.Set("workspaces."+workspace+".slack_token", slackToken)
	// OAuth credentials are sourced from env vars at runtime — never persisted to config.
	v.Set("ai.model", config.DefaultAIModel)
	v.Set("ai.context_budget", config.DefaultAIContextBudget)
	v.Set("sync.workers", config.DefaultSyncWorkers)
	v.Set("sync.initial_history_days", config.DefaultInitialHistDays)
	v.Set("sync.poll_interval", config.DefaultPollInterval.String())
	v.Set("sync.sync_threads", config.DefaultSyncThreads)
	v.Set("sync.sync_on_wake", config.DefaultSyncOnWake)
	v.Set("digest.enabled", config.DefaultDigestEnabled)
	v.Set("digest.model", config.DefaultDigestModel)
	v.Set("digest.min_messages", config.DefaultDigestMinMsgs)

	// Write to a temp file with restricted permissions, then atomically rename
	// to avoid a race window where secrets could be world-readable.
	if err := writeConfigAtomic(v, configPath); err != nil {
		return err
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

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Config written to: %s\n", configPath)
	fmt.Fprintf(out, "Database directory: %s\n", dbDir)
	fmt.Fprintln(out, "Run 'watchtower sync' to start syncing your workspace.")

	return nil
}

// knownConfigKeys is the set of recognized configuration keys.
var knownConfigKeys = map[string]bool{
	"active_workspace":          true,
	"ai.model":                  true,
	"ai.context_budget":         true,
	"sync.workers":              true,
	"sync.initial_history_days": true,
	"sync.poll_interval":        true,
	"sync.sync_threads":         true,
	"sync.sync_on_wake":         true,
	"sync.thread_sync_limit":    true,
	"digest.enabled":            true,
	"digest.model":              true,
	"digest.min_messages":       true,
	"digest.language":           true,
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

	// Warn on unrecognized keys (allow workspace-level keys like workspaces.*.slack_token)
	if !knownConfigKeys[key] && !strings.HasPrefix(key, "workspaces.") {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %q is not a recognized config key\n", key)
	}

	// Type-aware parsing: booleans (exact "true"/"false" only), then
	// durations (so "15m" or "900s" are stored correctly), then integers,
	// then fall back to string.
	var typedValue interface{} = value
	if value == "true" {
		typedValue = true
	} else if value == "false" {
		typedValue = false
	} else if i, err := strconv.Atoi(value); err == nil {
		typedValue = i
	} else if _, err := time.ParseDuration(value); err == nil {
		// Store duration strings as-is (e.g., "15m", "900s") so viper
		// can parse them correctly into time.Duration on read.
		typedValue = value
	}
	v.Set(key, typedValue)
	if err := writeConfigAtomic(v, configPath); err != nil {
		return err
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
	fmt.Fprintf(out, "ai.context_budget: %d\n", cfg.AI.ContextBudget)
	fmt.Fprintf(out, "sync.workers: %d\n", cfg.Sync.Workers)
	fmt.Fprintf(out, "sync.initial_history_days: %d\n", cfg.Sync.InitialHistoryDays)
	fmt.Fprintf(out, "sync.poll_interval: %s\n", cfg.Sync.PollInterval)
	fmt.Fprintf(out, "sync.sync_threads: %t\n", cfg.Sync.SyncThreads)
	fmt.Fprintf(out, "sync.sync_on_wake: %t\n", cfg.Sync.SyncOnWake)
	fmt.Fprintf(out, "digest.enabled: %t\n", cfg.Digest.Enabled)
	fmt.Fprintf(out, "digest.model: %s\n", cfg.Digest.Model)
	fmt.Fprintf(out, "digest.min_messages: %d\n", cfg.Digest.MinMessages)
	for name, ws := range cfg.Workspaces {
		if ws.SlackToken != "" {
			fmt.Fprintf(out, "workspaces.%s.slack_token: %s\n", name, maskValue(ws.SlackToken))
		}
	}
	return nil
}

func maskValue(s string) string {
	if len(s) > 5 {
		return s[:5] + "****"
	}
	return "****"
}

// writeConfigAtomic writes viper config to a temp file with 0o600 permissions,
// then atomically renames it into place to avoid a race window where secrets
// could be briefly world-readable.
func writeConfigAtomic(v *viper.Viper, configPath string) error {
	dir := filepath.Dir(configPath)

	// Set restrictive umask before creating the temp file so it's never
	// world-readable, even briefly. Restore the original umask afterward.
	oldMask := syscall.Umask(0o077)
	tmp, err := os.CreateTemp(dir, ".watchtower-config-*.yaml")
	syscall.Umask(oldMask)
	if err != nil {
		return fmt.Errorf("creating temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()

	if err := v.WriteConfigAs(tmpPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing config: %w", err)
	}

	// Ensure permissions are 0600 even if viper recreated the file.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("setting config file permissions: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming config file: %w", err)
	}
	return nil
}
