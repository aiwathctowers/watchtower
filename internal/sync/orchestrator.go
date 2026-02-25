package sync

import (
	"context"
	"database/sql"
	"encoding/json"
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
	Full       bool     // Re-fetch all history within the initial_history_days window
	Channels   []string // Limit sync to specific channel names/IDs (empty = all)
	Workers    int      // Number of concurrent sync workers (0 = use config default)
	DaemonMode bool     // Running as background daemon
}

// Orchestrator coordinates the sync phases.
type Orchestrator struct {
	db          *db.DB
	slackClient *watchtowerslack.Client
	config      *config.Config
	logger      *log.Logger
}

// NewOrchestrator creates a new sync orchestrator.
func NewOrchestrator(database *db.DB, slackClient *watchtowerslack.Client, cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		db:          database,
		slackClient: slackClient,
		config:      cfg,
		logger:      log.Default(),
	}
}

// SetLogger sets a custom logger for the orchestrator.
func (o *Orchestrator) SetLogger(l *log.Logger) {
	o.logger = l
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
	if err := o.syncMetadata(ctx); err != nil {
		return fmt.Errorf("metadata sync: %w", err)
	}

	// Phase 2: messages (stub — implemented in Task 10)
	o.logger.Println("phase 2: syncing messages")
	if err := o.syncMessages(ctx, opts); err != nil {
		return fmt.Errorf("message sync: %w", err)
	}

	// Phase 3: threads (stub — implemented in Task 11)
	o.logger.Println("phase 3: syncing threads")
	if err := o.syncThreads(ctx, opts); err != nil {
		return fmt.Errorf("thread sync: %w", err)
	}

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
	for _, u := range users {
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
	for _, ch := range channels {
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
	}
	o.logger.Printf("channels: %d synced", len(channels))

	return nil
}

// syncMessages is a stub for message sync (implemented in Task 10).
func (o *Orchestrator) syncMessages(ctx context.Context, opts SyncOptions) error {
	// Will be implemented in Task 10 (message_sync.go)
	return nil
}

// syncThreads is a stub for thread sync (implemented in Task 11).
func (o *Orchestrator) syncThreads(ctx context.Context, opts SyncOptions) error {
	// Will be implemented in Task 11 (thread_sync.go)
	return nil
}

// isNonFatalError returns true for Slack errors that should be logged but not stop the sync.
func isNonFatalError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	nonFatal := []string{
		"channel_not_found",
		"account_inactive",
		"is_archived",
		"not_in_channel",
		"missing_scope",
	}
	for _, nf := range nonFatal {
		if strings.Contains(msg, nf) {
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
