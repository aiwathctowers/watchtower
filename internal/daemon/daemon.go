package daemon

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/sync"
)

// Daemon runs periodic incremental syncs on a timer and after wake-from-sleep events.
type Daemon struct {
	orchestrator *sync.Orchestrator
	config       *config.Config
	logger       *log.Logger
	wakeCh       <-chan struct{}
}

// New creates a Daemon that runs incremental syncs via the given orchestrator.
func New(orchestrator *sync.Orchestrator, cfg *config.Config) *Daemon {
	return &Daemon{
		orchestrator: orchestrator,
		config:       cfg,
		logger:       log.New(os.Stderr, "[daemon] ", log.LstdFlags),
	}
}

// SetLogger replaces the daemon's logger.
func (d *Daemon) SetLogger(l *log.Logger) {
	d.logger = l
}

// Run starts the daemon poll loop. It blocks until ctx is cancelled or
// SIGINT/SIGTERM is received. Each tick or wake event triggers an incremental sync.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if d.config.Sync.SyncOnWake {
		d.wakeCh = WatchWake(ctx, d.config.Sync.PollInterval)
	}

	// Run an initial sync immediately on startup.
	d.runSync(ctx)

	ticker := time.NewTicker(d.config.Sync.PollInterval)
	defer ticker.Stop()

	d.logger.Printf("daemon started, polling every %s", d.config.Sync.PollInterval)

	for {
		select {
		case <-ctx.Done():
			d.logger.Println("shutting down")
			return nil
		case <-ticker.C:
			d.runSync(ctx)
		case <-d.wakeChannel():
			d.logger.Println("wake event detected, syncing")
			d.runSync(ctx)
			// Reset the ticker so the next poll is a full interval from now.
			ticker.Reset(d.config.Sync.PollInterval)
		}
	}
}

// wakeChannel returns the wake channel or a nil channel (blocks forever) when
// wake detection is disabled.
func (d *Daemon) wakeChannel() <-chan struct{} {
	if d.wakeCh != nil {
		return d.wakeCh
	}
	return nil
}

func (d *Daemon) runSync(ctx context.Context) {
	opts := sync.SyncOptions{}
	if err := d.orchestrator.Run(ctx, opts); err != nil {
		d.logger.Printf("sync error: %v", err)
	}
}
