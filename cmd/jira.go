package cmd

import (
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

func init() {
	rootCmd.AddCommand(jiraCmd)
	jiraCmd.AddCommand(jiraLoginCmd)
	jiraCmd.AddCommand(jiraLogoutCmd)
	jiraCmd.AddCommand(jiraStatusCmd)
	jiraCmd.AddCommand(jiraBoardsCmd)
	jiraBoardsCmd.AddCommand(jiraBoardsSelectCmd)
	jiraBoardsCmd.AddCommand(jiraBoardsDeselectCmd)
	jiraCmd.AddCommand(jiraUsersCmd)
	jiraUsersCmd.AddCommand(jiraUsersMapCmd)
	jiraCmd.AddCommand(jiraSyncCmd)

	jiraLoginCmd.Flags().Bool("no-open", false, "don't open the browser automatically")
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

	// Use the first site.
	site := resources[0]

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
