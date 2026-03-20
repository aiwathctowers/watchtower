// Package daemon provides background daemon and service management capabilities.
package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"watchtower/internal/chains"
	"watchtower/internal/config"
	"watchtower/internal/digest"
	"watchtower/internal/guide"
	"watchtower/internal/sync"
	"watchtower/internal/tracks"
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
	chainsPipe   *chains.Pipeline
	tracksPipe   *tracks.Pipeline
	peoplePipe   *guide.Pipeline
	lastPeople   time.Time // when people cards last ran (once per day)
	lastTracks   time.Time // when tracks last ran (throttled)
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

// SetChainsPipeline sets the chains pipeline for post-digest chain linking.
func (d *Daemon) SetChainsPipeline(p *chains.Pipeline) {
	d.chainsPipe = p
}

// SetTracksPipeline sets the tracks pipeline for post-digest extraction.
func (d *Daemon) SetTracksPipeline(p *tracks.Pipeline) {
	d.tracksPipe = p
}

// SetPeoplePipeline sets the people card pipeline (REDUCE phase).
func (d *Daemon) SetPeoplePipeline(p *guide.Pipeline) {
	d.peoplePipe = p
}


// SetPIDPath sets the path where the daemon will write its PID file.
func (d *Daemon) SetPIDPath(path string) {
	d.pidPath = path
}


// Run starts the daemon poll loop. It blocks until ctx is cancelled.
// The caller is responsible for wiring signal handling into the context.
// Each tick or wake event triggers an incremental sync.
func (d *Daemon) Run(ctx context.Context) error {
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

	// Restore last pipeline times from disk so throttle guards survive restarts.
	d.loadLastPeople()
	d.loadLastTracks()

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
	// Pre-sync: reactivate snoozed tracks whose snooze_until has passed.
	if d.tracksPipe != nil {
		if n, err := d.tracksPipe.ReactivateSnoozed(ctx); err != nil {
			d.logger.Printf("snooze reactivation error: %v", err)
		} else if n > 0 {
			d.logger.Printf("reactivated %d snoozed track(s)", n)
		}
	}

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

	// Run pipelines even if sync had a non-fatal error (e.g. rate-limited,
	// partial fetch). The DB still has messages that need processing.
	// Only skip pipelines if the context itself was cancelled (shutdown).
	if ctx.Err() != nil {
		d.logger.Printf("context cancelled, skipping pipelines")
		return
	}

	if syncErr != nil {
		d.logger.Printf("sync had errors, but running pipelines on existing data")
	}

	// Phase 1: Channel digests (generates people_signals in MAP phase).
	if d.digestPipe != nil {
		n, usage, err := d.digestPipe.RunChannelDigestsOnly(ctx)
		if err != nil {
			d.logger.Printf("digest error: %v", err)
		} else if n > 0 {
			if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
				d.logger.Printf("generated %d digest(s) (%d+%d tokens, $%.4f)",
					n, usage.InputTokens, usage.OutputTokens, usage.CostUSD)
			} else {
				d.logger.Printf("generated %d digest(s)", n)
			}
		}
	}

	// Phase 2: Chains (depends on channel digests being generated).
	// Links decisions from channel digests into thematic chains.
	if d.chainsPipe != nil {
		n, err := d.chainsPipe.Run(ctx)
		if err != nil {
			d.logger.Printf("chains error: %v", err)
		} else if n > 0 {
			d.logger.Printf("linked %d decision(s) to chains", n)
		}

		// Inject chain context into digest and tracks pipelines for chain-aware rollups.
		if chainCtx, err := d.chainsPipe.FormatActiveChainsForPrompt(ctx); err == nil && chainCtx != "" {
			if d.digestPipe != nil {
				d.digestPipe.ChainContext = chainCtx
			}
			if d.tracksPipe != nil {
				d.tracksPipe.ChainContext = chainCtx
			}
		}
	}

	// Phase 3: Daily/weekly rollups (chain-aware — chained decisions collapsed).
	if d.digestPipe != nil {
		if err := d.digestPipe.RunRollups(ctx); err != nil {
			d.logger.Printf("rollup error: %v", err)
		}
	}

	// Phase 4: People REDUCE (reads signals from Phase 1, generates people_cards).
	// Must run AFTER channel digests because it reads people_signals from them.
	if d.peoplePipe != nil {
		now := time.Now()
		if d.lastPeople.IsZero() || now.Sub(d.lastPeople) >= 24*time.Hour {
			n, err := d.peoplePipe.Run(ctx)
			if err != nil {
				d.logger.Printf("people cards error: %v", err)
			} else {
				if n > 0 {
					d.logger.Printf("generated %d people card(s)", n)
				}
				d.lastPeople = now
				d.saveLastPeople()
			}
		}
	}

	// Phase 5: Tracks (depend on digests + chains for context).
	// Throttled to run at most once per tracks interval (default 1h).
	if d.tracksPipe != nil {
		interval := d.config.Digest.TracksInterval
		if interval <= 0 {
			interval = config.DefaultTracksInterval
		}
		now := time.Now()
		if d.lastTracks.IsZero() || now.Sub(d.lastTracks) >= interval {
			n, err := d.tracksPipe.Run(ctx)
			if err != nil {
				d.logger.Printf("tracks error: %v", err)
			} else {
				if n > 0 {
					d.logger.Printf("extracted %d track(s)", n)
				}
				d.lastTracks = now
				d.saveLastTracks()
			}
		}
	}

	// Check for updates on existing items (lightweight, runs every sync).
	if d.tracksPipe != nil {
		n, err := d.tracksPipe.CheckForUpdates(ctx)
		if err != nil {
			d.logger.Printf("tracks update check error: %v", err)
		} else if n > 0 {
			d.logger.Printf("detected updates on %d track(s)", n)
		}
	}

}

func (d *Daemon) lastPeoplePath() string {
	return filepath.Join(d.config.WorkspaceDir(), "last_people.txt")
}

func (d *Daemon) loadLastPeople() {
	data, err := os.ReadFile(d.lastPeoplePath())
	if err != nil {
		return
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return
	}
	d.lastPeople = time.Unix(unix, 0)
	d.logger.Printf("restored last people time: %s", d.lastPeople.Format(time.RFC3339))
}

func (d *Daemon) saveLastPeople() {
	data := strconv.FormatInt(d.lastPeople.Unix(), 10)
	if err := os.WriteFile(d.lastPeoplePath(), []byte(data), 0o600); err != nil {
		d.logger.Printf("failed to save last people time: %v", err)
	}
}

// lastTracksPath returns the file path for persisting the last tracks time.
// Keeps the old filename "last_action_items.txt" for backward compatibility
// with existing daemon installations.
func (d *Daemon) lastTracksPath() string {
	return filepath.Join(d.config.WorkspaceDir(), "last_action_items.txt")
}

// loadLastTracks restores lastTracks from disk so the throttle survives restarts.
func (d *Daemon) loadLastTracks() {
	data, err := os.ReadFile(d.lastTracksPath())
	if err != nil {
		return
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return
	}
	d.lastTracks = time.Unix(unix, 0)
	d.logger.Printf("restored last tracks time: %s", d.lastTracks.Format(time.RFC3339))
}

// saveLastTracks persists lastTracks to disk.
func (d *Daemon) saveLastTracks() {
	data := strconv.FormatInt(d.lastTracks.Unix(), 10)
	if err := os.WriteFile(d.lastTracksPath(), []byte(data), 0o600); err != nil {
		d.logger.Printf("failed to save last tracks time: %v", err)
	}
}

