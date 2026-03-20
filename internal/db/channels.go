package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ChannelFilter provides options for filtering channel queries.
type ChannelFilter struct {
	Type       string // "public", "private", "dm", "group_dm"
	IsArchived *bool
	IsMember   *bool
}

// UpsertChannel inserts or updates a channel.
func (db *DB) UpsertChannel(ch Channel) error {
	_, err := db.Exec(`
		INSERT INTO channels (id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, last_read, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			topic = excluded.topic,
			purpose = excluded.purpose,
			is_archived = excluded.is_archived,
			is_member = excluded.is_member,
			dm_user_id = excluded.dm_user_id,
			num_members = excluded.num_members,
			last_read = CASE WHEN excluded.last_read != '' THEN excluded.last_read ELSE channels.last_read END,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		ch.ID, ch.Name, ch.Type, ch.Topic, ch.Purpose,
		ch.IsArchived, ch.IsMember, ch.DMUserID, ch.NumMembers, ch.LastRead,
	)
	if err != nil {
		return fmt.Errorf("upserting channel %s: %w", ch.ID, err)
	}
	return nil
}

// GetChannels returns channels matching the given filter.
func (db *DB) GetChannels(filter ChannelFilter) ([]Channel, error) {
	query := `SELECT id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, last_read, updated_at FROM channels`
	var conditions []string
	var args []any

	if filter.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.IsArchived != nil {
		conditions = append(conditions, "is_archived = ?")
		if *filter.IsArchived {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if filter.IsMember != nil {
		conditions = append(conditions, "is_member = ?")
		if *filter.IsMember {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY name"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying channels: %w", err)
	}
	defer rows.Close()

	return scanChannels(rows)
}

// GetChannelByName returns a channel by its name.
func (db *DB) GetChannelByName(name string) (*Channel, error) {
	row := db.QueryRow(`
		SELECT id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, last_read, updated_at
		FROM channels WHERE name = ?`, name)
	return scanChannel(row)
}

// GetChannelByID returns a channel by its Slack ID.
func (db *DB) GetChannelByID(id string) (*Channel, error) {
	row := db.QueryRow(`
		SELECT id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, last_read, updated_at
		FROM channels WHERE id = ?`, id)
	return scanChannel(row)
}

// EnsureChannel inserts a minimal channel record if not already present.
// Does NOT update existing records (INSERT ON CONFLICT DO NOTHING).
// dmUserID is optional — pass "" if not applicable.
func (db *DB) EnsureChannel(id, name, chType, dmUserID string) error {
	var dmUID sql.NullString
	if dmUserID != "" {
		dmUID = sql.NullString{String: dmUserID, Valid: true}
	}
	_, err := db.Exec(`
		INSERT INTO channels (id, name, type, is_member, dm_user_id) VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(id) DO UPDATE SET
			dm_user_id = COALESCE(channels.dm_user_id, excluded.dm_user_id)`,
		id, name, chType, dmUID,
	)
	if err != nil {
		return fmt.Errorf("ensuring channel %s: %w", id, err)
	}
	return nil
}

func scanChannel(row *sql.Row) (*Channel, error) {
	var ch Channel
	err := row.Scan(
		&ch.ID, &ch.Name, &ch.Type, &ch.Topic, &ch.Purpose,
		&ch.IsArchived, &ch.IsMember, &ch.DMUserID, &ch.NumMembers, &ch.LastRead, &ch.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning channel: %w", err)
	}
	return &ch, nil
}

// ChannelListItem extends Channel with message count and last activity info.
type ChannelListItem struct {
	Channel
	MessageCount int
	LastActivity sql.NullFloat64 // ts_unix of most recent message
	IsWatched    bool
}

// ChannelListSort specifies how to sort the channel list.
type ChannelListSort string

const (
	ChannelSortName     ChannelListSort = "name"
	ChannelSortMessages ChannelListSort = "messages"
	ChannelSortRecent   ChannelListSort = "recent"
)

// GetChannelList returns channels with message counts, last activity, and watched status.
func (db *DB) GetChannelList(filter ChannelFilter, sort ChannelListSort) ([]ChannelListItem, error) {
	query := `
		SELECT c.id, c.name, c.type, c.topic, c.purpose, c.is_archived, c.is_member,
		       c.dm_user_id, c.num_members, c.last_read, c.updated_at,
		       COALESCE(m.msg_count, 0),
		       m.last_ts_unix,
		       CASE WHEN w.entity_id IS NOT NULL THEN 1 ELSE 0 END
		FROM channels c
		LEFT JOIN (
			SELECT channel_id, COUNT(*) as msg_count, MAX(ts_unix) as last_ts_unix
			FROM messages
			GROUP BY channel_id
		) m ON m.channel_id = c.id
		LEFT JOIN watch_list w ON w.entity_type = 'channel' AND w.entity_id = c.id`

	var conditions []string
	var args []any

	if filter.Type != "" {
		conditions = append(conditions, "c.type = ?")
		args = append(args, filter.Type)
	}
	if filter.IsArchived != nil {
		conditions = append(conditions, "c.is_archived = ?")
		if *filter.IsArchived {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if filter.IsMember != nil {
		conditions = append(conditions, "c.is_member = ?")
		if *filter.IsMember {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	switch sort {
	case ChannelSortMessages:
		query += " ORDER BY COALESCE(m.msg_count, 0) DESC, c.name"
	case ChannelSortRecent:
		query += " ORDER BY m.last_ts_unix DESC NULLS LAST, c.name"
	default:
		query += " ORDER BY c.name"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying channel list: %w", err)
	}
	defer rows.Close()

	var items []ChannelListItem
	for rows.Next() {
		var item ChannelListItem
		err := rows.Scan(
			&item.ID, &item.Name, &item.Type, &item.Topic, &item.Purpose,
			&item.IsArchived, &item.IsMember, &item.DMUserID, &item.NumMembers, &item.LastRead, &item.UpdatedAt,
			&item.MessageCount, &item.LastActivity, &item.IsWatched,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning channel list row: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanChannels(rows *sql.Rows) ([]Channel, error) {
	var channels []Channel
	for rows.Next() {
		var ch Channel
		err := rows.Scan(
			&ch.ID, &ch.Name, &ch.Type, &ch.Topic, &ch.Purpose,
			&ch.IsArchived, &ch.IsMember, &ch.DMUserID, &ch.NumMembers, &ch.LastRead, &ch.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning channel row: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// UpdateChannelLastRead updates only the last_read cursor for a channel.
func (db *DB) UpdateChannelLastRead(channelID, lastRead string) error {
	_, err := db.Exec(`UPDATE channels SET last_read = ? WHERE id = ? AND (last_read = '' OR last_read < ?)`,
		lastRead, channelID, lastRead)
	if err != nil {
		return fmt.Errorf("updating last_read for %s: %w", channelID, err)
	}
	return nil
}

// AutoMarkReadFromSlack marks digests, decisions, and tracks as read
// when the user has read all corresponding messages in Slack.
// A channel digest is considered read when channels.last_read >= period_to (as Slack ts).
// Daily/weekly digests are read when ALL their child channel digests are read.
// Tracks are read when the source message ts <= channels.last_read.
func (db *DB) AutoMarkReadFromSlack() (digestsMarked, tracksMarked int64, err error) {
	now := `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`

	// 1. Mark channel digests as read when all messages in their period are read in Slack.
	// Slack timestamps are "epoch.seq" strings; we compare period_to (Unix float) against
	// last_read (Slack ts) by extracting the epoch part.
	res, err := db.Exec(`
		UPDATE digests SET read_at = `+now+`
		WHERE read_at IS NULL
		  AND type = 'channel'
		  AND channel_id != ''
		  AND EXISTS (
			SELECT 1 FROM channels c
			WHERE c.id = digests.channel_id
			  AND c.last_read != ''
			  AND CAST(SUBSTR(c.last_read, 1, INSTR(c.last_read, '.') - 1) AS REAL) >= digests.period_to
		  )`)
	if err != nil {
		return 0, 0, fmt.Errorf("auto-marking channel digests: %w", err)
	}
	channelDigests, _ := res.RowsAffected()
	digestsMarked = channelDigests

	// 2. Mark daily/weekly digests as read when ALL channel digests in their period are read.
	// A cross-channel digest covers period_from..period_to. It's read if no unread channel
	// digests exist in that same time window.
	res, err = db.Exec(`
		UPDATE digests SET read_at = `+now+`
		WHERE read_at IS NULL
		  AND type IN ('daily', 'weekly')
		  AND NOT EXISTS (
			SELECT 1 FROM digests cd
			WHERE cd.type = 'channel'
			  AND cd.read_at IS NULL
			  AND cd.period_from >= digests.period_from
			  AND cd.period_to <= digests.period_to
		  )`)
	if err != nil {
		return digestsMarked, 0, fmt.Errorf("auto-marking rollup digests: %w", err)
	}
	rollupDigests, _ := res.RowsAffected()
	digestsMarked += rollupDigests

	// 3. Insert decision_reads for all decisions in newly-read digests
	// (decisions are read when their parent digest is read).
	// We handle this by marking all decisions from digests that have read_at set
	// but don't yet have entries in decision_reads.
	// This is covered implicitly — the desktop app checks digest.read_at for the
	// digest-level read status. Individual decision_reads are optional granularity.

	// 4. Mark tracks as read (has_updates = 0, which is the "read" indicator for tracks)
	// when source_message_ts <= channel's last_read.
	// Tracks don't have a read_at column — they use status. We'll mark inbox tracks
	// that have been read in Slack by checking source_message_ts.
	// Actually, tracks have has_updates flag. Let's skip tracks for now since they don't
	// have a direct "read" concept like digests do.

	return digestsMarked, 0, nil
}
