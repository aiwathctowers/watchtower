package sync

import (
	"context"
	"encoding/json"
	"fmt"

	"watchtower/internal/db"
)

// usersBulkThreshold: if more unknown users than this, use users.list instead of individual fetches.
const usersBulkThreshold = 50

// syncUserProfiles fetches full profiles for users that appear in messages
// but don't yet have complete records in the users table.
func (o *Orchestrator) syncUserProfiles(ctx context.Context) error {
	unknownIDs, err := o.db.GetUnknownUserIDs()
	if err != nil {
		return fmt.Errorf("getting unknown user IDs: %w", err)
	}

	if len(unknownIDs) == 0 {
		o.logger.Println("user profiles: all users known")
		return nil
	}

	o.logger.Printf("user profiles: %d unknown users to fetch", len(unknownIDs))
	o.progress.SetUserProfiles(len(unknownIDs), 0)

	if len(unknownIDs) > usersBulkThreshold {
		return o.fetchAllUserProfiles(ctx)
	}

	return o.fetchUserProfilesIndividually(ctx, unknownIDs)
}

// fetchUserProfilesIndividually fetches each user via users.info (Tier 4).
func (o *Orchestrator) fetchUserProfilesIndividually(ctx context.Context, userIDs []string) error {
	for i, userID := range userIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		user, err := o.slackClient.GetUserInfo(ctx, userID)
		if err != nil {
			if isNonFatalError(err) {
				o.logger.Printf("user profiles: skipping %s: %v", userID, err)
				o.progress.SetUserProfiles(len(userIDs), i+1)
				continue
			}
			return fmt.Errorf("fetching user %s: %w", userID, err)
		}

		profileJSON, err := json.Marshal(user.Profile)
		if err != nil {
			profileJSON = []byte("{}")
		}

		if err := o.db.UpsertUser(db.User{
			ID:          user.ID,
			Name:        user.Name,
			DisplayName: user.Profile.DisplayName,
			RealName:    user.RealName,
			Email:       user.Profile.Email,
			IsBot:       user.IsBot,
			IsDeleted:   user.Deleted,
			ProfileJSON: string(profileJSON),
		}); err != nil {
			return fmt.Errorf("upserting user %s: %w", userID, err)
		}

		o.progress.SetUserProfiles(len(userIDs), i+1)
		o.logger.Printf("user profiles: %d/%d @%s", i+1, len(userIDs), user.Name)
	}

	return nil
}

// fetchAllUserProfiles falls back to users.list when too many unknown users.
func (o *Orchestrator) fetchAllUserProfiles(ctx context.Context) error {
	o.logger.Println("user profiles: too many unknown users, falling back to users.list")

	users, err := o.slackClient.GetUsers(ctx, func(fetched int) {
		o.progress.SetUserProfiles(fetched, 0)
	})
	if err != nil {
		return fmt.Errorf("fetching all users: %w", err)
	}

	o.progress.SetUserProfiles(len(users), 0)
	for i, u := range users {
		if u.Deleted {
			continue
		}

		profileJSON, err := json.Marshal(u.Profile)
		if err != nil {
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

		o.progress.SetUserProfiles(len(users), i+1)
	}

	return nil
}
