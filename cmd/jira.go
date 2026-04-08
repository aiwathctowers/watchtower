package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var jiraCmd = &cobra.Command{
	Use:   "jira",
	Short: "Jira Cloud integration",
	RunE:  runJiraStatus,
}

var jiraLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Connect Jira Cloud via OAuth",
	RunE:  runJiraLogin,
}

var jiraLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Disconnect Jira Cloud",
	RunE:  runJiraLogout,
}

var jiraStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Jira connection status",
	RunE:  runJiraStatus,
}

var jiraBoardsCmd = &cobra.Command{
	Use:   "boards",
	Short: "List Jira boards",
	RunE:  runJiraBoards,
}

var jiraBoardsSelectCmd = &cobra.Command{
	Use:   "select [board-ids...]",
	Short: "Select boards for sync",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runJiraBoardsSelect,
}

var jiraBoardsDeselectCmd = &cobra.Command{
	Use:   "deselect [board-ids...]",
	Short: "Deselect boards from sync",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runJiraBoardsDeselect,
}

var jiraUsersCmd = &cobra.Command{
	Use:   "users",
	Short: "Show Jira-to-Slack user mappings",
	RunE:  runJiraUsers,
}

var jiraUsersMapCmd = &cobra.Command{
	Use:   "map <jira_account_id> <slack_user_id>",
	Short: "Manually map a Jira user to a Slack user",
	Args:  cobra.ExactArgs(2),
	RunE:  runJiraUsersMap,
}

var jiraSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Manually sync Jira issues",
	RunE:  runJiraSync,
}

var jiraFeaturesCmd = &cobra.Command{
	Use:   "features",
	Short: "Show Jira feature toggles",
	RunE:  runJiraFeatures,
}

var jiraFeaturesEnableCmd = &cobra.Command{
	Use:   "enable <feature>",
	Short: "Enable a Jira feature",
	Args:  cobra.ExactArgs(1),
	RunE:  runJiraFeaturesEnable,
}

var jiraFeaturesDisableCmd = &cobra.Command{
	Use:   "disable <feature>",
	Short: "Disable a Jira feature",
	Args:  cobra.ExactArgs(1),
	RunE:  runJiraFeaturesDisable,
}

var jiraFeaturesResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset feature toggles to role defaults",
	RunE:  runJiraFeaturesReset,
}

var jiraBoardsAnalyzeCmd = &cobra.Command{
	Use:   "analyze [board-ids...]",
	Short: "Analyze board workflow with LLM",
	RunE:  runJiraBoardsAnalyze,
}

var jiraBoardsOverrideCmd = &cobra.Command{
	Use:   "override <boardID>",
	Short: "Set user overrides for a board",
	Args:  cobra.ExactArgs(1),
	RunE:  runJiraBoardsOverride,
}

func init() {
	rootCmd.AddCommand(jiraCmd)
	jiraCmd.AddCommand(jiraLoginCmd)
	jiraCmd.AddCommand(jiraLogoutCmd)
	jiraCmd.AddCommand(jiraStatusCmd)
	jiraCmd.AddCommand(jiraBoardsCmd)
	jiraBoardsCmd.AddCommand(jiraBoardsSelectCmd)
	jiraBoardsCmd.AddCommand(jiraBoardsDeselectCmd)
	jiraBoardsCmd.AddCommand(jiraBoardsAnalyzeCmd)
	jiraBoardsCmd.AddCommand(jiraBoardsOverrideCmd)
	jiraCmd.AddCommand(jiraUsersCmd)
	jiraUsersCmd.AddCommand(jiraUsersMapCmd)
	jiraCmd.AddCommand(jiraSyncCmd)
	jiraCmd.AddCommand(jiraFeaturesCmd)
	jiraFeaturesCmd.AddCommand(jiraFeaturesEnableCmd)
	jiraFeaturesCmd.AddCommand(jiraFeaturesDisableCmd)
	jiraFeaturesCmd.AddCommand(jiraFeaturesResetCmd)

	jiraLoginCmd.Flags().Bool("no-open", false, "don't open the browser automatically")
	jiraLoginCmd.Flags().String("site", "", "select Jira site by URL (e.g. https://mysite.atlassian.net)")
	jiraFeaturesCmd.Flags().Bool("json", false, "output as JSON (for Swift integration)")
	jiraBoardsAnalyzeCmd.Flags().Bool("force", false, "re-analyze even if config hash unchanged")
	jiraBoardsOverrideCmd.Flags().String("stale", "", "stale thresholds (e.g. 'Code Review=1,QA=2')")
}

func runJiraLogin(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	jiraCfg := resolveJiraOAuthConfig()
	noOpen, _ := cmd.Flags().GetBool("no-open")
	out := cmd.OutOrStdout()

	token, err := jira.Login(cmd.Context(), jiraCfg, out, jira.LoginOptions{SkipBrowserOpen: noOpen})
	if err != nil {
		return fmt.Errorf("jira login: %w", err)
	}

	store := jira.NewTokenStore(cfg.WorkspaceDir())
	if err := store.Save(token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	// Fetch accessible resources to get cloud ID.
	resources, err := jira.FetchAccessibleResources(cmd.Context(), token.AccessToken)
	if err != nil {
		return fmt.Errorf("fetching accessible resources: %w", err)
	}

	if len(resources) == 0 {
		fmt.Fprintln(out, "No Jira Cloud sites found for this account.")
		return nil
	}

	// Select site — flag, auto if one, prompt if multiple.
	var site jira.CloudResource
	siteFlag, _ := cmd.Flags().GetString("site")
	if siteFlag != "" {
		found := false
		for _, r := range resources {
			if strings.Contains(r.URL, siteFlag) || strings.Contains(r.Name, siteFlag) {
				site = r
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintln(out, "Available sites:")
			for _, r := range resources {
				fmt.Fprintf(out, "  - %s (%s)\n", r.Name, r.URL)
			}
			return fmt.Errorf("site %q not found", siteFlag)
		}
	} else if len(resources) == 1 {
		site = resources[0]
	} else {
		fmt.Fprintln(out, "\nAvailable Jira Cloud sites:")
		for i, r := range resources {
			fmt.Fprintf(out, "  [%d] %s (%s)\n", i+1, r.Name, r.URL)
		}
		fmt.Fprintf(out, "\nSelect site [1-%d]: ", len(resources))
		var choice int
		if _, err := fmt.Fscan(cmd.InOrStdin(), &choice); err != nil || choice < 1 || choice > len(resources) {
			return fmt.Errorf("invalid selection")
		}
		site = resources[choice-1]
	}

	// Persist Jira config so downstream commands (boards, sync) can find cloud_id.
	v := viper.New()
	v.SetConfigFile(flagConfig)
	_ = v.ReadInConfig()
	v.Set("jira.cloud_id", site.ID)
	v.Set("jira.site_url", site.URL)
	v.Set("jira.user_display_name", site.Name)
	v.Set("jira.enabled", true)
	if err := writeConfigAtomic(v, flagConfig); err != nil {
		return fmt.Errorf("saving jira config: %w", err)
	}

	fmt.Fprintf(out, "\nJira Cloud connected!\n")
	fmt.Fprintf(out, "Site: %s (%s)\n", site.Name, site.URL)
	fmt.Fprintf(out, "Cloud ID: %s\n", site.ID)
	fmt.Fprintf(out, "Token saved to: %s\n", store.Path())
	fmt.Fprintf(out, "\nRun 'watchtower jira boards' to see available boards.\n")
	fmt.Fprintf(out, "Run 'watchtower jira sync' to sync issues.\n")

	return nil
}

func runJiraLogout(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	store := jira.NewTokenStore(cfg.WorkspaceDir())
	if err := store.Delete(); err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	if err := database.ClearJiraData(); err != nil {
		return fmt.Errorf("clearing jira data: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Jira Cloud disconnected. Token and data removed.")
	return nil
}

func runJiraStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	store := jira.NewTokenStore(cfg.WorkspaceDir())

	if !store.Exists() {
		fmt.Fprintln(out, "Jira Cloud: not connected")
		fmt.Fprintln(out, "Run 'watchtower jira login' to connect.")
		return nil
	}

	fmt.Fprintln(out, "Jira Cloud: connected")
	fmt.Fprintf(out, "Token file: %s\n", store.Path())
	fmt.Fprintf(out, "Enabled: %v\n", cfg.Jira.Enabled)

	if cfg.Jira.SiteURL != "" {
		fmt.Fprintf(out, "Site: %s\n", cfg.Jira.SiteURL)
	}
	if cfg.Jira.UserDisplayName != "" {
		fmt.Fprintf(out, "User: %s\n", cfg.Jira.UserDisplayName)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil // non-fatal
	}
	defer database.Close()

	boards, _ := database.GetJiraSelectedBoards()
	if len(boards) > 0 {
		names := make([]string, len(boards))
		for i, b := range boards {
			names[i] = b.Name
		}
		fmt.Fprintf(out, "Selected boards: %s\n", strings.Join(names, ", "))
	}

	issueCount, _ := database.GetJiraIssueCount()
	fmt.Fprintf(out, "Issues synced: %d\n", issueCount)

	states, _ := database.GetJiraSyncStates()
	for _, s := range states {
		if s.LastSyncedAt != "" {
			fmt.Fprintf(out, "Last sync (%s): %s\n", s.ProjectKey, s.LastSyncedAt)
		}
	}

	return nil
}

func runJiraBoards(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	store := jira.NewTokenStore(cfg.WorkspaceDir())
	if !store.Exists() {
		fmt.Fprintln(cmd.OutOrStdout(), "Jira not connected. Run 'watchtower jira login' first.")
		return nil
	}

	// Fetch boards from API and update DB.
	jiraCfg := resolveJiraOAuthConfig()
	if cfg.Jira.CloudID != "" {
		client := jira.NewClient(cfg.Jira.CloudID, jiraCfg, store)
		boards, err := client.FetchAllBoards(cmd.Context())
		if err != nil {
			return fmt.Errorf("fetching boards: %w", err)
		}

		for _, b := range boards {
			dbBoard := db.JiraBoard{
				ID:         b.ID,
				Name:       b.Name,
				ProjectKey: b.Location.ProjectKey,
				BoardType:  b.Type,
				SyncedAt:   "now",
			}
			_ = database.UpsertJiraBoard(dbBoard)
		}
	}

	boards, err := database.GetJiraBoards()
	if err != nil {
		return fmt.Errorf("querying boards: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(boards) == 0 {
		fmt.Fprintln(out, "No boards found.")
		return nil
	}

	fmt.Fprintf(out, "%-6s %-30s %-12s %-10s %-8s %-8s\n", "#", "Name", "Project", "Type", "Issues", "Selected")
	fmt.Fprintf(out, "%-6s %-30s %-12s %-10s %-8s %-8s\n", "------", "------------------------------", "------------", "----------", "--------", "--------")
	for _, b := range boards {
		selected := " "
		if b.IsSelected {
			selected = "*"
		}
		fmt.Fprintf(out, "%-6d %-30s %-12s %-10s %-8d %-8s\n",
			b.ID, truncate(b.Name, 30), b.ProjectKey, b.BoardType, b.IssueCount, selected)
	}
	return nil
}

func runJiraBoardsSelect(cmd *cobra.Command, args []string) error {
	return setJiraBoardSelection(cmd, args, true)
}

func runJiraBoardsDeselect(cmd *cobra.Command, args []string) error {
	return setJiraBoardSelection(cmd, args, false)
}

func setJiraBoardSelection(cmd *cobra.Command, args []string, selected bool) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	for _, arg := range args {
		id, err := strconv.Atoi(arg)
		if err != nil {
			return fmt.Errorf("invalid board ID %q: %w", arg, err)
		}
		if err := database.SetJiraBoardSelected(id, selected); err != nil {
			return fmt.Errorf("updating board %d: %w", id, err)
		}
		action := "selected"
		if !selected {
			action = "deselected"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Board %d %s.\n", id, action)
	}
	return nil
}

func runJiraUsers(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	maps, err := database.GetJiraUserMaps()
	if err != nil {
		return fmt.Errorf("querying user maps: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(maps) == 0 {
		fmt.Fprintln(out, "No Jira user mappings. Run 'watchtower jira sync' first.")
		return nil
	}

	fmt.Fprintf(out, "%-25s %-30s %-15s %-10s %-10s\n", "Jira Name", "Email", "Slack User", "Match", "Confidence")
	fmt.Fprintf(out, "%-25s %-30s %-15s %-10s %-10s\n", "-------------------------", "------------------------------", "---------------", "----------", "----------")
	for _, m := range maps {
		confidence := ""
		if m.MatchConfidence > 0 {
			confidence = fmt.Sprintf("%.0f%%", m.MatchConfidence*100)
		}
		slackUser := m.SlackUserID
		if slackUser == "" {
			slackUser = "-"
		}
		fmt.Fprintf(out, "%-25s %-30s %-15s %-10s %-10s\n",
			truncate(m.DisplayName, 25), truncate(m.Email, 30), slackUser, m.MatchMethod, confidence)
	}
	return nil
}

func runJiraUsersMap(cmd *cobra.Command, args []string) error {
	jiraAccountID := args[0]
	slackUserID := args[1]

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	mapping := db.JiraUserMap{
		JiraAccountID:   jiraAccountID,
		SlackUserID:     slackUserID,
		MatchMethod:     "manual",
		MatchConfidence: 1.0,
		ResolvedAt:      now,
	}
	if err := database.UpsertJiraUserMap(mapping); err != nil {
		return fmt.Errorf("upserting user map: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Mapped Jira user %s → Slack user %s (manual, confidence=1.0)\n", jiraAccountID, slackUserID)
	return nil
}

func runJiraSync(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	if cfg.Jira.CloudID == "" {
		return fmt.Errorf("jira cloud_id not configured, run 'watchtower jira login' first")
	}

	store := jira.NewTokenStore(cfg.WorkspaceDir())
	if !store.Exists() {
		return fmt.Errorf("jira not connected, run 'watchtower jira login' first")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	jiraCfg := resolveJiraOAuthConfig()
	client := jira.NewClient(cfg.Jira.CloudID, jiraCfg, store)
	mapper := jira.NewUserMapper(client, database)

	// Get selected board IDs.
	boards, err := database.GetJiraSelectedBoards()
	if err != nil {
		return fmt.Errorf("getting selected boards: %w", err)
	}

	boardIDs := make([]int, len(boards))
	for i, b := range boards {
		boardIDs[i] = b.ID
	}

	syncer := jira.NewSyncer(client, database, mapper, boardIDs)

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Syncing Jira issues...")

	count, err := syncer.Sync(cmd.Context())
	if err != nil {
		return fmt.Errorf("syncing: %w", err)
	}

	fmt.Fprintf(out, "Synced %d Jira issues.\n", count)

	// Resolve users after sync.
	if err := mapper.ResolveAll(cmd.Context(), cfg.Jira.UserMap); err != nil {
		fmt.Fprintf(out, "Warning: user mapping failed: %v\n", err)
	}

	return nil
}

// resolveJiraOAuthConfig returns Jira OAuth credentials from env or ldflags.
func resolveJiraOAuthConfig() jira.JiraOAuthConfig {
	clientID := os.Getenv("WATCHTOWER_JIRA_CLIENT_ID")
	if clientID == "" {
		clientID = jira.DefaultJiraClientID
	}
	clientSecret := os.Getenv("WATCHTOWER_JIRA_CLIENT_SECRET")
	if clientSecret == "" {
		clientSecret = jira.DefaultJiraClientSecret
	}
	return jira.JiraOAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}
}

// featureToggleRef returns a pointer to a feature toggle field by name.
func featureToggleRef(f *config.JiraFeatureToggles, name string) (*bool, bool) {
	switch name {
	case "my_issues", "my_issues_in_briefing":
		return &f.MyIssuesInBriefing, true
	case "awaiting_input", "awaiting_my_input":
		return &f.AwaitingMyInput, true
	case "who_ping":
		return &f.WhoPing, true
	case "track_linking", "track_jira_linking":
		return &f.TrackJiraLinking, true
	case "team_workload":
		return &f.TeamWorkload, true
	case "blocker_map":
		return &f.BlockerMap, true
	case "iteration_progress":
		return &f.IterationProgress, true
	case "epic_progress":
		return &f.EpicProgress, true
	case "write_back", "write_back_suggestions":
		return &f.WriteBackSuggestions, true
	case "release_dashboard":
		return &f.ReleaseDashboard, true
	case "without_jira", "without_jira_detection":
		return &f.WithoutJiraDetection, true
	default:
		return nil, false
	}
}

// featureNames is the ordered list of feature toggle short names.
var featureNames = []string{
	"my_issues", "awaiting_input", "who_ping", "track_linking",
	"team_workload", "blocker_map", "iteration_progress", "epic_progress",
	"write_back", "release_dashboard", "without_jira",
}

func runJiraFeatures(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	// Determine role from DB user profile.
	role := config.DefaultJiraFeaturesRole
	database, err := db.Open(cfg.DBPath())
	if err == nil {
		defer database.Close()
		userID, _ := database.GetCurrentUserID()
		if userID != "" {
			if profile, _ := database.GetUserProfile(userID); profile != nil && profile.Role != "" {
				role = profile.Role
			}
		}
	}

	features := cfg.Jira.Features
	defaults := config.DefaultJiraFeatures(role)

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		roleDisplay := config.RoleDisplayNames[role]
		if roleDisplay == "" {
			roleDisplay = role
		}
		output := map[string]interface{}{
			"role":         role,
			"role_display": roleDisplay,
			"features":     features,
			"defaults":     defaults,
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	out := cmd.OutOrStdout()
	roleDisplay := config.RoleDisplayNames[role]
	if roleDisplay == "" {
		roleDisplay = role
	}
	fmt.Fprintf(out, "Role: %s (%s)\n\n", role, roleDisplay)
	fmt.Fprintf(out, "%-22s %-10s %-10s\n", "Feature", "Enabled", "Default")
	fmt.Fprintf(out, "%-22s %-10s %-10s\n", "----------------------", "----------", "----------")

	for _, name := range featureNames {
		ptr, _ := featureToggleRef(&features, name)
		defPtr, _ := featureToggleRef(&defaults, name)
		enabled := "false"
		defVal := "false"
		if ptr != nil && *ptr {
			enabled = "true"
		}
		if defPtr != nil && *defPtr {
			defVal = "true"
		}
		fmt.Fprintf(out, "%-22s %-10s %-10s\n", name, enabled, defVal)
	}
	return nil
}

func runJiraFeaturesEnable(cmd *cobra.Command, args []string) error {
	return setJiraFeatureToggle(cmd, args[0], true)
}

func runJiraFeaturesDisable(cmd *cobra.Command, args []string) error {
	return setJiraFeatureToggle(cmd, args[0], false)
}

func setJiraFeatureToggle(cmd *cobra.Command, name string, value bool) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	features := cfg.Jira.Features
	ptr, ok := featureToggleRef(&features, name)
	if !ok {
		return fmt.Errorf("unknown feature %q; valid: %s", name, strings.Join(featureNames, ", "))
	}
	*ptr = value

	v := viper.New()
	v.SetConfigFile(flagConfig)
	_ = v.ReadInConfig()
	v.Set("jira.features", features)
	if err := writeConfigAtomic(v, flagConfig); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	action := "enabled"
	if !value {
		action = "disabled"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Feature %q %s.\n", name, action)
	return nil
}

func runJiraFeaturesReset(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	role := config.DefaultJiraFeaturesRole
	database, err := db.Open(cfg.DBPath())
	if err == nil {
		defer database.Close()
		userID, _ := database.GetCurrentUserID()
		if userID != "" {
			if profile, _ := database.GetUserProfile(userID); profile != nil && profile.Role != "" {
				role = profile.Role
			}
		}
	}

	defaults := config.DefaultJiraFeatures(role)

	v := viper.New()
	v.SetConfigFile(flagConfig)
	_ = v.ReadInConfig()
	v.Set("jira.features", defaults)
	if err := writeConfigAtomic(v, flagConfig); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	roleDisplay := config.RoleDisplayNames[role]
	if roleDisplay == "" {
		roleDisplay = role
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Feature toggles reset to %s (%s) defaults.\n", role, roleDisplay)
	return nil
}

func runJiraBoardsAnalyze(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	if cfg.Jira.CloudID == "" {
		return fmt.Errorf("jira cloud_id not configured, run 'watchtower jira login' first")
	}

	store := jira.NewTokenStore(cfg.WorkspaceDir())
	if !store.Exists() {
		return fmt.Errorf("jira not connected, run 'watchtower jira login' first")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	jiraCfg := resolveJiraOAuthConfig()
	client := jira.NewClient(cfg.Jira.CloudID, jiraCfg, store)

	applyProviderOverride(cfg)
	aiProvider := newAIClient(cfg, cfg.DBPath())

	analyzer := jira.NewBoardAnalyzer(client, database, aiProvider)

	force, _ := cmd.Flags().GetBool("force")
	out := cmd.OutOrStdout()

	if len(args) > 0 {
		// Analyze specific boards.
		for _, arg := range args {
			boardID, err := strconv.Atoi(arg)
			if err != nil {
				return fmt.Errorf("invalid board ID %q: %w", arg, err)
			}
			board, err := database.GetJiraBoardProfile(boardID)
			if err != nil {
				return fmt.Errorf("getting board %d: %w", boardID, err)
			}
			if board == nil {
				return fmt.Errorf("board %d not found", boardID)
			}
			if force {
				board.ConfigHash = ""
			}
			profile, err := analyzer.AnalyzeBoard(cmd.Context(), *board)
			if err != nil {
				return fmt.Errorf("analyzing board %d: %w", boardID, err)
			}
			fmt.Fprintf(out, "Board %d (%s): %s\n", boardID, board.Name, profile.WorkflowSummary)
		}
	} else {
		// Analyze all selected boards.
		if force {
			boards, err := database.GetJiraSelectedBoards()
			if err != nil {
				return fmt.Errorf("getting selected boards: %w", err)
			}
			for _, b := range boards {
				full, err := database.GetJiraBoardProfile(b.ID)
				if err != nil || full == nil {
					full = &b
				}
				full.ConfigHash = ""
				profile, err := analyzer.AnalyzeBoard(cmd.Context(), *full)
				if err != nil {
					fmt.Fprintf(out, "Warning: failed to analyze board %d (%s): %v\n", b.ID, b.Name, err)
					continue
				}
				fmt.Fprintf(out, "Board %d (%s): %s\n", b.ID, b.Name, profile.WorkflowSummary)
			}
		} else {
			count, err := analyzer.AnalyzeAllSelected(cmd.Context())
			if err != nil {
				return fmt.Errorf("analyzing boards: %w", err)
			}
			fmt.Fprintf(out, "Analyzed %d boards.\n", count)
		}
	}

	return nil
}

func runJiraBoardsOverride(cmd *cobra.Command, args []string) error {
	boardID, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid board ID %q: %w", args[0], err)
	}

	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	staleFlag, _ := cmd.Flags().GetString("stale")
	if staleFlag == "" {
		return fmt.Errorf("--stale flag is required (e.g. 'Code Review=1,QA=2')")
	}

	thresholds := make(map[string]int)
	for _, part := range strings.Split(staleFlag, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("invalid stale threshold format: %q (expected 'Name=days')", part)
		}
		days, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			return fmt.Errorf("invalid days value %q: %w", kv[1], err)
		}
		thresholds[strings.TrimSpace(kv[0])] = days
	}

	// Read existing overrides and merge new values on top.
	board, err := database.GetJiraBoardProfile(boardID)
	if err != nil {
		return fmt.Errorf("getting board %d: %w", boardID, err)
	}

	var overrides jira.UserOverrides
	if board != nil && board.UserOverridesJSON != "" {
		_ = json.Unmarshal([]byte(board.UserOverridesJSON), &overrides)
	}
	if overrides.StaleThresholds == nil {
		overrides.StaleThresholds = make(map[string]int)
	}
	for k, v := range thresholds {
		overrides.StaleThresholds[k] = v
	}
	overridesJSON, _ := json.Marshal(overrides)

	if err := database.UpdateJiraBoardUserOverrides(boardID, string(overridesJSON)); err != nil {
		return fmt.Errorf("updating overrides: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Board %d overrides updated.\n", boardID)
	return nil
}

// truncate shortens a string to maxLen, appending "..." if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
