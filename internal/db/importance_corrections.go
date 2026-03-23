package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// AddImportanceCorrection records a user override of decision importance.
// Uses INSERT OR REPLACE to allow changing the correction for the same decision.
func (db *DB) AddImportanceCorrection(c ImportanceCorrection) (int64, error) {
	res, err := db.Exec(`INSERT OR REPLACE INTO decision_importance_corrections
		(digest_id, decision_idx, decision_text, original_importance, new_importance)
		VALUES (?, ?, ?, ?, ?)`,
		c.DigestID, c.DecisionIdx, c.DecisionText, c.OriginalImportance, c.NewImportance)
	if err != nil {
		return 0, fmt.Errorf("inserting importance correction: %w", err)
	}
	return res.LastInsertId()
}

// GetImportanceCorrections returns all pending corrections, newest first.
func (db *DB) GetImportanceCorrections() ([]ImportanceCorrection, error) {
	rows, err := db.Query(`SELECT id, digest_id, decision_idx, decision_text,
		original_importance, new_importance, created_at
		FROM decision_importance_corrections ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying importance corrections: %w", err)
	}
	defer rows.Close()

	var results []ImportanceCorrection
	for rows.Next() {
		var c ImportanceCorrection
		if err := rows.Scan(&c.ID, &c.DigestID, &c.DecisionIdx, &c.DecisionText,
			&c.OriginalImportance, &c.NewImportance, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning importance correction: %w", err)
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// HasImportanceCorrections returns true if at least one correction exists.
func (db *DB) HasImportanceCorrections() (bool, error) {
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM decision_importance_corrections)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking importance corrections: %w", err)
	}
	return exists, nil
}

// GetImportanceCorrectionFor returns the correction for a specific decision, if any.
func (db *DB) GetImportanceCorrectionFor(digestID int, decisionIdx int) (*ImportanceCorrection, error) {
	row := db.QueryRow(`SELECT id, digest_id, decision_idx, decision_text,
		original_importance, new_importance, created_at
		FROM decision_importance_corrections
		WHERE digest_id = ? AND decision_idx = ?`, digestID, decisionIdx)
	var c ImportanceCorrection
	if err := row.Scan(&c.ID, &c.DigestID, &c.DecisionIdx, &c.DecisionText,
		&c.OriginalImportance, &c.NewImportance, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying importance correction: %w", err)
	}
	return &c, nil
}

// ClearImportanceCorrections deletes all corrections (called after tuning is applied).
func (db *DB) ClearImportanceCorrections() error {
	_, err := db.Exec(`DELETE FROM decision_importance_corrections`)
	if err != nil {
		return fmt.Errorf("clearing importance corrections: %w", err)
	}
	return nil
}
