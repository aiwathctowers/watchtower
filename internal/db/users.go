package db

import (
	"database/sql"
	"fmt"
)

// UserFilter provides options for filtering user queries.
type UserFilter struct {
	ExcludeBots    bool
	ExcludeDeleted bool
}

// UpsertUser inserts or updates a user with full profile data (is_stub = 0).
func (db *DB) UpsertUser(u User) error {
	_, err := db.Exec(`
		INSERT INTO users (id, name, display_name, real_name, email, is_bot, is_deleted, is_stub, profile_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			display_name = excluded.display_name,
			real_name = excluded.real_name,
			email = excluded.email,
			is_bot = excluded.is_bot,
			is_deleted = excluded.is_deleted,
			is_stub = 0,
			profile_json = excluded.profile_json,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		u.ID, u.Name, u.DisplayName, u.RealName, u.Email,
		u.IsBot, u.IsDeleted, u.ProfileJSON,
	)
	if err != nil {
		return fmt.Errorf("upserting user %s: %w", u.ID, err)
	}
	return nil
}

// GetUsers returns users matching the given filter.
func (db *DB) GetUsers(filter UserFilter) ([]User, error) {
	query := `SELECT id, name, display_name, real_name, email, is_bot, is_deleted, is_stub, profile_json, updated_at FROM users`
	var conditions []string

	if filter.ExcludeBots {
		conditions = append(conditions, "COALESCE(is_bot_override, is_bot) = 0")
	}
	if filter.ExcludeDeleted {
		conditions = append(conditions, "is_deleted = 0")
	}

	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for _, c := range conditions[1:] {
			query += " AND " + c
		}
	}
	query += " ORDER BY name"

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying users: %w", err)
	}
	defer rows.Close()

	return scanUsers(rows)
}

// GetUserByName returns a user by their Slack username.
func (db *DB) GetUserByName(name string) (*User, error) {
	row := db.QueryRow(`
		SELECT id, name, display_name, real_name, email, is_bot, is_deleted, is_stub, profile_json, updated_at
		FROM users WHERE name = ?`, name)
	return scanUser(row)
}

// GetUserByID returns a user by their Slack ID.
func (db *DB) GetUserByID(id string) (*User, error) {
	row := db.QueryRow(`
		SELECT id, name, display_name, real_name, email, is_bot, is_deleted, is_stub, profile_json, updated_at
		FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetUserByEmail returns a user by their email address.
func (db *DB) GetUserByEmail(email string) (*User, error) {
	row := db.QueryRow(`
		SELECT id, name, display_name, real_name, email, is_bot, is_deleted, is_stub, profile_json, updated_at
		FROM users WHERE email = ? AND email != ''`, email)
	return scanUser(row)
}

// EnsureUser inserts a minimal stub user record if not already present.
// Stubs are marked with is_stub=1 so syncUserProfiles can backfill them.
// Does NOT update existing records (INSERT ON CONFLICT DO NOTHING).
func (db *DB) EnsureUser(id, name string) error {
	_, err := db.Exec(`
		INSERT INTO users (id, name, is_stub) VALUES (?, ?, 1)
		ON CONFLICT(id) DO NOTHING`,
		id, name,
	)
	if err != nil {
		return fmt.Errorf("ensuring user %s: %w", id, err)
	}
	return nil
}

// GetIncompleteUserIDs returns user IDs that either:
// - appear in messages but not in the users table, or
// - exist as stub records (is_stub=1) needing full profile backfill.
func (db *DB) GetIncompleteUserIDs() ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT id FROM (
			SELECT m.user_id AS id
			FROM messages m
			LEFT JOIN users u ON u.id = m.user_id
			WHERE m.user_id != '' AND u.id IS NULL
			UNION
			SELECT u.id
			FROM users u
			WHERE u.is_stub = 1
		)`)
	if err != nil {
		return nil, fmt.Errorf("querying incomplete user IDs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning user ID: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UserNameByID returns a display name for a user by their Slack ID.
// Returns display_name if set, otherwise name.
func (db *DB) UserNameByID(userID string) (string, error) {
	var name, displayName string
	err := db.QueryRow(`SELECT name, display_name FROM users WHERE id = ?`, userID).Scan(&name, &displayName)
	if err != nil {
		return userID, err // fallback to ID
	}
	if displayName != "" {
		return displayName, nil
	}
	return name, nil
}

// SetBotOverride sets or clears the manual bot override for a user.
// Pass nil to clear (revert to Slack-provided value), or a bool pointer to force.
func (db *DB) SetBotOverride(userID string, isBot *bool) error {
	var val any
	if isBot != nil {
		if *isBot {
			val = 1
		} else {
			val = 0
		}
	}
	_, err := db.Exec(`UPDATE users SET is_bot_override = ? WHERE id = ?`, val, userID)
	if err != nil {
		return fmt.Errorf("setting bot override for %s: %w", userID, err)
	}
	return nil
}

// SetUserMutedForLLM sets or clears the is_muted_for_llm flag for a user.
func (db *DB) SetUserMutedForLLM(userID string, muted bool) error {
	val := 0
	if muted {
		val = 1
	}
	_, err := db.Exec(`UPDATE users SET is_muted_for_llm = ? WHERE id = ?`, val, userID)
	if err != nil {
		return fmt.Errorf("setting user muted for llm %s: %w", userID, err)
	}
	return nil
}

// GetMutedUserIDs returns the list of user IDs that are muted for LLM processing.
func (db *DB) GetMutedUserIDs() ([]string, error) {
	rows, err := db.Query(`SELECT id FROM users WHERE is_muted_for_llm = 1`)
	if err != nil {
		return nil, fmt.Errorf("querying muted users: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning muted user id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Name, &u.DisplayName, &u.RealName, &u.Email,
		&u.IsBot, &u.IsDeleted, &u.IsStub, &u.ProfileJSON, &u.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning user: %w", err)
	}
	return &u, nil
}

func scanUsers(rows *sql.Rows) ([]User, error) {
	var users []User
	for rows.Next() {
		var u User
		err := rows.Scan(
			&u.ID, &u.Name, &u.DisplayName, &u.RealName, &u.Email,
			&u.IsBot, &u.IsDeleted, &u.IsStub, &u.ProfileJSON, &u.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
