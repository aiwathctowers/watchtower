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

// ActionItem represents an AI-extracted action item for a user.
type ActionItem struct {
	ID                int
	ChannelID         string
	AssigneeUserID    string
	AssigneeRaw       string
	Text              string
	Context           string
	SourceMessageTS   string
	SourceChannelName string
	Status            string  // "inbox", "active", "done", "dismissed", "snoozed"
	Priority          string  // "high", "medium", "low"
	DueDate           float64 // Unix timestamp, 0 = no deadline
	PeriodFrom        float64
	PeriodTo          float64
	Model             string
	InputTokens       int
	OutputTokens      int
	CostUSD           float64
	CreatedAt         string
	CompletedAt       sql.NullString
	HasUpdates        bool    // true if source thread has new activity
	LastCheckedTS     string  // Slack ts of last checked reply
	SnoozeUntil       float64 // Unix timestamp when snooze expires, 0 = not set
	PreSnoozeStatus   string  // status to restore after snooze
	Participants      string  // JSON array of participants with stances
	SourceRefs        string  // JSON array of source message references
	RequesterName     string  // who made the request (@username)
	RequesterUserID   string  // Slack user_id of the requester
	Category          string  // code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
	Blocking          string  // who/what is blocked if this isn't done
	Tags              string  // JSON array of project/topic tags
	DecisionSummary   string  // how the group arrived at the decision
	DecisionOptions   string  // JSON array of options if decision pending
	RelatedDigestIDs  string  // JSON array of related digest IDs
	SubItems          string  // JSON array of sub-tasks with statuses
}

// Digest represents an AI-generated summary of channel activity.
type Digest struct {
	ID           int
	ChannelID    string  // "" for cross-channel digests
	PeriodFrom   float64 // Unix timestamp
	PeriodTo     float64 // Unix timestamp
	Type         string  // "channel", "daily", "weekly"
	Summary      string
	Topics       string // JSON array
	Decisions    string // JSON array
	ActionItems  string // JSON array
	MessageCount int
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	CreatedAt    string
	ReadAt       sql.NullString // NULL = unread, ISO8601 = when read
}
