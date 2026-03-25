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
	tracksFlagPriority string
	tracksFlagChannel  string
	tracksFlagUpdates  bool
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
	tracksCmd.Flags().StringVar(&tracksFlagChannel, "channel", "", "filter by channel name")
	tracksCmd.Flags().BoolVar(&tracksFlagUpdates, "updates", false, "show only tracks with updates")
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

	for _, item := range items {
		icon := priorityIcon[item.Priority]
		if icon == "" {
			icon = "-"
		}

		updateBadge := ""
		if item.HasUpdates {
			updateBadge = " [NEW]"
		}

		readBadge := ""
		if item.ReadAt == "" {
			readBadge = " *"
		}

		fmt.Fprintf(w, "%s #%d **%s**%s%s\n", icon, item.ID, item.Title, updateBadge, readBadge)

		if item.CurrentStatus != "" {
			fmt.Fprintf(w, "   Status: %s\n", item.CurrentStatus)
		}

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

		// Tags
		var tags []string
		if json.Unmarshal([]byte(item.Tags), &tags) == nil && len(tags) > 0 {
			fmt.Fprintf(w, "   Tags: %s\n", strings.Join(tags, ", "))
		}

		// Age
		if item.CreatedAt != "" {
			if t, err := time.Parse("2006-01-02T15:04:05Z", item.CreatedAt); err == nil {
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
	fmt.Fprintf(out, "Track #%d: %s\n", track.ID, track.Title)
	fmt.Fprintf(out, "Priority: %s | Updated: %s\n", track.Priority, track.UpdatedAt)

	if track.CurrentStatus != "" {
		fmt.Fprintf(out, "\nCurrent Status: %s\n", track.CurrentStatus)
	}

	if track.Narrative != "" {
		fmt.Fprintf(out, "\nNarrative:\n%s\n", track.Narrative)
	}

	// Participants
	if track.Participants != "" && track.Participants != "[]" {
		type participant struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
			Role   string `json:"role"`
		}
		var parts []participant
		if json.Unmarshal([]byte(track.Participants), &parts) == nil && len(parts) > 0 {
			fmt.Fprintf(out, "\nParticipants:\n")
			for _, p := range parts {
				fmt.Fprintf(out, "  - %s (%s)\n", p.Name, p.Role)
			}
		}
	}

	// Timeline
	if track.Timeline != "" && track.Timeline != "[]" {
		type event struct {
			Date      string `json:"date"`
			Event     string `json:"event"`
			ChannelID string `json:"channel_id"`
		}
		var events []event
		if json.Unmarshal([]byte(track.Timeline), &events) == nil && len(events) > 0 {
			fmt.Fprintf(out, "\nTimeline:\n")
			for _, e := range events {
				chName := e.ChannelID
				if ch, chErr := database.GetChannelByID(e.ChannelID); chErr == nil && ch != nil && ch.Name != "" {
					chName = "#" + ch.Name
				}
				if chName != "" && chName != e.ChannelID {
					fmt.Fprintf(out, "  %s  %s — %s\n", e.Date, chName, e.Event)
				} else {
					fmt.Fprintf(out, "  %s  %s\n", e.Date, e.Event)
				}
			}
		}
	}

	// Key Messages
	if track.KeyMessages != "" && track.KeyMessages != "[]" {
		type keyMsg struct {
			TS        string `json:"ts"`
			Author    string `json:"author"`
			Text      string `json:"text"`
			ChannelID string `json:"channel_id"`
		}
		var msgs []keyMsg
		if json.Unmarshal([]byte(track.KeyMessages), &msgs) == nil && len(msgs) > 0 {
			fmt.Fprintf(out, "\nKey Messages:\n")
			for _, m := range msgs {
				chName := m.ChannelID
				if ch, chErr := database.GetChannelByID(m.ChannelID); chErr == nil && ch != nil && ch.Name != "" {
					chName = "#" + ch.Name
				}
				fmt.Fprintf(out, "  [%s] %s: %s\n", chName, m.Author, m.Text)
			}
		}
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
	fmt.Fprintln(out, "Running tracks pipeline...")

	created, updated, err := pipe.Run(cmd.Context())
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
