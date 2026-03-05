package sync

import (
	"encoding/json"
	"os"
	"time"
)

// SyncResult stores the outcome of a sync run for display by `watchtower status`.
type SyncResult struct {
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	DurationSecs    float64   `json:"duration_secs"`
	MessagesFetched int       `json:"messages_fetched"`
	ThreadsFetched  int       `json:"threads_fetched"`
	Error           string    `json:"error,omitempty"`
}

// ResultFromSnapshot builds a SyncResult from a progress snapshot.
func ResultFromSnapshot(snap Snapshot, syncErr error) SyncResult {
	now := time.Now()
	r := SyncResult{
		StartedAt:       snap.StartTime,
		FinishedAt:      now,
		DurationSecs:    now.Sub(snap.StartTime).Seconds(),
		MessagesFetched: snap.MessagesFetched,
		ThreadsFetched:  snap.ThreadsFetched,
	}
	if syncErr != nil {
		r.Error = syncErr.Error()
	}
	return r
}

// WriteSyncResult writes the result to a JSON file.
func WriteSyncResult(path string, result SyncResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ReadSyncResult reads a sync result from a JSON file.
func ReadSyncResult(path string) (*SyncResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r SyncResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
