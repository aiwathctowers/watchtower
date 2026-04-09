package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"
	"watchtower/internal/ui"

	"github.com/spf13/cobra"
)

var trendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Show weekly trending topics and team pulse",
	Long:  "Displays weekly trends analysis from AI-generated digests including hot topics, key decisions, and outstanding action items.",
	RunE:  runTrends,
}

func init() {
	rootCmd.AddCommand(trendsCmd)
}

func runTrends(cmd *cobra.Command, args []string) error {
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

	out := cmd.OutOrStdout()

	// Look for the latest weekly digest
	weekAgo := float64(time.Now().Add(-7 * 24 * time.Hour).Unix())
	digests, err := database.GetDigests(db.DigestFilter{
		Type:     "weekly",
		FromUnix: weekAgo,
		Limit:    1,
	})
	if err != nil {
		return fmt.Errorf("querying weekly digests: %w", err)
	}

	if len(digests) == 0 {
		// Fallback: show aggregated topics from channel digests
		if err := showTopicsSummary(out, database); err != nil {
			return err
		}
	} else {
		d := digests[0]
		periodFrom := time.Unix(int64(d.PeriodFrom), 0)
		periodTo := time.Unix(int64(d.PeriodTo), 0)

		var buf strings.Builder
		fmt.Fprintf(&buf, "## Weekly Trends — %s to %s\n\n",
			periodFrom.Format("2006-01-02"), periodTo.Format("2006-01-02"))

		fmt.Fprintf(&buf, "%s\n\n", d.Summary)

		// Try topic-structured data first
		topics, topicErr := database.GetDigestTopics(d.ID)
		if topicErr == nil && len(topics) > 0 {
			for _, t := range topics {
				fmt.Fprintf(&buf, "### %s\n\n", t.Title)
				if t.Summary != "" {
					fmt.Fprintf(&buf, "%s\n\n", t.Summary)
				}

				var decisions []struct {
					Text string `json:"text"`
					By   string `json:"by"`
				}
				if err := json.Unmarshal([]byte(t.Decisions), &decisions); err == nil && len(decisions) > 0 {
					for _, dec := range decisions {
						if dec.By != "" {
							fmt.Fprintf(&buf, "- **Decision:** %s (by %s)\n", dec.Text, dec.By)
						} else {
							fmt.Fprintf(&buf, "- **Decision:** %s\n", dec.Text)
						}
					}
				}

				var actions []struct {
					Text     string `json:"text"`
					Assignee string `json:"assignee"`
				}
				if err := json.Unmarshal([]byte(t.ActionItems), &actions); err == nil && len(actions) > 0 {
					for _, a := range actions {
						assignee := ""
						if a.Assignee != "" {
							assignee = " -> " + a.Assignee
						}
						fmt.Fprintf(&buf, "- %s%s\n", a.Text, assignee)
					}
				}
				fmt.Fprintln(&buf)
			}
		} else {
			// Fallback to old flat fields for legacy digests
			var topicNames []string
			if err := json.Unmarshal([]byte(d.Topics), &topicNames); err == nil && len(topicNames) > 0 {
				fmt.Fprintln(&buf, "**Trending Topics:**")
				fmt.Fprintln(&buf)
				for _, t := range topicNames {
					fmt.Fprintf(&buf, "- %s\n", t)
				}
				fmt.Fprintln(&buf)
			}

			var decisions []struct {
				Text string `json:"text"`
				By   string `json:"by"`
			}
			if err := json.Unmarshal([]byte(d.Decisions), &decisions); err == nil && len(decisions) > 0 {
				fmt.Fprintln(&buf, "**Key Decisions:**")
				fmt.Fprintln(&buf)
				for _, dec := range decisions {
					if dec.By != "" {
						fmt.Fprintf(&buf, "- %s (by %s)\n", dec.Text, dec.By)
					} else {
						fmt.Fprintf(&buf, "- %s\n", dec.Text)
					}
				}
				fmt.Fprintln(&buf)
			}

			var actions []struct {
				Text     string `json:"text"`
				Assignee string `json:"assignee"`
			}
			if err := json.Unmarshal([]byte(d.ActionItems), &actions); err == nil && len(actions) > 0 {
				fmt.Fprintln(&buf, "**Outstanding Actions:**")
				fmt.Fprintln(&buf)
				for _, a := range actions {
					assignee := ""
					if a.Assignee != "" {
						assignee = " -> " + a.Assignee
					}
					fmt.Fprintf(&buf, "- %s%s\n", a.Text, assignee)
				}
				fmt.Fprintln(&buf)
			}
		}

		fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	}

	// --- Jira sections (appended after existing trends output) ---
	if cfg.Jira.Enabled {
		now := time.Now()
		renderJiraEpicProgress(out, database, cfg, now)
		renderJiraWithoutJira(out, database, cfg, now)
	}

	return nil
}

func showTopicsSummary(out interface{ Write([]byte) (int, error) }, database *db.DB) error {
	weekAgo := float64(time.Now().Add(-7 * 24 * time.Hour).Unix())
	digests, err := database.GetDigests(db.DigestFilter{
		Type:     "channel",
		FromUnix: weekAgo,
	})
	if err != nil {
		return fmt.Errorf("querying channel digests: %w", err)
	}

	if len(digests) == 0 {
		fmt.Fprintln(out, "No trends data available. Run 'watchtower sync --daemon' to generate digests.")
		return nil
	}

	// Aggregate topics from channel digests
	topicCounts := make(map[string]int)
	for _, d := range digests {
		// Try topic-structured data first
		dbTopics, topicErr := database.GetDigestTopics(d.ID)
		if topicErr == nil && len(dbTopics) > 0 {
			for _, t := range dbTopics {
				topicCounts[t.Title]++
			}
			continue
		}
		// Fallback to old flat topics
		var topics []string
		if err := json.Unmarshal([]byte(d.Topics), &topics); err == nil {
			for _, t := range topics {
				topicCounts[t]++
			}
		}
	}

	if len(topicCounts) == 0 {
		fmt.Fprintln(out, "No topics extracted yet.")
		return nil
	}

	// Sort topics by frequency (descending)
	type topicEntry struct {
		name  string
		count int
	}
	sorted := make([]topicEntry, 0, len(topicCounts))
	for t, c := range topicCounts {
		sorted = append(sorted, topicEntry{t, c})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	var buf strings.Builder
	fmt.Fprintf(&buf, "## Trending Topics (last 7 days, from %d channel digests)\n\n", len(digests))
	for _, e := range sorted {
		fmt.Fprintf(&buf, "- %s (%d channels)\n", e.name, e.count)
	}
	fmt.Fprintln(&buf)

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))

	return nil
}

// renderJiraEpicProgress appends the "Epic Progress" section to trends output.
func renderJiraEpicProgress(out io.Writer, database *db.DB, cfg *config.Config, now time.Time) {
	entries, err := jira.ComputeEpicProgress(database, cfg, now)
	if err != nil || len(entries) == 0 {
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Epic Progress")
	fmt.Fprintln(out, "═══════════════════════════════════════════════")
	fmt.Fprintln(out)

	for i, e := range entries {
		icon := epicStatusIcon(e.StatusBadge)
		label := epicStatusLabel(e.StatusBadge)

		name := e.EpicName
		if name == "" {
			name = e.EpicKey
		}

		fmt.Fprintf(out, "%s %s — %.0f%% done (+%.0f%% this week) [%s]\n",
			icon, name, e.ProgressPct, e.WeeklyDeltaPct, label)

		bar := progressBar(e.DoneIssues, e.TotalIssues, 20)
		fmt.Fprintf(out, "   %s  %d/%d issues\n", bar, e.DoneIssues, e.TotalIssues)

		if e.VelocityPerWeek <= 0 {
			fmt.Fprintln(out, "   Forecast: stalled (no velocity)")
		} else {
			fmt.Fprintf(out, "   Forecast: ~%.0f weeks remaining\n", e.ForecastWeeks)
		}

		if i < len(entries)-1 {
			fmt.Fprintln(out)
		}
	}
}

// renderJiraWithoutJira appends the "Without Jira" warnings section to trends output.
func renderJiraWithoutJira(out io.Writer, database *db.DB, cfg *config.Config, now time.Time) {
	since := now.AddDate(0, 0, -7)
	warnings, err := jira.DetectWithoutJira(database, cfg, since)
	if err != nil || len(warnings) == 0 {
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "⚠️ Slack Discussions Without Jira")
	fmt.Fprintln(out, "──────────────────────────────────")

	for _, w := range warnings {
		fmt.Fprintf(out, "  #%s — discussed %d days (%d messages), no Jira issue found\n",
			w.ChannelName, w.DaysDiscussed, w.MessageCount)
	}
}

// epicStatusIcon returns the emoji for a status badge.
func epicStatusIcon(badge string) string {
	switch badge {
	case "on_track":
		return "✅"
	case "at_risk":
		return "⚠️"
	case "behind":
		return "🔴"
	default:
		return "❓"
	}
}

// epicStatusLabel returns the human label for a status badge.
func epicStatusLabel(badge string) string {
	switch badge {
	case "on_track":
		return "On Track"
	case "at_risk":
		return "At Risk"
	case "behind":
		return "Behind"
	default:
		return badge
	}
}

// progressBar renders an ASCII progress bar of given width.
func progressBar(done, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
