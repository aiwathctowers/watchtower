package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"watchtower/internal/briefing"
	"watchtower/internal/calendar"
	"watchtower/internal/config"
	"watchtower/internal/db"
	"watchtower/internal/jira"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newQuietDaemon(t *testing.T) *Daemon {
	t.Helper()
	d := &Daemon{
		config: &config.Config{},
		logger: log.New(io.Discard, "", 0),
	}
	return d
}

func TestDaemon_Setters_AssignDeps(t *testing.T) {
	d := newQuietDaemon(t)

	briefingPipe := &briefing.Pipeline{}
	d.SetBriefingPipeline(briefingPipe)
	assert.Same(t, briefingPipe, d.briefingPipe)

	calendarSyncer := &calendar.Syncer{}
	d.SetCalendarSyncer(calendarSyncer)
	assert.Same(t, calendarSyncer, d.calendarSyncer)

	jiraSyncer := &jira.Syncer{}
	d.SetJiraSyncer(jiraSyncer)
	assert.Same(t, jiraSyncer, d.jiraSyncer)
}

func TestSameCalendarDay(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name string
		a, b time.Time
		want bool
	}{
		{"same instant", time.Date(2026, 4, 2, 12, 0, 0, 0, loc), time.Date(2026, 4, 2, 12, 0, 0, 0, loc), true},
		{"same day diff time", time.Date(2026, 4, 2, 0, 0, 0, 0, loc), time.Date(2026, 4, 2, 23, 59, 59, 0, loc), true},
		{"next day", time.Date(2026, 4, 2, 23, 59, 59, 0, loc), time.Date(2026, 4, 3, 0, 0, 0, 0, loc), false},
		{"different month", time.Date(2026, 4, 30, 12, 0, 0, 0, loc), time.Date(2026, 5, 1, 12, 0, 0, 0, loc), false},
		{"different year", time.Date(2025, 12, 31, 12, 0, 0, 0, loc), time.Date(2026, 1, 1, 12, 0, 0, 0, loc), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sameCalendarDay(tc.a, tc.b))
		})
	}
}

func TestShouldRunBriefing_AfterHourFreshDB(t *testing.T) {
	d := newQuietDaemon(t)
	d.config.Briefing.Hour = 1 // pretty much always past 1 AM

	if time.Now().Hour() < 1 {
		t.Skip("hour is below briefing threshold")
	}
	assert.True(t, d.shouldRunBriefing(), "should run when no last briefing recorded")
}

func TestShouldRunBriefing_BeforeHour(t *testing.T) {
	d := newQuietDaemon(t)
	// Set hour higher than current to guarantee we're below it.
	cur := time.Now().Hour()
	if cur >= 23 {
		t.Skip("can't test before-hour at 23:00")
	}
	d.config.Briefing.Hour = cur + 1
	assert.False(t, d.shouldRunBriefing(), "should not run before configured hour")
}

func TestShouldRunBriefing_AlreadyRanToday(t *testing.T) {
	d := newQuietDaemon(t)
	d.config.Briefing.Hour = 0
	d.lastBriefing = time.Now()
	assert.False(t, d.shouldRunBriefing(), "should not run twice the same day")
}

func TestShouldRunBriefing_DefaultHourWhenZero(t *testing.T) {
	d := newQuietDaemon(t)
	d.config.Briefing.Hour = 0 // Should fall back to DefaultBriefingHour.
	cur := time.Now().Hour()

	got := d.shouldRunBriefing()
	want := cur >= config.DefaultBriefingHour
	assert.Equal(t, want, got)
}

func TestSaveAndLoadLastBriefing_RoundTrip(t *testing.T) {
	d := newQuietDaemon(t)
	dir := t.TempDir()
	d.config.ActiveWorkspace = "test"

	// Override HOME so WorkspaceDir lands in our temp dir.
	t.Setenv("HOME", dir)

	stamp := time.Date(2026, 4, 2, 9, 30, 0, 0, time.UTC)
	d.lastBriefing = stamp

	require.NoError(t, os.MkdirAll(d.config.WorkspaceDir(), 0o700))
	d.saveLastBriefing()

	// Verify file exists with the unix timestamp.
	data, err := os.ReadFile(d.lastBriefingPath())
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// New daemon loads it.
	d2 := newQuietDaemon(t)
	d2.config.ActiveWorkspace = "test"
	d2.loadLastBriefing()
	assert.Equal(t, stamp.Unix(), d2.lastBriefing.Unix())
}

func TestLastBriefingPath_UnderWorkspaceDir(t *testing.T) {
	d := newQuietDaemon(t)
	d.config.ActiveWorkspace = "ws"
	t.Setenv("HOME", "/fake/home")

	got := d.lastBriefingPath()
	assert.Equal(t, filepath.Join("/fake/home", ".local", "share", "watchtower", "ws", "last_briefing.txt"), got)
}

// Confirm helper does not panic with an unset DB.
func TestDaemon_New(t *testing.T) {
	d := New(nil, &config.Config{})
	require.NotNil(t, d)
	assert.NotNil(t, d.logger)
	assert.Nil(t, d.db) // SetDB hasn't been called.
	d.SetDB((*db.DB)(nil))
	d.SetLogger(log.New(io.Discard, "", 0))
}
