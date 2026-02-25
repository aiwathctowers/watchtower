package config

import "time"

const (
	DefaultActiveWorkspace  = ""
	DefaultAIModel          = "claude-sonnet-4-20250514"
	DefaultAIMaxTokens      = 4096
	DefaultAIContextBudget  = 150000
	DefaultSyncWorkers      = 5
	DefaultInitialHistDays  = 30
	DefaultPollInterval     = 15 * time.Minute
	DefaultSyncThreads      = true
	DefaultSyncOnWake       = true
)
