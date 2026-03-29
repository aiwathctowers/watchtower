package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

// trackSelectCols is the standard SELECT column list for tracks.
const trackSelectCols = `id, assignee_user_id, text, context, category,
	ownership, ball_on, owner_user_id, requester_name, requester_user_id,
	blocking, decision_summary, decision_options, sub_items,
	participants, source_refs, tags, channel_ids, related_digest_ids,
	priority, COALESCE(due_date, 0), fingerprint,
	COALESCE(read_at,''), has_updates,
	model, input_tokens, output_tokens, cost_usd, prompt_version,
	created_at, updated_at`

// scanTrack scans a Track from a row with the standard SELECT column list.
func scanTrack(row interface{ Scan(...any) error }) (*Track, error) {
	var t Track
	if err := row.Scan(
		&t.ID, &t.AssigneeUserID, &t.Text, &t.Context, &t.Category,
		&t.Ownership, &t.BallOn, &t.OwnerUserID, &t.RequesterName, &t.RequesterUserID,
		&t.Blocking, &t.DecisionSummary, &t.DecisionOptions, &t.SubItems,
		&t.Participants, &t.SourceRefs, &t.Tags, &t.ChannelIDs, &t.RelatedDigestIDs,
		&t.Priority, &t.DueDate, &t.Fingerprint,
		&t.ReadAt, &t.HasUpdates,
		&t.Model, &t.InputTokens, &t.OutputTokens, &t.CostUSD, &t.PromptVersion,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// UpsertTrack inserts a new track or updates an existing one by ID.
func (db *DB) UpsertTrack(t Track) (int64, error) {
	// Apply defaults for CHECK-constrained fields.
	if t.Ownership == "" {
		t.Ownership = "mine"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if t.Category == "" {
		t.Category = "task"
	}

	if t.ID > 0 {
		_, err := db.Exec(`UPDATE tracks SET
			assignee_user_id = ?, text = ?, context = ?, category = ?,
			ownership = ?, ball_on = ?, owner_user_id = ?,
			requester_name = ?, requester_user_id = ?,
			blocking = ?, decision_summary = ?, decision_options = ?, sub_items = ?,
			participants = ?, source_refs = ?, tags = ?, channel_ids = ?, related_digest_ids = ?,
			priority = ?, due_date = NULLIF(?, 0), fingerprint = ?,
			has_updates = CASE WHEN read_at IS NOT NULL THEN 1 ELSE has_updates END,
			model = ?, input_tokens = ?, output_tokens = ?, cost_usd = ?, prompt_version = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ?`,
			t.AssigneeUserID, t.Text, t.Context, t.Category,
			t.Ownership, t.BallOn, t.OwnerUserID,
			t.RequesterName, t.RequesterUserID,
			t.Blocking, t.DecisionSummary, t.DecisionOptions, t.SubItems,
			t.Participants, t.SourceRefs, t.Tags, t.ChannelIDs, t.RelatedDigestIDs,
			t.Priority, t.DueDate, t.Fingerprint,
			t.Model, t.InputTokens, t.OutputTokens, t.CostUSD, t.PromptVersion,
			t.ID,
		)
		if err != nil {
			return 0, fmt.Errorf("updating track %d: %w", t.ID, err)
		}
		return int64(t.ID), nil
	}

	res, err := db.Exec(`INSERT INTO tracks (
		assignee_user_id, text, context, category,
		ownership, ball_on, owner_user_id,
		requester_name, requester_user_id,
		blocking, decision_summary, decision_options, sub_items,
		participants, source_refs, tags, channel_ids, related_digest_ids,
		priority, due_date, fingerprint,
		model, input_tokens, output_tokens, cost_usd, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?, ?, ?, ?, ?)`,
		t.AssigneeUserID, t.Text, t.Context, t.Category,
		t.Ownership, t.BallOn, t.OwnerUserID,
		t.RequesterName, t.RequesterUserID,
		t.Blocking, t.DecisionSummary, t.DecisionOptions, t.SubItems,
		t.Participants, t.SourceRefs, t.Tags, t.ChannelIDs, t.RelatedDigestIDs,
		t.Priority, t.DueDate, t.Fingerprint,
		t.Model, t.InputTokens, t.OutputTokens, t.CostUSD, t.PromptVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting track: %w", err)
	}
	return res.LastInsertId()
}

// UpdateTrackFromExtraction updates a track's content fields from AI re-extraction.
// Preserves ID, created_at, read_at. Sets has_updates=1 if track was already read.
// Also merges channel_ids (adds new channel if not already present).
func (db *DB) UpdateTrackFromExtraction(id int, t Track) (int64, error) {
	// Apply defaults for CHECK-constrained fields.
	if t.Ownership == "" {
		t.Ownership = "mine"
	}
	if t.Priority == "" {
		t.Priority = "medium"
	}
	if t.Category == "" {
		t.Category = "task"
	}

	// Merge channel_ids: load existing, add new ones.
	existing, err := db.GetTrackByID(id)
	if err != nil {
		return 0, fmt.Errorf("loading existing track %d for merge: %w", id, err)
	}
	mergedChannelIDs := mergeJSONArrays(existing.ChannelIDs, t.ChannelIDs)
	mergedDigestIDs := mergeJSONArrays(existing.RelatedDigestIDs, t.RelatedDigestIDs)

	_, err = db.Exec(`UPDATE tracks SET
		text = ?, context = ?, category = ?,
		ownership = ?, ball_on = ?, owner_user_id = ?,
		requester_name = ?, requester_user_id = ?,
		blocking = ?, decision_summary = ?, decision_options = ?, sub_items = ?,
		participants = ?, source_refs = ?, tags = ?,
		channel_ids = ?, related_digest_ids = ?,
		priority = ?, due_date = NULLIF(?, 0), fingerprint = ?,
		has_updates = CASE WHEN read_at IS NOT NULL THEN 1 ELSE has_updates END,
		model = ?, input_tokens = ?, output_tokens = ?, cost_usd = ?, prompt_version = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`,
		t.Text, t.Context, t.Category,
		t.Ownership, t.BallOn, t.OwnerUserID,
		t.RequesterName, t.RequesterUserID,
		t.Blocking, t.DecisionSummary, t.DecisionOptions, t.SubItems,
		t.Participants, t.SourceRefs, t.Tags,
		mergedChannelIDs, mergedDigestIDs,
		t.Priority, t.DueDate, t.Fingerprint,
		t.Model, t.InputTokens, t.OutputTokens, t.CostUSD, t.PromptVersion,
		id,
	)
	if err != nil {
		return 0, fmt.Errorf("updating track %d from extraction: %w", id, err)
	}
	return int64(id), nil
}

// GetTrackByID returns a single track by ID.
func (db *DB) GetTrackByID(id int) (*Track, error) {
	row := db.QueryRow(`SELECT `+trackSelectCols+` FROM tracks WHERE id = ?`, id)
	t, err := scanTrack(row)
	if err != nil {
		return nil, fmt.Errorf("getting track %d: %w", id, err)
	}
	return t, nil
}

// GetAllActiveTracks returns all tracks ordered by updated_at DESC.
func (db *DB) GetAllActiveTracks() ([]Track, error) {
	rows, err := db.Query(`SELECT ` + trackSelectCols + ` FROM tracks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying active tracks: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning track: %w", err)
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

// TrackFilter specifies criteria for querying tracks.
type TrackFilter struct {
	Priority   string // "" = any
	Ownership  string // "" = any
	HasUpdates *bool  // nil = any
	ChannelID  string // "" = any, filter via JSON LIKE
	Limit      int    // 0 = no limit
}

// GetTracks returns tracks matching the filter, ordered by has_updates DESC, updated_at DESC.
func (db *DB) GetTracks(f TrackFilter) ([]Track, error) {
	query := `SELECT ` + trackSelectCols + ` FROM tracks`
	var conditions []string
	var args []any

	if f.Priority != "" {
		conditions = append(conditions, "priority = ?")
		args = append(args, f.Priority)
	}
	if f.Ownership != "" {
		conditions = append(conditions, "ownership = ?")
		args = append(args, f.Ownership)
	}
	if f.HasUpdates != nil {
		if *f.HasUpdates {
			conditions = append(conditions, "has_updates = 1")
		} else {
			conditions = append(conditions, "has_updates = 0")
		}
	}
	if f.ChannelID != "" {
		conditions = append(conditions, `channel_ids LIKE ?`)
		args = append(args, "%"+f.ChannelID+"%")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY has_updates DESC, updated_at DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tracks: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning track: %w", err)
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

// MarkTrackRead sets read_at=now and has_updates=0 for a track.
// Also cascade-marks linked digests as read if they are unread.
func (db *DB) MarkTrackRead(id int) error {
	_, err := db.Exec(`UPDATE tracks SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), has_updates = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("marking track %d read: %w", id, err)
	}

	// Cascade: mark linked digests as read.
	track, err := db.GetTrackByID(id)
	if err != nil {
		return nil //nolint:nilerr // track read was set, cascade is best-effort
	}

	var digestIDs []int
	_ = json.Unmarshal([]byte(track.RelatedDigestIDs), &digestIDs)

	if len(digestIDs) > 0 {
		placeholders := make([]string, len(digestIDs))
		args := make([]any, len(digestIDs))
		for i, did := range digestIDs {
			placeholders[i] = "?"
			args[i] = did
		}
		q := `UPDATE digests SET read_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id IN (` + strings.Join(placeholders, ",") + `) AND read_at IS NULL`
		_, _ = db.Exec(q, args...)
	}

	return nil
}

// SetTrackHasUpdates sets has_updates=1 for a track.
func (db *DB) SetTrackHasUpdates(id int) error {
	_, err := db.Exec(`UPDATE tracks SET has_updates = 1 WHERE id = ?`, id)
	return err
}

// GetTrackCount returns (total, updated) track counts.
func (db *DB) GetTrackCount() (int, int, error) {
	var total, updated int
	err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(has_updates), 0) FROM tracks`).Scan(&total, &updated)
	return total, updated, err
}

// GetTrackAssignee returns the assignee_user_id for a track.
func (db *DB) GetTrackAssignee(id int) (string, error) {
	var uid string
	err := db.QueryRow(`SELECT assignee_user_id FROM tracks WHERE id = ?`, id).Scan(&uid)
	return uid, err
}

// HasTracksForUser checks if there are any tracks for the given user.
func (db *DB) HasTracksForUser(userID string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM tracks WHERE assignee_user_id = ? LIMIT 1`, userID).Scan(&count)
	return count > 0, err
}

// FindRelatedDigestIDs finds digest IDs that overlap with the given channel and time window.
func (db *DB) FindRelatedDigestIDs(channelID string, from, to float64) ([]int, error) {
	rows, err := db.Query(`SELECT id FROM digests WHERE channel_id = ? AND period_from <= ? AND period_to >= ? AND type = 'channel' ORDER BY period_to DESC LIMIT 5`,
		channelID, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FindTracksByFingerprint finds tracks where fingerprint JSON arrays overlap.
func (db *DB) FindTracksByFingerprint(assigneeUserID string, fp []string) ([]Track, error) {
	if len(fp) == 0 {
		return nil, nil
	}
	// Search for any fingerprint entity match.
	var conditions []string
	var args []any
	for _, entity := range fp {
		conditions = append(conditions, "fingerprint LIKE ?")
		args = append(args, "%"+entity+"%")
	}
	args = append(args, assigneeUserID)

	query := `SELECT ` + trackSelectCols + ` FROM tracks WHERE (` + strings.Join(conditions, " OR ") + `) AND assignee_user_id = ?`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

// UpdateTrackOwnership updates the ownership field for a track.
func (db *DB) UpdateTrackOwnership(id int, ownership string) error {
	_, err := db.Exec(`UPDATE tracks SET ownership = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, ownership, id)
	return err
}

// UpdateTrackPriority updates the priority field for a track.
func (db *DB) UpdateTrackPriority(id int, priority string) error {
	_, err := db.Exec(`UPDATE tracks SET priority = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, priority, id)
	return err
}

// UpdateTrackSubItems updates the sub_items JSON for a track.
func (db *DB) UpdateTrackSubItems(id int, subItems string) error {
	_, err := db.Exec(`UPDATE tracks SET sub_items = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, subItems, id)
	return err
}

// mergeJSONArrays merges two JSON arrays (strings or ints), deduplicating.
func mergeJSONArrays(existingJSON, newJSON string) string {
	var existing, newArr []json.RawMessage
	_ = json.Unmarshal([]byte(existingJSON), &existing)
	_ = json.Unmarshal([]byte(newJSON), &newArr)

	seen := make(map[string]bool)
	for _, e := range existing {
		seen[string(e)] = true
	}
	for _, n := range newArr {
		if !seen[string(n)] {
			existing = append(existing, n)
			seen[string(n)] = true
		}
	}
	if len(existing) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(existing)
	return string(data)
}

// UnlinkedTopic is a digest topic not yet linked to any track.
type UnlinkedTopic struct {
	TopicID     int
	DigestID    int
	ChannelID   string
	ChannelName string
	Title       string
	Summary     string
	Decisions   string // raw JSON
	PeriodTo    float64
}

// GetUnlinkedTopics returns digest topics from recent channel digests that are
// not yet linked to any track via source_refs.
func (db *DB) GetUnlinkedTopics(sinceUnix float64) ([]UnlinkedTopic, error) {
	// Build in-memory set of linked topic IDs from existing tracks' source_refs.
	tracks, err := db.GetAllActiveTracks()
	if err != nil {
		return nil, fmt.Errorf("loading tracks for linked set: %w", err)
	}

	type topicKey struct {
		digestID int
		topicID  int
	}
	linked := make(map[topicKey]bool)
	for _, t := range tracks {
		type ref struct {
			DigestID int `json:"digest_id"`
			TopicID  int `json:"topic_id"`
		}
		var refs []ref
		if err := json.Unmarshal([]byte(t.SourceRefs), &refs); err != nil {
			continue
		}
		for _, r := range refs {
			if r.TopicID > 0 {
				linked[topicKey{r.DigestID, r.TopicID}] = true
			}
		}
	}

	// Get all topics from recent channel digests.
	rows, err := db.Query(`SELECT dt.id, dt.digest_id, d.channel_id, dt.title, dt.summary, dt.decisions, d.period_to
		FROM digest_topics dt
		JOIN digests d ON d.id = dt.digest_id
		WHERE d.type = 'channel' AND d.period_from >= ?
		ORDER BY d.period_to DESC`, sinceUnix)
	if err != nil {
		return nil, fmt.Errorf("querying topics: %w", err)
	}
	defer rows.Close()

	var result []UnlinkedTopic
	for rows.Next() {
		var t UnlinkedTopic
		if err := rows.Scan(&t.TopicID, &t.DigestID, &t.ChannelID, &t.Title, &t.Summary, &t.Decisions, &t.PeriodTo); err != nil {
			return nil, fmt.Errorf("scanning topic: %w", err)
		}
		if !linked[topicKey{t.DigestID, t.TopicID}] {
			result = append(result, t)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Resolve channel names after closing cursor to avoid deadlock on single-connection DBs.
	for i := range result {
		result[i].ChannelName, _ = db.ChannelNameByID(result[i].ChannelID)
	}
	return result, nil
}
