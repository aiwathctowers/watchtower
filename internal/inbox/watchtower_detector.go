package inbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"watchtower/internal/db"
)

// digestSituation is a minimal representation of a situation JSON entry used for
// decision detection. Only the fields relevant to this detector are decoded.
type digestSituation struct {
	Type       string `json:"type"`
	Topic      string `json:"topic"`
	Importance string `json:"importance"`
}

// wtExistsInboxItem returns true if an inbox_items row already exists for the
// given (channel_id, message_ts, trigger_type) triple. Uses a package-local
// name to avoid collisions with helpers in other detector files.
func wtExistsInboxItem(database *db.DB, channelID, messageTS, triggerType string) bool {
	var n int
	database.QueryRow( //nolint:errcheck
		`SELECT COUNT(*) FROM inbox_items WHERE channel_id=? AND message_ts=? AND trigger_type=?`,
		channelID, messageTS, triggerType,
	).Scan(&n) //nolint:errcheck
	return n > 0
}

// pendingDecision holds data for a high-importance decision extracted from a digest.
type pendingDecision struct {
	channelID string
	msgTS     string
	topic     string
}

// pendingBriefing holds data for a newly detected briefing.
type pendingBriefing struct {
	msgTS string
	date  string
}

// DetectWatchtowerInternal scans digests and briefings created after sinceTS
// and creates inbox items for:
//   - decision_made  — digest situations of type="decision" with importance="high"
//   - briefing_ready — any new briefing row
//
// Returns the number of new inbox items created.
func DetectWatchtowerInternal(_ context.Context, database *db.DB, sinceTS time.Time) (int, error) {
	sinceISO := sinceTS.UTC().Format(time.RFC3339)

	// Phase 1: collect high-importance decisions from digests.
	// We fully consume the rows cursor before doing any further DB calls to
	// avoid connection exhaustion on single-connection (in-memory) DBs.
	var decisions []pendingDecision
	rows, err := database.Query(
		`SELECT id, channel_id, situations FROM digests
		 WHERE created_at > ? AND situations IS NOT NULL AND situations != '' AND situations != '[]'`,
		sinceISO,
	)
	if err != nil {
		return 0, fmt.Errorf("watchtower detector query digests: %w", err)
	}
	for rows.Next() {
		var digestID int64
		var channelID, situations string
		if err := rows.Scan(&digestID, &channelID, &situations); err != nil {
			continue
		}
		var list []digestSituation
		if err := json.Unmarshal([]byte(situations), &list); err != nil {
			continue
		}
		for idx, s := range list {
			if s.Type == "decision" && s.Importance == "high" {
				decisions = append(decisions, pendingDecision{
					channelID: channelID,
					msgTS:     fmt.Sprintf("digest:%d:%d", digestID, idx),
					topic:     s.Topic,
				})
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("watchtower detector iterate digests: %w", err)
	}
	rows.Close()

	// Phase 2: collect new briefings.
	var briefings []pendingBriefing
	rows, err = database.Query(
		`SELECT id, date FROM briefings WHERE created_at > ?`,
		sinceISO,
	)
	if err != nil {
		return 0, fmt.Errorf("watchtower detector query briefings: %w", err)
	}
	for rows.Next() {
		var briefingID int64
		var date string
		if err := rows.Scan(&briefingID, &date); err != nil {
			continue
		}
		briefings = append(briefings, pendingBriefing{
			msgTS: fmt.Sprintf("briefing:%d", briefingID),
			date:  date,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("watchtower detector iterate briefings: %w", err)
	}
	rows.Close()

	// Phase 3: dedup-check and create inbox items (rows fully closed above).
	created := 0
	for _, d := range decisions {
		if wtExistsInboxItem(database, d.channelID, d.msgTS, "decision_made") {
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		item := db.InboxItem{
			ChannelID:    d.channelID,
			MessageTS:    d.msgTS,
			SenderUserID: "watchtower",
			TriggerType:  "decision_made",
			Snippet:      d.topic,
			ItemClass:    DefaultItemClass("decision_made"),
			Status:       "pending",
			Priority:     "medium",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if _, err := database.CreateInboxItem(item); err == nil {
			created++
		}
	}
	for _, b := range briefings {
		if wtExistsInboxItem(database, "briefing", b.msgTS, "briefing_ready") {
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339)
		item := db.InboxItem{
			ChannelID:    "briefing",
			MessageTS:    b.msgTS,
			SenderUserID: "watchtower",
			TriggerType:  "briefing_ready",
			Snippet:      "Daily briefing ready for " + b.date,
			ItemClass:    DefaultItemClass("briefing_ready"),
			Status:       "pending",
			Priority:     "low",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if _, err := database.CreateInboxItem(item); err == nil {
			created++
		}
	}
	return created, nil
}
