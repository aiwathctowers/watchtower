package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"watchtower/internal/auth"
	"watchtower/internal/config"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Slack via OAuth",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Slack via browser-based OAuth flow",
	Long: `Starts a browser-based OAuth flow to obtain a Slack user token.

Uses the built-in Watchtower Slack app by default. Override with
WATCHTOWER_OAUTH_CLIENT_ID and WATCHTOWER_OAUTH_CLIENT_SECRET env vars.`,
	RunE: runAuthLogin,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLoginCmd)
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	clientID := os.Getenv("WATCHTOWER_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("WATCHTOWER_OAUTH_CLIENT_SECRET")
	if clientID == "" {
		clientID = auth.DefaultClientID
	}
	if clientSecret == "" {
		clientSecret = auth.DefaultClientSecret
	}
	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("OAuth credentials not configured. Set WATCHTOWER_OAUTH_CLIENT_ID and WATCHTOWER_OAUTH_CLIENT_SECRET environment variables, or use an official release build")
	}

	cfg := auth.OAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}

	out := cmd.OutOrStdout()
	result, err := auth.Login(cmd.Context(), cfg, out)
	if err != nil {
		return fmt.Errorf("oauth login: %w", err)
	}

	if result.ExpiresIn > 0 {
		fmt.Fprintf(out, "\nWarning: token expires in %d seconds. Token rotation is not yet supported.\n", result.ExpiresIn)
	}

	workspace := sanitizeWorkspaceName(result.TeamName)
	if workspace == "" {
		workspace = result.TeamID
	}

	// Save token to config, merging with existing config if present
	configPath := flagConfig
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	_ = v.ReadInConfig() // ignore error if file doesn't exist yet

	v.Set("active_workspace", workspace)
	v.Set("workspaces."+workspace+".slack_token", result.AccessToken)

	// Set defaults only if not already configured (use constants from config/defaults.go)
	defaults := map[string]any{
		"ai.model":                  config.DefaultAIModel,
		"ai.context_budget":         config.DefaultAIContextBudget,
		"sync.workers":              config.DefaultSyncWorkers,
		"sync.initial_history_days": config.DefaultInitialHistDays,
		"sync.poll_interval":        config.DefaultPollInterval.String(),
		"sync.sync_threads":         config.DefaultSyncThreads,
		"sync.sync_on_wake":         config.DefaultSyncOnWake,
		"digest.enabled":            config.DefaultDigestEnabled,
		"digest.model":              config.DefaultDigestModel,
		"digest.min_messages":       config.DefaultDigestMinMsgs,
	}
	for key, val := range defaults {
		if !v.IsSet(key) {
			v.Set(key, val)
		}
	}

	// Atomic write to avoid a race window where secrets could be world-readable
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

	fmt.Fprintf(out, "\nLogged in to workspace %q (team: %s, user: %s)\n", workspace, result.TeamID, result.UserID)
	fmt.Fprintf(out, "Config written to: %s\n", configPath)
	fmt.Fprintf(out, "Run 'watchtower sync' to start syncing.\n")

	return nil
}

var sanitizeRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitizeWorkspaceName converts a team name to a safe config key:
// lowercase, special characters replaced with hyphens.
func sanitizeWorkspaceName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = sanitizeRe.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	return name
}
