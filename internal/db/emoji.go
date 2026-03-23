package db

import "fmt"

// UpsertCustomEmoji inserts or updates a custom workspace emoji.
func (db *DB) UpsertCustomEmoji(e CustomEmoji) error {
	_, err := db.Exec(`
		INSERT INTO custom_emojis (name, url, alias_for, updated_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(name) DO UPDATE SET
			url = excluded.url,
			alias_for = excluded.alias_for,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		e.Name, e.URL, e.AliasFor,
	)
	if err != nil {
		return fmt.Errorf("upserting custom emoji %s: %w", e.Name, err)
	}
	return nil
}

// BulkUpsertCustomEmojis replaces all custom emojis with the given set.
// Uses a transaction: deletes all existing, inserts new ones.
func (db *DB) BulkUpsertCustomEmojis(emojis []CustomEmoji) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning emoji bulk upsert tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM custom_emojis`); err != nil {
		return fmt.Errorf("clearing custom emojis: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO custom_emojis (name, url, alias_for, updated_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))`)
	if err != nil {
		return fmt.Errorf("preparing emoji insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range emojis {
		if _, err := stmt.Exec(e.Name, e.URL, e.AliasFor); err != nil {
			return fmt.Errorf("inserting emoji %s: %w", e.Name, err)
		}
	}

	return tx.Commit()
}

// GetCustomEmojis returns all custom workspace emojis.
func (db *DB) GetCustomEmojis() ([]CustomEmoji, error) {
	rows, err := db.Query(`SELECT name, url, alias_for FROM custom_emojis ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("querying custom emojis: %w", err)
	}
	defer rows.Close()

	var emojis []CustomEmoji
	for rows.Next() {
		var e CustomEmoji
		if err := rows.Scan(&e.Name, &e.URL, &e.AliasFor); err != nil {
			return nil, fmt.Errorf("scanning custom emoji: %w", err)
		}
		emojis = append(emojis, e)
	}
	return emojis, rows.Err()
}

// GetCustomEmojiMap returns a map of emoji name → URL for all custom emojis,
// resolving aliases to their target URLs.
func (db *DB) GetCustomEmojiMap() (map[string]string, error) {
	emojis, err := db.GetCustomEmojis()
	if err != nil {
		return nil, err
	}

	// First pass: build name→URL map
	result := make(map[string]string, len(emojis))
	for _, e := range emojis {
		result[e.Name] = e.URL
	}

	// Build name→alias lookup for O(1) resolution instead of O(n) inner loop.
	aliasOf := make(map[string]string, len(emojis))
	for _, e := range emojis {
		if e.AliasFor != "" {
			aliasOf[e.Name] = e.AliasFor
		}
	}

	// Resolve aliases transitively (with depth limit to prevent cycles)
	for _, e := range emojis {
		if e.AliasFor != "" {
			target := e.AliasFor
			resolved := false
			for depth := 0; depth < 10; depth++ {
				url, ok := result[target]
				if !ok {
					break
				}
				if next, isAlias := aliasOf[target]; isAlias {
					target = next
				} else {
					result[e.Name] = url
					resolved = true
					break
				}
			}
			if !resolved {
				// Alias chain is broken or has a cycle — remove unresolvable entry
				delete(result, e.Name)
			}
		}
	}

	return result, nil
}
