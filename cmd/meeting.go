package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/meeting"

	"github.com/spf13/cobra"
)

var (
	meetingPrepFlagJSON         bool
	meetingPrepFlagForceRefresh bool
	meetingPrepFlagUserNotes    string

	meetingExtractTopicsFlagText    string
	meetingExtractTopicsFlagEventID string
	meetingExtractTopicsFlagJSON    bool
)

var meetingPrepCmd = &cobra.Command{
	Use:   "meeting-prep [event-id|next]",
	Short: "Generate AI-powered meeting preparation",
	Long:  "Analyzes attendees, shared tracks, open items, and people cards to create a meeting brief.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMeetingPrep,
}

var meetingExtractTopicsCmd = &cobra.Command{
	Use:   "extract-topics",
	Short: "Split pasted text into discrete discussion topics (AI)",
	Long:  "Extracts atomic discussion topics from pasted text (recap, notes, rambling status) for seeding a meeting's Discussion Topics list. Non-interactive — caller persists the result.",
	RunE:  runMeetingExtractTopics,
}

func init() {
	rootCmd.AddCommand(meetingPrepCmd)
	meetingPrepCmd.Flags().BoolVar(&meetingPrepFlagJSON, "json", false, "output as JSON")
	meetingPrepCmd.Flags().BoolVar(&meetingPrepFlagForceRefresh, "force-refresh", false, "regenerate even if cached result exists")
	meetingPrepCmd.Flags().StringVar(&meetingPrepFlagUserNotes, "user-notes", "", "additional context or agenda notes from the user")

	meetingPrepCmd.AddCommand(meetingExtractTopicsCmd)
	meetingExtractTopicsCmd.Flags().StringVar(&meetingExtractTopicsFlagText, "text", "", "raw text to split into topics (required)")
	meetingExtractTopicsCmd.Flags().StringVar(&meetingExtractTopicsFlagEventID, "event-id", "", "optional event id for title context")
	meetingExtractTopicsCmd.Flags().BoolVar(&meetingExtractTopicsFlagJSON, "json", false, "output as JSON (default format is also JSON — kept for symmetry)")
}

func runMeetingPrep(cmd *cobra.Command, args []string) error {
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

	gen := cliGenerator(cfg)

	pipe := meeting.New(database, cfg, gen, nil)

	ctx := cmd.Context()
	var result *meeting.MeetingPrepResult

	eventID := ""
	if len(args) > 0 && args[0] != "next" {
		eventID = args[0]
	}

	// Check cache first (unless force-refresh).
	if eventID != "" && !meetingPrepFlagForceRefresh {
		if cached, err := database.GetMeetingPrepCache(eventID); err == nil && cached != nil && cached.ResultJSON != "" {
			var cachedResult meeting.MeetingPrepResult
			if json.Unmarshal([]byte(cached.ResultJSON), &cachedResult) == nil {
				result = &cachedResult
			}
		}
	}

	if result == nil {
		if eventID == "" {
			result, err = pipe.PrepareForNext(ctx, meetingPrepFlagUserNotes)
		} else {
			result, err = pipe.PrepareForEvent(ctx, eventID, meetingPrepFlagUserNotes)
		}
		if err != nil {
			return err
		}

		// Cache the result.
		if resultJSON, jsonErr := json.Marshal(result); jsonErr == nil {
			_ = database.SaveMeetingPrepCache(db.MeetingPrepCache{
				EventID:    result.EventID,
				ResultJSON: string(resultJSON),
				UserNotes:  meetingPrepFlagUserNotes,
			})
		}
	}

	out := cmd.OutOrStdout()

	if meetingPrepFlagJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	// Formatted output.
	start, _ := time.Parse(time.RFC3339, result.StartTime)
	start = start.Local()

	fmt.Fprintf(out, "Meeting Prep: %s\n", result.Title)
	fmt.Fprintf(out, "Time: %s\n", start.Format("Mon Jan 2, 15:04"))
	fmt.Fprintln(out)

	if len(result.TalkingPoints) > 0 {
		fmt.Fprintln(out, "Talking Points:")
		for i, tp := range result.TalkingPoints {
			priority := ""
			if tp.Priority == "high" {
				priority = " [HIGH]"
			}
			fmt.Fprintf(out, "  %d. %s%s\n", i+1, tp.Text, priority)
			if tp.SourceType != "" && tp.SourceID != "" {
				fmt.Fprintf(out, "     (%s #%s)\n", tp.SourceType, tp.SourceID)
			}
		}
		fmt.Fprintln(out)
	}

	if len(result.OpenItems) > 0 {
		fmt.Fprintln(out, "Open Items:")
		for _, item := range result.OpenItems {
			fmt.Fprintf(out, "  - %s", item.Text)
			if item.PersonName != "" {
				fmt.Fprintf(out, " (%s)", item.PersonName)
			}
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out)
	}

	if len(result.PeopleNotes) > 0 {
		fmt.Fprintln(out, "People Notes:")
		for _, pn := range result.PeopleNotes {
			fmt.Fprintf(out, "  %s:\n", pn.Name)
			if pn.CommunicationTip != "" {
				fmt.Fprintf(out, "    Tip: %s\n", pn.CommunicationTip)
			}
			if pn.RecentContext != "" {
				fmt.Fprintf(out, "    Context: %s\n", pn.RecentContext)
			}
		}
		fmt.Fprintln(out)
	}

	if len(result.SuggestedPrep) > 0 {
		fmt.Fprintln(out, "Suggested Prep:")
		for _, s := range result.SuggestedPrep {
			fmt.Fprintf(out, "  - %s\n", s)
		}
		fmt.Fprintln(out)
	}

	return nil
}

func runMeetingExtractTopics(cmd *cobra.Command, args []string) error {
	if meetingExtractTopicsFlagText == "" {
		return fmt.Errorf("--text is required")
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

	gen := cliGenerator(cfg)
	pipe := meeting.New(database, cfg, gen, nil)

	eventTitle := ""
	if meetingExtractTopicsFlagEventID != "" {
		if ev, err := database.GetCalendarEventByID(meetingExtractTopicsFlagEventID); err == nil && ev != nil {
			eventTitle = ev.Title
		}
	}

	result, err := pipe.ExtractDiscussionTopics(cmd.Context(), meetingExtractTopicsFlagText, eventTitle)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
