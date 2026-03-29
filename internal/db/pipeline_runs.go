package db

import (
	"database/sql"
	"fmt"
	"time"
)

// PipelineRun represents a single invocation of a pipeline (digests, tracks, people, briefing).
type PipelineRun struct {
	ID              int64
	Pipeline        string
	Source          string // "cli", "daemon"
	Model           string
	Status          string // "running", "done", "error"
	ErrorMsg        string
	ItemsFound      int
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
	TotalAPITokens  int
	PeriodFrom      *float64
	PeriodTo        *float64
	StartedAt       string
	FinishedAt      *string
	DurationSeconds float64
}

// PipelineStep represents a single step within a pipeline run.
type PipelineStep struct {
	ID              int64
	RunID           int64
	Step            int
	Total           int
	Status          string
	ChannelID       string
	ChannelName     string
	InputTokens     int
	OutputTokens    int
	CostUSD         float64
	TotalAPITokens  int
	MessageCount    int
	PeriodFrom      *float64
	PeriodTo        *float64
	DurationSeconds float64
	CreatedAt       string
}

// CreatePipelineRun inserts a new pipeline run with status "running" and returns its ID.
func (db *DB) CreatePipelineRun(pipeline, source, model string) (int64, error) {
	res, err := db.DB.Exec(
		`INSERT INTO pipeline_runs (pipeline, source, model) VALUES (?, ?, ?)`,
		pipeline, source, model,
	)
	if err != nil {
		return 0, fmt.Errorf("insert pipeline_run: %w", err)
	}
	return res.LastInsertId()
}

// CompletePipelineRun marks a run as done or error, updating usage totals and timing.
func (db *DB) CompletePipelineRun(id int64, itemsFound, inputTokens, outputTokens int, costUSD float64, totalAPITokens int, periodFrom, periodTo *float64, errMsg string) error {
	status := "done"
	if errMsg != "" {
		status = "error"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec(`
		UPDATE pipeline_runs SET
			status = ?,
			error_msg = ?,
			items_found = ?,
			input_tokens = ?,
			output_tokens = ?,
			cost_usd = ?,
			total_api_tokens = ?,
			period_from = ?,
			period_to = ?,
			finished_at = ?,
			duration_seconds = ROUND((julianday(?) - julianday(started_at)) * 86400, 2)
		WHERE id = ?`,
		status, errMsg, itemsFound,
		inputTokens, outputTokens, costUSD, totalAPITokens,
		periodFrom, periodTo,
		now, now,
		id,
	)
	if err != nil {
		return fmt.Errorf("complete pipeline_run %d: %w", id, err)
	}
	return nil
}

// InsertPipelineStep inserts a step record within a pipeline run.
func (db *DB) InsertPipelineStep(s PipelineStep) error {
	_, err := db.DB.Exec(`
		INSERT INTO pipeline_steps (run_id, step, total, status, channel_id, channel_name,
			input_tokens, output_tokens, cost_usd, total_api_tokens,
			message_count, period_from, period_to, duration_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.RunID, s.Step, s.Total, s.Status, s.ChannelID, s.ChannelName,
		s.InputTokens, s.OutputTokens, s.CostUSD, s.TotalAPITokens,
		s.MessageCount, s.PeriodFrom, s.PeriodTo, s.DurationSeconds,
	)
	if err != nil {
		return fmt.Errorf("insert pipeline_step: %w", err)
	}
	return nil
}

// GetPipelineRuns returns recent pipeline runs, newest first.
func (db *DB) GetPipelineRuns(limit int) ([]PipelineRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.DB.Query(`
		SELECT id, pipeline, source, model, status, error_msg, items_found,
			input_tokens, output_tokens, cost_usd, total_api_tokens,
			period_from, period_to, started_at, finished_at, duration_seconds
		FROM pipeline_runs
		ORDER BY started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pipeline_runs: %w", err)
	}
	defer rows.Close()
	return scanPipelineRuns(rows)
}

// GetPipelineRunsByDate returns pipeline runs that started on a given date.
func (db *DB) GetPipelineRunsByDate(date time.Time) ([]PipelineRun, error) {
	dayStart := date.Truncate(24 * time.Hour).UTC().Format(time.RFC3339)
	dayEnd := date.Truncate(24 * time.Hour).Add(24 * time.Hour).UTC().Format(time.RFC3339)
	rows, err := db.DB.Query(`
		SELECT id, pipeline, source, model, status, error_msg, items_found,
			input_tokens, output_tokens, cost_usd, total_api_tokens,
			period_from, period_to, started_at, finished_at, duration_seconds
		FROM pipeline_runs
		WHERE started_at >= ? AND started_at < ?
		ORDER BY started_at DESC`, dayStart, dayEnd)
	if err != nil {
		return nil, fmt.Errorf("query pipeline_runs by date: %w", err)
	}
	defer rows.Close()
	return scanPipelineRuns(rows)
}

// GetPipelineSteps returns all steps for a given run, ordered by step number.
func (db *DB) GetPipelineSteps(runID int64) ([]PipelineStep, error) {
	rows, err := db.DB.Query(`
		SELECT id, run_id, step, total, status, channel_id, channel_name,
			input_tokens, output_tokens, cost_usd, total_api_tokens,
			message_count, period_from, period_to, duration_seconds, created_at
		FROM pipeline_steps
		WHERE run_id = ?
		ORDER BY step`, runID)
	if err != nil {
		return nil, fmt.Errorf("query pipeline_steps: %w", err)
	}
	defer rows.Close()

	var steps []PipelineStep
	for rows.Next() {
		var s PipelineStep
		if err := rows.Scan(
			&s.ID, &s.RunID, &s.Step, &s.Total, &s.Status,
			&s.ChannelID, &s.ChannelName,
			&s.InputTokens, &s.OutputTokens, &s.CostUSD, &s.TotalAPITokens,
			&s.MessageCount, &s.PeriodFrom, &s.PeriodTo, &s.DurationSeconds, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pipeline_step: %w", err)
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

// GetLatestPipelineRunPeriodTo returns the MAX(period_to) for a given pipeline
// with status='done' and non-null period_to. Returns 0 if none found.
func (db *DB) GetLatestPipelineRunPeriodTo(pipeline string) (float64, error) {
	var result sql.NullFloat64
	err := db.DB.QueryRow(`
		SELECT MAX(period_to) FROM pipeline_runs
		WHERE pipeline = ? AND status = 'done' AND period_to IS NOT NULL`,
		pipeline,
	).Scan(&result)
	if err != nil {
		return 0, fmt.Errorf("get latest pipeline run period_to: %w", err)
	}
	if !result.Valid {
		return 0, nil
	}
	return result.Float64, nil
}

func scanPipelineRuns(rows *sql.Rows) ([]PipelineRun, error) {
	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		if err := rows.Scan(
			&r.ID, &r.Pipeline, &r.Source, &r.Model, &r.Status, &r.ErrorMsg, &r.ItemsFound,
			&r.InputTokens, &r.OutputTokens, &r.CostUSD, &r.TotalAPITokens,
			&r.PeriodFrom, &r.PeriodTo, &r.StartedAt, &r.FinishedAt, &r.DurationSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan pipeline_run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
