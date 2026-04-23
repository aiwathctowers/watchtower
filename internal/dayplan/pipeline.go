package dayplan

import (
	"context"
	"log"

	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/prompts"
)

// Pipeline orchestrates day-plan generation and persistence.
type Pipeline struct {
	db          *db.DB
	cfg         *config.Config
	generator   digest.Generator
	logger      *log.Logger
	promptStore *prompts.Store
}

// New constructs a Pipeline.
func New(database *db.DB, cfg *config.Config, gen digest.Generator, logger *log.Logger) *Pipeline {
	return &Pipeline{db: database, cfg: cfg, generator: gen, logger: logger}
}

// SetPromptStore wires an optional customisable prompt store.
func (p *Pipeline) SetPromptStore(store *prompts.Store) { p.promptStore = store }

// Run generates or regenerates the day plan for the target date.
// Full implementation arrives in Task 8.
func (p *Pipeline) Run(ctx context.Context, opts RunOptions) (*db.DayPlan, error) {
	// Skeleton — filled in Task 8.
	return nil, nil
}
