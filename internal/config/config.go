package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type WorkspaceConfig struct {
	SlackToken string `mapstructure:"slack_token"`
}

type AIConfig struct {
	Model         string `mapstructure:"model"`
	ContextBudget int    `mapstructure:"context_budget"`
}

type SyncConfig struct {
	Workers            int           `mapstructure:"workers"`
	InitialHistoryDays int           `mapstructure:"initial_history_days"`
	PollInterval       time.Duration `mapstructure:"poll_interval"`
	SyncThreads        bool          `mapstructure:"sync_threads"`
	SyncOnWake         bool          `mapstructure:"sync_on_wake"`
	ThreadSyncLimit    int           `mapstructure:"thread_sync_limit"`
}

type DigestConfig struct {
	Enabled             bool          `mapstructure:"enabled"`
	Model               string        `mapstructure:"model"`
	MinMessages         int           `mapstructure:"min_messages"`
	Language            string        `mapstructure:"language"`
	Workers             int           `mapstructure:"workers"`
	ActionItemsInterval time.Duration `mapstructure:"action_items_interval"`
}

type Config struct {
	ActiveWorkspace string                      `mapstructure:"active_workspace"`
	Workspaces      map[string]*WorkspaceConfig `mapstructure:"workspaces"`
	AI              AIConfig                    `mapstructure:"ai"`
	Sync            SyncConfig                  `mapstructure:"sync"`
	Digest          DigestConfig                `mapstructure:"digest"`
	ClaudePath      string                      `mapstructure:"claude_path"`
}

// Load reads config from the given path, binds env vars, and returns the config.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("active_workspace", DefaultActiveWorkspace)
	v.SetDefault("ai.model", DefaultAIModel)
	v.SetDefault("ai.context_budget", DefaultAIContextBudget)
	v.SetDefault("sync.workers", DefaultSyncWorkers)
	v.SetDefault("sync.initial_history_days", DefaultInitialHistDays)
	v.SetDefault("sync.poll_interval", DefaultPollInterval)
	v.SetDefault("sync.sync_threads", DefaultSyncThreads)
	v.SetDefault("sync.sync_on_wake", DefaultSyncOnWake)
	v.SetDefault("digest.enabled", DefaultDigestEnabled)
	v.SetDefault("digest.model", DefaultDigestModel)
	v.SetDefault("digest.min_messages", DefaultDigestMinMsgs)
	v.SetDefault("digest.language", DefaultDigestLang)
	v.SetDefault("digest.workers", DefaultDigestWorkers)
	v.SetDefault("digest.action_items_interval", DefaultActionItemsInterval)

	// Config file
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		// Missing config file is OK — use defaults
		var configNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configNotFound) && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	// Env var bindings
	v.SetEnvPrefix("WATCHTOWER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Explicit bindings for key env vars
	_ = v.BindEnv("ai.model", "WATCHTOWER_AI_MODEL")
	_ = v.BindEnv("sync.workers", "WATCHTOWER_SYNC_WORKERS")
	_ = v.BindEnv("digest.model", "WATCHTOWER_DIGEST_MODEL")

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Bind workspace-level slack token from env
	if token := os.Getenv("WATCHTOWER_SLACK_TOKEN"); token != "" && cfg.ActiveWorkspace != "" {
		if cfg.Workspaces == nil {
			cfg.Workspaces = make(map[string]*WorkspaceConfig)
		}
		ws, ok := cfg.Workspaces[cfg.ActiveWorkspace]
		if !ok {
			ws = &WorkspaceConfig{}
			cfg.Workspaces[cfg.ActiveWorkspace] = ws
		}
		if ws.SlackToken == "" {
			ws.SlackToken = token
		}
	}

	return cfg, nil
}

// ValidWorkspaceRe matches valid workspace names: alphanumeric start, followed by
// alphanumerics, hyphens, dots, or underscores.
var ValidWorkspaceRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ValidateWorkspace checks that a workspace name is set and safe for use in
// file paths. It does NOT require a Slack token or workspace config entry,
// making it suitable for commands that only need database access.
func (c *Config) ValidateWorkspace() error {
	if c.ActiveWorkspace == "" {
		return fmt.Errorf("active_workspace is required; run 'watchtower config init' first")
	}
	if !ValidWorkspaceRe.MatchString(c.ActiveWorkspace) {
		return fmt.Errorf("invalid workspace name %q: must contain only alphanumeric characters, hyphens, dots, and underscores", c.ActiveWorkspace)
	}
	return nil
}

// Validate checks that required fields are present, including Slack token.
// Use ValidateWorkspace for commands that only need database access.
func (c *Config) Validate() error {
	if err := c.ValidateWorkspace(); err != nil {
		return err
	}
	ws, err := c.GetActiveWorkspace()
	if err != nil {
		return err
	}
	if ws.SlackToken == "" {
		return fmt.Errorf("slack_token is required for workspace %q", c.ActiveWorkspace)
	}
	if !isValidSlackToken(ws.SlackToken) {
		return fmt.Errorf("slack_token for workspace %q has invalid format (expected xoxp-*, xoxb-*, xoxa-*, or xoxe.*)", c.ActiveWorkspace)
	}
	return nil
}

// isValidSlackToken checks that the token has a recognized Slack token prefix.
func isValidSlackToken(token string) bool {
	validPrefixes := []string{"xoxp-", "xoxb-", "xoxa-", "xoxe.xoxp-", "xoxe."}
	for _, p := range validPrefixes {
		if strings.HasPrefix(token, p) {
			return true
		}
	}
	return false
}

// GetActiveWorkspace returns the config for the active workspace.
func (c *Config) GetActiveWorkspace() (*WorkspaceConfig, error) {
	if c.ActiveWorkspace == "" {
		return nil, fmt.Errorf("no active workspace set")
	}
	ws, ok := c.Workspaces[c.ActiveWorkspace]
	if !ok {
		return nil, fmt.Errorf("workspace %q not found in config", c.ActiveWorkspace)
	}
	return ws, nil
}

// WorkspaceDir returns the data directory for the active workspace
// (~/.local/share/watchtower/{workspace}/).
func (c *Config) WorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fatal: storing sensitive data in a temp dir is unsafe.
		log.Fatalf("could not determine home directory: %v", err)
	}
	return filepath.Join(home, ".local", "share", "watchtower", c.ActiveWorkspace)
}

// DBPath returns the path to the SQLite database for the active workspace.
func (c *Config) DBPath() string {
	return filepath.Join(c.WorkspaceDir(), "watchtower.db")
}
