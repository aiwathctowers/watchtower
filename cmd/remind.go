package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"watchtower/internal/db"
)

const remindDueLayout = "2006-01-02T15:04"

var (
	remindFlagAt       string
	remindFlagIn       string
	remindFlagPriority string
	remindFlagIntent   string
)

var remindCmd = &cobra.Command{
	Use:   "remind <text>",
	Short: "Schedule a reminder that surfaces in the inbox once due",
	Long: `Create a target with a due date — the daemon surfaces it into the
inbox the moment it becomes overdue, so you get an OS notification through
the same channel as Slack/Jira/Calendar reminders.

Examples:
  watchtower remind "Send Q1 review" --in 4h
  watchtower remind "Call Vasya"      --at 2026-05-01T16:00:00Z
  watchtower remind "Renew cert"      --in 7d --priority high`,
	Args: cobra.ExactArgs(1),
	RunE: runRemind,
}

func runRemind(cmd *cobra.Command, args []string) error {
	due, err := parseRemindWhen(remindFlagAt, remindFlagIn, time.Now().UTC())
	if err != nil {
		return err
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	today := time.Now().UTC().Format("2006-01-02")
	id, err := database.CreateTarget(db.Target{
		Text:        args[0],
		Intent:      remindFlagIntent,
		Level:       "day",
		PeriodStart: today,
		PeriodEnd:   today,
		Status:      "todo",
		Priority:    remindFlagPriority,
		Ownership:   "mine",
		DueDate:     due,
		SourceType:  "manual",
	})
	if err != nil {
		return fmt.Errorf("creating reminder: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Reminder #%d scheduled for %s UTC\n", id, due)
	return nil
}

// parseRemindWhen converts the user's --at / --in flags into the due_date
// string used by the targets table. Exactly one of `at` or `in` must be set.
//
// `at`  accepts RFC3339 (`2026-05-01T16:00:00Z`) or short form
//
//	(`2026-05-01T16:00`); both are interpreted in UTC.
//
// `in`  accepts a duration like `30m`, `4h`, or `2d`. Days are not supported
//
//	by Go's time.ParseDuration so we handle the `d` suffix ourselves.
func parseRemindWhen(at, in string, now time.Time) (string, error) {
	switch {
	case at != "" && in != "":
		return "", fmt.Errorf("--at and --in are mutually exclusive")
	case at == "" && in == "":
		return "", fmt.Errorf("specify when: pass --at <timestamp> or --in <duration>")
	}

	if at != "" {
		for _, layout := range []string{time.RFC3339, remindDueLayout} {
			if t, err := time.Parse(layout, at); err == nil {
				return t.UTC().Format(remindDueLayout), nil
			}
		}
		return "", fmt.Errorf("--at: parse %q as RFC3339 or %s", at, remindDueLayout)
	}

	d, err := parseRemindDuration(in)
	if err != nil {
		return "", fmt.Errorf("--in: %w", err)
	}
	return now.Add(d).UTC().Format(remindDueLayout), nil
}

// parseRemindDuration accepts the same syntax as time.ParseDuration plus a
// trailing `d` for days.
func parseRemindDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("parse %q as N-days", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func init() {
	remindCmd.Flags().StringVar(&remindFlagAt, "at", "", "absolute time (RFC3339 or YYYY-MM-DDTHH:MM)")
	remindCmd.Flags().StringVar(&remindFlagIn, "in", "", "relative duration: 30m, 4h, 2d")
	remindCmd.Flags().StringVar(&remindFlagPriority, "priority", "medium", "priority (high, medium, low)")
	remindCmd.Flags().StringVar(&remindFlagIntent, "intent", "", "free-text intent or context")
	rootCmd.AddCommand(remindCmd)
}
