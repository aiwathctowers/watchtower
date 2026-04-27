package config

import "time"

const (
	DefaultActiveWorkspace    = ""
	DefaultAIProvider         = "claude"
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

	// Calendar defaults
	DefaultCalendarEnabled       = false
	DefaultCalendarSyncDaysAhead = 7

	// Jira defaults
	DefaultJiraEnabled          = false
	DefaultJiraSyncIntervalMins = 15

	// DefaultJiraFeaturesRole is the default role for Jira feature toggles.
	DefaultJiraFeaturesRole = "ic"

	// DayPlan defaults
	DefaultDayPlanEnabled           = true
	DefaultDayPlanHour              = 8
	DefaultDayPlanWorkingHoursStart = "09:00"
	DefaultDayPlanWorkingHoursEnd   = "19:00"
	DefaultDayPlanMaxTimeblocks     = 3
	DefaultDayPlanMinBacklog        = 3
	DefaultDayPlanMaxBacklog        = 8

	// Targets defaults
	DefaultTargetsExtractEnabled        = true
	DefaultTargetsExtractMaxPerCall     = 10
	DefaultTargetsExtractTimeoutSeconds = 45
	DefaultTargetsExtractModel          = "" // empty → provider default

	DefaultTargetsResolverSlackEnabled        = true
	DefaultTargetsResolverJiraEnabled         = true
	DefaultTargetsResolverMCPTimeoutSeconds   = 10
	DefaultTargetsResolverActiveSnapshotLimit = 100
)

// RoleDisplayNames maps role keys to human-readable display names.
var RoleDisplayNames = map[string]string{
	"ic":                "IC",
	"senior_ic":         "Tech Lead",
	"middle_management": "EM",
	"top_management":    "Director",
	"direction_owner":   "PM",
}

// DefaultJiraFeatures returns the default feature toggles for a given role.
func DefaultJiraFeatures(role string) JiraFeatureToggles {
	switch role {
	case "senior_ic":
		return JiraFeatureToggles{
			MyIssuesInBriefing:   true,
			AwaitingMyInput:      true,
			WhoPing:              true,
			TrackJiraLinking:     true,
			BlockerMap:           true,
			WriteBackSuggestions: true,
			WithoutJiraDetection: true,
		}
	case "middle_management":
		return JiraFeatureToggles{
			MyIssuesInBriefing: true,
			AwaitingMyInput:    true,
			WhoPing:            true,
			TrackJiraLinking:   true,
			TeamWorkload:       true,
			BlockerMap:         true,
			IterationProgress:  true,
		}
	case "direction_owner":
		return JiraFeatureToggles{
			WhoPing:              true,
			TrackJiraLinking:     true,
			BlockerMap:           true,
			IterationProgress:    true,
			EpicProgress:         true,
			WithoutJiraDetection: true,
		}
	case "top_management":
		return JiraFeatureToggles{
			TrackJiraLinking:  true,
			TeamWorkload:      true,
			BlockerMap:        true,
			IterationProgress: true,
			EpicProgress:      true,
			ReleaseDashboard:  true,
		}
	default: // "ic" and any unknown role
		return JiraFeatureToggles{
			MyIssuesInBriefing: true,
			AwaitingMyInput:    true,
			WhoPing:            true,
			TrackJiraLinking:   true,
		}
	}
}
