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

	gosync "sync"
	"watchtower/internal/briefing"
	"watchtower/internal/calendar"
	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/digest"
	"watchtower/internal/guide"
	"watchtower/internal/inbox"
	"watchtower/internal/jira"

	"watchtower/internal/sync"
	"watchtower/internal/tracks"
)

// minPollInterval is the minimum allowed poll interval. Values below this
// (e.g. nanosecond-scale durations from misconfigured integer values) are
// replaced with DefaultPollInterval. Tests may lower this for fast execution.
var minPollInterval = 1 * time.Second

// Daemon runs periodic incremental syncs on a timer and after wake-from-sleep events.
type Daemon struct {
	orchestrator   *sync.Orchestrator
	config         *config.Config
	logger         *log.Logger
	wakeCh         <-chan struct{}
	pidPath        string
	db             *db.DB
	digestPipe     *digest.Pipeline
	tracksPipe     *tracks.Pipeline
	peoplePipe     *guide.Pipeline
	briefingPipe   *briefing.Pipeline
	inboxPipe      *inbox.Pipeline
	calendarSyncer *calendar.Syncer
	jiraSyncer     *jira.Syncer
	lastJira       time.Time
	lastPeople     time.Time // when people cards last ran (once per day)
	lastBriefing   time.Time // when briefing last ran (once per day)
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

// SetDB sets the database for post-pipeline operations like auto-marking read status.
func (d *Daemon) SetDB(database *db.DB) {
	d.db = database
}

// SetDigestPipeline sets the digest pipeline for post-sync digest generation.
func (d *Daemon) SetDigestPipeline(p *digest.Pipeline) {
	d.digestPipe = p
}

// SetTracksPipeline sets the tracks pipeline for post-digest extraction.
func (d *Daemon) SetTracksPipeline(p *tracks.Pipeline) {
	d.tracksPipe = p
}

// SetBriefingPipeline sets the daily briefing pipeline.
func (d *Daemon) SetBriefingPipeline(p *briefing.Pipeline) {
	d.briefingPipe = p
}

// SetInboxPipeline sets the inbox detection pipeline.
func (d *Daemon) SetInboxPipeline(p *inbox.Pipeline) {
	d.inboxPipe = p
}

// SetCalendarSyncer sets the calendar syncer for post-sync calendar fetch.
func (d *Daemon) SetCalendarSyncer(s *calendar.Syncer) {
	d.calendarSyncer = s
}

// SetJiraSyncer sets the Jira syncer for periodic sync.
func (d *Daemon) SetJiraSyncer(s *jira.Syncer) {
	d.jiraSyncer = s
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
	d.loadLastBriefing()

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

	// Calendar sync — lightweight, runs after Slack sync, before pipelines.
	if d.calendarSyncer != nil {
		n, err := d.calendarSyncer.Sync(ctx)
		if err != nil {
			d.logger.Printf("calendar sync error: %v", err)
		} else if n > 0 {
			d.logger.Printf("calendar: %d events synced", n)
		}
	}

	// Jira sync — runs after calendar sync, before pipelines.
	if d.jiraSyncer != nil {
		interval := time.Duration(d.config.Jira.SyncIntervalMins) * time.Minute
		if interval <= 0 {
			interval = time.Duration(config.DefaultJiraSyncIntervalMins) * time.Minute
		}
		if d.lastJira.IsZero() || time.Since(d.lastJira) >= interval {
			n, err := d.jiraSyncer.Sync(ctx)
			if err != nil {
				d.logger.Printf("jira sync error: %v", err)
			} else if n > 0 {
				d.logger.Printf("jira: %d issues synced", n)
			}
			d.lastJira = time.Now()

			// Sync task statuses from Jira issues after successful sync.
			if err == nil && d.db != nil {
				if synced, serr := d.db.SyncJiraTaskStatuses(); serr != nil {
					d.logger.Printf("jira task status sync warning: %v", serr)
				} else if synced > 0 {
					d.logger.Printf("jira-tasks: synced %d task status(es)", synced)
				}
			}
		}
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

	// Inbox runs fully independently — it does not block any other pipeline.
	go func() {
		if d.inboxPipe == nil {
			return
		}
		var inboxRunID int64
		if d.db != nil {
			inboxRunID, _ = d.db.CreatePipelineRun("inbox", "daemon", "auto")
		}
		created, resolved, err := d.inboxPipe.Run(ctx)
		if err != nil {
			d.logger.Printf("inbox error: %v", err)
		} else if created > 0 || resolved > 0 {
			d.logger.Printf("inbox: %d new, %d resolved", created, resolved)
		}
		if inboxRunID > 0 {
			var errMsg string
			if err != nil {
				errMsg = err.Error()
			}
			inTok, outTok, cost, totalAPI := d.inboxPipe.AccumulatedUsage()
			if inTok > 0 || outTok > 0 {
				d.logger.Printf("inbox: %d+%d tokens", inTok, outTok)
			}
			_ = d.db.CompletePipelineRun(inboxRunID, created+resolved, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
		}
	}()

	// Phase 1: Channel digests (generates people_signals in MAP phase).
	if d.digestPipe != nil {
		var runID int64
		if d.db != nil {
			runID, _ = d.db.CreatePipelineRun("digests", "daemon", "auto")
		}
		n, usage, err := d.digestPipe.RunChannelDigestsOnly(ctx)
		if err != nil {
			d.logger.Printf("digest error: %v", err)
		} else if n > 0 {
			if usage != nil && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
				d.logger.Printf("generated %d digest(s) (%d+%d tokens)",
					n, usage.InputTokens, usage.OutputTokens)
			} else {
				d.logger.Printf("generated %d digest(s)", n)
			}
		}
		if runID > 0 {
			var errMsg string
			if err != nil {
				errMsg = err.Error()
			}
			inTok, outTok, totalAPI := 0, 0, 0
			if usage != nil {
				inTok, outTok, totalAPI = usage.InputTokens, usage.OutputTokens, usage.TotalAPITokens
			}
			_ = d.db.CompletePipelineRun(runID, n, inTok, outTok, 0, totalAPI, nil, nil, errMsg)
		}
	}

	// Note: auto-mark read runs once after all analysis phases complete (below).

	// Unsnooze tasks whose snooze_until date has passed.
	if d.db != nil {
		if n, err := d.db.UnsnoozeExpiredTasks(); err != nil {
			d.logger.Printf("unsnooze tasks error: %v", err)
		} else if n > 0 {
			d.logger.Printf("unsnoozed %d task(s)", n)
		}
		if n, err := d.db.UnsnoozeExpiredInboxItems(); err != nil {
			d.logger.Printf("unsnooze inbox error: %v", err)
		} else if n > 0 {
			d.logger.Printf("unsnoozed %d inbox item(s)", n)
		}
	}

	// Phases 2-4 run in parallel where possible:
	//   Group A: Tracks → inject track context → Rollups
	//   Group B: People Cards (only depends on Phase 1 channel digests)
	var phasesWg gosync.WaitGroup

	// Group A: Tracks → inject track context → Rollups → auto-mark rollups.
	phasesWg.Add(1)
	go func() {
		defer phasesWg.Done()

		// Phase 2: Tracks (auto-create/update from unlinked topics).
		if d.tracksPipe != nil {
			var trackRunID int64
			if d.db != nil {
				trackRunID, _ = d.db.CreatePipelineRun("tracks", "daemon", "auto")
			}
			n, updated, err := d.tracksPipe.Run(ctx)
			if err != nil {
				d.logger.Printf("tracks error: %v", err)
			} else if n > 0 || updated > 0 {
				d.logger.Printf("tracks: created %d, updated %d", n, updated)
			}
			if trackRunID > 0 {
				var errMsg string
				if err != nil {
					errMsg = err.Error()
				}
				inTok, outTok, cost, totalAPI := d.tracksPipe.AccumulatedUsage()
				var pFrom, pTo *float64
				if d.tracksPipe.LastFrom > 0 {
					pFrom = &d.tracksPipe.LastFrom
				}
				if d.tracksPipe.LastTo > 0 {
					pTo = &d.tracksPipe.LastTo
				}
				_ = d.db.CompletePipelineRun(trackRunID, n+updated, inTok, outTok, cost, totalAPI, pFrom, pTo, errMsg)
			}

			// Inject track context into digest pipeline for track-aware rollups.
			if trackCtx, err := d.tracksPipe.FormatActiveTracksForPrompt(); err == nil && trackCtx != "" {
				if d.digestPipe != nil {
					d.digestPipe.TrackContext = trackCtx
				}
			}
		}

		// Phase 3: Daily/weekly rollups (track-aware).
		if d.digestPipe != nil {
			if err := d.digestPipe.RunRollups(ctx); err != nil {
				d.logger.Printf("rollup error: %v", err)
			}
		}

		// Note: auto-mark read runs once after all analysis phases complete (below).
	}()

	// Group B: People REDUCE (reads signals from Phase 1, generates people_cards).
	phasesWg.Add(1)
	go func() {
		defer phasesWg.Done()

		if d.peoplePipe != nil {
			now := time.Now()
			if d.lastPeople.IsZero() || now.Sub(d.lastPeople) >= 24*time.Hour {
				var peopleRunID int64
				if d.db != nil {
					peopleRunID, _ = d.db.CreatePipelineRun("people", "daemon", "auto")
				}
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
				if peopleRunID > 0 {
					var errMsg string
					if err != nil {
						errMsg = err.Error()
					}
					inTok, outTok, cost, totalAPI := d.peoplePipe.AccumulatedUsage()
					_ = d.db.CompletePipelineRun(peopleRunID, n, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
				}
			}
		}
	}()

	phasesWg.Wait()

	// Auto-mark digests and tracks as read based on Slack read cursors.
	// Runs once after all analysis phases so channel digests, rollups, and tracks
	// are all available for marking.
	d.autoMarkRead()

	// Phase 6: Daily briefing (depends on digests + tracks + people cards).
	// Throttled to run at most once per day, triggered by schedule time.
	if d.briefingPipe != nil && d.shouldRunBriefing() {
		var briefingRunID int64
		if d.db != nil {
			briefingRunID, _ = d.db.CreatePipelineRun("briefing", "daemon", "auto")
		}
		id, err := d.briefingPipe.Run(ctx)
		if err != nil {
			d.logger.Printf("briefing error: %v", err)
		} else if id > 0 {
			d.logger.Printf("generated briefing (id=%d)", id)
			d.lastBriefing = time.Now()
			d.saveLastBriefing()
		}
		if briefingRunID > 0 {
			var errMsg string
			if err != nil {
				errMsg = err.Error()
			}
			items := 0
			if id > 0 {
				items = 1
			}
			inTok, outTok, cost, totalAPI := d.briefingPipe.AccumulatedUsage()
			_ = d.db.CompletePipelineRun(briefingRunID, items, inTok, outTok, cost, totalAPI, nil, nil, errMsg)
		}
	}
}

// autoMarkRead marks digests as read based on Slack read cursors.
// Safe to call when db is nil (no-op).
func (d *Daemon) autoMarkRead() {
	if d.db == nil {
		return
	}
	digestsMarked, tracksMarked, err := d.db.AutoMarkReadFromSlack()
	if err != nil {
		d.logger.Printf("auto-mark read error: %v", err)
	} else if digestsMarked > 0 || tracksMarked > 0 {
		d.logger.Printf("auto-marked %d digest(s), %d track(s) as read", digestsMarked, tracksMarked)
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

// shouldRunBriefing checks if the daily briefing should run.
// Runs at most once per day, after the configured briefing.hour.
func (d *Daemon) shouldRunBriefing() bool {
	now := time.Now()

	// Already ran today?
	if !d.lastBriefing.IsZero() && now.Sub(d.lastBriefing) < 24*time.Hour {
		return false
	}

	targetHour := d.config.Briefing.Hour
	if targetHour <= 0 {
		targetHour = config.DefaultBriefingHour
	}

	return now.Hour() >= targetHour
}

func (d *Daemon) lastBriefingPath() string {
	return filepath.Join(d.config.WorkspaceDir(), "last_briefing.txt")
}

func (d *Daemon) loadLastBriefing() {
	data, err := os.ReadFile(d.lastBriefingPath())
	if err != nil {
		return
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return
	}
	d.lastBriefing = time.Unix(unix, 0)
	d.logger.Printf("restored last briefing time: %s", d.lastBriefing.Format(time.RFC3339))
}

func (d *Daemon) saveLastBriefing() {
	data := strconv.FormatInt(d.lastBriefing.Unix(), 10)
	if err := os.WriteFile(d.lastBriefingPath(), []byte(data), 0o600); err != nil {
		d.logger.Printf("failed to save last briefing time: %v", err)
	}
}
