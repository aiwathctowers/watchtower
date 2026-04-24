package db

import (
	"database/sql"
	"time"
)

// Workspace represents a Slack workspace (team).
type Workspace struct {
	ID            string         // Slack team_id
	Name          string         // Workspace name
	Domain        string         // Workspace domain
	SyncedAt      sql.NullString // ISO8601 timestamp of last sync
	CurrentUserID string         // Slack user_id of the token owner (from auth.test)
}

// User represents a Slack user.
type User struct {
	ID          string
	Name        string
	DisplayName string
	RealName    string
	Email       string
	IsBot       bool
	IsDeleted   bool
	IsStub      bool
	ProfileJSON string
	UpdatedAt   string
}

// Channel represents a Slack channel.
type Channel struct {
	ID         string
	Name       string
	Type       string // "public", "private", "dm", "group_dm"
	Topic      string
	Purpose    string
	IsArchived bool
	IsMember   bool
	DMUserID   sql.NullString
	NumMembers int
	LastRead   string // Slack conversations.mark cursor (message ts)
	UpdatedAt  string
}

// Message represents a Slack message.
type Message struct {
	ChannelID  string
	TS         string
	UserID     string
	Text       string
	ThreadTS   sql.NullString
	ReplyCount int
	IsEdited   bool
	IsDeleted  bool
	Subtype    string
	Permalink  string
	TSUnix     float64
	RawJSON    string
}

// Reaction represents a reaction on a Slack message.
type Reaction struct {
	ChannelID string
	MessageTS string
	UserID    string
	Emoji     string
}

// ReactionSummary is an aggregated reaction count for a single emoji on a message.
type ReactionSummary struct {
	Emoji string
	Count int
}

// File represents a file attached to a Slack message.
type File struct {
	ID               string
	MessageChannelID string
	MessageTS        string
	Name             string
	Mimetype         string
	Size             int64
	Permalink        string
}

// SyncState tracks the sync progress for a channel.
type SyncState struct {
	ChannelID             string
	LastSyncedTS          string
	OldestSyncedTS        string
	IsInitialSyncComplete bool
	Cursor                string
	MessagesSynced        int
	LastSyncAt            sql.NullString
	Error                 string
}

// WatchItem represents an entry in the watch list.
type WatchItem struct {
	EntityType string // "channel" or "user"
	EntityID   string
	EntityName string
	Priority   string // "high", "normal", "low"
	CreatedAt  string
}

// UserCheckpoint tracks the user's last catchup time.
type UserCheckpoint struct {
	ID            int
	LastCheckedAt string
}

// UserAnalysis represents an AI-generated communication analysis for a user
// within a sliding time window (typically 7 days).
type UserAnalysis struct {
	ID                 int
	UserID             string
	PeriodFrom         float64 // Unix timestamp
	PeriodTo           float64 // Unix timestamp
	MessageCount       int
	ChannelsActive     int
	ThreadsInitiated   int
	ThreadsReplied     int
	AvgMessageLength   float64
	ActiveHoursJSON    string  // JSON: {"9":12,"10":8,...}
	VolumeChangePct    float64 // vs previous window
	Summary            string
	CommunicationStyle string
	DecisionRole       string // "driver","approver","observer",...
	RedFlags           string // JSON array
	Highlights         string // JSON array
	StyleDetails       string // detailed style evaluation
	Recommendations    string // JSON array
	Concerns           string // JSON array - specific issues with examples
	Accomplishments    string // JSON array - what was delivered/completed
	Model              string
	InputTokens        int
	OutputTokens       int
	CostUSD            float64
	PromptVersion      int // version of prompt used for generation
	CreatedAt          string
}

// PeriodSummary is a cross-user summary for a time window.
type PeriodSummary struct {
	ID           int
	PeriodFrom   float64 // Unix timestamp
	PeriodTo     float64 // Unix timestamp
	Summary      string  // overall team summary
	Attention    string  // JSON array - things to pay attention to
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	CreatedAt    string
}

// CustomEmoji represents a custom workspace emoji.
type CustomEmoji struct {
	Name     string // Shortcode without colons
	URL      string // URL to image, or "alias:other_name"
	AliasFor string // Target emoji name if this is an alias
}

// Track represents an action-item extracted from Slack conversations.
type Track struct {
	ID               int
	AssigneeUserID   string
	Text             string
	Context          string  // 3-5 sentence explanation
	Category         string  // code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
	Ownership        string  // "mine", "delegated", "watching"
	BallOn           string  // user_id of next actor
	OwnerUserID      string  // for delegated: report's user_id
	RequesterName    string  // who made the request
	RequesterUserID  string  // requester's Slack user_id
	Blocking         string  // who/what is blocked
	DecisionSummary  string  // how the group arrived at the decision
	DecisionOptions  string  // JSON: [{option, supporters, pros, cons}]
	SubItems         string  // JSON: [{text, status}]
	Participants     string  // JSON: [{name, user_id, stance}]
	SourceRefs       string  // JSON: [{ts, author, text}] key message quotes
	Tags             string  // JSON: ["tag1","tag2"]
	ChannelIDs       string  // JSON: ["C1","C2"] cross-channel
	RelatedDigestIDs string  // JSON: [1,2,3]
	Priority         string  // "high", "medium", "low"
	DueDate          float64 // Unix timestamp, 0 = no deadline
	Fingerprint      string  // JSON: extracted entities for dedup
	ReadAt           string  // "" = unread, ISO8601 = when read
	HasUpdates       bool
	DismissedAt      string // "" = active, ISO8601 = when dismissed
	Model            string
	InputTokens      int
	OutputTokens     int
	CostUSD          float64
	PromptVersion    int
	CreatedAt        string
	UpdatedAt        string
}

// Digest represents an AI-generated summary of channel activity.
type Digest struct {
	ID             int
	ChannelID      string  // "" for cross-channel digests
	PeriodFrom     float64 // Unix timestamp
	PeriodTo       float64 // Unix timestamp
	Type           string  // "channel", "daily", "weekly"
	Summary        string
	Topics         string // JSON array
	Decisions      string // JSON array
	ActionItems    string // JSON array
	MessageCount   int
	Model          string
	InputTokens    int
	OutputTokens   int
	CostUSD        float64
	CreatedAt      string
	ReadAt         sql.NullString // NULL = unread, ISO8601 = when read
	PromptVersion  int            // version of prompt used for generation
	PeopleSignals  string         // JSON array of PersonSignals from MAP phase (legacy)
	Situations     string         // JSON array of Situation objects
	RunningSummary string         // JSON running context for next digest (channel memory)
}

// DigestTopic represents a single topic within a digest — a granular, self-contained
// unit carrying its own decisions, action items, situations, and key messages.
type DigestTopic struct {
	ID          int
	DigestID    int
	Idx         int
	Title       string
	Summary     string
	Decisions   string // JSON array of Decision objects
	ActionItems string // JSON array of ActionItem objects
	Situations  string // JSON array of Situation objects
	KeyMessages string // JSON array of message timestamps
}

// Situation represents a notable interaction pattern observed in a channel digest.
// Each situation involves multiple participants and captures dynamics between people.
type Situation struct {
	Topic        string                 `json:"topic"`
	Type         string                 `json:"type"` // e.g. "bottleneck", "conflict", "collaboration", etc.
	Participants []SituationParticipant `json:"participants"`
	Dynamic      string                 `json:"dynamic"`
	Outcome      string                 `json:"outcome"`
	RedFlags     []string               `json:"red_flags"`
	Observations []string               `json:"observations"`
	MessageRefs  []string               `json:"message_refs"`
}

// SituationParticipant is a person involved in a situation with their role.
type SituationParticipant struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"` // e.g. "blocker", "initiator", "affected", "resolver"
}

// DigestDecisionRow represents a single decision extracted from a digest.
type DigestDecisionRow struct {
	DigestID    int
	ChannelName string
	PeriodTo    float64
	Decision    string
}

// Feedback represents a user rating on AI-generated content.
type Feedback struct {
	ID         int
	EntityType string // "digest", "track", "decision", "user_analysis"
	EntityID   string // entity-specific ID
	Rating     int    // +1 = good, -1 = bad
	Comment    string
	CreatedAt  string
}

// ImportanceCorrection records a user's override of AI-assigned decision importance.
type ImportanceCorrection struct {
	ID                 int
	DigestID           int
	DecisionIdx        int
	DecisionText       string
	OriginalImportance string
	NewImportance      string
	CreatedAt          string
}

// UserProfile stores the current user's role, team, relationships, and personalization data.
type UserProfile struct {
	ID                  int
	SlackUserID         string
	Role                string
	Team                string
	Responsibilities    string // JSON array of strings
	Reports             string // JSON array of Slack user_ids
	Peers               string // JSON array of Slack user_ids
	Manager             string // Slack user_id
	StarredChannels     string // JSON array of channel_ids
	StarredPeople       string // JSON array of Slack user_ids
	PainPoints          string // JSON array from onboarding
	TrackFocus          string // JSON array of focus areas
	OnboardingDone      bool
	CustomPromptContext string
	CreatedAt           string
	UpdatedAt           string
}

// UserInteraction stores interaction metrics between two users for a time window.
type UserInteraction struct {
	UserA             string  // current user
	UserB             string  // the other person
	PeriodFrom        float64 // window start (Unix ts)
	PeriodTo          float64 // window end (Unix ts)
	MessagesTo        int     // A's messages in channels where B is active
	MessagesFrom      int     // B's messages in channels where A is active
	SharedChannels    int     // channels where both posted
	ThreadRepliesTo   int     // A replied to B's threads
	ThreadRepliesFrom int     // B replied to A's threads
	SharedChannelIDs  string  // JSON array of shared channel IDs
	DMMessagesTo      int     // A's DM messages to B
	DMMessagesFrom    int     // B's DM messages to A
	MentionsTo        int     // A @-mentioned B
	MentionsFrom      int     // B @-mentioned A
	ReactionsTo       int     // A reacted to B's messages
	ReactionsFrom     int     // B reacted to A's messages
	InteractionScore  float64 // weighted composite score
	ConnectionType    string  // peer, i_depend, depends_on_me, weak
}

// CommunicationGuide represents an AI-generated communication coaching guide for a user.
type CommunicationGuide struct {
	ID                       int
	UserID                   string
	PeriodFrom               float64
	PeriodTo                 float64
	MessageCount             int
	ChannelsActive           int
	ThreadsInitiated         int
	ThreadsReplied           int
	AvgMessageLength         float64
	ActiveHoursJSON          string // JSON: {"9":12,"10":8,...}
	VolumeChangePct          float64
	Summary                  string // how to communicate effectively with this person
	CommunicationPreferences string // preferred style, format, timing
	AvailabilityPatterns     string // when they are most responsive
	DecisionProcess          string // how they make/participate in decisions
	SituationalTactics       string // JSON array: if X happens, do Y
	EffectiveApproaches      string // JSON array: what works well
	Recommendations          string // JSON array: actionable tips
	RelationshipContext      string // peer/report/manager dynamics
	Model                    string
	InputTokens              int
	OutputTokens             int
	CostUSD                  float64
	PromptVersion            int
	CreatedAt                string
}

// GuideSummary is a cross-user team communication health summary.
type GuideSummary struct {
	ID            int
	PeriodFrom    float64
	PeriodTo      float64
	Summary       string // team communication health overview
	Tips          string // JSON array: team-level tips
	Model         string
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
	PromptVersion int
	CreatedAt     string
}

// PeopleCard is a unified per-user card combining analysis + guide data.
// Generated by the REDUCE phase from situations in channel digests.
type PeopleCard struct {
	ID                  int
	UserID              string
	PeriodFrom          float64
	PeriodTo            float64
	MessageCount        int
	ChannelsActive      int
	ThreadsInitiated    int
	ThreadsReplied      int
	AvgMessageLength    float64
	ActiveHoursJSON     string // JSON: {"9":12,"10":8,...}
	VolumeChangePct     float64
	Summary             string
	CommunicationStyle  string // driver|collaborator|executor|observer|facilitator
	DecisionRole        string // decision-maker|approver|contributor|observer|blocker
	RedFlags            string // JSON array
	Highlights          string // JSON array
	Accomplishments     string // JSON array
	CommunicationGuide  string // coaching paragraph (was how_to_communicate)
	DecisionStyle       string // how they participate in decisions
	Tactics             string // JSON array of "If X, then Y"
	RelationshipContext string
	Status              string // "ready" or "insufficient_data"
	Model               string
	InputTokens         int
	OutputTokens        int
	CostUSD             float64
	PromptVersion       int
	CreatedAt           string
}

// PeopleCardSummary is a cross-user team health summary for a time window.
type PeopleCardSummary struct {
	ID            int
	PeriodFrom    float64
	PeriodTo      float64
	Summary       string // team communication health overview
	Attention     string // JSON array: who needs attention and why
	Tips          string // JSON array: team-level tips
	Model         string
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
	PromptVersion int
	CreatedAt     string
}

// Briefing represents a daily personalized briefing for the user.
type Briefing struct {
	ID            int
	WorkspaceID   string
	UserID        string
	Date          string // YYYY-MM-DD
	Role          string
	Attention     string // JSON array of AttentionItem
	YourDay       string // JSON array of YourDayItem
	WhatHappened  string // JSON array of WhatHappenedItem
	TeamPulse     string // JSON array of TeamPulseItem
	Coaching      string // JSON array of CoachingItem
	Model         string
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
	PromptVersion int
	ReadAt        sql.NullString // NULL = unread, ISO8601 = when read
	CreatedAt     string
}

// Prompt represents an editable AI prompt template.
type Prompt struct {
	ID        string // "digest.channel", "tracks.extract", "analysis.user", etc.
	Template  string
	Version   int
	Language  string // "" = auto-detect, "en", "ru", etc.
	UpdatedAt string
}

// ChannelSettings stores per-channel user preferences (mute for AI, favorite).
type ChannelSettings struct {
	ChannelID     string
	IsMutedForLLM bool
	IsFavorite    bool
	UpdatedAt     string
}

// Target represents a hierarchical goal at any level (replaces Task).
type Target struct {
	ID                int
	Text              string
	Intent            string
	Level             string        // "quarter", "month", "week", "day", "custom"
	CustomLabel       string        // free text when level="custom"
	PeriodStart       string        // YYYY-MM-DD
	PeriodEnd         string        // YYYY-MM-DD
	ParentID          sql.NullInt64 // references targets(id)
	Status            string        // "todo", "in_progress", "blocked", "done", "dismissed", "snoozed"
	Priority          string        // "high", "medium", "low"
	Ownership         string        // "mine", "delegated", "watching"
	BallOn            string
	DueDate           string // "YYYY-MM-DDTHH:MM" or ""
	SnoozeUntil       string // "YYYY-MM-DDTHH:MM" or ""
	Blocking          string
	Tags              string  // JSON
	SubItems          string  // JSON
	Notes             string  // JSON
	Progress          float64 // 0.0..1.0
	SourceType        string  // "extract", "briefing", "manual", "chat", "inbox", "jira", "slack"
	SourceID          string
	AILevelConfidence sql.NullFloat64
	CreatedAt         string
	UpdatedAt         string
}

// TargetNote represents a single note entry in a target's notes JSON array.
type TargetNote struct {
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// TargetFilter specifies criteria for querying targets.
type TargetFilter struct {
	Status      string
	Priority    string
	Ownership   string
	Level       string
	ParentID    *int64
	SourceType  string
	SourceID    string
	Search      string
	Limit       int
	IncludeDone bool
}

// TargetLink represents a typed link between two targets or to an external reference.
type TargetLink struct {
	ID             int
	SourceTargetID int
	TargetTargetID sql.NullInt64
	ExternalRef    string
	Relation       string // "contributes_to", "blocks", "related", "duplicates"
	Confidence     sql.NullFloat64
	CreatedBy      string // "ai", "user"
	CreatedAt      string
}

// InboxItem represents a Slack message awaiting user response.
type InboxItem struct {
	ID             int
	ChannelID      string
	MessageTS      string
	ThreadTS       string
	SenderUserID   string
	TriggerType    string // "mention", "dm"
	Snippet        string
	Context        string
	RawText        string
	Permalink      string
	Status         string // "pending", "resolved", "dismissed", "snoozed"
	Priority       string // "high", "medium", "low"
	AIReason       string
	ResolvedReason string
	SnoozeUntil    string
	WaitingUserIDs string // JSON array of user IDs waiting for response, e.g. ["U123","U456"]
	TargetID       *int
	ReadAt         string
	CreatedAt      string
	UpdatedAt      string
	ItemClass      string // "actionable" or "ambient"
	Pinned         bool
	ArchivedAt     string // empty if not archived
	ArchiveReason  string
}

// InboxCandidate is a potential inbox item found by detection queries.
type InboxCandidate struct {
	ChannelID    string
	MessageTS    string
	ThreadTS     string
	SenderUserID string
	Text         string
	Permalink    string
	TriggerType  string // "mention", "dm", "thread_reply", "reaction"
	TSUnix       float64
}

// InboxFilter specifies criteria for querying inbox items.
type InboxFilter struct {
	Status          string // "" = any
	Priority        string // "" = any
	TriggerType     string // "" = any
	ChannelID       string // "" = any
	Limit           int    // 0 = no limit
	IncludeResolved bool   // include resolved/dismissed
}

// CalendarCalendar represents a Google Calendar.
type CalendarCalendar struct {
	ID         string
	Name       string
	IsPrimary  bool
	IsSelected bool
	Color      string
	SyncedAt   string
}

// CalendarEvent represents a Google Calendar event stored locally.
type CalendarEvent struct {
	ID             string
	CalendarID     string
	Title          string
	Description    string
	Location       string
	StartTime      string // ISO8601
	EndTime        string // ISO8601
	OrganizerEmail string
	Attendees      string // JSON
	IsRecurring    bool
	IsAllDay       bool
	EventStatus    string
	EventType      string
	HTMLLink       string
	RawJSON        string
	SyncedAt       string
	UpdatedAt      string
}

// CalendarAttendeeMap caches email to Slack user_id resolution.
type CalendarAttendeeMap struct {
	Email       string
	SlackUserID string
	ResolvedAt  string
}

// CalendarEventFilter specifies criteria for querying calendar events.
type CalendarEventFilter struct {
	CalendarID string
	FromTime   string // ISO8601
	ToTime     string // ISO8601
	Limit      int
}

// MeetingPrepCache stores a cached meeting prep result for an event.
type MeetingPrepCache struct {
	EventID     string
	ResultJSON  string
	UserNotes   string
	GeneratedAt string
}

// JiraBoard represents a Jira agile board stored locally.
type JiraBoard struct {
	ID                 int
	Name               string
	ProjectKey         string
	BoardType          string
	IsSelected         bool
	IssueCount         int
	SyncedAt           string
	RawColumnsJSON     string `json:"raw_columns_json"`
	RawConfigJSON      string `json:"raw_config_json"`
	LLMProfileJSON     string `json:"llm_profile_json"`
	WorkflowSummary    string `json:"workflow_summary"`
	UserOverridesJSON  string `json:"user_overrides_json"`
	ConfigHash         string `json:"config_hash"`
	ProfileGeneratedAt string `json:"profile_generated_at"`
}

// JiraSlackLink represents a detected Jira key mention in Slack.
type JiraSlackLink struct {
	ID         int
	IssueKey   string
	ChannelID  string
	MessageTS  string
	TrackID    *int
	DigestID   *int
	LinkType   string
	DetectedAt string
}

// JiraIssue represents a Jira issue stored locally.
type JiraIssue struct {
	Key                     string
	ID                      string
	ProjectKey              string
	BoardID                 int
	Summary                 string
	DescriptionText         string
	IssueType               string
	IssueTypeCategory       string
	IsBug                   bool
	Status                  string
	StatusCategory          string
	StatusCategoryChangedAt string
	AssigneeAccountID       string
	AssigneeEmail           string
	AssigneeDisplayName     string
	AssigneeSlackID         string
	ReporterAccountID       string
	ReporterEmail           string
	ReporterDisplayName     string
	ReporterSlackID         string
	Priority                string
	StoryPoints             *float64
	DueDate                 string
	SprintID                int
	SprintName              string
	EpicKey                 string
	Labels                  string // JSON array
	Components              string // JSON array
	FixVersions             string // JSON array of version names
	CreatedAt               string
	UpdatedAt               string
	ResolvedAt              string
	RawJSON                 string
	CustomFieldsJSON        string
	SyncedAt                string
	IsDeleted               bool
}

// JiraCustomField represents a discovered Jira custom field.
type JiraCustomField struct {
	ID        string
	Name      string
	FieldType string
	ItemsType string
	IsUseful  bool
	UsageHint string
	SyncedAt  string
}

// JiraBoardFieldMap maps a custom field to a role on a specific board.
type JiraBoardFieldMap struct {
	BoardID int
	FieldID string
	Role    string
}

// JiraSprint represents a Jira sprint stored locally.
type JiraSprint struct {
	ID           int
	BoardID      int
	Name         string
	State        string
	Goal         string
	StartDate    string
	EndDate      string
	CompleteDate string
	SyncedAt     string
}

// JiraIssueLink represents a link between two Jira issues.
type JiraIssueLink struct {
	ID        string
	SourceKey string
	TargetKey string
	LinkType  string
	SyncedAt  string
}

// JiraUserMap maps a Jira account to a Slack user.
type JiraUserMap struct {
	JiraAccountID   string
	Email           string
	SlackUserID     string
	DisplayName     string
	MatchMethod     string
	MatchConfidence float64
	ResolvedAt      string
}

// JiraSyncState tracks the sync state for a Jira project.
type JiraSyncState struct {
	ProjectKey   string
	LastSyncedAt string
	IssuesSynced int
	LastError    string
	LastErrorAt  string
}

// JiraRelease represents a Jira fix version (release) stored locally.
type JiraRelease struct {
	ID          int    `json:"id"`
	ProjectKey  string `json:"project_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
	Released    bool   `json:"released"`
	Archived    bool   `json:"archived"`
	SyncedAt    string `json:"synced_at"`
}

// SprintStats holds aggregated issue counts for an active sprint.
type SprintStats struct {
	SprintName string
	Total      int
	Done       int
	InProgress int
	Todo       int
	DaysLeft   int
}

// DeliveryStats holds delivery metrics for a user over a time range.
type DeliveryStats struct {
	IssuesClosed         int
	AvgCycleTimeDays     float64
	StoryPointsCompleted float64
	OpenIssues           int
	OverdueIssues        int
	Components           []string
	Labels               []string
}

// TeamWorkloadRow holds aggregated workload metrics for a single assignee.
type TeamWorkloadRow struct {
	SlackUserID      string
	DisplayName      string
	OpenIssues       int
	StoryPoints      float64
	OverdueCount     int
	BlockedCount     int
	AvgCycleTimeDays float64
}

// PromptHistory records a snapshot of a prompt at a specific version.
type PromptHistory struct {
	ID        int
	PromptID  string
	Version   int
	Template  string
	Reason    string // "tuned: 12 negative feedbacks", "manual edit", "rollback to v3"
	CreatedAt string
}

// DayPlan is the top-level day-planning record, one per user per date.
type DayPlan struct {
	ID                int64
	UserID            string
	PlanDate          string // YYYY-MM-DD, local date
	Status            string // active | archived
	HasConflicts      bool
	ConflictSummary   sql.NullString
	GeneratedAt       time.Time
	LastRegeneratedAt sql.NullTime
	RegenerateCount   int
	FeedbackHistory   string // JSON []string
	PromptVersion     sql.NullString
	BriefingID        sql.NullInt64
	ReadAt            sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// DayPlanItem is an individual item inside a day plan.
type DayPlanItem struct {
	ID          int64
	DayPlanID   int64
	Kind        string // timeblock | backlog
	SourceType  string // task | briefing_attention | jira | calendar | manual | focus
	SourceID    sql.NullString
	Title       string
	Description sql.NullString
	Rationale   sql.NullString
	StartTime   sql.NullTime
	EndTime     sql.NullTime
	DurationMin sql.NullInt64
	Priority    sql.NullString
	Status      string // pending | done | skipped
	OrderIndex  int
	Tags        string // JSON, may be empty string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Day plan status constants.
const (
	DayPlanStatusActive   = "active"
	DayPlanStatusArchived = "archived"
)

// Day plan item kind / source / status constants.
const (
	DayPlanItemKindTimeblock = "timeblock"
	DayPlanItemKindBacklog   = "backlog"

	DayPlanItemSourceTask              = "task"
	DayPlanItemSourceBriefingAttention = "briefing_attention"
	DayPlanItemSourceJira              = "jira"
	DayPlanItemSourceCalendar          = "calendar"
	DayPlanItemSourceManual            = "manual"
	DayPlanItemSourceFocus             = "focus"

	DayPlanItemStatusPending = "pending"
	DayPlanItemStatusDone    = "done"
	DayPlanItemStatusSkipped = "skipped"
)
