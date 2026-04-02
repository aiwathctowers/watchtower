// Package prompts manages AI prompt templates with versioning, DB persistence,
// and fallback to built-in defaults. It supports language-aware loading so
// prompts can be tuned per-language.
package prompts

import (
	"fmt"
	"log/slog"

	"watchtower/internal/db"
)

// Prompt IDs — the three main prompts targeted for feedback & tuning.
const (
	DigestChannel      = "digest.channel"
	DigestDaily        = "digest.daily"
	DigestWeekly       = "digest.weekly"
	DigestPeriod       = "digest.period"
	TracksExtract      = "tracks.extract"
	TracksUpdate       = "tracks.update"
	AnalysisUser       = "analysis.user"
	AnalysisPeriod     = "analysis.period"
	GuideUser          = "guide.user"
	GuidePeriod        = "guide.period"
	PeopleReduce       = "people.reduce"
	PeopleTeam         = "people.team"
	BriefingDaily      = "briefing.daily"
	InboxPrioritize    = "inbox.prioritize"
	DigestChannelBatch = "digest.channel_batch"
	TracksExtractBatch = "tracks.extract_batch"
	PeopleBatch        = "people.batch"
	TasksGenerate      = "tasks.generate"
	MeetingPrep        = "meeting.prep"
)

// Store loads, caches, and persists prompt templates.
type Store struct {
	db     *db.DB
	logger *slog.Logger
	seeded bool
}

// New creates a new prompt store backed by the given database.
func New(database *db.DB, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{db: database, logger: logger}
}

// Seed inserts all built-in default prompts into the database if they don't
// already exist. This is safe to call multiple times.
func (s *Store) Seed() error {
	if s.seeded {
		return nil
	}
	for id, tmpl := range Defaults {
		existing, err := s.db.GetPrompt(id)
		if err != nil {
			return fmt.Errorf("checking prompt %q: %w", id, err)
		}
		defaultVer := DefaultVersions[id]
		if defaultVer == 0 {
			defaultVer = 1
		}
		if existing == nil {
			// New prompt — seed it.
			if err := s.db.UpsertPrompt(db.Prompt{
				ID:       id,
				Template: tmpl,
				Version:  defaultVer,
			}); err != nil {
				return fmt.Errorf("seeding prompt %q: %w", id, err)
			}
			s.logger.Debug("seeded default prompt", "id", id)
			continue
		}
		// Auto-upgrade: if the default version is higher and the user hasn't
		// customized the template (i.e., it still matches a previous default),
		// update it to the new default.
		if existing.Version < defaultVer {
			if err := s.db.UpsertPrompt(db.Prompt{
				ID:       id,
				Template: tmpl,
				Version:  defaultVer,
			}); err != nil {
				return fmt.Errorf("upgrading prompt %q to v%d: %w", id, defaultVer, err)
			}
			s.logger.Debug("upgraded prompt to new default", "id", id, "from", existing.Version, "to", defaultVer)
		}
	}
	s.seeded = true
	return nil
}

// Get returns the current template for a prompt ID.
// It checks the database first, then falls back to the built-in default.
func (s *Store) Get(id string) (string, int, error) {
	p, err := s.db.GetPrompt(id)
	if err != nil {
		return "", 0, fmt.Errorf("loading prompt %q: %w", id, err)
	}
	if p != nil {
		return p.Template, p.Version, nil
	}
	// Fallback to built-in default
	if tmpl, ok := Defaults[id]; ok {
		return tmpl, 0, nil
	}
	return "", 0, fmt.Errorf("unknown prompt %q", id)
}

// GetForRole returns a prompt template customized for a user's role.
// It tries role-specific variants first (e.g., "tracks.extract_direction_owner"),
// then falls back to the standard prompt.
func (s *Store) GetForRole(id string, role string) (string, int, error) {
	if role != "" {
		// Try role-specific variant first
		roleVariantID := id + "_" + role
		p, err := s.db.GetPrompt(roleVariantID)
		if err != nil {
			s.logger.Debug("error loading role variant", "id", roleVariantID, "err", err)
		} else if p != nil {
			s.logger.Debug("loaded role-specific prompt", "id", roleVariantID)
			return p.Template, p.Version, nil
		}
		// Check built-in defaults for role variant
		if tmpl, ok := Defaults[roleVariantID]; ok {
			s.logger.Debug("loaded built-in role variant", "id", roleVariantID)
			return tmpl, 0, nil
		}
	}
	// Fallback to standard prompt
	return s.Get(id)
}

// GetAll returns all prompts (from DB, with defaults for any missing).
func (s *Store) GetAll() ([]db.Prompt, error) {
	dbPrompts, err := s.db.GetAllPrompts()
	if err != nil {
		return nil, err
	}

	// Build a map of what's in DB
	inDB := make(map[string]bool, len(dbPrompts))
	for _, p := range dbPrompts {
		inDB[p.ID] = true
	}

	// Add defaults that aren't in DB yet
	for id, tmpl := range Defaults {
		if !inDB[id] {
			dbPrompts = append(dbPrompts, db.Prompt{
				ID:       id,
				Template: tmpl,
				Version:  0,
			})
		}
	}
	return dbPrompts, nil
}

// Update modifies a prompt's template and records the change reason.
func (s *Store) Update(id, template, reason string) error {
	return s.db.UpdatePrompt(id, template, reason)
}

// Rollback reverts a prompt to a specific version.
func (s *Store) Rollback(id string, version int) error {
	return s.db.RollbackPrompt(id, version)
}

// Reset restores a prompt to its built-in default.
func (s *Store) Reset(id string) error {
	tmpl, ok := Defaults[id]
	if !ok {
		return fmt.Errorf("unknown prompt %q — no default available", id)
	}
	return s.db.UpdatePrompt(id, tmpl, "reset to default")
}

// History returns the version history for a prompt.
func (s *Store) History(id string) ([]db.PromptHistory, error) {
	return s.db.GetPromptHistory(id)
}

// DB returns the underlying database handle (H3 fix: avoids double-open in cmd/prompts.go).
func (s *Store) DB() *db.DB {
	return s.db
}
