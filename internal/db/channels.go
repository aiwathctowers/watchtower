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
		INSERT INTO channels (id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			type = excluded.type,
			topic = excluded.topic,
			purpose = excluded.purpose,
			is_archived = excluded.is_archived,
			is_member = excluded.is_member,
			dm_user_id = excluded.dm_user_id,
			num_members = excluded.num_members,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		ch.ID, ch.Name, ch.Type, ch.Topic, ch.Purpose,
		ch.IsArchived, ch.IsMember, ch.DMUserID, ch.NumMembers,
	)
	if err != nil {
		return fmt.Errorf("upserting channel %s: %w", ch.ID, err)
	}
	return nil
}

// GetChannels returns channels matching the given filter.
func (db *DB) GetChannels(filter ChannelFilter) ([]Channel, error) {
	query := `SELECT id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, updated_at FROM channels`
	var conditions []string
	var args []interface{}

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
		SELECT id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, updated_at
		FROM channels WHERE name = ?`, name)
	return scanChannel(row)
}

// GetChannelByID returns a channel by its Slack ID.
func (db *DB) GetChannelByID(id string) (*Channel, error) {
	row := db.QueryRow(`
		SELECT id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, updated_at
		FROM channels WHERE id = ?`, id)
	return scanChannel(row)
}

func scanChannel(row *sql.Row) (*Channel, error) {
	var ch Channel
	err := row.Scan(
		&ch.ID, &ch.Name, &ch.Type, &ch.Topic, &ch.Purpose,
		&ch.IsArchived, &ch.IsMember, &ch.DMUserID, &ch.NumMembers, &ch.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning channel: %w", err)
	}
	return &ch, nil
}

func scanChannels(rows *sql.Rows) ([]Channel, error) {
	var channels []Channel
	for rows.Next() {
		var ch Channel
		err := rows.Scan(
			&ch.ID, &ch.Name, &ch.Type, &ch.Topic, &ch.Purpose,
			&ch.IsArchived, &ch.IsMember, &ch.DMUserID, &ch.NumMembers, &ch.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning channel row: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}
