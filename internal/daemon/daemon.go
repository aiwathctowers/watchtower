package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"watchtower/internal/config"
	"watchtower/internal/digest"
	"watchtower/internal/sync"
)

// minPollInterval is the minimum allowed poll interval. Values below this
// (e.g. nanosecond-scale durations from misconfigured integer values) are
// replaced with DefaultPollInterval. Tests may lower this for fast execution.
var minPollInterval = 1 * time.Second

// Daemon runs periodic incremental syncs on a timer and after wake-from-sleep events.
type Daemon struct {
	orchestrator *sync.Orchestrator
	config       *config.Config
	logger       *log.Logger
	wakeCh       <-chan struct{}
	pidPath      string
	digestPipe   *digest.Pipeline
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

// SetDigestPipeline sets the digest pipeline for post-sync digest generation.
func (d *Daemon) SetDigestPipeline(p *digest.Pipeline) {
	d.digestPipe = p
}

// SetPIDPath sets the path where the daemon will write its PID file.
func (d *Daemon) SetPIDPath(path string) {
	d.pidPath = path
}

// Run starts the daemon poll loop. It blocks until ctx is cancelled or
// SIGINT/SIGTERM is received. Each tick or wake event triggers an incremental sync.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if d.pidPath != "" {
		if err := WritePID(d.pidPath); err != nil {
			return fmt.Errorf("writing pid file: %w", err)
		}
		defer RemovePID(d.pidPath)
	}

	pollInterval := d.config.Sync.PollInterval
	if pollInterval < minPollInterval {
		pollInterval = config.DefaultPollInterval
	}

	if d.config.Sync.SyncOnWake {
		d.wakeCh = WatchWake(ctx, pollInterval)
	}

	d.logger.Printf("daemon started, polling every %s", pollInterval)

	// Run an initial sync immediately on startup.
	d.runSync(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

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
			ticker.Reset(pollInterval)
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
	syncErr := d.orchestrator.Run(ctx, opts)
	if syncErr != nil {
		d.logger.Printf("sync error: %v", syncErr)
	}

	// Persist last sync result for `watchtower status`.
	snap := d.orchestrator.Progress().Snapshot()
	resultPath := filepath.Join(d.config.WorkspaceDir(), "last_sync.json")
	if err := sync.WriteSyncResult(resultPath, sync.ResultFromSnapshot(snap, syncErr)); err != nil {
		d.logger.Printf("failed to write sync result: %v", err)
	}

	// Post-sync: generate digests if enabled and sync succeeded.
	if syncErr == nil && d.digestPipe != nil {
		n, err := d.digestPipe.Run(ctx)
		if err != nil {
			d.logger.Printf("digest error: %v", err)
		} else if n > 0 {
			d.logger.Printf("generated %d digest(s)", n)
		}
	}
}
