package db

import (
	"fmt"
)

// AddWatch adds an entity to the watch list. If the entity already exists,
// it updates the name and priority.
func (db *DB) AddWatch(entityType, entityID, entityName, priority string) error {
	switch entityType {
	case "channel", "user":
	default:
		return fmt.Errorf("invalid entity type: %q (must be \"channel\" or \"user\")", entityType)
	}
	switch priority {
	case "high", "normal", "low":
	default:
		return fmt.Errorf("invalid priority: %q (must be \"high\", \"normal\", or \"low\")", priority)
	}
	_, err := db.Exec(`
		INSERT INTO watch_list (entity_type, entity_id, entity_name, priority)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET
			entity_name = excluded.entity_name,
			priority = excluded.priority`,
		entityType, entityID, entityName, priority,
	)
	if err != nil {
		return fmt.Errorf("adding watch for %s/%s: %w", entityType, entityID, err)
	}
	return nil
}

// RemoveWatch removes an entity from the watch list.
func (db *DB) RemoveWatch(entityType, entityID string) error {
	switch entityType {
	case "channel", "user":
	default:
		return fmt.Errorf("invalid entity type: %q (must be \"channel\" or \"user\")", entityType)
	}
	result, err := db.Exec(
		`DELETE FROM watch_list WHERE entity_type = ? AND entity_id = ?`,
		entityType, entityID,
	)
	if err != nil {
		return fmt.Errorf("removing watch for %s/%s: %w", entityType, entityID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("no watch entry found for %s/%s", entityType, entityID)
	}
	return nil
}

// GetWatchList returns all watch list entries.
func (db *DB) GetWatchList() ([]WatchItem, error) {
	rows, err := db.Query(`
		SELECT entity_type, entity_id, entity_name, priority, created_at
		FROM watch_list
		ORDER BY
			CASE priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 WHEN 'low' THEN 2 END,
			entity_type, entity_name`)
	if err != nil {
		return nil, fmt.Errorf("querying watch list: %w", err)
	}
	defer rows.Close()

	var items []WatchItem
	for rows.Next() {
		var item WatchItem
		err := rows.Scan(&item.EntityType, &item.EntityID, &item.EntityName, &item.Priority, &item.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning watch item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
