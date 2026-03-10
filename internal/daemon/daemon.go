package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	gosync "sync"
	"time"

	"watchtower/internal/actionitems"
	"watchtower/internal/analysis"
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
	orchestrator    *sync.Orchestrator
	config          *config.Config
	logger          *log.Logger
	wakeCh          <-chan struct{}
	pidPath         string
	digestPipe      *digest.Pipeline
	analysisPipe    *analysis.Pipeline
	actionItemsPipe *actionitems.Pipeline
	lastAnalysis    time.Time // tracks when analysis last ran (once per day)
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

// SetAnalysisPipeline sets the people analysis pipeline for post-digest analysis.
func (d *Daemon) SetAnalysisPipeline(p *analysis.Pipeline) {
	d.analysisPipe = p
}

// SetActionItemsPipeline sets the action items pipeline for post-digest extraction.
func (d *Daemon) SetActionItemsPipeline(p *actionitems.Pipeline) {
	d.actionItemsPipe = p
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

	// Restore last analysis time from disk so the 24h guard survives restarts.
	d.loadLastAnalysis()

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
	// Pre-sync: reactivate snoozed action items whose snooze_until has passed.
	if d.actionItemsPipe != nil {
		if n, err := d.actionItemsPipe.ReactivateSnoozed(ctx); err != nil {
			d.logger.Printf("snooze reactivation error: %v", err)
		} else if n > 0 {
			d.logger.Printf("reactivated %d snoozed action item(s)", n)
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

	// Phase 1: Digests + People in parallel (independent pipelines).
	// People analysis runs once per day; digests run every sync.
	var wg gosync.WaitGroup

	if d.digestPipe != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, usage, err := d.digestPipe.Run(ctx)
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
		}()
	}

	if d.analysisPipe != nil {
		now := time.Now()
		if d.lastAnalysis.IsZero() || now.Sub(d.lastAnalysis) >= 24*time.Hour {
			wg.Add(1)
			go func() {
				defer wg.Done()
				n, err := d.analysisPipe.Run(ctx)
				if err != nil {
					d.logger.Printf("people analysis error: %v", err)
				} else {
					if n > 0 {
						d.logger.Printf("analyzed %d user(s)", n)
					}
					d.lastAnalysis = now
					d.saveLastAnalysis()
				}
			}()
		}
	}

	wg.Wait()

	// Phase 2: Action items (depend on digests for related_digest_ids).
	if d.actionItemsPipe != nil {
		n, err := d.actionItemsPipe.Run(ctx)
		if err != nil {
			d.logger.Printf("action-items error: %v", err)
		} else if n > 0 {
			d.logger.Printf("extracted %d action item(s)", n)
		}
	}

	// After action items extraction, check for updates on existing items.
	if d.actionItemsPipe != nil {
		n, err := d.actionItemsPipe.CheckForUpdates(ctx)
		if err != nil {
			d.logger.Printf("action-items update check error: %v", err)
		} else if n > 0 {
			d.logger.Printf("detected updates on %d action item(s)", n)
		}
	}
}

// lastAnalysisPath returns the file path for persisting the last analysis time.
func (d *Daemon) lastAnalysisPath() string {
	return filepath.Join(d.config.WorkspaceDir(), "last_analysis.txt")
}

// loadLastAnalysis restores lastAnalysis from disk so the 24h guard survives daemon restarts.
func (d *Daemon) loadLastAnalysis() {
	data, err := os.ReadFile(d.lastAnalysisPath())
	if err != nil {
		return
	}
	unix, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return
	}
	d.lastAnalysis = time.Unix(unix, 0)
	d.logger.Printf("restored last analysis time: %s", d.lastAnalysis.Format(time.RFC3339))
}

// saveLastAnalysis persists lastAnalysis to disk.
func (d *Daemon) saveLastAnalysis() {
	data := strconv.FormatInt(d.lastAnalysis.Unix(), 10)
	if err := os.WriteFile(d.lastAnalysisPath(), []byte(data), 0o600); err != nil {
		d.logger.Printf("failed to save last analysis time: %v", err)
	}
}
