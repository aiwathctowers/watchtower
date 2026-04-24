package db

import (
	"fmt"
)

const targetLinkSelectCols = `id, source_target_id, target_target_id, external_ref,
	relation, confidence, created_by, created_at`

func scanTargetLink(row interface{ Scan(...any) error }) (*TargetLink, error) {
	var l TargetLink
	if err := row.Scan(
		&l.ID, &l.SourceTargetID, &l.TargetTargetID, &l.ExternalRef,
		&l.Relation, &l.Confidence, &l.CreatedBy, &l.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &l, nil
}

// CreateTargetLink inserts a new target link and returns its ID.
func (db *DB) CreateTargetLink(l TargetLink) (int64, error) {
	if l.CreatedBy == "" {
		l.CreatedBy = "user"
	}
	res, err := db.Exec(`INSERT INTO target_links
		(source_target_id, target_target_id, external_ref, relation, confidence, created_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		l.SourceTargetID, l.TargetTargetID, l.ExternalRef, l.Relation, l.Confidence, l.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting target link: %w", err)
	}
	return res.LastInsertId()
}

// GetLinksForTarget returns links involving the given target.
// direction: "inbound" (target_target_id=id), "outbound" (source_target_id=id), "both".
func (db *DB) GetLinksForTarget(targetID int64, direction string) ([]TargetLink, error) {
	var query string
	var args []any
	switch direction {
	case "inbound":
		query = `SELECT ` + targetLinkSelectCols + ` FROM target_links WHERE target_target_id = ?`
		args = []any{targetID}
	case "outbound":
		query = `SELECT ` + targetLinkSelectCols + ` FROM target_links WHERE source_target_id = ?`
		args = []any{targetID}
	default: // "both"
		query = `SELECT ` + targetLinkSelectCols + ` FROM target_links
			WHERE source_target_id = ? OR target_target_id = ?`
		args = []any{targetID, targetID}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying links for target %d: %w", targetID, err)
	}
	defer rows.Close()

	var links []TargetLink
	for rows.Next() {
		l, err := scanTargetLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning target link: %w", err)
		}
		links = append(links, *l)
	}
	return links, rows.Err()
}

// GetLinksBySource returns all links where source_target_id = sourceID.
func (db *DB) GetLinksBySource(sourceID int64) ([]TargetLink, error) {
	return db.GetLinksForTarget(sourceID, "outbound")
}

// GetLinksByTarget returns all links where target_target_id = targetID.
func (db *DB) GetLinksByTarget(targetID int64) ([]TargetLink, error) {
	return db.GetLinksForTarget(targetID, "inbound")
}

// DeleteTargetLink removes a target link by ID.
func (db *DB) DeleteTargetLink(id int) error {
	_, err := db.Exec(`DELETE FROM target_links WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting target link %d: %w", id, err)
	}
	return nil
}

// DeleteLinksForTarget removes all target_links where source_target_id = targetID.
func (db *DB) DeleteLinksForTarget(targetID int64) error {
	_, err := db.Exec(`DELETE FROM target_links WHERE source_target_id = ?`, targetID)
	if err != nil {
		return fmt.Errorf("deleting links for target %d: %w", targetID, err)
	}
	return nil
}
