package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"

	"github.com/spf13/cobra"
)

// This file hosts the read-only Jira dashboard commands: workload, blockers,
// project-map and releases. Each one follows the same shape — config + DB
// open, feature gate check, build a domain DTO, then either marshal JSON or
// render a text table. They were extracted from cmd/jira.go to make that file
// navigable; the cobra command vars and init() wiring live in jira.go.

func runJiraWorkload(cmd *cobra.Command, _ []string) error {
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

	if !jira.IsFeatureEnabled(cfg, "team_workload") {
		return fmt.Errorf("team_workload feature is disabled; enable with 'watchtower jira features enable team_workload'")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	to := time.Now()
	from := to.AddDate(0, 0, -30)

	entries, err := jira.ComputeTeamWorkload(database, cfg, from, to)
	if err != nil {
		return fmt.Errorf("computing workload: %w", err)
	}

	out := cmd.OutOrStdout()

	if len(entries) == 0 {
		fmt.Fprintln(out, "No workload data. Make sure Jira is connected and synced.")
		return nil
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	fmt.Fprintln(out, "Team Workload")
	fmt.Fprintln(out, strings.Repeat("─", 95))
	fmt.Fprintf(out, "%-14s %5s %6s %8s %8s %7s %6s %6s  %s\n",
		"Name", "Open", "SP", "Overdue", "Blocked", "Cycle", "Slack", "Mtgs", "Signal")
	fmt.Fprintln(out, strings.Repeat("─", 95))

	for _, e := range entries {
		signal := formatWorkloadSignal(e.Signal)
		name := truncate(e.DisplayName, 14)
		if name == "" {
			name = truncate(e.SlackUserID, 14)
		}
		fmt.Fprintf(out, "%-14s %5d %6.1f %8d %8d %6.1fd %6d %5.1fh  %s\n",
			name, e.OpenIssues, e.StoryPoints,
			e.OverdueCount, e.BlockedCount,
			e.AvgCycleTimeDays, e.SlackMessageCount,
			e.MeetingHours, signal)
	}

	return nil
}

func formatWorkloadSignal(s jira.WorkloadSignal) string {
	switch s {
	case jira.SignalOverload:
		return "⚠️  Overload"
	case jira.SignalWatch:
		return "\U0001f440 Watch"
	case jira.SignalLow:
		return "\U0001f4a4 Low"
	default:
		return "✅ Normal"
	}
}

func runJiraBlockers(cmd *cobra.Command, _ []string) error {
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

	if !jira.IsFeatureEnabled(cfg, "blocker_map") {
		return fmt.Errorf("blocker_map feature is disabled; enable with 'watchtower jira features enable blocker_map'")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	entries, err := jira.ComputeBlockerMap(database, cfg)
	if err != nil {
		return fmt.Errorf("computing blocker map: %w", err)
	}

	out := cmd.OutOrStdout()

	if len(entries) == 0 {
		fmt.Fprintln(out, "No blocked or stale issues found. \U0001f389")
		return nil
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	fmt.Fprintf(out, "Blocker Map (%d issues)\n", len(entries))
	fmt.Fprintln(out, strings.Repeat("═", 50))
	fmt.Fprintln(out)

	for _, e := range entries {
		icon := blockerUrgencyIcon(e.Urgency)
		statusLabel := e.Status
		if e.BlockerType == "stale" {
			statusLabel = "Stale in " + e.Status
		}
		fmt.Fprintf(out, "%s %s %q [%s, %d days]\n", icon, e.IssueKey, e.Summary, statusLabel, e.BlockedDays)

		if len(e.BlockingChain) > 1 {
			fmt.Fprintf(out, "   Chain: %s (root cause)\n", formatBlockingChain(e.BlockingChain))
		} else {
			fmt.Fprintln(out, "   No blocking chain")
		}

		if e.DownstreamCount > 0 {
			fmt.Fprintf(out, "   Downstream: blocks %d issues\n", e.DownstreamCount)
		}

		if len(e.WhoToPing) > 0 {
			pings := make([]string, len(e.WhoToPing))
			for i, p := range e.WhoToPing {
				name := p.DisplayName
				if name == "" {
					name = p.SlackUserID
				}
				pings[i] = fmt.Sprintf("@%s (%s)", name, p.Reason)
			}
			fmt.Fprintf(out, "   Who to ping: %s\n", strings.Join(pings, ", "))
		}

		if e.SlackContext != "" {
			snippet := e.SlackContext
			if len(snippet) > 60 {
				snippet = snippet[:60]
			}
			fmt.Fprintf(out, "   Slack: %q...\n", snippet)
		}

		fmt.Fprintln(out)
	}

	return nil
}

func blockerUrgencyIcon(u jira.BlockerUrgency) string {
	switch u {
	case jira.UrgencyRed:
		return "\U0001f534"
	case jira.UrgencyYellow:
		return "\U0001f7e1"
	default:
		return "⚪"
	}
}

func formatBlockingChain(chain []string) string {
	return strings.Join(chain, " ← ")
}

func runJiraProjectMap(cmd *cobra.Command, _ []string) error {
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

	if !jira.IsFeatureEnabled(cfg, "epic_progress") {
		return fmt.Errorf("epic_progress feature is disabled; enable with 'watchtower jira features enable epic_progress'")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()
	jsonFlag, _ := cmd.Flags().GetBool("json")
	epicFlag, _ := cmd.Flags().GetString("epic")

	if epicFlag != "" {
		return renderProjectMapEpicDetail(out, database, cfg, epicFlag, jsonFlag)
	}
	return renderProjectMapList(out, database, cfg, jsonFlag)
}

func renderProjectMapEpicDetail(out io.Writer, database *db.DB, cfg *config.Config, epicKey string, jsonFlag bool) error {
	epic, err := jira.BuildProjectMapForEpic(database, cfg, epicKey, time.Now())
	if err != nil {
		return fmt.Errorf("building project map for epic: %w", err)
	}
	if epic == nil {
		fmt.Fprintf(out, "Epic %s not found or has too few issues.\n", epicKey)
		return nil
	}

	pingTargets, err := jira.ComputeWhoToPingForEpic(database, epicKey)
	if err != nil {
		return fmt.Errorf("computing who to ping: %w", err)
	}

	if jsonFlag {
		payload := struct {
			*jira.ProjectMapEpic
			WhoToPing []jira.PingTarget `json:"who_to_ping"`
		}{epic, pingTargets}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	fmt.Fprintf(out, "Epic: %s — %s\n", epic.EpicKey, epic.EpicName)
	fmt.Fprintf(out, "Progress: %.0f%% (%d/%d done, %d in progress)\n",
		epic.ProgressPct, epic.DoneIssues, epic.TotalIssues, epic.InProgressIssues)
	fmt.Fprintf(out, "Status: %s\n", epic.StatusBadge)
	fmt.Fprintf(out, "Forecast: %.1f weeks at current velocity\n", epic.ForecastWeeks)
	fmt.Fprintln(out)

	if len(epic.Issues) > 0 {
		fmt.Fprintln(out, "Issues:")
		fmt.Fprintf(out, "  %-12s %-30s %-15s %-13s %s\n", "KEY", "SUMMARY", "STATUS", "ASSIGNEE", "DAYS")
		for _, iss := range epic.Issues {
			assignee := iss.AssigneeName
			if assignee == "" {
				assignee = "unassigned"
			}
			staleMarker := ""
			if iss.IsStale {
				staleMarker = " ⚠️ stale"
			}
			fmt.Fprintf(out, "  %-12s %-30s %-15s %-13s %d%s\n",
				iss.Key, truncate(iss.Summary, 30), truncate(iss.Status, 15),
				truncate("@"+assignee, 13), iss.DaysInStatus, staleMarker)
		}
		fmt.Fprintln(out)
	}

	if len(epic.StaleIssues) > 0 {
		fmt.Fprintf(out, "Stale Issues (%d):\n", len(epic.StaleIssues))
		for _, iss := range epic.StaleIssues {
			fmt.Fprintf(out, "  %-12s %-30s %-15s %d days\n",
				iss.Key, truncate(iss.Summary, 30), truncate(iss.Status, 15), iss.DaysInStatus)
		}
		fmt.Fprintln(out)
	}

	if len(epic.Participants) > 0 {
		fmt.Fprintln(out, "Participants:")
		parts := make([]string, len(epic.Participants))
		for i, p := range epic.Participants {
			name := p.DisplayName
			if name == "" {
				name = p.SlackUserID
			}
			parts[i] = fmt.Sprintf("@%s", name)
		}
		fmt.Fprintf(out, "  %s\n", strings.Join(parts, ", "))
		fmt.Fprintln(out)
	}

	if len(pingTargets) > 0 {
		fmt.Fprintln(out, "Who to Ping:")
		for _, p := range pingTargets {
			name := p.DisplayName
			if name == "" {
				name = p.SlackUserID
			}
			fmt.Fprintf(out, "  @%s — %s\n", name, p.Reason)
		}
	}
	return nil
}

func renderProjectMapList(out io.Writer, database *db.DB, cfg *config.Config, jsonFlag bool) error {
	epics, err := jira.BuildProjectMap(database, cfg, time.Now())
	if err != nil {
		return fmt.Errorf("building project map: %w", err)
	}

	if len(epics) == 0 {
		fmt.Fprintln(out, "No epics found.")
		return nil
	}

	if jsonFlag {
		data, err := json.MarshalIndent(epics, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	fmt.Fprintln(out, "Project Map — Epics")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%-12s %-23s %10s  %-10s %7s %6s %8s\n",
		"KEY", "NAME", "PROGRESS", "STATUS", "ISSUES", "STALE", "BLOCKED")
	for _, e := range epics {
		fmt.Fprintf(out, "%-12s %-23s %9.0f%%  %-10s %7d %6d %8d\n",
			e.EpicKey, truncate(e.EpicName, 23),
			e.ProgressPct, e.StatusBadge,
			e.TotalIssues, e.StaleCount, e.BlockedCount)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%d epics found.\n", len(epics))
	return nil
}

func runJiraReleases(cmd *cobra.Command, _ []string) error {
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

	if !jira.IsFeatureEnabled(cfg, "release_dashboard") {
		return fmt.Errorf("release_dashboard feature is disabled; enable with 'watchtower jira features enable release_dashboard'")
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()
	jsonFlag, _ := cmd.Flags().GetBool("json")
	releaseFlag, _ := cmd.Flags().GetString("release")
	now := time.Now()

	if releaseFlag != "" {
		return renderReleaseDetail(out, database, cfg, releaseFlag, now, jsonFlag)
	}
	return renderReleaseList(out, database, cfg, now, jsonFlag)
}

func renderReleaseDetail(out io.Writer, database *db.DB, cfg *config.Config, releaseName string, now time.Time, jsonFlag bool) error {
	entry, err := jira.BuildReleaseDetail(database, cfg, releaseName, now)
	if err != nil {
		return fmt.Errorf("building release detail: %w", err)
	}
	if entry == nil {
		fmt.Fprintf(out, "Release %q not found.\n", releaseName)
		return nil
	}

	if jsonFlag {
		data, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	status := releaseStatus(entry)
	fmt.Fprintf(out, "Release: %s\n", entry.Name)
	fmt.Fprintf(out, "Date: %s\n", entry.ReleaseDate)
	fmt.Fprintf(out, "Progress: %.0f%% (%d/%d done)\n", entry.ProgressPct, entry.DoneIssues, entry.TotalIssues)
	fmt.Fprintf(out, "Status: %s", status)
	if entry.AtRiskReason != "" {
		fmt.Fprintf(out, " — %s", entry.AtRiskReason)
	}
	fmt.Fprintln(out)

	if len(entry.EpicProgress) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Epic Progress:")
		fmt.Fprintf(out, "  %-12s %-23s %10s  %-10s\n", "EPIC", "NAME", "PROGRESS", "STATUS")
		for _, ep := range entry.EpicProgress {
			fmt.Fprintf(out, "  %-12s %-23s %9.0f%%  %-10s\n",
				ep.EpicKey, truncate(ep.EpicName, 23), ep.ProgressPct, ep.StatusBadge)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Blocked Issues: %d\n", entry.BlockedCount)

	if entry.ScopeChanges.AddedLastWeek > 0 {
		fmt.Fprintf(out, "Scope Changes (last week): +%d added\n", entry.ScopeChanges.AddedLastWeek)
	}

	if entry.AtRiskReason != "" {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "At Risk Reason: %s\n", entry.AtRiskReason)
	}
	return nil
}

func renderReleaseList(out io.Writer, database *db.DB, cfg *config.Config, now time.Time, jsonFlag bool) error {
	entries, err := jira.BuildReleaseDashboard(database, cfg, "", now)
	if err != nil {
		return fmt.Errorf("building release dashboard: %w", err)
	}

	if len(entries) == 0 {
		fmt.Fprintln(out, "No releases found.")
		return nil
	}

	if jsonFlag {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling JSON: %w", err)
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	fmt.Fprintln(out, "Releases")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%-14s %-13s %10s  %-12s %6s %8s\n",
		"NAME", "DATE", "PROGRESS", "STATUS", "EPICS", "BLOCKED")
	for _, e := range entries {
		status := releaseStatus(&e)
		fmt.Fprintf(out, "%-14s %-13s %9.0f%%  %-12s %6d %8d\n",
			truncate(e.Name, 14), e.ReleaseDate,
			e.ProgressPct, status,
			len(e.EpicProgress), e.BlockedCount)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%d releases found.\n", len(entries))
	return nil
}

func releaseStatus(e *jira.ReleaseEntry) string {
	if e.Released {
		return "released"
	}
	if e.IsOverdue {
		return "overdue"
	}
	if e.AtRisk {
		return "at_risk"
	}
	return "unreleased"
}
