package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Show current user profile",
	Long: `Display the current user profile used for personalization.

The profile includes your role, team, reports, peers, manager,
starred channels/people, and other settings that influence
how Watchtower prioritizes tracks and generates insights.`,
	RunE: runProfile,
}

func init() {
	rootCmd.AddCommand(profileCmd)
}

func runProfile(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	// Get current user ID from workspace.
	ws, err := database.GetWorkspace()
	if err != nil {
		return fmt.Errorf("getting workspace: %w", err)
	}
	if ws == nil || ws.CurrentUserID == "" {
		return fmt.Errorf("no workspace found — run 'watchtower sync' first")
	}

	out := cmd.OutOrStdout()

	profile, err := database.GetUserProfile(ws.CurrentUserID)
	if err != nil {
		return fmt.Errorf("getting profile: %w", err)
	}
	if profile == nil {
		fmt.Fprintln(out, "No profile configured yet.")
		fmt.Fprintln(out, "Set up your profile in the Desktop app (Settings > Profile).")
		return nil
	}

	fmt.Fprintf(out, "Profile for %s\n\n", profile.SlackUserID)

	if profile.Role != "" {
		fmt.Fprintf(out, "  Role:             %s\n", profile.Role)
	}
	if profile.Team != "" {
		fmt.Fprintf(out, "  Team:             %s\n", profile.Team)
	}
	if profile.Manager != "" {
		fmt.Fprintf(out, "  Manager:          %s\n", profile.Manager)
	}
	printJSONList(out, "  Reports:          ", profile.Reports)
	printJSONList(out, "  Peers:            ", profile.Peers)
	printJSONList(out, "  Responsibilities: ", profile.Responsibilities)
	printJSONList(out, "  Starred channels: ", profile.StarredChannels)
	printJSONList(out, "  Starred people:   ", profile.StarredPeople)
	printJSONList(out, "  Pain points:      ", profile.PainPoints)
	printJSONList(out, "  Track focus:      ", profile.TrackFocus)

	if profile.OnboardingDone {
		fmt.Fprintln(out, "\n  Onboarding: done")
	} else {
		fmt.Fprintln(out, "\n  Onboarding: not completed")
	}

	if profile.CustomPromptContext != "" {
		fmt.Fprintf(out, "\n  Prompt context:\n    %s\n", profile.CustomPromptContext)
	}

	return nil
}

// printJSONList prints a JSON array as a comma-separated line. Skips if empty.
func printJSONList(w interface{ Write([]byte) (int, error) }, prefix, jsonArr string) {
	if jsonArr == "" || jsonArr == "[]" {
		return
	}
	var items []string
	if err := json.Unmarshal([]byte(jsonArr), &items); err != nil {
		fmt.Fprintf(w, "%s(invalid JSON)\n", prefix)
		return
	}
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "%s%s\n", prefix, strings.Join(items, ", "))
}
