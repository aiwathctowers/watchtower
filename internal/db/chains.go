package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// CreateChain inserts a new chain and returns its ID.
// If a chain with the same slug already exists, returns the existing chain's ID.
func (db *DB) CreateChain(c Chain) (int64, error) {
	var parentID any
	if c.ParentID > 0 {
		parentID = c.ParentID
	}
	res, err := db.Exec(`INSERT OR IGNORE INTO chains (parent_id, title, slug, status, summary, channel_ids, first_seen, last_seen, item_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		parentID, c.Title, c.Slug, c.Status, c.Summary, c.ChannelIDs, c.FirstSeen, c.LastSeen, c.ItemCount)
	if err != nil {
		return 0, fmt.Errorf("creating chain: %w", err)
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// Slug conflict — return existing chain's ID.
		var existingID int64
		if err := db.QueryRow(`SELECT id FROM chains WHERE slug = ?`, c.Slug).Scan(&existingID); err != nil {
			return 0, fmt.Errorf("finding existing chain by slug %q: %w", c.Slug, err)
		}
		return existingID, nil
	}
	return id, nil
}

// UpdateChainSummary updates the summary, last_seen, item_count, and channel_ids of a chain.
func (db *DB) UpdateChainSummary(id int, summary string, lastSeen float64, itemCount int, channelIDs string) error {
	_, err := db.Exec(`UPDATE chains SET summary = ?, last_seen = ?, item_count = ?, channel_ids = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		summary, lastSeen, itemCount, channelIDs, id)
	if err != nil {
		return fmt.Errorf("updating chain %d: %w", id, err)
	}
	return nil
}

// UpdateChainStatus sets the status of a chain.
func (db *DB) UpdateChainStatus(id int, status string) error {
	_, err := db.Exec(`UPDATE chains SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		status, id)
	if err != nil {
		return fmt.Errorf("updating chain status %d: %w", id, err)
	}
	return nil
}

// GetActiveChains returns chains with status='active' that had activity within staleDays.
func (db *DB) GetActiveChains(staleDays int) ([]Chain, error) {
	cutoff := float64(time.Now().AddDate(0, 0, -staleDays).Unix())
	rows, err := db.Query(`SELECT id, COALESCE(parent_id, 0), title, slug, status, summary, channel_ids, first_seen, last_seen, item_count, COALESCE(read_at, ''), created_at, updated_at
		FROM chains WHERE status = 'active' AND last_seen >= ? ORDER BY last_seen DESC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("querying active chains: %w", err)
	}
	defer rows.Close()

	var chains []Chain
	for rows.Next() {
		var c Chain
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Title, &c.Slug, &c.Status, &c.Summary, &c.ChannelIDs,
			&c.FirstSeen, &c.LastSeen, &c.ItemCount, &c.ReadAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning chain: %w", err)
		}
		chains = append(chains, c)
	}
	return chains, rows.Err()
}

// ChainFilter specifies criteria for querying chains.
type ChainFilter struct {
	Status string // filter by status (empty = any)
	Limit  int    // max results (0 = no limit)
}

// GetChains returns chains matching the filter, newest first.
func (db *DB) GetChains(f ChainFilter) ([]Chain, error) {
	query := `SELECT id, COALESCE(parent_id, 0), title, slug, status, summary, channel_ids, first_seen, last_seen, item_count, COALESCE(read_at, ''), created_at, updated_at FROM chains`
	var conditions []string
	var args []any

	if f.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, f.Status)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY last_seen DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying chains: %w", err)
	}
	defer rows.Close()

	var chains []Chain
	for rows.Next() {
		var c Chain
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Title, &c.Slug, &c.Status, &c.Summary, &c.ChannelIDs,
			&c.FirstSeen, &c.LastSeen, &c.ItemCount, &c.ReadAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning chain: %w", err)
		}
		chains = append(chains, c)
	}
	return chains, rows.Err()
}

// GetChainByID returns a single chain by ID.
func (db *DB) GetChainByID(id int) (*Chain, error) {
	var c Chain
	err := db.QueryRow(`SELECT id, COALESCE(parent_id, 0), title, slug, status, summary, channel_ids, first_seen, last_seen, item_count, COALESCE(read_at, ''), created_at, updated_at
		FROM chains WHERE id = ?`, id).
		Scan(&c.ID, &c.ParentID, &c.Title, &c.Slug, &c.Status, &c.Summary, &c.ChannelIDs,
			&c.FirstSeen, &c.LastSeen, &c.ItemCount, &c.ReadAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting chain %d: %w", id, err)
	}
	return &c, nil
}

// InsertChainRef adds a reference linking a chain to a decision or track.
func (db *DB) InsertChainRef(ref ChainRef) error {
	_, err := db.Exec(`INSERT OR IGNORE INTO chain_refs (chain_id, ref_type, digest_id, decision_idx, track_id, channel_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ref.ChainID, ref.RefType, ref.DigestID, ref.DecisionIdx, ref.TrackID, ref.ChannelID, ref.Timestamp)
	if err != nil {
		return fmt.Errorf("inserting chain ref: %w", err)
	}
	return nil
}

// GetChainRefs returns all refs for a chain, ordered by timestamp.
func (db *DB) GetChainRefs(chainID int) ([]ChainRef, error) {
	rows, err := db.Query(`SELECT id, chain_id, ref_type, digest_id, decision_idx, track_id, channel_id, timestamp, created_at
		FROM chain_refs WHERE chain_id = ? ORDER BY timestamp ASC`, chainID)
	if err != nil {
		return nil, fmt.Errorf("querying chain refs: %w", err)
	}
	defer rows.Close()

	var refs []ChainRef
	for rows.Next() {
		var r ChainRef
		if err := rows.Scan(&r.ID, &r.ChainID, &r.RefType, &r.DigestID, &r.DecisionIdx, &r.TrackID,
			&r.ChannelID, &r.Timestamp, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning chain ref: %w", err)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// UnlinkedDecision is a decision from a digest that is not yet linked to any chain.
type UnlinkedDecision struct {
	DigestID     int
	DecisionIdx  int
	ChannelID    string
	ChannelName  string
	DigestType   string
	PeriodTo     float64
	DecisionText string
	DecisionBy   string
	Importance   string
}

// GetUnlinkedDecisions returns decisions from recent digests not yet in any chain.
// Only considers channel-level digests (not daily/weekly rollups) within sinceUnix.
func (db *DB) GetUnlinkedDecisions(sinceUnix float64) ([]UnlinkedDecision, error) {
	// Get all channel digests since the cutoff that have decisions.
	digests, err := db.GetDigests(DigestFilter{
		Type:     "channel",
		FromUnix: sinceUnix,
	})
	if err != nil {
		return nil, fmt.Errorf("getting recent digests: %w", err)
	}

	// Build a set of already-linked (digest_id, decision_idx) pairs.
	linkedRows, err := db.Query(`SELECT digest_id, decision_idx FROM chain_refs WHERE ref_type = 'decision' AND digest_id > 0`)
	if err != nil {
		return nil, fmt.Errorf("querying linked decisions: %w", err)
	}
	defer linkedRows.Close()

	type decKey struct {
		digestID    int
		decisionIdx int
	}
	linked := make(map[decKey]bool)
	for linkedRows.Next() {
		var k decKey
		if err := linkedRows.Scan(&k.digestID, &k.decisionIdx); err != nil {
			return nil, fmt.Errorf("scanning linked decision: %w", err)
		}
		linked[k] = true
	}
	if err := linkedRows.Err(); err != nil {
		return nil, err
	}

	// Parse decisions from each digest, skip already-linked ones.
	var result []UnlinkedDecision
	for _, d := range digests {
		if d.Decisions == "" || d.Decisions == "[]" || d.Decisions == "null" {
			continue
		}
		type jsonDecision struct {
			Text       string `json:"text"`
			By         string `json:"by"`
			Importance string `json:"importance"`
		}
		var decs []jsonDecision
		if err := json.Unmarshal([]byte(d.Decisions), &decs); err != nil {
			continue
		}
		chName, _ := db.ChannelNameByID(d.ChannelID)
		for i, dec := range decs {
			if linked[decKey{d.ID, i}] {
				continue
			}
			result = append(result, UnlinkedDecision{
				DigestID:     d.ID,
				DecisionIdx:  i,
				ChannelID:    d.ChannelID,
				ChannelName:  chName,
				DigestType:   d.Type,
				PeriodTo:     d.PeriodTo,
				DecisionText: dec.Text,
				DecisionBy:   dec.By,
				Importance:   dec.Importance,
			})
		}
	}
	return result, nil
}

// MarkStaleChains sets status='stale' for active chains whose last_seen is older than cutoffUnix.
func (db *DB) MarkStaleChains(cutoffUnix float64) (int64, error) {
	res, err := db.Exec(`UPDATE chains SET status = 'stale', updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE status = 'active' AND last_seen < ?`, cutoffUnix)
	if err != nil {
		return 0, fmt.Errorf("marking stale chains: %w", err)
	}
	return res.RowsAffected()
}

// GetChainItemCount returns the number of refs in a chain.
func (db *DB) GetChainItemCount(chainID int) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM chain_refs WHERE chain_id = ?`, chainID).Scan(&count)
	return count, err
}

// IsDecisionChained returns the chain ID if a specific decision (digest_id + decision_idx) is linked to a chain.
// Returns 0 if not chained.
func (db *DB) IsDecisionChained(digestID, decisionIdx int) int {
	var chainID int
	err := db.QueryRow(`SELECT chain_id FROM chain_refs WHERE ref_type = 'decision' AND digest_id = ? AND decision_idx = ?`,
		digestID, decisionIdx).Scan(&chainID)
	if err != nil {
		return 0
	}
	return chainID
}

// GetTrackByID returns a single track by ID.
func (db *DB) GetTrackByID(id int) (*Track, error) {
	var t Track
	var dueDate sql.NullFloat64
	var snoozeUntil sql.NullFloat64
	err := db.QueryRow(`SELECT id, channel_id, assignee_user_id, assignee_raw, text, context,
		source_message_ts, source_channel_name, status, priority, due_date,
		period_from, period_to, model, input_tokens, output_tokens, cost_usd,
		created_at, completed_at,
		has_updates, last_checked_ts, snooze_until, pre_snooze_status,
		participants, source_refs,
		requester_name, requester_user_id, category, blocking, tags,
		decision_summary, decision_options, related_digest_ids, sub_items, prompt_version,
		ownership, ball_on, owner_user_id
		FROM tracks WHERE id = ?`, id).Scan(
		&t.ID, &t.ChannelID, &t.AssigneeUserID, &t.AssigneeRaw, &t.Text, &t.Context,
		&t.SourceMessageTS, &t.SourceChannelName, &t.Status, &t.Priority, &dueDate,
		&t.PeriodFrom, &t.PeriodTo, &t.Model, &t.InputTokens, &t.OutputTokens, &t.CostUSD,
		&t.CreatedAt, &t.CompletedAt,
		&t.HasUpdates, &t.LastCheckedTS, &snoozeUntil, &t.PreSnoozeStatus,
		&t.Participants, &t.SourceRefs,
		&t.RequesterName, &t.RequesterUserID, &t.Category, &t.Blocking, &t.Tags,
		&t.DecisionSummary, &t.DecisionOptions, &t.RelatedDigestIDs, &t.SubItems, &t.PromptVersion,
		&t.Ownership, &t.BallOn, &t.OwnerUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting track %d: %w", id, err)
	}
	if dueDate.Valid {
		t.DueDate = dueDate.Float64
	}
	if snoozeUntil.Valid {
		t.SnoozeUntil = snoozeUntil.Float64
	}
	return &t, nil
}

// GetChildChains returns child chains of a parent chain.
func (db *DB) GetChildChains(parentID int) ([]Chain, error) {
	rows, err := db.Query(`SELECT id, COALESCE(parent_id, 0), title, slug, status, summary, channel_ids, first_seen, last_seen, item_count, COALESCE(read_at, ''), created_at, updated_at
		FROM chains WHERE parent_id = ? ORDER BY last_seen DESC`, parentID)
	if err != nil {
		return nil, fmt.Errorf("querying child chains: %w", err)
	}
	defer rows.Close()

	var chains []Chain
	for rows.Next() {
		var c Chain
		if err := rows.Scan(&c.ID, &c.ParentID, &c.Title, &c.Slug, &c.Status, &c.Summary, &c.ChannelIDs,
			&c.FirstSeen, &c.LastSeen, &c.ItemCount, &c.ReadAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning child chain: %w", err)
		}
		chains = append(chains, c)
	}
	return chains, rows.Err()
}

// MarkChainRead sets read_at to now for a chain.
func (db *DB) MarkChainRead(id int) error {
	_, err := db.Exec(`UPDATE chains SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, id)
	return err
}

// GetUnreadChainCount returns the number of chains with read_at IS NULL.
func (db *DB) GetUnreadChainCount() (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM chains WHERE status = 'active' AND read_at IS NULL`).Scan(&count)
	return count, err
}

// SetChainParent sets the parent_id of a chain.
func (db *DB) SetChainParent(childID, parentID int) error {
	var pid any
	if parentID > 0 {
		pid = parentID
	}
	_, err := db.Exec(`UPDATE chains SET parent_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		pid, childID)
	return err
}

// GetUnlinkedDigests returns channel digests from the given period that are not yet linked to any chain.
func (db *DB) GetUnlinkedDigests(sinceUnix float64) ([]Digest, error) {
	digests, err := db.GetDigests(DigestFilter{
		Type:     "channel",
		FromUnix: sinceUnix,
	})
	if err != nil {
		return nil, fmt.Errorf("getting recent digests: %w", err)
	}

	// Build set of already-linked digest IDs.
	linkedRows, err := db.Query(`SELECT DISTINCT digest_id FROM chain_refs WHERE ref_type = 'digest' AND digest_id > 0`)
	if err != nil {
		return nil, fmt.Errorf("querying linked digests: %w", err)
	}
	defer linkedRows.Close()

	linked := make(map[int]bool)
	for linkedRows.Next() {
		var id int
		if err := linkedRows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning linked digest: %w", err)
		}
		linked[id] = true
	}
	if err := linkedRows.Err(); err != nil {
		return nil, err
	}

	var result []Digest
	for _, d := range digests {
		if !linked[d.ID] && d.Summary != "" {
			result = append(result, d)
		}
	}
	return result, nil
}

// AddChannelToChain adds a channel_id to the chain's channel_ids JSON array if not already present.
func (db *DB) AddChannelToChain(chainID int, channelID string) error {
	chain, err := db.GetChainByID(chainID)
	if err != nil {
		return err
	}
	var ids []string
	if err := json.Unmarshal([]byte(chain.ChannelIDs), &ids); err != nil {
		ids = []string{}
	}
	if slices.Contains(ids, channelID) {
		return nil // already present
	}
	ids = append(ids, channelID)
	data, _ := json.Marshal(ids)
	_, err = db.Exec(`UPDATE chains SET channel_ids = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		string(data), chainID)
	return err
}
