package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type WorkspaceConfig struct {
	SlackToken string `mapstructure:"slack_token"`
}

type AIConfig struct {
	ApiKey        string `mapstructure:"api_key"`
	Model         string `mapstructure:"model"`
	MaxTokens     int    `mapstructure:"max_tokens"`
	ContextBudget int    `mapstructure:"context_budget"`
}

type SyncConfig struct {
	Workers           int           `mapstructure:"workers"`
	InitialHistoryDays int          `mapstructure:"initial_history_days"`
	PollInterval      time.Duration `mapstructure:"poll_interval"`
	SyncThreads       bool          `mapstructure:"sync_threads"`
	SyncOnWake        bool          `mapstructure:"sync_on_wake"`
}

type WatchChannel struct {
	Name     string `mapstructure:"name"`
	Priority string `mapstructure:"priority"`
}

type WatchUser struct {
	Name     string `mapstructure:"name"`
	Priority string `mapstructure:"priority"`
}

type WatchConfig struct {
	Channels []WatchChannel `mapstructure:"channels"`
	Users    []WatchUser    `mapstructure:"users"`
}

type Config struct {
	ActiveWorkspace string                      `mapstructure:"active_workspace"`
	Workspaces      map[string]*WorkspaceConfig `mapstructure:"workspaces"`
	AI              AIConfig                    `mapstructure:"ai"`
	Sync            SyncConfig                  `mapstructure:"sync"`
	Watch           WatchConfig                 `mapstructure:"watch"`
}

// Load reads config from the given path, binds env vars, and returns the config.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("active_workspace", DefaultActiveWorkspace)
	v.SetDefault("ai.model", DefaultAIModel)
	v.SetDefault("ai.max_tokens", DefaultAIMaxTokens)
	v.SetDefault("ai.context_budget", DefaultAIContextBudget)
	v.SetDefault("sync.workers", DefaultSyncWorkers)
	v.SetDefault("sync.initial_history_days", DefaultInitialHistDays)
	v.SetDefault("sync.poll_interval", DefaultPollInterval)
	v.SetDefault("sync.sync_threads", DefaultSyncThreads)
	v.SetDefault("sync.sync_on_wake", DefaultSyncOnWake)

	// Config file
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config: %w", err)
			}
		}
	}

	// Env var bindings
	v.SetEnvPrefix("WATCHTOWER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Explicit bindings for key env vars
	_ = v.BindEnv("ai.api_key", "ANTHROPIC_API_KEY")
	_ = v.BindEnv("ai.model", "WATCHTOWER_AI_MODEL")
	_ = v.BindEnv("sync.workers", "WATCHTOWER_SYNC_WORKERS")

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

// Validate checks that required fields are present.
func (c *Config) Validate() error {
	if c.ActiveWorkspace == "" {
		return fmt.Errorf("active_workspace is required")
	}
	if strings.ContainsAny(c.ActiveWorkspace, "/\\") || strings.Contains(c.ActiveWorkspace, "..") {
		return fmt.Errorf("active_workspace %q contains invalid characters", c.ActiveWorkspace)
	}
	ws, err := c.GetActiveWorkspace()
	if err != nil {
		return err
	}
	if ws.SlackToken == "" {
		return fmt.Errorf("slack_token is required for workspace %q", c.ActiveWorkspace)
	}
	return nil
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

// DBPath returns the path to the SQLite database for the active workspace.
func (c *Config) DBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "watchtower", c.ActiveWorkspace, "watchtower.db")
}
