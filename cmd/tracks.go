package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/tracks"
	"watchtower/internal/ui"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	tracksFlagPriority    string
	tracksFlagOwnership   string
	tracksFlagChannel     string
	tracksFlagUpdates     bool
	tracksGenFlagProgress bool
)

var tracksCmd = &cobra.Command{
	Use:   "tracks",
	Short: "Show auto-generated informational tracks",
	Long:  "Displays tracks — auto-generated thematic summaries that evolve over time across channels.",
	RunE:  runTracks,
}

var tracksShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show track details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTracksShow,
}

var tracksReadCmd = &cobra.Command{
	Use:   "read <id>",
	Short: "Mark a track as read",
	Args:  cobra.ExactArgs(1),
	RunE:  runTracksRead,
}

var tracksGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run tracks pipeline to create/update tracks from digest topics",
	RunE:  runTracksGenerate,
}

func init() {
	rootCmd.AddCommand(tracksCmd)
	tracksCmd.AddCommand(tracksShowCmd)
	tracksCmd.AddCommand(tracksReadCmd)
	tracksCmd.AddCommand(tracksGenerateCmd)
	tracksCmd.Flags().StringVar(&tracksFlagPriority, "priority", "", "filter by priority (high, medium, low)")
	tracksCmd.Flags().StringVar(&tracksFlagOwnership, "ownership", "", "filter by ownership (mine, delegated, watching)")
	tracksCmd.Flags().StringVar(&tracksFlagChannel, "channel", "", "filter by channel name")
	tracksCmd.Flags().BoolVar(&tracksFlagUpdates, "updates", false, "show only tracks with updates")
	tracksGenerateCmd.Flags().BoolVar(&tracksGenFlagProgress, "progress-json", false, "output progress as JSON lines")
}

func runTracks(cmd *cobra.Command, args []string) error {
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

	// Validate flag values
	validPriorities := map[string]bool{"high": true, "medium": true, "low": true, "": true}
	if !validPriorities[tracksFlagPriority] {
		return fmt.Errorf("invalid --priority %q: must be one of high, medium, low", tracksFlagPriority)
	}
	validOwnerships := map[string]bool{"mine": true, "delegated": true, "watching": true, "": true}
	if !validOwnerships[tracksFlagOwnership] {
		return fmt.Errorf("invalid --ownership %q: must be one of mine, delegated, watching", tracksFlagOwnership)
	}

	channelIDFilter := ""
	if tracksFlagChannel != "" {
		ch, err := database.GetChannelByName(tracksFlagChannel)
		if err != nil {
			return fmt.Errorf("looking up channel: %w", err)
		}
		if ch == nil {
			return fmt.Errorf("channel #%s not found", tracksFlagChannel)
		}
		channelIDFilter = ch.ID
	}

	f := db.TrackFilter{
		Priority:  tracksFlagPriority,
		Ownership: tracksFlagOwnership,
		ChannelID: channelIDFilter,
	}
	if tracksFlagUpdates {
		v := true
		f.HasUpdates = &v
	}

	items, err := database.GetTracks(f)
	if err != nil {
		return fmt.Errorf("querying tracks: %w", err)
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No tracks found. Tracks are generated automatically from digest topics.")
		return nil
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "## Tracks (%d)\n\n", len(items))
	printTracks(&buf, items, database)

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
	return nil
}

func printTracks(w io.Writer, items []db.Track, database *db.DB) {
	priorityIcon := map[string]string{
		"high":   "!",
		"medium": "-",
		"low":    ".",
	}

	ownershipBadge := map[string]string{
		"mine":      "[mine]",
		"delegated": "[delegated]",
		"watching":  "[watching]",
	}

	for _, item := range items {
		icon := priorityIcon[item.Priority]
		if icon == "" {
			icon = "-"
		}

		badge := ownershipBadge[item.Ownership]
		if badge == "" {
			badge = "[" + item.Ownership + "]"
		}

		updateBadge := ""
		if item.HasUpdates {
			updateBadge = " [NEW]"
		}

		readBadge := ""
		if item.ReadAt == "" {
			readBadge = " *"
		}

		catLabel := ""
		if item.Category != "" {
			catLabel = " (" + item.Category + ")"
		}

		fmt.Fprintf(w, "%s #%d %s%s **%s**%s%s\n", icon, item.ID, badge, catLabel, item.Text, updateBadge, readBadge)

		// Show channels
		var channelIDs []string
		if json.Unmarshal([]byte(item.ChannelIDs), &channelIDs) == nil && len(channelIDs) > 0 {
			var names []string
			for _, chID := range channelIDs {
				name := chID
				if ch, err := database.GetChannelByID(chID); err == nil && ch != nil && ch.Name != "" {
					name = "#" + ch.Name
				}
				names = append(names, name)
			}
			fmt.Fprintf(w, "   Channels: %s\n", strings.Join(names, ", "))
		}

		// Age
		if item.UpdatedAt != "" {
			if t, err := time.Parse("2006-01-02T15:04:05Z", item.UpdatedAt); err == nil {
				fmt.Fprintf(w, "   %s\n", humanize.Time(t))
			}
		}
		fmt.Fprintln(w)
	}
}

func runTracksShow(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid track ID %q: must be a positive integer", args[0])
	}

	database, err := openTracksDB()
	if err != nil {
		return err
	}
	defer database.Close()

	track, err := database.GetTrackByID(id)
	if err != nil {
		return fmt.Errorf("track #%d not found: %w", id, err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Track #%d: %s\n", track.ID, track.Text)
	fmt.Fprintf(out, "Priority: %s | Category: %s | Ownership: %s | Updated: %s\n",
		track.Priority, track.Category, track.Ownership, track.UpdatedAt)

	if track.Context != "" {
		fmt.Fprintf(out, "\nContext:\n%s\n", track.Context)
	}

	if track.RequesterName != "" {
		fmt.Fprintf(out, "\nRequester: %s\n", track.RequesterName)
	}

	if track.Blocking != "" {
		fmt.Fprintf(out, "Blocking: %s\n", track.Blocking)
	}

	if track.DueDate != 0 {
		dueTime := time.Unix(int64(track.DueDate), 0)
		fmt.Fprintf(out, "Due: %s\n", dueTime.Format("2006-01-02"))
	}

	// Participants
	if track.Participants != "" && track.Participants != "[]" {
		type participant struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
			Stance string `json:"stance"`
		}
		var parts []participant
		if json.Unmarshal([]byte(track.Participants), &parts) == nil && len(parts) > 0 {
			fmt.Fprintf(out, "\nParticipants:\n")
			for _, p := range parts {
				if p.Stance != "" {
					fmt.Fprintf(out, "  - %s (%s)\n", p.Name, p.Stance)
				} else {
					fmt.Fprintf(out, "  - %s\n", p.Name)
				}
			}
		}
	}

	// Source refs (key message quotes)
	if track.SourceRefs != "" && track.SourceRefs != "[]" {
		type sourceRef struct {
			TS        string `json:"ts"`
			Author    string `json:"author"`
			Text      string `json:"text"`
			ChannelID string `json:"channel_id"`
			DigestID  int    `json:"digest_id"`
			TopicID   int    `json:"topic_id"`
		}
		var refs []sourceRef
		if json.Unmarshal([]byte(track.SourceRefs), &refs) == nil && len(refs) > 0 {
			fmt.Fprintf(out, "\nSource Refs:\n")
			for _, r := range refs {
				if r.Author != "" && r.Text != "" {
					chName := r.ChannelID
					if ch, chErr := database.GetChannelByID(r.ChannelID); chErr == nil && ch != nil && ch.Name != "" {
						chName = "#" + ch.Name
					}
					fmt.Fprintf(out, "  [%s] %s: %s\n", chName, r.Author, r.Text)
				} else if r.DigestID > 0 {
					fmt.Fprintf(out, "  digest=%d topic=%d channel=%s\n", r.DigestID, r.TopicID, r.ChannelID)
				}
			}
		}
	}

	// Sub-items
	if track.SubItems != "" && track.SubItems != "[]" {
		type subItem struct {
			Text   string `json:"text"`
			Status string `json:"status"`
		}
		var subs []subItem
		if json.Unmarshal([]byte(track.SubItems), &subs) == nil && len(subs) > 0 {
			fmt.Fprintf(out, "\nSub-items:\n")
			for _, s := range subs {
				marker := "[ ]"
				if s.Status == "done" {
					marker = "[x]"
				}
				fmt.Fprintf(out, "  %s %s\n", marker, s.Text)
			}
		}
	}

	// Decision options
	if track.DecisionOptions != "" && track.DecisionOptions != "[]" {
		type decOption struct {
			Option     string   `json:"option"`
			Supporters []string `json:"supporters"`
			Pros       string   `json:"pros"`
			Cons       string   `json:"cons"`
		}
		var opts []decOption
		if json.Unmarshal([]byte(track.DecisionOptions), &opts) == nil && len(opts) > 0 {
			fmt.Fprintf(out, "\nDecision Options:\n")
			for _, o := range opts {
				fmt.Fprintf(out, "  - %s\n", o.Option)
				if len(o.Supporters) > 0 {
					fmt.Fprintf(out, "    Supporters: %s\n", strings.Join(o.Supporters, ", "))
				}
				if o.Pros != "" {
					fmt.Fprintf(out, "    Pros: %s\n", o.Pros)
				}
				if o.Cons != "" {
					fmt.Fprintf(out, "    Cons: %s\n", o.Cons)
				}
			}
		}
	}

	if track.DecisionSummary != "" {
		fmt.Fprintf(out, "\nDecision Summary: %s\n", track.DecisionSummary)
	}

	// Channels
	var channelIDs []string
	if json.Unmarshal([]byte(track.ChannelIDs), &channelIDs) == nil && len(channelIDs) > 0 {
		var names []string
		for _, chID := range channelIDs {
			name := chID
			if ch, chErr := database.GetChannelByID(chID); chErr == nil && ch != nil && ch.Name != "" {
				name = "#" + ch.Name
			}
			names = append(names, name)
		}
		fmt.Fprintf(out, "\nChannels: %s\n", strings.Join(names, ", "))
	}

	// Tags
	var tags []string
	if json.Unmarshal([]byte(track.Tags), &tags) == nil && len(tags) > 0 {
		fmt.Fprintf(out, "Tags: %s\n", strings.Join(tags, ", "))
	}

	return nil
}

func runTracksRead(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid track ID %q: must be a positive integer", args[0])
	}

	database, err := openTracksDB()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.MarkTrackRead(id); err != nil {
		return fmt.Errorf("marking track read: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Track #%d marked as read\n", id)
	return nil
}

func runTracksGenerate(cmd *cobra.Command, args []string) error {
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

	if cfg.Digest.Model == "" {
		cfg.Digest.Model = config.DefaultDigestModel
	}
	if err := validateModel(cfg); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	logger := log.New(cmd.ErrOrStderr(), "[tracks] ", log.LstdFlags)

	gen, cleanupPool := cliPooledGenerator(cfg, logger)
	defer cleanupPool()
	pipe := tracks.New(database, cfg, gen, logger)

	out := cmd.OutOrStdout()

	if tracksGenFlagProgress {
		type pj struct {
			Pipeline         string  `json:"pipeline"`
			Done             int     `json:"done"`
			Total            int     `json:"total"`
			Status           string  `json:"status,omitempty"`
			InputTokens      int     `json:"input_tokens"`
			OutputTokens     int     `json:"output_tokens"`
			CostUSD          float64 `json:"cost_usd"`
			Error            string  `json:"error,omitempty"`
			Finished         bool    `json:"finished"`
			ItemsFound       int     `json:"items_found"`
			StepDurationSec  float64 `json:"step_duration_seconds,omitempty"`
			StepInputTokens  int     `json:"step_input_tokens,omitempty"`
			StepOutputTokens int     `json:"step_output_tokens,omitempty"`
			StepCostUSD      float64 `json:"step_cost_usd,omitempty"`
			TotalAPITokens   int     `json:"total_api_tokens,omitempty"`
		}
		emit := func(p pj) { data, _ := json.Marshal(p); fmt.Fprintln(out, string(data)) }

		runID, _ := database.CreatePipelineRun("tracks", "cli", cfg.Digest.Model)

		pipe.OnProgress = func(done, total int, status string) {
			inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
			p := pj{Pipeline: "tracks", Done: done, Total: total, Status: status, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost, TotalAPITokens: totalAPI}
			if pipe.LastStepDurationSeconds > 0 {
				p.StepDurationSec = pipe.LastStepDurationSeconds
			}
			p.StepInputTokens = pipe.LastStepInputTokens
			p.StepOutputTokens = pipe.LastStepOutputTokens
			p.StepCostUSD = pipe.LastStepCostUSD
			emit(p)

			// Log step to DB.
			if runID > 0 && p.StepDurationSec > 0 {
				_ = database.InsertPipelineStep(db.PipelineStep{
					RunID: runID, Step: done, Total: total, Status: status,
					InputTokens:     p.StepInputTokens,
					OutputTokens:    p.StepOutputTokens,
					CostUSD:         p.StepCostUSD,
					TotalAPITokens:  totalAPI,
					DurationSeconds: p.StepDurationSec,
				})
			}
		}

		created, updated, err := pipe.Run(cmd.Context())
		inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
			emit(pj{Pipeline: "tracks", Finished: true, Error: errMsg, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost, TotalAPITokens: totalAPI})
		} else {
			emit(pj{Pipeline: "tracks", Finished: true, ItemsFound: created + updated, InputTokens: inTok, OutputTokens: outTok, CostUSD: cost, TotalAPITokens: totalAPI})
		}

		if runID > 0 {
			var pFrom, pTo *float64
			if pipe.LastFrom > 0 {
				pFrom = &pipe.LastFrom
			}
			if pipe.LastTo > 0 {
				pTo = &pipe.LastTo
			}
			_ = database.CompletePipelineRun(runID, created+updated, inTok, outTok, cost, totalAPI, pFrom, pTo, errMsg)
		}

		if err != nil {
			return fmt.Errorf("tracks pipeline: %w", err)
		}
		return nil
	}

	fmt.Fprintln(out, "Running tracks pipeline...")

	runID, _ := database.CreatePipelineRun("tracks", "cli", cfg.Digest.Model)

	created, updated, err := pipe.Run(cmd.Context())
	inTok, outTok, cost, totalAPI := pipe.AccumulatedUsage()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	if runID > 0 {
		var pFrom, pTo *float64
		if pipe.LastFrom > 0 {
			pFrom = &pipe.LastFrom
		}
		if pipe.LastTo > 0 {
			pTo = &pipe.LastTo
		}
		_ = database.CompletePipelineRun(runID, created+updated, inTok, outTok, cost, totalAPI, pFrom, pTo, errMsg)
	}
	if err != nil {
		return fmt.Errorf("tracks pipeline: %w", err)
	}

	if created > 0 || updated > 0 {
		fmt.Fprintf(out, "Tracks: created %d, updated %d\n", created, updated)
	} else {
		fmt.Fprintln(out, "No new or updated tracks.")
	}

	return nil
}

func openTracksDB() (*db.DB, error) {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	if err := cfg.ValidateWorkspace(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return db.Open(cfg.DBPath())
}
