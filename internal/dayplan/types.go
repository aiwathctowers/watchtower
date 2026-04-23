// Package dayplan builds personalized daily plans aggregating tasks, calendar,
// briefing, Jira, and people signals into time-blocks plus a backlog.
package dayplan

import "time"

// GenerateResult is the parsed JSON output of the AI.
type GenerateResult struct {
	Timeblocks []AIItem `json:"timeblocks"`
	Backlog    []AIItem `json:"backlog"`
	Summary    string   `json:"summary"`
}

// AIItem is a single item inside the AI response (timeblock or backlog).
type AIItem struct {
	SourceType     string `json:"source_type"`
	SourceID       any    `json:"source_id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Rationale      string `json:"rationale"`
	StartTimeLocal string `json:"start_time_local,omitempty"`
	EndTimeLocal   string `json:"end_time_local,omitempty"`
	Priority       string `json:"priority"`
}

// RunOptions controls a single invocation of Pipeline.Run.
type RunOptions struct {
	UserID   string
	Date     string    // YYYY-MM-DD
	Force    bool      // regenerate even if plan exists
	Feedback string    // triggers regeneration, non-empty implies Force
	Now      time.Time // test hook; zero value → time.Now()
}
