package db

import "fmt"

// AddFeedback records a thumbs up (+1) or thumbs down (-1) for an entity.
func (db *DB) AddFeedback(f Feedback) (int64, error) {
	res, err := db.Exec(`INSERT INTO feedback (entity_type, entity_id, rating, comment)
		VALUES (?, ?, ?, ?)`, f.EntityType, f.EntityID, f.Rating, f.Comment)
	if err != nil {
		return 0, fmt.Errorf("inserting feedback: %w", err)
	}
	return res.LastInsertId()
}

// FeedbackFilter specifies criteria for querying feedback.
type FeedbackFilter struct {
	EntityType string // filter by type (empty = any)
	EntityID   string // filter by specific entity (empty = any)
	Rating     int    // 0 = any, +1 or -1 = specific rating
}

// GetFeedback returns feedback matching the filter, newest first.
func (db *DB) GetFeedback(f FeedbackFilter) ([]Feedback, error) {
	return db.GetFeedbackWithLimit(f, 500)
}

// FeedbackStats holds aggregate feedback counts.
type FeedbackStats struct {
	EntityType string
	Positive   int
	Negative   int
	Total      int
}

// GetFeedbackStats returns aggregate positive/negative counts per entity type.
func (db *DB) GetFeedbackStats() ([]FeedbackStats, error) {
	rows, err := db.Query(`SELECT entity_type,
		SUM(CASE WHEN rating = 1 THEN 1 ELSE 0 END) as positive,
		SUM(CASE WHEN rating = -1 THEN 1 ELSE 0 END) as negative,
		COUNT(*) as total
		FROM feedback GROUP BY entity_type ORDER BY entity_type`)
	if err != nil {
		return nil, fmt.Errorf("querying feedback stats: %w", err)
	}
	defer rows.Close()

	var stats []FeedbackStats
	for rows.Next() {
		var s FeedbackStats
		if err := rows.Scan(&s.EntityType, &s.Positive, &s.Negative, &s.Total); err != nil {
			return nil, fmt.Errorf("scanning feedback stats: %w", err)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// GetFeedbackForPrompt returns feedback entries for entities generated with a specific prompt.
// Used by the tuner to gather training examples.
func (db *DB) GetFeedbackForPrompt(promptID string, limit int) ([]Feedback, error) {
	// Map prompt IDs to entity types
	var entityType string
	switch promptID {
	case "digest.channel", "digest.daily", "digest.weekly", "digest.period":
		entityType = "digest"
	case "tracks.extract", "tracks.update", "tracks.create":
		entityType = "track"
	case "analysis.user", "analysis.period", "people.reduce", "people.team":
		entityType = "user_analysis"
	case "guide.user", "guide.period":
		entityType = "user_analysis"
	default:
		return nil, fmt.Errorf("unknown prompt ID %q: cannot determine feedback entity type", promptID)
	}

	if limit <= 0 {
		limit = 50
	}

	return db.GetFeedbackWithLimit(FeedbackFilter{EntityType: entityType}, limit)
}

// GetFeedbackWithLimit is like GetFeedback but applies a custom LIMIT.
func (db *DB) GetFeedbackWithLimit(f FeedbackFilter, limit int) ([]Feedback, error) {
	query := `SELECT id, entity_type, entity_id, rating, comment, created_at FROM feedback`
	var conditions []string
	var args []any

	if f.EntityType != "" {
		conditions = append(conditions, "entity_type = ?")
		args = append(args, f.EntityType)
	}
	if f.EntityID != "" {
		conditions = append(conditions, "entity_id = ?")
		args = append(args, f.EntityID)
	}
	if f.Rating != 0 {
		conditions = append(conditions, "rating = ?")
		args = append(args, f.Rating)
	}

	if len(conditions) > 0 {
		query += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying feedback: %w", err)
	}
	defer rows.Close()

	var results []Feedback
	for rows.Next() {
		var fb Feedback
		if err := rows.Scan(&fb.ID, &fb.EntityType, &fb.EntityID, &fb.Rating, &fb.Comment, &fb.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning feedback: %w", err)
		}
		results = append(results, fb)
	}
	return results, rows.Err()
}
