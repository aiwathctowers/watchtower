// Package cmd contains the command-line interface for Watchtower.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/repl"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	flagWorkspace string
	flagConfig    string
	flagVerbose   bool
	flagProvider  string
)

var rootCmd = &cobra.Command{
	Use:               "watchtower",
	Short:             "Slack workspace intelligence tool",
	Long:              "Watchtower syncs a Slack workspace into a local SQLite database and provides an AI-powered interface for analysis via the Claude API.",
	PersistentPreRunE: ensureSchemaFormat,
	RunE:              runREPL,
}

// ensureSchemaFormat runs once before every command. When the on-disk
// db.schema_format flag is below db.CurrentSchemaFormat, it invokes the
// one-shot RunSchemaUpgrade against the live DB (idempotent) and bumps
// the flag in config. Skips silently when no config / no DB exists.
func ensureSchemaFormat(_ *cobra.Command, _ []string) error {
	if _, err := os.Stat(flagConfig); err != nil {
		return nil
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.DB.SchemaFormat >= db.CurrentSchemaFormat {
		return nil
	}

	dbPath := cfg.DBPath()
	if _, err := os.Stat(dbPath); err == nil {
		if err := db.RunSchemaUpgrade(dbPath); err != nil {
			return fmt.Errorf("schema upgrade: %w", err)
		}
	}

	v := viper.New()
	v.SetConfigFile(flagConfig)
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("re-reading config for schema_format bump: %w", err)
	}
	v.Set("db.schema_format", db.CurrentSchemaFormat)
	if err := writeConfigAtomic(v, flagConfig); err != nil {
		return fmt.Errorf("persisting schema_format: %w", err)
	}
	return nil
}

// Execute runs the root command and returns the exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func init() {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	rootCmd.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "workspace name to use")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", defaultConfigPath(), "path to config file")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "enable verbose output")
	rootCmd.PersistentFlags().StringVar(&flagProvider, "provider", "", "AI provider to use (claude|codex)")
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "watchtower", "config.yaml")
}

func runREPL(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	applyProviderOverride(cfg)
	if err := cfg.ValidateWorkspace(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	ws, err := database.GetWorkspace()
	if err != nil {
		return fmt.Errorf("getting workspace: %w", err)
	}

	domain := ""
	teamID := ""
	workspace := cfg.ActiveWorkspace
	if ws != nil {
		domain = ws.Domain
		teamID = ws.ID
		workspace = ws.Name
	}

	deps := repl.Deps{
		Config:     cfg,
		DB:         database,
		DBPath:     cfg.DBPath(),
		Domain:     domain,
		TeamID:     teamID,
		Workspace:  workspace,
		AIProvider: newAIClient(cfg, cfg.DBPath()),
	}

	return repl.Run(deps)
}
