package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/guide"
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
	Short: "Show people cards (unified analysis + coaching)",
	Long:  "Displays AI-generated people cards combining communication analysis and coaching advice. Cards are generated from behavioral signals observed in channel digests.",
	RunE:  runPeople,
}

var peopleGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate people cards from existing data",
	Long:  "Runs the people card pipeline (REDUCE phase) on signals from channel digests.",
	RunE:  runPeopleGenerate,
}

func init() {
	rootCmd.AddCommand(peopleCmd)
	peopleCmd.AddCommand(peopleGenerateCmd)
	peopleCmd.Flags().StringVar(&peopleFlagUser, "user", "", "show card for a specific user (@username)")
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

func showPeopleList(_ *cobra.Command, out io.Writer, database *db.DB, _ *config.Config, fromUnix, toUnix float64, from, to time.Time) error {
	cards, err := database.GetPeopleCardsForWindow(fromUnix, toUnix)
	if err != nil {
		return fmt.Errorf("querying people cards: %w", err)
	}

	if len(cards) == 0 {
		fmt.Fprintf(out, "No people cards available for %s to %s.\n",
			from.Format("2006-01-02"), to.Format("2006-01-02"))
		fmt.Fprintln(out, "Run 'watchtower people generate' to create them.")
		return nil
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "## People Cards — %s to %s\n\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Fprintf(&buf, "%d users\n\n", len(cards))

	// Team summary
	if ps, err := database.GetPeopleCardSummary(fromUnix, toUnix); err == nil && ps != nil {
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

		var tips []string
		if err := json.Unmarshal([]byte(ps.Tips), &tips); err == nil && len(tips) > 0 {
			fmt.Fprintln(&buf, "**Tips:**")
			fmt.Fprintln(&buf)
			for _, tip := range tips {
				fmt.Fprintf(&buf, "- %s\n", tip)
			}
			fmt.Fprintln(&buf)
		}
		fmt.Fprintln(&buf, "---")
		fmt.Fprintln(&buf)
	}

	for _, c := range cards {
		userName := c.UserID
		if u, err := database.GetUserByID(c.UserID); err == nil && u != nil {
			userName = displayName(u)
		}

		badges := formatBadges(c.CommunicationStyle, c.DecisionRole)
		if c.Status == "insufficient_data" {
			badges += " [insufficient data]"
		}

		fmt.Fprintf(&buf, "### @%s %s\n", userName, badges)
		fmt.Fprintf(&buf, "%d msgs | %d channels", c.MessageCount, c.ChannelsActive)
		if c.ThreadsInitiated > 0 || c.ThreadsReplied > 0 {
			fmt.Fprintf(&buf, " | %d threads started | %d replied", c.ThreadsInitiated, c.ThreadsReplied)
		}
		if c.VolumeChangePct != 0 {
			fmt.Fprintf(&buf, " | volume: %+.0f%%", c.VolumeChangePct)
		}
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf)

		if c.Summary != "" {
			fmt.Fprintf(&buf, "%s\n\n", c.Summary)
		}

		var redFlags []string
		if err := json.Unmarshal([]byte(c.RedFlags), &redFlags); err == nil && len(redFlags) > 0 {
			for _, rf := range redFlags {
				fmt.Fprintf(&buf, "- **!** %s\n", rf)
			}
			fmt.Fprintln(&buf)
		}

		var highlights []string
		if err := json.Unmarshal([]byte(c.Highlights), &highlights); err == nil && len(highlights) > 0 {
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
	user, err := database.GetUserByName(username)
	if err != nil {
		return fmt.Errorf("looking up user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user @%s not found", username)
	}

	cards, err := database.GetPeopleCards(db.PeopleCardFilter{
		UserID:   user.ID,
		FromUnix: fromUnix,
		ToUnix:   toUnix,
	})
	if err != nil {
		return fmt.Errorf("querying people cards: %w", err)
	}

	if len(cards) == 0 {
		fmt.Fprintf(out, "No people card available for @%s in this period.\n", username)
		fmt.Fprintln(out, "Run 'watchtower people generate' to create one.")
		return nil
	}

	var buf strings.Builder
	for _, c := range cards {
		from := time.Unix(int64(c.PeriodFrom), 0).UTC()
		to := time.Unix(int64(c.PeriodTo), 0).UTC()

		fmt.Fprintf(&buf, "## @%s — %s to %s\n\n", displayName(user), from.Format("2006-01-02"), to.Format("2006-01-02"))

		if c.Status == "insufficient_data" {
			fmt.Fprintln(&buf, "*Insufficient data for full analysis this period.*")
		}

		// Stats table
		fmt.Fprintln(&buf, "| Metric | Value |")
		fmt.Fprintln(&buf, "|--------|-------|")
		fmt.Fprintf(&buf, "| Messages | %d |\n", c.MessageCount)
		fmt.Fprintf(&buf, "| Channels active | %d |\n", c.ChannelsActive)
		fmt.Fprintf(&buf, "| Threads started | %d |\n", c.ThreadsInitiated)
		fmt.Fprintf(&buf, "| Threads replied | %d |\n", c.ThreadsReplied)
		fmt.Fprintf(&buf, "| Avg message length | %.0f chars |\n", c.AvgMessageLength)
		fmt.Fprintf(&buf, "| Volume change | %+.0f%% |\n", c.VolumeChangePct)
		fmt.Fprintf(&buf, "| Style | %s |\n", c.CommunicationStyle)
		fmt.Fprintf(&buf, "| Decision role | %s |\n", c.DecisionRole)
		fmt.Fprintln(&buf)

		if c.Summary != "" {
			fmt.Fprintf(&buf, "**Summary:** %s\n\n", c.Summary)
		}

		if c.CommunicationGuide != "" {
			fmt.Fprintf(&buf, "**Communication Guide:** %s\n\n", c.CommunicationGuide)
		}

		if c.DecisionStyle != "" {
			fmt.Fprintf(&buf, "**Decision Style:** %s\n\n", c.DecisionStyle)
		}

		var redFlags []string
		if err := json.Unmarshal([]byte(c.RedFlags), &redFlags); err == nil && len(redFlags) > 0 {
			fmt.Fprintln(&buf, "**Red Flags:**")
			fmt.Fprintln(&buf)
			for _, rf := range redFlags {
				fmt.Fprintf(&buf, "- %s\n", rf)
			}
			fmt.Fprintln(&buf)
		}

		var highlights []string
		if err := json.Unmarshal([]byte(c.Highlights), &highlights); err == nil && len(highlights) > 0 {
			fmt.Fprintln(&buf, "**Highlights:**")
			fmt.Fprintln(&buf)
			for _, h := range highlights {
				fmt.Fprintf(&buf, "- %s\n", h)
			}
			fmt.Fprintln(&buf)
		}

		var accomplishments []string
		if err := json.Unmarshal([]byte(c.Accomplishments), &accomplishments); err == nil && len(accomplishments) > 0 {
			fmt.Fprintln(&buf, "**Accomplishments:**")
			fmt.Fprintln(&buf)
			for _, item := range accomplishments {
				fmt.Fprintf(&buf, "- %s\n", item)
			}
			fmt.Fprintln(&buf)
		}

		var tactics []string
		if err := json.Unmarshal([]byte(c.Tactics), &tactics); err == nil && len(tactics) > 0 {
			fmt.Fprintln(&buf, "**Tactics:**")
			fmt.Fprintln(&buf)
			for _, t := range tactics {
				fmt.Fprintf(&buf, "- %s\n", t)
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

	gen, savePool := cliPooledGenerator(cfg, logger)
	defer savePool()
	pipe := guide.New(database, cfg, gen, logger)
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

	fmt.Fprintf(out, "Generating people cards (7-day window) using %s...\n", cfg.Digest.Model)

	n, err := pipe.Run(cmd.Context())
	fmt.Fprintln(out)
	if err != nil {
		return fmt.Errorf("people card pipeline: %w", err)
	}

	if n == 0 {
		fmt.Fprintln(out, "No users with enough messages to analyze.")
	} else {
		fmt.Fprintf(out, "Generated %d people card(s).\n", n)
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
