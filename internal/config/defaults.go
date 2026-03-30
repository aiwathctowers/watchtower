package config

import "time"

const (
	DefaultActiveWorkspace    = ""
	DefaultAIModel            = "claude-sonnet-4-6"
	DefaultAIContextBudget    = 150000
	DefaultAIWorkers          = 5
	DefaultSyncWorkers        = 1
	DefaultInitialHistDays    = 2
	DefaultPollInterval       = 15 * time.Minute
	DefaultSyncThreads        = true
	DefaultSyncOnWake         = true
	DefaultDigestEnabled      = true
	DefaultDigestMinMsgs      = 10
	DefaultDigestLang         = "Russian"
	DefaultDigestWorkers      = 5 // Deprecated: use DefaultAIWorkers. Kept for backward compat.
	DefaultTracksInterval     = 1 * time.Hour
	DefaultBriefingEnabled    = true
	DefaultBriefingHour       = 8
	DefaultInboxEnabled       = true
	DefaultInboxMaxItems      = 100
	DefaultInboxLookbackDays  = 7
	DefaultTracksMinMsgs      = 3
	DefaultBatchMaxChannels   = 20
	DefaultBatchMaxMessages   = 1500
	DefaultMaxBatchesPerRun   = 25  // max AI calls per digest run (budget cap)
	DefaultDigestCooldownMins = 30  // skip channel if digested < N minutes ago with few messages
	DefaultMessageTruncateLen = 500 // truncate individual messages longer than this (chars)

	// Tiered batching thresholds (visible message count).
	DefaultBatchHighActivityThreshold = 200 // >200 → individual batch (1 channel)
	DefaultBatchLowActivityThreshold  = 30  // <30 → triple channel limit per batch
)
