package db

import "database/sql"

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

// Track represents an auto-generated informational track (v3).
type Track struct {
	ID            int
	Title         string
	Narrative     string
	CurrentStatus string
	Participants  string // JSON: [{"user_id":"U...","name":"...","role":"driver|reviewer|blocker|observer"}]
	Timeline      string // JSON: [{"date":"2026-03-20","event":"...","channel_id":"C..."}]
	KeyMessages   string // JSON: [{"ts":"...","author":"...","text":"...","channel_id":"C..."}]
	Priority      string // "high", "medium", "low"
	Tags          string // JSON: ["tag1","tag2"]
	ChannelIDs    string // JSON: ["C1","C2"]
	SourceRefs    string // JSON: [{"digest_id":1,"topic_id":42,"channel_id":"C...","timestamp":1234.0}]
	ReadAt        string // "" = unread, ISO8601 = when read
	HasUpdates    bool
	Model         string
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
	PromptVersion int
	CreatedAt     string
	UpdatedAt     string
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

// PromptHistory records a snapshot of a prompt at a specific version.
type PromptHistory struct {
	ID        int
	PromptID  string
	Version   int
	Template  string
	Reason    string // "tuned: 12 negative feedbacks", "manual edit", "rollback to v3"
	CreatedAt string
}
