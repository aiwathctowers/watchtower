package inbox

import (
	"context"
	"fmt"
	"time"

	"watchtower/internal/db"
)

const (
	minEvidence  = 5
	muteRateSend = 0.8
	muteRateChan = 0.7
)

type dismissStat struct {
	key      string // "sender:X" or "channel:X"
	weight   float64
	evidence int
}

// RunImplicitLearner scans inbox_items within the lookback window, computes
// per-sender and per-channel dismiss rates, and upserts source_mute learned
// rules when thresholds are exceeded. It never overwrites user_rule entries.
// Returns the number of rules upserted and any error.
func RunImplicitLearner(ctx context.Context, database *db.DB, lookback time.Duration) (int, error) {
	cutoff := time.Now().Add(-lookback).UTC().Format(time.RFC3339)

	var rules []dismissStat

	// Per-sender dismiss rate — collect all rows before issuing any further queries.
	senderRows, err := database.Query(`
		SELECT sender_user_id,
		       COUNT(*) AS total,
		       SUM(CASE WHEN status='dismissed' THEN 1 ELSE 0 END) AS dismisses
		FROM inbox_items
		WHERE created_at > ?
		GROUP BY sender_user_id
		HAVING total >= ?
	`, cutoff, minEvidence)
	if err != nil {
		return 0, fmt.Errorf("sender query: %w", err)
	}
	for senderRows.Next() {
		var sender string
		var total, dismisses int
		if err := senderRows.Scan(&sender, &total, &dismisses); err != nil {
			senderRows.Close()
			return 0, fmt.Errorf("sender scan: %w", err)
		}
		if float64(dismisses)/float64(total) >= muteRateSend {
			rules = append(rules, dismissStat{key: "sender:" + sender, weight: -0.7, evidence: dismisses})
		}
	}
	if err := senderRows.Err(); err != nil {
		senderRows.Close()
		return 0, fmt.Errorf("sender rows: %w", err)
	}
	senderRows.Close()

	// Per-channel dismiss rate — collect all rows before issuing any further queries.
	chanRows, err := database.Query(`
		SELECT channel_id,
		       COUNT(*) AS total,
		       SUM(CASE WHEN status='dismissed' THEN 1 ELSE 0 END) AS dismisses
		FROM inbox_items
		WHERE created_at > ?
		GROUP BY channel_id
		HAVING total >= ?
	`, cutoff, minEvidence)
	if err != nil {
		return 0, fmt.Errorf("channel query: %w", err)
	}
	for chanRows.Next() {
		var ch string
		var total, dismisses int
		if err := chanRows.Scan(&ch, &total, &dismisses); err != nil {
			chanRows.Close()
			return 0, fmt.Errorf("channel scan: %w", err)
		}
		if float64(dismisses)/float64(total) >= muteRateChan {
			rules = append(rules, dismissStat{key: "channel:" + ch, weight: -0.5, evidence: dismisses})
		}
	}
	if err := chanRows.Err(); err != nil {
		chanRows.Close()
		return 0, fmt.Errorf("channel rows: %w", err)
	}
	chanRows.Close()

	// Upsert all collected rules — cursor is closed, connection is free.
	upserted := 0
	for _, r := range rules {
		if err := database.UpsertLearnedRuleImplicit(db.InboxLearnedRule{
			RuleType:      "source_mute",
			ScopeKey:      r.key,
			Weight:        r.weight,
			EvidenceCount: r.evidence,
		}); err != nil {
			return upserted, err
		}
		upserted++
	}
	return upserted, nil
}
