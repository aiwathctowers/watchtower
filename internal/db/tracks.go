package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

// scanTrack scans a Track from a row with the standard SELECT column list.
func scanTrack(row interface{ Scan(...any) error }) (*Track, error) {
	var t Track
	if err := row.Scan(
		&t.ID, &t.Title, &t.Narrative, &t.CurrentStatus,
		&t.Participants, &t.Timeline, &t.KeyMessages,
		&t.Priority, &t.Tags, &t.ChannelIDs, &t.SourceRefs,
		&t.ReadAt, &t.HasUpdates,
		&t.Model, &t.InputTokens, &t.OutputTokens, &t.CostUSD, &t.PromptVersion,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

// trackSelectCols is the standard SELECT column list for tracks.
const trackSelectCols = `id, title, narrative, current_status,
	participants, timeline, key_messages,
	priority, tags, channel_ids, source_refs,
	COALESCE(read_at,''), has_updates,
	model, input_tokens, output_tokens, cost_usd, prompt_version,
	created_at, updated_at`

// UpsertTrack inserts a new track or updates an existing one by ID.
// On UPDATE: sets updated_at=now, has_updates=1 if track was already read.
func (db *DB) UpsertTrack(t Track) (int64, error) {
	if t.ID > 0 {
		// Update existing track.
		_, err := db.Exec(`UPDATE tracks SET
			title = ?, narrative = ?, current_status = ?,
			participants = ?, timeline = ?, key_messages = ?,
			priority = ?, tags = ?, channel_ids = ?, source_refs = ?,
			has_updates = CASE WHEN read_at IS NOT NULL THEN 1 ELSE has_updates END,
			model = ?, input_tokens = ?, output_tokens = ?, cost_usd = ?, prompt_version = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
			WHERE id = ?`,
			t.Title, t.Narrative, t.CurrentStatus,
			t.Participants, t.Timeline, t.KeyMessages,
			t.Priority, t.Tags, t.ChannelIDs, t.SourceRefs,
			t.Model, t.InputTokens, t.OutputTokens, t.CostUSD, t.PromptVersion,
			t.ID,
		)
		if err != nil {
			return 0, fmt.Errorf("updating track %d: %w", t.ID, err)
		}
		return int64(t.ID), nil
	}

	// Insert new track.
	res, err := db.Exec(`INSERT INTO tracks (title, narrative, current_status,
		participants, timeline, key_messages,
		priority, tags, channel_ids, source_refs,
		model, input_tokens, output_tokens, cost_usd, prompt_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Title, t.Narrative, t.CurrentStatus,
		t.Participants, t.Timeline, t.KeyMessages,
		t.Priority, t.Tags, t.ChannelIDs, t.SourceRefs,
		t.Model, t.InputTokens, t.OutputTokens, t.CostUSD, t.PromptVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting track: %w", err)
	}
	return res.LastInsertId()
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

	type sourceRef struct {
		DigestID int `json:"digest_id"`
	}
	var refs []sourceRef
	if err := json.Unmarshal([]byte(track.SourceRefs), &refs); err != nil {
		return nil //nolint:nilerr // cascade is best-effort, unmarshal failure is non-critical
	}

	var digestIDs []int
	seen := make(map[int]bool)
	for _, r := range refs {
		if r.DigestID > 0 && !seen[r.DigestID] {
			seen[r.DigestID] = true
			digestIDs = append(digestIDs, r.DigestID)
		}
	}

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
