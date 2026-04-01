package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"watchtower/internal/briefing"
	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/ui"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var briefingCmd = &cobra.Command{
	Use:   "briefing",
	Short: "Show today's daily briefing",
	Long:  "Displays your personalized daily briefing with attention items, tracks, digest highlights, team pulse, and coaching tips.",
	RunE:  runBriefing,
}

var briefingGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a daily briefing from existing data",
	Long:  "Runs the briefing pipeline on already-generated digests, tracks, and people cards.",
	RunE:  runBriefingGenerate,
}

var briefingListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent briefings",
	RunE:  runBriefingList,
}

var (
	briefingListFlagLimit int
	briefingFlagJSON      bool
)

func init() {
	rootCmd.AddCommand(briefingCmd)
	briefingCmd.AddCommand(briefingGenerateCmd)
	briefingCmd.AddCommand(briefingListCmd)

	briefingCmd.Flags().BoolVar(&briefingFlagJSON, "json", false, "output as JSON")
	briefingListCmd.Flags().IntVar(&briefingListFlagLimit, "limit", 10, "number of briefings to show")
}

func runBriefing(cmd *cobra.Command, _ []string) error {
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

	userID, err := database.GetCurrentUserID()
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set. Run 'watchtower sync' first.")
		return nil
	}

	today := time.Now().Format("2006-01-02")
	b, err := database.GetBriefing(userID, today)
	if err != nil {
		return fmt.Errorf("loading briefing: %w", err)
	}
	if b == nil {
		fmt.Fprintln(out, "No briefing for today. Run 'watchtower briefing generate' or wait for the daemon.")
		return nil
	}

	if briefingFlagJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(b)
	}

	printBriefing(out, b)

	_ = database.MarkBriefingRead(b.ID)

	return nil
}

func runBriefingGenerate(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if flagWorkspace != "" {
		cfg.ActiveWorkspace = flagWorkspace
	}
	applyProviderOverride(cfg)
	if err := cfg.ValidateWorkspace(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	cfg.Briefing.Enabled = true

	if err := validateModel(cfg); err != nil {
		return err
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

	pipe := briefing.New(database, cfg, gen, logger)

	id, err := pipe.Run(cmd.Context())
	if err != nil {
		return fmt.Errorf("generating briefing: %w", err)
	}
	if id == 0 {
		fmt.Fprintln(out, "No data available for briefing generation. Run 'watchtower digest generate' first.")
		return nil
	}

	b, err := database.GetBriefingByID(id)
	if err != nil {
		return fmt.Errorf("loading generated briefing: %w", err)
	}

	printBriefing(out, b)
	return nil
}

func runBriefingList(cmd *cobra.Command, _ []string) error {
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

	userID, err := database.GetCurrentUserID()
	if err != nil || userID == "" {
		fmt.Fprintln(out, "No current user set.")
		return nil
	}

	briefings, err := database.GetRecentBriefings(userID, briefingListFlagLimit)
	if err != nil {
		return fmt.Errorf("listing briefings: %w", err)
	}

	if len(briefings) == 0 {
		fmt.Fprintln(out, "No briefings found.")
		return nil
	}

	for _, b := range briefings {
		readStatus := "unread"
		if b.ReadAt.Valid {
			readStatus = "read"
		}
		var attention []json.RawMessage
		_ = json.Unmarshal([]byte(b.Attention), &attention)
		fmt.Fprintf(out, "#%d  %s  [%s]  %d attention item(s)\n",
			b.ID, b.Date, readStatus, len(attention))
	}

	return nil
}

func printBriefing(out io.Writer, b *db.Briefing) {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("# Daily Briefing — %s\n\n", b.Date))
	if b.Role != "" {
		buf.WriteString(fmt.Sprintf("*Role: %s*\n\n", b.Role))
	}

	// Attention.
	var attention []briefing.AttentionItem
	if err := json.Unmarshal([]byte(b.Attention), &attention); err == nil && len(attention) > 0 {
		buf.WriteString("## Attention Required\n\n")
		for _, a := range attention {
			priority := ""
			if a.Priority == "high" {
				priority = " **[HIGH]**"
			}
			buf.WriteString(fmt.Sprintf("- %s%s\n", a.Text, priority))
			if a.Reason != "" {
				buf.WriteString(fmt.Sprintf("  *%s*\n", a.Reason))
			}
		}
		buf.WriteString("\n")
	}

	// Your Day.
	var yourDay []briefing.YourDayItem
	if err := json.Unmarshal([]byte(b.YourDay), &yourDay); err == nil && len(yourDay) > 0 {
		buf.WriteString("## Your Day\n\n")
		for _, item := range yourDay {
			priorityTag := ""
			if item.Priority == "high" {
				priorityTag = " **[HIGH]**"
			}
			buf.WriteString(fmt.Sprintf("- %s%s", item.Text, priorityTag))
			if item.DueDate != "" {
				buf.WriteString(fmt.Sprintf(" (due: %s)", item.DueDate))
			}
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}

	// What Happened.
	var whatHappened []briefing.WhatHappenedItem
	if err := json.Unmarshal([]byte(b.WhatHappened), &whatHappened); err == nil && len(whatHappened) > 0 {
		buf.WriteString("## What Happened\n\n")
		for _, item := range whatHappened {
			typeTag := ""
			if item.ItemType != "" {
				typeTag = fmt.Sprintf(" [%s]", item.ItemType)
			}
			channel := ""
			if item.ChannelName != "" {
				channel = fmt.Sprintf(" (%s)", item.ChannelName)
			}
			buf.WriteString(fmt.Sprintf("- %s%s%s\n", item.Text, typeTag, channel))
		}
		buf.WriteString("\n")
	}

	// Team Pulse.
	var teamPulse []briefing.TeamPulseItem
	if err := json.Unmarshal([]byte(b.TeamPulse), &teamPulse); err == nil && len(teamPulse) > 0 {
		buf.WriteString("## Team Pulse\n\n")
		for _, item := range teamPulse {
			signalTag := ""
			if item.SignalType != "" {
				signalTag = fmt.Sprintf(" [%s]", item.SignalType)
			}
			buf.WriteString(fmt.Sprintf("- %s%s\n", item.Text, signalTag))
			if item.Detail != "" {
				buf.WriteString(fmt.Sprintf("  *%s*\n", item.Detail))
			}
		}
		buf.WriteString("\n")
	}

	// Coaching.
	var coaching []briefing.CoachingItem
	if err := json.Unmarshal([]byte(b.Coaching), &coaching); err == nil && len(coaching) > 0 {
		buf.WriteString("## Coaching Tips\n\n")
		for _, item := range coaching {
			catTag := ""
			if item.Category != "" {
				catTag = fmt.Sprintf(" [%s]", item.Category)
			}
			buf.WriteString(fmt.Sprintf("- **%s**%s\n", item.Text, catTag))
		}
		buf.WriteString("\n")
	}

	// Footer.
	created, _ := time.Parse("2006-01-02T15:04:05Z", b.CreatedAt)
	if !created.IsZero() {
		buf.WriteString(fmt.Sprintf("---\n*Generated %s*\n", humanize.Time(created)))
	}

	fmt.Fprint(out, ui.RenderMarkdown(buf.String()))
}
