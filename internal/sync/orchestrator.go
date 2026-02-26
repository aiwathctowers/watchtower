package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"watchtower/internal/config"
	"watchtower/internal/db"
	watchtowerslack "watchtower/internal/slack"

	"github.com/slack-go/slack"
)

// SyncOptions configures how a sync run behaves.
type SyncOptions struct {
	Full     bool     // Re-fetch all history within the initial_history_days window
	Channels []string // Limit sync to specific channel names/IDs (empty = all)
	Workers  int      // Number of concurrent sync workers (0 = use config default)
}

// Orchestrator coordinates the sync phases.
type Orchestrator struct {
	db          *db.DB
	slackClient *watchtowerslack.Client
	config      *config.Config
	logger      *log.Logger
	progress    *Progress
}

// NewOrchestrator creates a new sync orchestrator.
func NewOrchestrator(database *db.DB, slackClient *watchtowerslack.Client, cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		db:          database,
		slackClient: slackClient,
		config:      cfg,
		logger:      log.Default(),
		progress:    NewProgress(),
	}
}

// SetLogger sets a custom logger for the orchestrator.
func (o *Orchestrator) SetLogger(l *log.Logger) {
	o.logger = l
}

// Progress returns the progress tracker for this orchestrator.
func (o *Orchestrator) Progress() *Progress {
	return o.progress
}

// Run executes the sync pipeline in three sequential phases:
// 1. Metadata sync (workspace info, users, channels)
// 2. Message sync (parallel across channels)
// 3. Thread sync (parallel across threads)
// Non-fatal errors (channel_not_found, access_denied) are logged and skipped.
// Fatal errors (DB failures) stop the sync.
func (o *Orchestrator) Run(ctx context.Context, opts SyncOptions) error {
	o.logger.Println("starting sync")

	// Phase 1: metadata
	o.logger.Println("phase 1: syncing metadata")
	o.progress.SetPhase(PhaseMetadata)
	if err := o.syncMetadata(ctx); err != nil {
		return fmt.Errorf("metadata sync: %w", err)
	}

	// Phase 2: messages
	o.logger.Println("phase 2: syncing messages")
	o.progress.SetPhase(PhaseMessages)
	if err := o.syncMessages(ctx, opts); err != nil {
		return fmt.Errorf("message sync: %w", err)
	}

	// Phase 3: threads
	o.logger.Println("phase 3: syncing threads")
	o.progress.SetPhase(PhaseThreads)
	if err := o.syncThreads(ctx, opts); err != nil {
		return fmt.Errorf("thread sync: %w", err)
	}

	o.progress.SetPhase(PhaseDone)
	o.logger.Println("sync complete")
	return nil
}

// syncMetadata fetches workspace info, users, and channels from Slack and upserts into DB.
func (o *Orchestrator) syncMetadata(ctx context.Context) error {
	// Workspace info
	teamInfo, err := o.slackClient.GetTeamInfo(ctx)
	if err != nil {
		return fmt.Errorf("fetching team info: %w", err)
	}
	if err := o.db.UpsertWorkspace(db.Workspace{
		ID:     teamInfo.ID,
		Name:   teamInfo.Name,
		Domain: teamInfo.Domain,
	}); err != nil {
		return fmt.Errorf("upserting workspace: %w", err)
	}
	o.logger.Printf("workspace: %s (%s)", teamInfo.Name, teamInfo.ID)

	// Users
	users, err := o.slackClient.GetUsers(ctx)
	if err != nil {
		return fmt.Errorf("fetching users: %w", err)
	}
	existingUsers, err := o.db.GetUsers(db.UserFilter{})
	if err != nil {
		return fmt.Errorf("fetching existing users: %w", err)
	}
	existingUserIDs := make(map[string]bool, len(existingUsers))
	for _, u := range existingUsers {
		existingUserIDs[u.ID] = true
	}

	apiUserIDs := make(map[string]bool, len(users))
	o.progress.SetMetadataUsers(len(users), 0)
	for i, u := range users {
		apiUserIDs[u.ID] = true
		profileJSON, _ := json.Marshal(u.Profile)
		if err := o.db.UpsertUser(db.User{
			ID:          u.ID,
			Name:        u.Name,
			DisplayName: u.Profile.DisplayName,
			RealName:    u.RealName,
			Email:       u.Profile.Email,
			IsBot:       u.IsBot,
			IsDeleted:   u.Deleted,
			ProfileJSON: string(profileJSON),
		}); err != nil {
			return fmt.Errorf("upserting user %s: %w", u.ID, err)
		}
		o.progress.SetMetadataUsers(len(users), i+1)
	}

	// Detect deleted users: in DB but not returned by API
	for _, u := range existingUsers {
		if !apiUserIDs[u.ID] && !u.IsDeleted {
			if err := o.db.UpsertUser(db.User{
				ID:          u.ID,
				Name:        u.Name,
				DisplayName: u.DisplayName,
				RealName:    u.RealName,
				Email:       u.Email,
				IsBot:       u.IsBot,
				IsDeleted:   true,
				ProfileJSON: u.ProfileJSON,
			}); err != nil {
				return fmt.Errorf("marking user %s as deleted: %w", u.ID, err)
			}
		}
	}
	o.logger.Printf("users: %d synced", len(users))

	// Channels
	channels, err := o.slackClient.GetChannels(ctx)
	if err != nil {
		return fmt.Errorf("fetching channels: %w", err)
	}
	o.progress.SetMetadataChannels(len(channels), 0)
	for i, ch := range channels {
		chType := slackChannelType(ch)
		if err := o.db.UpsertChannel(db.Channel{
			ID:         ch.ID,
			Name:       ch.Name,
			Type:       chType,
			Topic:      ch.Topic.Value,
			Purpose:    ch.Purpose.Value,
			IsArchived: ch.IsArchived,
			IsMember:   ch.IsMember,
			DMUserID:   sql.NullString{String: ch.User, Valid: ch.User != ""},
			NumMembers: ch.NumMembers,
		}); err != nil {
			return fmt.Errorf("upserting channel %s: %w", ch.ID, err)
		}
		o.progress.SetMetadataChannels(len(channels), i+1)
	}
	o.logger.Printf("channels: %d synced", len(channels))

	return nil
}

// syncMessages is implemented in message_sync.go.

// syncThreads is implemented in thread_sync.go.

// nonFatalSlackErrors are Slack API error codes that should be logged but not stop the sync.
var nonFatalSlackErrors = map[string]bool{
	"channel_not_found": true,
	"account_inactive":  true,
	"is_archived":       true,
	"not_in_channel":    true,
	"missing_scope":     true,
	"access_denied":     true,
}

// isNonFatalError returns true for Slack errors that should be logged but not stop the sync.
func isNonFatalError(err error) bool {
	if err == nil {
		return false
	}
	// Check for structured Slack API errors first.
	var slackErr slack.SlackErrorResponse
	if errors.As(err, &slackErr) {
		return nonFatalSlackErrors[slackErr.Err]
	}
	// Fallback: string matching for wrapped or non-typed errors.
	msg := err.Error()
	for code := range nonFatalSlackErrors {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// slackChannelType maps a Slack channel object to our type string.
func slackChannelType(ch slack.Channel) string {
	if ch.IsIM {
		return "dm"
	}
	if ch.IsMpIM {
		return "group_dm"
	}
	if ch.IsPrivate {
		return "private"
	}
	return "public"
}
