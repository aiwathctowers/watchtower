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
	SkipDMs  bool     // Skip syncing DMs and group DMs
}

// Orchestrator coordinates the sync phases.
type Orchestrator struct {
	db                   *db.DB
	slackClient          *watchtowerslack.Client
	config               *config.Config
	logger               *log.Logger
	progress             *Progress
	channelNames         map[string]string // channel ID -> name, populated during message sync
	discoveredChannelIDs map[string]bool   // channels found active by discovery phase
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

// SetLogger sets a custom logger for the orchestrator and its Slack client.
func (o *Orchestrator) SetLogger(l *log.Logger) {
	o.logger = l
	o.slackClient.SetLogger(l)
}

// Progress returns the progress tracker for this orchestrator.
func (o *Orchestrator) Progress() *Progress {
	return o.progress
}

// resolveWorkerCount clamps the requested worker count to a safe range,
// falling back to the config default or 1 if not specified.
func (o *Orchestrator) resolveWorkerCount(requested int) int {
	workers := requested
	if workers <= 0 {
		workers = o.config.Sync.Workers
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > 100 {
		workers = 100
	}
	return workers
}

// Run executes the sync pipeline. For incremental sync (default), it uses
// search.messages to directly save messages, avoiding per-channel API calls.
// For --full or --channels, it runs the full pipeline:
// 1. Workspace info (team.info, cached)
// 2. Metadata sync (conversations.list, users.list)
// 3. Messages (conversations.history per channel)
// 4. User profiles (users.info for unknown users)
// 5. Threads (conversations.replies)
func (o *Orchestrator) Run(ctx context.Context, opts SyncOptions) error {
	o.logger.Println("starting sync")

	// Phase 1: workspace info
	o.progress.SetPhase(PhaseMetadata)

	// Ensure workspace record exists (team.info, cached after first call)
	ws, err := o.db.GetWorkspace()
	if err != nil {
		return fmt.Errorf("checking workspace: %w", err)
	}
	if ws == nil {
		if err := o.ensureWorkspace(ctx); err != nil {
			return fmt.Errorf("workspace sync: %w", err)
		}
	} else {
		o.logger.Printf("workspace: %s (%s) [cached]", ws.Name, ws.ID)
		// Retry syncCurrentUser if it failed on a previous run (e.g. auth.test error).
		// Required for action items pipeline which needs current_user_id.
		if ws.CurrentUserID == "" {
			o.syncCurrentUser(ctx)
		}
	}

	// Sync custom emojis (fast, single API call)
	if err := o.syncEmoji(ctx); err != nil {
		o.logger.Printf("warning: emoji sync failed: %v", err)
		// Non-fatal: continue with message sync
	}

	if opts.Full || len(opts.Channels) > 0 {
		return o.runFullSync(ctx, opts)
	}
	return o.runSearchSync(ctx, opts)
}

// runFullSync executes the full sync pipeline with per-channel conversations.history.
func (o *Orchestrator) runFullSync(ctx context.Context, opts SyncOptions) error {
	// Phase 2: full metadata sync
	o.logger.Println("phase 2: full metadata sync")
	if err := o.syncMetadata(ctx, opts); err != nil {
		return fmt.Errorf("metadata sync: %w", err)
	}

	// Phase 3: messages
	o.logger.Println("phase 3: syncing messages")
	o.progress.SetPhase(PhaseMessages)
	if err := o.syncMessages(ctx, opts); err != nil {
		return fmt.Errorf("message sync: %w", err)
	}

	// Phase 4: user profiles
	o.logger.Println("phase 4: syncing user profiles")
	o.progress.SetPhase(PhaseUsers)
	if err := o.syncUserProfiles(ctx); err != nil {
		return fmt.Errorf("user profile sync: %w", err)
	}

	// Phase 5: threads
	o.logger.Println("phase 5: syncing threads")
	o.progress.SetPhase(PhaseThreads)
	if err := o.syncThreads(ctx, opts); err != nil {
		return fmt.Errorf("thread sync: %w", err)
	}

	return o.finishSync()
}

// runSearchSync uses search.messages to save messages directly, then fetches
// profiles for any unknown users. Much fewer API calls than full sync.
func (o *Orchestrator) runSearchSync(ctx context.Context, opts SyncOptions) error {
	// Phase 2: search-based sync (messages saved directly from search results)
	o.progress.SetPhase(PhaseDiscovery)
	o.logger.Println("phase 2: search-based sync")
	if err := o.syncViaSearch(ctx); err != nil {
		if isNonFatalError(err) {
			o.logger.Printf("search sync failed, falling back to full sync: %v", err)
			return o.runFullSync(ctx, opts)
		}
		return fmt.Errorf("search sync: %w", err)
	}

	// Fallback: if search found 0 channels (e.g. missing search:read scope),
	// check if DB already has channels from a previous sync; if not, fall back
	// to full sync so we have something to work with.
	snap := o.progress.Snapshot()
	if snap.DiscoveryChannels == 0 {
		stats, err := o.db.GetStats()
		if err != nil || stats.ChannelCount == 0 {
			o.logger.Println("search found 0 channels, falling back to full sync")
			return o.runFullSync(ctx, opts)
		}
	}

	// Phase 3: user profiles
	o.logger.Println("phase 3: syncing user profiles")
	o.progress.SetPhase(PhaseUsers)
	if err := o.syncUserProfiles(ctx); err != nil {
		return fmt.Errorf("user profile sync: %w", err)
	}

	return o.finishSync()
}

// finishSync logs API stats and marks sync as done.
func (o *Orchestrator) finishSync() error {
	o.progress.SetPhase(PhaseDone)
	counts, retries := o.slackClient.APIStats()
	total := 0
	for _, v := range counts {
		total += v
	}
	o.logger.Printf("sync complete: %d API calls (tier2: %d, tier3: %d, tier4: %d), %d retries",
		total, counts[watchtowerslack.Tier2], counts[watchtowerslack.Tier3], counts[watchtowerslack.Tier4], retries)
	o.slackClient.ResetAPIStats()
	return nil
}

// ensureWorkspace fetches and caches workspace info. Skips the API call if already in DB.
func (o *Orchestrator) ensureWorkspace(ctx context.Context) error {
	ws, err := o.db.GetWorkspace()
	if err != nil {
		return fmt.Errorf("checking workspace: %w", err)
	}
	if ws != nil {
		o.logger.Printf("workspace: %s (%s) [cached]", ws.Name, ws.ID)
		return nil
	}

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

	// Identify the current user via auth.test
	o.syncCurrentUser(ctx)

	return nil
}

// syncCurrentUser calls auth.test to identify the token owner and stores
// the user_id in the workspace record. Errors are logged but non-fatal.
func (o *Orchestrator) syncCurrentUser(ctx context.Context) {
	authResp, err := o.slackClient.AuthTest(ctx)
	if err != nil {
		o.logger.Printf("warning: auth.test failed: %v", err)
		return
	}
	if err := o.db.SetCurrentUserID(authResp.UserID); err != nil {
		o.logger.Printf("warning: saving current user: %v", err)
		return
	}
	o.logger.Printf("current user: @%s (%s)", authResp.User, authResp.UserID)
}

// syncMetadata fetches workspace info, users, and channels from Slack and upserts into DB.
func (o *Orchestrator) syncMetadata(ctx context.Context, opts SyncOptions) error {
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

	// Identify the current user
	o.syncCurrentUser(ctx)

	// Users
	o.logger.Println("fetching users from Slack API...")
	users, err := o.slackClient.GetUsers(ctx, func(fetched int) {
		o.progress.SetMetadataUsers(fetched, 0)
	})
	if err != nil {
		return fmt.Errorf("fetching users: %w", err)
	}
	o.logger.Printf("fetched %d users, saving to DB...", len(users))
	// Filter: skip deleted and deactivated users
	var activeUsers []slack.User
	var skippedDeleted int
	for _, u := range users {
		if u.Deleted {
			skippedDeleted++
			continue
		}
		activeUsers = append(activeUsers, u)
	}
	o.logger.Printf("users: %d active, %d deleted (skipped)", len(activeUsers), skippedDeleted)

	apiUserIDs := make(map[string]bool, len(activeUsers))
	o.progress.SetMetadataUsers(len(activeUsers), 0)
	for i, u := range activeUsers {
		apiUserIDs[u.ID] = true
		tag := ""
		if u.IsBot {
			tag = " [bot]"
		}
		o.logger.Printf("  user %d/%d: @%s (%s)%s", i+1, len(activeUsers), u.Name, u.RealName, tag)
		profileJSON, err := json.Marshal(u.Profile)
		if err != nil {
			o.logger.Printf("warning: failed to marshal profile for user %s: %v", u.ID, err)
			profileJSON = []byte("{}")
		}
		if err := o.db.UpsertUser(db.User{
			ID:          u.ID,
			Name:        u.Name,
			DisplayName: u.Profile.DisplayName,
			RealName:    u.RealName,
			Email:       u.Profile.Email,
			IsBot:       u.IsBot,
			IsDeleted:   false,
			ProfileJSON: string(profileJSON),
		}); err != nil {
			return fmt.Errorf("upserting user %s: %w", u.ID, err)
		}
		o.progress.SetMetadataUsers(len(activeUsers), i+1)
	}
	o.logger.Printf("users: %d saved to DB", len(activeUsers))

	// Channels — include DMs by default, skip only if --skip-dms is set
	channelTypes := []string{"public_channel", "private_channel"}
	if !opts.SkipDMs {
		channelTypes = append(channelTypes, "im", "mpim")
	}
	o.logger.Printf("fetching channels from Slack API (types: %s)...", strings.Join(channelTypes, ", "))
	channels, err := o.slackClient.GetChannels(ctx, channelTypes, func(fetched int) {
		o.progress.SetMetadataChannels(fetched, 0)
	})
	if err != nil {
		return fmt.Errorf("fetching channels: %w", err)
	}
	o.logger.Printf("fetched %d channels, saving to DB...", len(channels))

	o.progress.SetMetadataChannels(len(channels), 0)
	for i, ch := range channels {
		chType := slackChannelType(ch)
		flags := []string{chType}
		if ch.IsArchived {
			flags = append(flags, "archived")
		}
		if ch.IsMember {
			flags = append(flags, "member")
		}
		name := ch.Name
		if name == "" {
			name = ch.ID
		}
		o.logger.Printf("  channel %d/%d: #%s [%s] %d members", i+1, len(channels), name, strings.Join(flags, ","), ch.NumMembers)
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

// syncEmoji fetches custom workspace emojis and stores them in the database.
func (o *Orchestrator) syncEmoji(ctx context.Context) error {
	o.logger.Println("syncing custom emojis")
	emojiMap, err := o.slackClient.GetEmoji(ctx)
	if err != nil {
		return fmt.Errorf("fetching emojis: %w", err)
	}

	emojis := make([]db.CustomEmoji, 0, len(emojiMap))
	for name, value := range emojiMap {
		e := db.CustomEmoji{Name: name, URL: value}
		if target, ok := strings.CutPrefix(value, "alias:"); ok {
			e.AliasFor = target
		}
		emojis = append(emojis, e)
	}

	if err := o.db.BulkUpsertCustomEmojis(emojis); err != nil {
		return fmt.Errorf("saving emojis: %w", err)
	}

	o.logger.Printf("emojis: %d custom emojis synced", len(emojis))
	return nil
}

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
	// Rate limit errors are non-fatal; the next sync run will resume via cursor.
	var rlErr *slack.RateLimitedError
	if errors.As(err, &rlErr) {
		return true
	}
	// Check for structured Slack API errors first.
	var slackErr slack.SlackErrorResponse
	if errors.As(err, &slackErr) {
		return nonFatalSlackErrors[slackErr.Err]
	}
	// Fallback: string matching for wrapped or non-typed errors.
	// These Slack error codes are specific enough that false positives are unlikely.
	msg := err.Error()
	for code := range nonFatalSlackErrors {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// channelName returns a human-readable channel identifier for logging.
func (o *Orchestrator) channelName(id string) string {
	if name, ok := o.channelNames[id]; ok && name != "" {
		return fmt.Sprintf("#%s (%s)", name, id)
	}
	return id
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
