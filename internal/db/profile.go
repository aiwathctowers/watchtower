package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// GetUserProfile returns the profile for the given Slack user ID, or nil if not found.
func (db *DB) GetUserProfile(slackUserID string) (*UserProfile, error) {
	row := db.QueryRow(`SELECT id, slack_user_id, role, team, responsibilities,
		reports, peers, manager, starred_channels, starred_people,
		pain_points, track_focus, onboarding_done, custom_prompt_context,
		created_at, updated_at
		FROM user_profile WHERE slack_user_id = ?`, slackUserID)

	var p UserProfile
	err := row.Scan(&p.ID, &p.SlackUserID, &p.Role, &p.Team, &p.Responsibilities,
		&p.Reports, &p.Peers, &p.Manager, &p.StarredChannels, &p.StarredPeople,
		&p.PainPoints, &p.TrackFocus, &p.OnboardingDone, &p.CustomPromptContext,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying user profile: %w", err)
	}
	return &p, nil
}

// UpsertUserProfile creates or updates a user profile.
func (db *DB) UpsertUserProfile(p UserProfile) error {
	_, err := db.Exec(`INSERT INTO user_profile
		(slack_user_id, role, team, responsibilities, reports, peers, manager,
		 starred_channels, starred_people, pain_points, track_focus,
		 onboarding_done, custom_prompt_context, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		ON CONFLICT(slack_user_id) DO UPDATE SET
			role = excluded.role,
			team = excluded.team,
			responsibilities = excluded.responsibilities,
			reports = excluded.reports,
			peers = excluded.peers,
			manager = excluded.manager,
			starred_channels = excluded.starred_channels,
			starred_people = excluded.starred_people,
			pain_points = excluded.pain_points,
			track_focus = excluded.track_focus,
			onboarding_done = excluded.onboarding_done,
			custom_prompt_context = excluded.custom_prompt_context,
			updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		p.SlackUserID, p.Role, p.Team, p.Responsibilities, p.Reports, p.Peers, p.Manager,
		p.StarredChannels, p.StarredPeople, p.PainPoints, p.TrackFocus,
		p.OnboardingDone, p.CustomPromptContext)
	if err != nil {
		return fmt.Errorf("upserting user profile: %w", err)
	}
	return nil
}

// AddStarredChannel adds a channel to the user's starred channels list.
func (db *DB) AddStarredChannel(slackUserID, channelID string) error {
	profile, err := db.GetUserProfile(slackUserID)
	if err != nil {
		return fmt.Errorf("getting user profile: %w", err)
	}
	if profile == nil {
		return errors.New("user profile not found")
	}

	var channels []string
	if profile.StarredChannels != "" {
		if err := json.Unmarshal([]byte(profile.StarredChannels), &channels); err != nil {
			return fmt.Errorf("unmarshaling starred channels: %w", err)
		}
	}

	// Check if already starred
	for _, ch := range channels {
		if ch == channelID {
			return nil // Already starred, idempotent
		}
	}

	channels = append(channels, channelID)
	data, err := json.Marshal(channels)
	if err != nil {
		return fmt.Errorf("marshaling starred channels: %w", err)
	}

	profile.StarredChannels = string(data)
	return db.UpsertUserProfile(*profile)
}

// RemoveStarredChannel removes a channel from the user's starred channels list.
func (db *DB) RemoveStarredChannel(slackUserID, channelID string) error {
	profile, err := db.GetUserProfile(slackUserID)
	if err != nil {
		return fmt.Errorf("getting user profile: %w", err)
	}
	if profile == nil {
		return errors.New("user profile not found")
	}

	var channels []string
	if profile.StarredChannels != "" {
		if err := json.Unmarshal([]byte(profile.StarredChannels), &channels); err != nil {
			return fmt.Errorf("unmarshaling starred channels: %w", err)
		}
	}

	// Remove the channel
	newChannels := []string{}
	for _, ch := range channels {
		if ch != channelID {
			newChannels = append(newChannels, ch)
		}
	}

	data, err := json.Marshal(newChannels)
	if err != nil {
		return fmt.Errorf("marshaling starred channels: %w", err)
	}

	profile.StarredChannels = string(data)
	return db.UpsertUserProfile(*profile)
}

// AddStarredPerson adds a person to the user's starred people list.
func (db *DB) AddStarredPerson(slackUserID, personUserID string) error {
	profile, err := db.GetUserProfile(slackUserID)
	if err != nil {
		return fmt.Errorf("getting user profile: %w", err)
	}
	if profile == nil {
		return errors.New("user profile not found")
	}

	var people []string
	if profile.StarredPeople != "" {
		if err := json.Unmarshal([]byte(profile.StarredPeople), &people); err != nil {
			return fmt.Errorf("unmarshaling starred people: %w", err)
		}
	}

	// Check if already starred
	for _, p := range people {
		if p == personUserID {
			return nil // Already starred, idempotent
		}
	}

	people = append(people, personUserID)
	data, err := json.Marshal(people)
	if err != nil {
		return fmt.Errorf("marshaling starred people: %w", err)
	}

	profile.StarredPeople = string(data)
	return db.UpsertUserProfile(*profile)
}

// RemoveStarredPerson removes a person from the user's starred people list.
func (db *DB) RemoveStarredPerson(slackUserID, personUserID string) error {
	profile, err := db.GetUserProfile(slackUserID)
	if err != nil {
		return fmt.Errorf("getting user profile: %w", err)
	}
	if profile == nil {
		return errors.New("user profile not found")
	}

	var people []string
	if profile.StarredPeople != "" {
		if err := json.Unmarshal([]byte(profile.StarredPeople), &people); err != nil {
			return fmt.Errorf("unmarshaling starred people: %w", err)
		}
	}

	// Remove the person
	newPeople := []string{}
	for _, p := range people {
		if p != personUserID {
			newPeople = append(newPeople, p)
		}
	}

	data, err := json.Marshal(newPeople)
	if err != nil {
		return fmt.Errorf("marshaling starred people: %w", err)
	}

	profile.StarredPeople = string(data)
	return db.UpsertUserProfile(*profile)
}
