package config

import "time"

const (
	DefaultActiveWorkspace = ""
	DefaultAIModel         = "claude-sonnet-4-20250514"
	DefaultAIContextBudget = 150000
	DefaultSyncWorkers     = 1
	DefaultInitialHistDays = 30
	DefaultPollInterval    = 15 * time.Minute
	DefaultSyncThreads     = true
	DefaultSyncOnWake      = true
	DefaultDigestEnabled   = true
	DefaultDigestModel     = "claude-haiku-4-5-20251001"
	DefaultDigestMinMsgs   = 5
)
