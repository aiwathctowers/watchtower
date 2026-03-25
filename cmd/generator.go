package cmd

import (
	"log"
	"path/filepath"

	"watchtower/internal/config"
	"watchtower/internal/digest"
	"watchtower/internal/sessions"
)

// cliGenerator creates a bare ClaudeGenerator for one-off CLI commands.
func cliGenerator(cfg *config.Config) digest.Generator {
	return digest.NewClaudeGenerator(cfg.Digest.Model, cfg.ClaudePath)
}

// cliPooledGenerator creates a PooledGenerator backed by a concurrency pool.
// Each call creates a fresh session (--no-session-persistence).
// The pool only limits how many claude processes run in parallel.
func cliPooledGenerator(cfg *config.Config, logger *log.Logger) (digest.Generator, func()) {
	rawGen := digest.NewClaudeGenerator(cfg.Digest.Model, cfg.ClaudePath)
	poolSize := cfg.AI.Workers
	if poolSize <= 0 {
		poolSize = config.DefaultAIWorkers
	}
	pool := sessions.NewSessionPool(poolSize)
	gen := digest.NewPooledGenerator(rawGen, pool)

	sessionLogPath := filepath.Join(cfg.WorkspaceDir(), "sessions.log")
	gen.SetSessionLog(sessions.NewSessionLog(sessionLogPath))

	cleanup := func() { pool.Close() }
	return gen, cleanup
}
