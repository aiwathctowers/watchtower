package db

import (
	"database/sql"
	"fmt"
)

// UpsertPrompt inserts or updates a prompt template.
// On insert, also records the initial version in prompt_history.
// The entire operation is wrapped in a transaction to prevent check-then-act races.
func (db *DB) UpsertPrompt(p Prompt) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning upsert tx: %w", err)
	}
	defer tx.Rollback()

	var existing int
	err = tx.QueryRow(`SELECT version FROM prompts WHERE id = ?`, p.ID).Scan(&existing)
	if err == sql.ErrNoRows {
		// New prompt: insert and record in history.
		if _, err := tx.Exec(`INSERT INTO prompts (id, template, version, language)
			VALUES (?, ?, ?, ?)`, p.ID, p.Template, p.Version, p.Language); err != nil {
			return fmt.Errorf("inserting prompt: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO prompt_history (prompt_id, version, template, reason)
			VALUES (?, ?, ?, ?)`, p.ID, p.Version, p.Template, "initial seed"); err != nil {
			return fmt.Errorf("recording initial prompt history: %w", err)
		}
		return tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("checking existing prompt: %w", err)
	}

	// Update existing: bump version and record history (M4 fix).
	newVersion := max(existing+1, p.Version)
	if _, err := tx.Exec(`UPDATE prompts SET template = ?, version = ?, language = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, p.Template, newVersion, p.Language, p.ID); err != nil {
		return fmt.Errorf("updating prompt: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO prompt_history (prompt_id, version, template, reason)
		VALUES (?, ?, ?, ?)`, p.ID, newVersion, p.Template, "upsert update"); err != nil {
		return fmt.Errorf("recording upsert prompt history: %w", err)
	}
	return tx.Commit()
}

// UpdatePrompt updates a prompt's template, bumps version, and records history.
// H2 fix: entire check-then-act is inside a single transaction to prevent race conditions.
func (db *DB) UpdatePrompt(id, template, reason string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback()

	var current Prompt
	err = tx.QueryRow(`SELECT id, template, version, language FROM prompts WHERE id = ?`, id).
		Scan(&current.ID, &current.Template, &current.Version, &current.Language)
	if err == sql.ErrNoRows {
		return fmt.Errorf("prompt %q not found", id)
	}
	if err != nil {
		return fmt.Errorf("reading prompt: %w", err)
	}

	newVersion := current.Version + 1

	if _, err := tx.Exec(`UPDATE prompts SET template = ?, version = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		WHERE id = ?`, template, newVersion, id); err != nil {
		return fmt.Errorf("updating prompt: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO prompt_history (prompt_id, version, template, reason)
		VALUES (?, ?, ?, ?)`, id, newVersion, template, reason); err != nil {
		return fmt.Errorf("recording prompt history: %w", err)
	}

	return tx.Commit()
}

// GetPrompt returns a prompt by ID, or nil if not found.
func (db *DB) GetPrompt(id string) (*Prompt, error) {
	var p Prompt
	err := db.QueryRow(`SELECT id, template, version, language, updated_at FROM prompts WHERE id = ?`, id).
		Scan(&p.ID, &p.Template, &p.Version, &p.Language, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting prompt: %w", err)
	}
	return &p, nil
}

// GetAllPrompts returns all prompts sorted by ID.
func (db *DB) GetAllPrompts() ([]Prompt, error) {
	rows, err := db.Query(`SELECT id, template, version, language, updated_at FROM prompts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("querying prompts: %w", err)
	}
	defer rows.Close()

	var prompts []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(&p.ID, &p.Template, &p.Version, &p.Language, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning prompt: %w", err)
		}
		prompts = append(prompts, p)
	}
	return prompts, rows.Err()
}

// GetPromptHistory returns version history for a prompt, newest first.
func (db *DB) GetPromptHistory(promptID string) ([]PromptHistory, error) {
	rows, err := db.Query(`SELECT id, prompt_id, version, template, reason, created_at
		FROM prompt_history WHERE prompt_id = ? ORDER BY version DESC`, promptID)
	if err != nil {
		return nil, fmt.Errorf("querying prompt history: %w", err)
	}
	defer rows.Close()

	var history []PromptHistory
	for rows.Next() {
		var h PromptHistory
		if err := rows.Scan(&h.ID, &h.PromptID, &h.Version, &h.Template, &h.Reason, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning prompt history: %w", err)
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

// GetPromptAtVersion returns the template text at a specific version.
func (db *DB) GetPromptAtVersion(promptID string, version int) (*PromptHistory, error) {
	var h PromptHistory
	err := db.QueryRow(`SELECT id, prompt_id, version, template, reason, created_at
		FROM prompt_history WHERE prompt_id = ? AND version = ?`, promptID, version).
		Scan(&h.ID, &h.PromptID, &h.Version, &h.Template, &h.Reason, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting prompt at version: %w", err)
	}
	return &h, nil
}

// RollbackPrompt reverts a prompt to a previous version.
func (db *DB) RollbackPrompt(promptID string, targetVersion int) error {
	hist, err := db.GetPromptAtVersion(promptID, targetVersion)
	if err != nil {
		return err
	}
	if hist == nil {
		return fmt.Errorf("version %d not found for prompt %q", targetVersion, promptID)
	}
	return db.UpdatePrompt(promptID, hist.Template, fmt.Sprintf("rollback to v%d", targetVersion))
}
