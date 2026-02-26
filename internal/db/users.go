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

// UpsertUser inserts or updates a user.
func (db *DB) UpsertUser(u User) error {
	_, err := db.Exec(`
		INSERT INTO users (id, name, display_name, real_name, email, is_bot, is_deleted, profile_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			display_name = excluded.display_name,
			real_name = excluded.real_name,
			email = excluded.email,
			is_bot = excluded.is_bot,
			is_deleted = excluded.is_deleted,
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
	query := `SELECT id, name, display_name, real_name, email, is_bot, is_deleted, profile_json, updated_at FROM users`
	var conditions []string

	if filter.ExcludeBots {
		conditions = append(conditions, "is_bot = 0")
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
		SELECT id, name, display_name, real_name, email, is_bot, is_deleted, profile_json, updated_at
		FROM users WHERE name = ?`, name)
	return scanUser(row)
}

// GetUserByID returns a user by their Slack ID.
func (db *DB) GetUserByID(id string) (*User, error) {
	row := db.QueryRow(`
		SELECT id, name, display_name, real_name, email, is_bot, is_deleted, profile_json, updated_at
		FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(
		&u.ID, &u.Name, &u.DisplayName, &u.RealName, &u.Email,
		&u.IsBot, &u.IsDeleted, &u.ProfileJSON, &u.UpdatedAt,
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
			&u.IsBot, &u.IsDeleted, &u.ProfileJSON, &u.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
