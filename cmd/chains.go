package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/chains"
	"watchtower/internal/config"
	"watchtower/internal/db"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var (
	chainsFlagStatus string
)

var chainsCmd = &cobra.Command{
	Use:   "chains [id]",
	Short: "Show thematic chains of related decisions and tracks",
	Long:  "Displays chains — groups of related decisions and tracks that evolve over time across channels.",
	RunE:  runChains,
}

var chainsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run chains pipeline to link unlinked decisions",
	RunE:  runChainsGenerate,
}

var chainsArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive a chain (mark as resolved)",
	Args:  cobra.ExactArgs(1),
	RunE:  runChainsArchive,
}

func init() {
	rootCmd.AddCommand(chainsCmd)
	chainsCmd.AddCommand(chainsGenerateCmd)
	chainsCmd.AddCommand(chainsArchiveCmd)
	chainsCmd.Flags().StringVar(&chainsFlagStatus, "status", "", "filter by status (active, resolved, stale)")
}

func channelNameFromDB(database *db.DB, channelID string) string {
	ch, err := database.GetChannelByID(channelID)
	if err != nil || ch == nil {
		return channelID
	}
	return ch.Name
}

func runChains(cmd *cobra.Command, args []string) error {
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

	// If an ID is given, show chain detail.
	if len(args) > 0 {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid chain ID: %s", args[0])
		}
		return showChainDetail(database, out, id)
	}

	// List chains.
	filter := db.ChainFilter{
		Status: chainsFlagStatus,
	}
	allChains, err := database.GetChains(filter)
	if err != nil {
		return fmt.Errorf("querying chains: %w", err)
	}

	if len(allChains) == 0 {
		fmt.Fprintln(out, "No chains found. Run 'watchtower chains generate' or wait for the next daemon sync.")
		return nil
	}

	for _, c := range allChains {
		statusIcon := "🔗"
		switch c.Status {
		case "resolved":
			statusIcon = "✅"
		case "stale":
			statusIcon = "💤"
		}

		lastSeen := time.Unix(int64(c.LastSeen), 0)
		ago := humanize.Time(lastSeen)

		var channelNames []string
		var channelIDs []string
		_ = json.Unmarshal([]byte(c.ChannelIDs), &channelIDs)
		for _, chID := range channelIDs {
			channelNames = append(channelNames, "#"+channelNameFromDB(database, chID))
		}

		fmt.Fprintf(out, "%s #%d  %s  (%d items, %s)\n", statusIcon, c.ID, c.Title, c.ItemCount, ago)
		if len(channelNames) > 0 {
			fmt.Fprintf(out, "   Channels: %s\n", strings.Join(channelNames, ", "))
		}
		if c.Summary != "" {
			fmt.Fprintf(out, "   %s\n", c.Summary)
		}
		fmt.Fprintln(out)
	}

	return nil
}

func showChainDetail(database *db.DB, out io.Writer, id int) error {
	chain, err := database.GetChainByID(id)
	if err != nil {
		return fmt.Errorf("chain #%d not found: %w", id, err)
	}

	fmt.Fprintf(out, "Chain #%d: %s\n", chain.ID, chain.Title)
	fmt.Fprintf(out, "Status: %s | Slug: %s\n", chain.Status, chain.Slug)
	fmt.Fprintf(out, "Items: %d | First: %s | Last: %s\n",
		chain.ItemCount,
		time.Unix(int64(chain.FirstSeen), 0).Local().Format("2006-01-02"),
		time.Unix(int64(chain.LastSeen), 0).Local().Format("2006-01-02"))

	if chain.Summary != "" {
		fmt.Fprintf(out, "\nSummary: %s\n", chain.Summary)
	}

	refs, err := database.GetChainRefs(id)
	if err != nil {
		return fmt.Errorf("getting chain refs: %w", err)
	}

	if len(refs) > 0 {
		fmt.Fprintf(out, "\nTimeline (%d items):\n", len(refs))
		for _, ref := range refs {
			ts := time.Unix(int64(ref.Timestamp), 0).Local().Format("2006-01-02 15:04")
			chName := channelNameFromDB(database, ref.ChannelID)

			switch ref.RefType {
			case "decision":
				digest, err := database.GetDigestByID(ref.DigestID)
				if err != nil {
					fmt.Fprintf(out, "  %s  [decision] #%s — (digest #%d not found)\n", ts, chName, ref.DigestID)
					continue
				}
				type dec struct {
					Text       string `json:"text"`
					By         string `json:"by"`
					Importance string `json:"importance"`
				}
				var decs []dec
				_ = json.Unmarshal([]byte(digest.Decisions), &decs)
				if ref.DecisionIdx < len(decs) {
					d := decs[ref.DecisionIdx]
					fmt.Fprintf(out, "  %s  [decision] #%s — %s (by %s, %s)\n", ts, chName, d.Text, d.By, d.Importance)
				}

			case "track":
				track, err := database.GetTrackByID(ref.TrackID)
				if err != nil {
					fmt.Fprintf(out, "  %s  [track] #%s — (track #%d not found)\n", ts, chName, ref.TrackID)
					continue
				}
				fmt.Fprintf(out, "  %s  [track] #%s — %s (%s, %s)\n", ts, chName, track.Text, track.Status, track.Priority)
			}
		}
	}

	return nil
}

func runChainsGenerate(cmd *cobra.Command, args []string) error {
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

	logger := log.New(cmd.ErrOrStderr(), "[chains] ", log.LstdFlags)

	gen, cleanupPool := cliPooledGenerator(cfg, logger)
	defer cleanupPool()
	pipe := chains.New(database, cfg, gen, logger)

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Linking decisions to chains...")
	n, err := pipe.Run(cmd.Context())
	if err != nil {
		return fmt.Errorf("chains pipeline: %w", err)
	}

	if n > 0 {
		fmt.Fprintf(out, "Linked %d decision(s) to chains.\n", n)
	} else {
		fmt.Fprintln(out, "No unlinked decisions found.")
	}

	return nil
}

func runChainsArchive(cmd *cobra.Command, args []string) error {
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

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid chain ID: %s", args[0])
	}

	if err := database.UpdateChainStatus(id, "resolved"); err != nil {
		return fmt.Errorf("archiving chain: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Chain #%d archived.\n", id)
	return nil
}
