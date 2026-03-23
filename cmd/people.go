package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/analysis"
	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/ui"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	peopleFlagUser            string
	peopleFlagPrevious        bool
	peopleFlagWeeks           int
	peopleFlagWorkers         int
	peopleGenFlagProgressJSON bool
)

var peopleCmd = &cobra.Command{
	Use:   "people",
	Short: "Show user communication analysis",
	Long:  "Displays AI-generated communication analysis for workspace users. Analysis covers a 7-day sliding window and includes communication style, decision participation, and red flags.",
	RunE:  runPeople,
}

var peopleGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate people analysis from existing data",
	Long:  "Runs the people analysis pipeline on already-synced messages. Analyzes communication patterns for all active users in a 7-day window.",
	RunE:  runPeopleGenerate,
}

func init() {
	rootCmd.AddCommand(peopleCmd)
	peopleCmd.AddCommand(peopleGenerateCmd)
	peopleCmd.Flags().StringVar(&peopleFlagUser, "user", "", "show analysis for a specific user (@username)")
	peopleCmd.Flags().BoolVar(&peopleFlagPrevious, "previous", false, "show previous 7-day window")
	peopleCmd.Flags().IntVar(&peopleFlagWeeks, "weeks", 1, "number of weeks to show")
	peopleGenerateCmd.Flags().IntVar(&peopleFlagWorkers, "workers", 10, "number of parallel workers")
	peopleGenerateCmd.Flags().BoolVar(&peopleGenFlagProgressJSON, "progress-json", false, "output progress as JSON lines")
}

func runPeople(cmd *cobra.Command, args []string) error {
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

	// If a specific user was requested via args (e.g., `watchtower people @john`)
	userFilter := peopleFlagUser
	if len(args) > 0 && userFilter == "" {
		userFilter = args[0]
	}
	userFilter = strings.TrimPrefix(userFilter, "@")

	weeks := peopleFlagWeeks
	if weeks <= 0 {
		weeks = 1
	}

	now := time.Now().UTC()
	offset := 0
	if peopleFlagPrevious {
		offset = 1
	}

	for w := range weeks {
		weekIdx := w + offset
		to := now.AddDate(0, 0, -weekIdx*7)
		from := to.AddDate(0, 0, -7)
		fromUnix := float64(from.Unix())
		toUnix := float64(to.Unix())

		if userFilter != "" {
			err := showUserDetail(out, database, userFilter, fromUnix, toUnix)
			if err != nil {
				return err
			}
		} else {
			err := showPeopleList(cmd, out, database, cfg, fromUnix, toUnix, from, to)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func showPeopleList(cmd *cobra.Command, out io.Writer, database *db.DB, cfg *config.Config, fromUnix, toUnix float64, from, to time.Time) error {
	analyses, err := database.GetUserAnalysesForWindow(fromUnix, toUnix)
	if err != nil {
		return fmt.Errorf("querying analyses: %w", err)
	}

	if len(analyses) == 0 {
		fmt.Fprintf(out, "No people analysis available for %s to %s.\n",
			from.Format("2006-01-02"), to.Format("2006-01-02"))
		fmt.Fprintln(out, "Run 'watchtower people generate' to create one.")
		return nil
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "## People Analysis — %s to %s\n\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Fprintf(&buf, "%d users analyzed\n\n", len(analyses))

	// Period summary (team-wide)
	if ps, err := database.GetPeriodSummary(fromUnix, toUnix); err == nil && ps != nil {
		fmt.Fprintln(&buf, "### Team Summary")
		fmt.Fprintln(&buf)
		fmt.Fprintf(&buf, "%s\n\n", ps.Summary)

		var attention []string
		if err := json.Unmarshal([]byte(ps.Attention), &attention); err == nil && len(attention) > 0 {
			fmt.Fprintln(&buf, "**Needs Attention:**")
			fmt.Fprintln(&buf)
			for _, item := range attention {
				fmt.Fprintf(&buf, "- %s\n", item)
			}
			fmt.Fprintln(&buf)
		}
		fmt.Fprintln(&buf, "---")
		fmt.Fprintln(&buf)
	}

	for _, a := range analyses {
		userName := a.UserID
		if u, err := database.GetUserByID(a.UserID); err == nil && u != nil {
			userName = displayName(u)
		}

		// Style + role badges
		badges := formatBadges(a.CommunicationStyle, a.DecisionRole)

		fmt.Fprintf(&buf, "### @%s %s\n", userName, badges)
		fmt.Fprintf(&buf, "%d msgs | %d channels | %d threads started | %d threads replied",
			a.MessageCount, a.ChannelsActive, a.ThreadsInitiated, a.ThreadsReplied)
		if a.VolumeChangePct != 0 {
			fmt.Fprintf(&buf, " | volume: %+.0f%%", a.VolumeChangePct)
		}
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf)

		if a.Summary != "" {
			fmt.Fprintf(&buf, "%s\n\n", a.Summary)
		}

		// Red flags
		var redFlags []string
		if err := json.Unmarshal([]byte(a.RedFlags), &redFlags); err == nil && len(redFlags) > 0 {
			for _, rf := range redFlags {
				fmt.Fprintf(&buf, "- **!** %s\n", rf)
			}
			fmt.Fprintln(&buf)
		}

		// Concerns
		var concerns []string
		if err := json.Unmarshal([]byte(a.Concerns), &concerns); err == nil && len(concerns) > 0 {
			fmt.Fprintln(&buf, "**Concerns:**")
			for _, c := range concerns {
				fmt.Fprintf(&buf, "- %s\n", c)
			}
			fmt.Fprintln(&buf)
		}

		// Highlights
		var highlights []string
		if err := json.Unmarshal([]byte(a.Highlights), &highlights); err == nil && len(highlights) > 0 {
			for _, h := range highlights {
				fmt.Fprintf(&buf, "- + %s\n", h)
			}
			fmt.Fprintln(&buf)
		}
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	return nil
}

func showUserDetail(out io.Writer, database *db.DB, username string, fromUnix, toUnix float64) error {
	// Look up user by name
	user, err := database.GetUserByName(username)
	if err != nil {
		return fmt.Errorf("looking up user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user @%s not found", username)
	}

	// Get analyses for this user
	analyses, err := database.GetUserAnalyses(db.UserAnalysisFilter{
		UserID:   user.ID,
		FromUnix: fromUnix,
		ToUnix:   toUnix,
	})
	if err != nil {
		return fmt.Errorf("querying user analyses: %w", err)
	}

	if len(analyses) == 0 {
		fmt.Fprintf(out, "No analysis available for @%s in this period.\n", username)
		fmt.Fprintln(out, "Run 'watchtower people generate' to create one.")
		return nil
	}

	var buf strings.Builder
	for _, a := range analyses {
		from := time.Unix(int64(a.PeriodFrom), 0).UTC()
		to := time.Unix(int64(a.PeriodTo), 0).UTC()

		fmt.Fprintf(&buf, "## @%s — %s to %s\n\n", displayName(user), from.Format("2006-01-02"), to.Format("2006-01-02"))

		// Stats table
		fmt.Fprintln(&buf, "| Metric | Value |")
		fmt.Fprintln(&buf, "|--------|-------|")
		fmt.Fprintf(&buf, "| Messages | %d |\n", a.MessageCount)
		fmt.Fprintf(&buf, "| Channels active | %d |\n", a.ChannelsActive)
		fmt.Fprintf(&buf, "| Threads started | %d |\n", a.ThreadsInitiated)
		fmt.Fprintf(&buf, "| Threads replied | %d |\n", a.ThreadsReplied)
		fmt.Fprintf(&buf, "| Avg message length | %.0f chars |\n", a.AvgMessageLength)
		fmt.Fprintf(&buf, "| Volume change | %+.0f%% |\n", a.VolumeChangePct)
		fmt.Fprintf(&buf, "| Style | %s |\n", a.CommunicationStyle)
		fmt.Fprintf(&buf, "| Decision role | %s |\n", a.DecisionRole)
		fmt.Fprintln(&buf)

		if a.Summary != "" {
			fmt.Fprintf(&buf, "**Summary:** %s\n\n", a.Summary)
		}

		if a.StyleDetails != "" {
			fmt.Fprintf(&buf, "**Communication Style Analysis:** %s\n\n", a.StyleDetails)
		}

		var redFlags []string
		if err := json.Unmarshal([]byte(a.RedFlags), &redFlags); err == nil && len(redFlags) > 0 {
			fmt.Fprintln(&buf, "**Red Flags:**")
			fmt.Fprintln(&buf)
			for _, rf := range redFlags {
				fmt.Fprintf(&buf, "- %s\n", rf)
			}
			fmt.Fprintln(&buf)
		}

		var concerns []string
		if err := json.Unmarshal([]byte(a.Concerns), &concerns); err == nil && len(concerns) > 0 {
			fmt.Fprintln(&buf, "**Concerns:**")
			fmt.Fprintln(&buf)
			for _, c := range concerns {
				fmt.Fprintf(&buf, "- %s\n", c)
			}
			fmt.Fprintln(&buf)
		}

		var highlights []string
		if err := json.Unmarshal([]byte(a.Highlights), &highlights); err == nil && len(highlights) > 0 {
			fmt.Fprintln(&buf, "**Highlights:**")
			fmt.Fprintln(&buf)
			for _, h := range highlights {
				fmt.Fprintf(&buf, "- %s\n", h)
			}
			fmt.Fprintln(&buf)
		}

		var accomplishments []string
		if err := json.Unmarshal([]byte(a.Accomplishments), &accomplishments); err == nil && len(accomplishments) > 0 {
			fmt.Fprintln(&buf, "**Accomplishments:**")
			fmt.Fprintln(&buf)
			for _, item := range accomplishments {
				fmt.Fprintf(&buf, "- %s\n", item)
			}
			fmt.Fprintln(&buf)
		}

		var recommendations []string
		if err := json.Unmarshal([]byte(a.Recommendations), &recommendations); err == nil && len(recommendations) > 0 {
			fmt.Fprintln(&buf, "**Recommendations:**")
			fmt.Fprintln(&buf)
			for _, r := range recommendations {
				fmt.Fprintf(&buf, "- %s\n", r)
			}
			fmt.Fprintln(&buf)
		}

		fmt.Fprintln(&buf, "---")
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	return nil
}

func runPeopleGenerate(cmd *cobra.Command, args []string) error {
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

	cfg.Digest.Enabled = true
	if cfg.Digest.Model == "" {
		cfg.Digest.Model = config.DefaultDigestModel
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	out := cmd.OutOrStdout()

	logger := log.New(io.Discard, "", 0)
	if flagVerbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	gen := digest.NewClaudeGenerator(cfg.Digest.Model, cfg.ClaudePath)
	pipe := analysis.New(database, cfg, gen, logger)
	pipe.ForceRegenerate = true
	pipe.Workers = peopleFlagWorkers

	if peopleGenFlagProgressJSON {
		type pj struct {
			Pipeline     string  `json:"pipeline"`
			Done         int     `json:"done"`
			Total        int     `json:"total"`
			Status       string  `json:"status,omitempty"`
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			CostUSD      float64 `json:"cost_usd"`
			Error        string  `json:"error,omitempty"`
			Finished     bool    `json:"finished"`
			ItemsFound   int     `json:"items_found"`
		}
		emit := func(p pj) { data, _ := json.Marshal(p); fmt.Fprintln(out, string(data)) }

		pipe.OnProgress = func(completed, total int, status string) {
			emit(pj{Pipeline: "people", Done: completed, Total: total, Status: status})
		}
		n, err := pipe.Run(cmd.Context())
		inTok, outTok, cost := pipe.AccumulatedUsage()
		final := pj{Pipeline: "people", Finished: true, ItemsFound: n, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost}
		if err != nil {
			final.Error = err.Error()
		}
		emit(final)
		return nil
	}

	startTime := time.Now()
	genIsTTY := false
	if f, ok := out.(*os.File); ok {
		genIsTTY = term.IsTerminal(int(f.Fd()))
	}
	pipe.OnProgress = func(completed, total int, status string) {
		if !genIsTTY {
			return
		}
		if total <= 0 {
			fmt.Fprintf(out, "\r\033[K%s", status)
			return
		}
		pct := float64(completed) / float64(total) * 100
		barWidth := 30
		filled := int(float64(barWidth) * float64(completed) / float64(total))
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		eta := ""
		if completed > 0 {
			elapsed := time.Since(startTime)
			remaining := time.Duration(float64(elapsed) / float64(completed) * float64(total-completed))
			eta = fmt.Sprintf(" ETA %s", remaining.Truncate(time.Second))
		}
		fmt.Fprintf(out, "\r\033[K[%s] %.0f%% (%d/%d)%s  %s", bar, pct, completed, total, eta, status)
	}

	fmt.Fprintf(out, "Analyzing user communication patterns (7-day window) using %s...\n", cfg.Digest.Model)

	n, err := pipe.Run(cmd.Context())
	fmt.Fprintln(out) // newline after progress
	if err != nil {
		return fmt.Errorf("analysis pipeline: %w", err)
	}

	if n == 0 {
		fmt.Fprintln(out, "No users with enough messages to analyze.")
	} else {
		fmt.Fprintf(out, "Analyzed %d user(s).\n", n)
		fmt.Fprintln(out, "Run 'watchtower people' to view results.")
	}

	return nil
}

func displayName(u *db.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

func formatBadges(style, role string) string {
	var parts []string
	if style != "" {
		parts = append(parts, "["+style+"]")
	}
	if role != "" {
		parts = append(parts, "["+role+"]")
	}
	return strings.Join(parts, " ")
}
