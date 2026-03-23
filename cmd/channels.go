package cmd

import (
	"fmt"
	"time"

	"watchtower/internal/db"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	channelsFlagType string
	channelsFlagSort string
)

var channelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "List synced channels",
	Long:  "List all channels synced to the local database with message counts, last activity, and watched status.",
	RunE:  runChannels,
}

func init() {
	rootCmd.AddCommand(channelsCmd)
	channelsCmd.Flags().StringVar(&channelsFlagType, "type", "", "filter by channel type: public, private, dm, group_dm")
	channelsCmd.Flags().StringVar(&channelsFlagSort, "sort", "name", "sort by: name, messages, recent")
}

func runChannels(cmd *cobra.Command, args []string) error {
	if channelsFlagType != "" {
		switch channelsFlagType {
		case "public", "private", "dm", "group_dm":
		default:
			return fmt.Errorf("invalid type %q: must be public, private, dm, or group_dm", channelsFlagType)
		}
	}

	var sort db.ChannelListSort
	switch channelsFlagSort {
	case "name":
		sort = db.ChannelSortName
	case "messages":
		sort = db.ChannelSortMessages
	case "recent":
		sort = db.ChannelSortRecent
	default:
		return fmt.Errorf("invalid sort %q: must be name, messages, or recent", channelsFlagSort)
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	filter := db.ChannelFilter{Type: channelsFlagType}
	channels, err := database.GetChannelList(filter, sort)
	if err != nil {
		return fmt.Errorf("listing channels: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(channels) == 0 {
		fmt.Fprintln(out, "No channels found. Run sync first?")
		return nil
	}

	for _, ch := range channels {
		watched := ""
		if ch.IsWatched {
			watched = " [watched]"
		}

		activity := "no messages"
		if ch.LastActivity.Valid {
			ts := ch.LastActivity.Float64
			// Upper bound: max integer exactly representable in float64
			if ts >= 0 && ts <= 1e13 {
				t := time.Unix(int64(ts), 0)
				activity = humanize.Time(t)
			} else {
				activity = "invalid date"
			}
		}

		fmt.Fprintf(out, "%-30s  %-10s  %5d members  %6s msgs  %s%s\n",
			ch.Name,
			ch.Type,
			ch.NumMembers,
			humanize.Comma(int64(ch.MessageCount)),
			activity,
			watched,
		)
	}

	fmt.Fprintf(out, "\n%d channels total\n", len(channels))
	return nil
}
