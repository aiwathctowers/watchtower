package targets

import (
	"context"
	"database/sql"
	"fmt"

	"watchtower/internal/db"
)

// Store wraps db.DB for transactional batch operations on targets.
type Store struct {
	db *db.DB
}

// NewStore creates a new Store.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// CreateBatch inserts all proposed targets and their secondary links in a single
// transaction. Returns the new target IDs in the same order as items. On any
// failure the transaction is rolled back.
func (s *Store) CreateBatch(ctx context.Context, items []ProposedTarget, sourceType, sourceRef string) ([]int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning batch tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ids := make([]int64, 0, len(items))
	for _, pt := range items {
		id, err := insertTargetTx(ctx, tx, pt, sourceType, sourceRef)
		if err != nil {
			return nil, fmt.Errorf("inserting target %q: %w", pt.Text, err)
		}
		ids = append(ids, id)

		// Insert secondary links.
		for _, sl := range pt.SecondaryLinks {
			if err := insertLinkTx(ctx, tx, id, sl); err != nil {
				return nil, fmt.Errorf("inserting link for target %d: %w", id, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing batch tx: %w", err)
	}
	return ids, nil
}

// insertTargetTx inserts a single target inside an existing transaction.
func insertTargetTx(ctx context.Context, tx *sql.Tx, pt ProposedTarget, sourceType, sourceRef string) (int64, error) {
	tags := "[]"
	subItems := "[]"
	notes := "[]"
	level := pt.Level
	if level == "" {
		level = "day"
	}
	priority := pt.Priority
	if priority == "" {
		priority = "medium"
	}
	if sourceType == "" {
		sourceType = "extract"
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO targets
		(text, intent, level, custom_label, period_start, period_end, parent_id,
		 status, priority, ownership, ball_on, due_date, snooze_until, blocking,
		 tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'todo', ?, 'mine', '', ?, '', '',
		        ?, ?, ?, 0.0, ?, ?, ?)`,
		pt.Text, pt.Intent, level, pt.CustomLabel, pt.PeriodStart, pt.PeriodEnd, pt.ParentID,
		priority, pt.DueDate,
		tags, subItems, notes,
		sourceType, sourceRef, pt.AILevelConfidence,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// insertLinkTx inserts a single target_link inside an existing transaction.
func insertLinkTx(ctx context.Context, tx *sql.Tx, sourceTargetID int64, sl ProposedLink) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO target_links
		(source_target_id, target_target_id, external_ref, relation, confidence, created_by)
		VALUES (?, ?, ?, ?, ?, 'ai')`,
		sourceTargetID, sl.TargetID, sl.ExternalRef, sl.Relation, sl.Confidence,
	)
	return err
}
