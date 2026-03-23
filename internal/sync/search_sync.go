package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/db"

	"github.com/slack-go/slack"
)

// maxDiscoveryPages limits pagination to avoid excessive API calls.
const maxDiscoveryPages = 200

// searchChannelType maps a search result CtxChannel to our type string.
func searchChannelType(ch slack.CtxChannel) string {
	if ch.IsMPIM {
		return "group_dm"
	}
	if ch.IsPrivate && strings.HasPrefix(ch.ID, "D") {
		return "dm"
	}
	if ch.IsPrivate {
		return "private"
	}
	return "public"
}

// syncViaSearch uses search.messages to find and save recent messages directly,
// without per-channel conversations.history calls. This dramatically reduces
// API calls for incremental sync (~8-10 calls vs ~50+).
func (o *Orchestrator) syncViaSearch(ctx context.Context) error {
	days := o.config.Sync.InitialHistoryDays
	if days <= 0 {
		days = 30
	}

	// Determine search start date
	lastDate, err := o.db.GetSearchLastDate()
	if err != nil {
		return fmt.Errorf("getting search_last_date: %w", err)
	}

	var searchAfter string
	if lastDate != "" {
		// Parse and subtract 2 days for overlap to account for Slack search indexing delays
		t, err := time.Parse("2006-01-02", lastDate)
		if err != nil {
			o.logger.Printf("warning: invalid search_last_date %q, using default", lastDate)
			searchAfter = time.Now().AddDate(0, 0, -days).Format("2006-01-02")
		} else {
			searchAfter = t.AddDate(0, 0, -2).Format("2006-01-02")
		}
	} else {
		searchAfter = time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	}

	query := fmt.Sprintf("after:%s", searchAfter)
	o.logger.Printf("search sync: query=%q", query)

	seenChannels := make(map[string]bool)
	seenUsers := make(map[string]bool)
	totalMessages := 0
	page := 1
	completedAllPages := false

	var oldestFetchedTS string // track oldest message timestamp across all pages

	for page <= maxDiscoveryPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		result, err := o.slackClient.SearchMessages(ctx, query, page)
		if err != nil {
			if isNonFatalError(err) {
				o.logger.Printf("search sync: non-fatal error on page %d, stopping early: %v", page, err)
				// Don't set completedAllPages — watermark stays unchanged.
				break
			}
			return fmt.Errorf("search sync (page %d): %w", page, err)
		}

		if len(result.Messages) == 0 {
			break
		}

		// Convert search messages to db.Message and collect channel/user info
		dbMsgs := make([]db.Message, 0, len(result.Messages))
		for _, msg := range result.Messages {
			// Track the oldest message timestamp (results are sorted newest-first)
			if oldestFetchedTS == "" || msg.Timestamp < oldestFetchedTS {
				oldestFetchedTS = msg.Timestamp
			}

			// Ensure channel
			if msg.Channel.ID != "" && !seenChannels[msg.Channel.ID] {
				seenChannels[msg.Channel.ID] = true
				chType := searchChannelType(msg.Channel)
				name := msg.Channel.Name
				if name == "" {
					name = msg.Channel.ID
				}
				// For DMs, Slack search returns the user ID as the channel name.
				// Extract it so we can resolve to a display name later.
				var dmUserID string
				if chType == "dm" && strings.HasPrefix(name, "U") {
					dmUserID = name
				}
				if err := o.db.EnsureChannel(msg.Channel.ID, name, chType, dmUserID); err != nil {
					return fmt.Errorf("ensuring channel %s: %w", msg.Channel.ID, err)
				}
			}

			// Ensure user
			if msg.User != "" && !seenUsers[msg.User] {
				seenUsers[msg.User] = true
				userName := msg.Username
				if userName == "" {
					userName = msg.User
				}
				if err := o.db.EnsureUser(msg.User, userName); err != nil {
					return fmt.Errorf("ensuring user %s: %w", msg.User, err)
				}
			}

			// Convert SearchMessage to db.Message
			rawJSON, err := json.Marshal(msg)
			if err != nil {
				o.logger.Printf("warning: failed to marshal search message %s: %v", msg.Timestamp, err)
				rawJSON = []byte("{}")
			}

			dbMsgs = append(dbMsgs, db.Message{
				ChannelID:  msg.Channel.ID,
				TS:         msg.Timestamp,
				UserID:     msg.User,
				Text:       msg.Text,
				ThreadTS:   sql.NullString{},
				ReplyCount: 0,
				IsEdited:   false,
				IsDeleted:  false,
				Subtype:    "",
				Permalink:  msg.Permalink,
				RawJSON:    string(rawJSON),
			})
		}

		// Batch upsert messages
		if len(dbMsgs) > 0 {
			count, err := o.upsertSearchPage(dbMsgs)
			if err != nil {
				return err
			}
			totalMessages += count
			o.progress.AddMessages(count)
		}

		o.progress.SetDiscovery(page, result.Pages, len(seenChannels), len(seenUsers))
		o.logger.Printf("search sync: page %d/%d, %d channels, %d users, %d messages",
			page, result.Pages, len(seenChannels), len(seenUsers), totalMessages)

		if page >= result.Pages {
			completedAllPages = true
			break
		}
		page++
	}

	// Log a warning when we hit the page limit without fetching everything
	if !completedAllPages && page > maxDiscoveryPages {
		o.logger.Printf("WARNING: search sync hit page limit (%d pages, %d messages fetched). "+
			"Some older messages in the search window may have been missed. "+
			"Consider running 'watchtower sync --full' to catch up.", maxDiscoveryPages, totalMessages)
	}

	// Advance the watermark based on what we fetched.
	if completedAllPages {
		// All pages fetched — safe to set watermark to today.
		today := time.Now().Format("2006-01-02")
		if err := o.db.SetSearchLastDate(today); err != nil {
			return fmt.Errorf("saving search_last_date: %w", err)
		}
	} else if oldestFetchedTS != "" && page > maxDiscoveryPages {
		// Hit the page limit — advance watermark to the oldest message we DID fetch.
		// This prevents the next sync from re-scanning the same pages endlessly
		// while messages beyond the page limit remain unreachable.
		// The 2-day overlap on next sync provides a safety buffer.
		ts, parseErr := parseSlackTS(oldestFetchedTS)
		if parseErr == nil {
			oldestDate := ts.Format("2006-01-02")
			if err := o.db.SetSearchLastDate(oldestDate); err != nil {
				return fmt.Errorf("saving search_last_date: %w", err)
			}
			o.logger.Printf("search sync: advanced watermark to %s (oldest fetched message)", oldestDate)
		}
	}

	// Populate discoveredChannelIDs so the full-sync fallback can skip inactive channels.
	o.discoveredChannelIDs = make(map[string]bool, len(seenChannels))
	for chID := range seenChannels {
		o.discoveredChannelIDs[chID] = true
	}

	o.progress.SetDiscovery(page, page, len(seenChannels), len(seenUsers))
	o.logger.Printf("search sync complete: %d channels, %d users, %d messages from %d pages",
		len(seenChannels), len(seenUsers), totalMessages, page)
	return nil
}

// parseSlackTS parses a Slack message timestamp ("1234567890.123456") into a time.Time.
func parseSlackTS(ts string) (time.Time, error) {
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return time.Time{}, fmt.Errorf("invalid slack timestamp: %q", ts)
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid slack timestamp: %q", ts)
	}
	return time.Unix(sec, 0), nil
}

// upsertSearchPage wraps a batch upsert in its own function scope so that
// defer tx.Rollback() runs per-page rather than accumulating in the caller's loop.
func (o *Orchestrator) upsertSearchPage(msgs []db.Message) (int, error) {
	tx, err := o.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	count, err := o.db.UpsertMessageBatch(tx, msgs)
	if err != nil {
		return 0, fmt.Errorf("upserting search messages: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing search messages: %w", err)
	}
	return count, nil
}
