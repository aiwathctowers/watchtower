package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"watchtower/internal/db"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTempWorkspace points the test at a temp HOME + minimal config so that
// commands relying on Config.WorkspaceDir / Config.DBPath / token store hit
// disposable directories. Returns the workspace dir and a teardown the caller
// can ignore (t.Cleanup handles file teardown).
func setupTempWorkspace(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".config", "watchtower")
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("active_workspace: test\n"), 0o600))

	wsDir := filepath.Join(home, ".local", "share", "watchtower", "test")
	require.NoError(t, os.MkdirAll(wsDir, 0o700))

	prevConfig, prevWS := flagConfig, flagWorkspace
	flagConfig = cfgPath
	flagWorkspace = ""
	t.Cleanup(func() {
		flagConfig = prevConfig
		flagWorkspace = prevWS
	})

	// Pre-create DB so commands that open it succeed.
	dbPath := filepath.Join(wsDir, "watchtower.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	return wsDir
}

func TestRunCalendarStatus_NotConnected(t *testing.T) {
	setupTempWorkspace(t)

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)

	require.NoError(t, runCalendarStatus(c, nil))
	out := buf.String()
	assert.Contains(t, out, "not connected")
	assert.Contains(t, out, "calendar login")
}

func TestRunCalendarStatus_Connected(t *testing.T) {
	wsDir := setupTempWorkspace(t)
	// Create token file so the status check sees it as connected.
	tokenPath := filepath.Join(wsDir, "google_token.json")
	require.NoError(t, os.WriteFile(tokenPath, []byte(`{"access_token":"x"}`), 0o600))

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	require.NoError(t, runCalendarStatus(c, nil))
	out := buf.String()
	assert.Contains(t, out, "connected")
	assert.Contains(t, out, tokenPath)
}

func TestRunCalendarList_Empty(t *testing.T) {
	setupTempWorkspace(t)

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	require.NoError(t, runCalendarList(c, nil))
	assert.Contains(t, buf.String(), "No calendars synced")
}

func TestRunCalendarList_WithCalendars(t *testing.T) {
	wsDir := setupTempWorkspace(t)

	dbPath := filepath.Join(wsDir, "watchtower.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertCalendar(db.CalendarCalendar{
		ID: "primary", Name: "Main", IsPrimary: true, IsSelected: true, SyncedAt: "2026-04-01T00:00:00Z",
	}))
	require.NoError(t, database.UpsertCalendar(db.CalendarCalendar{
		ID: "work@x.com", Name: "Work", IsSelected: false, SyncedAt: "2026-04-01T00:00:00Z",
	}))

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	require.NoError(t, runCalendarList(c, nil))
	out := buf.String()
	assert.Contains(t, out, "Main")
	assert.Contains(t, out, "(primary)")
	assert.Contains(t, out, "Work")
	// Selection markers
	assert.Contains(t, out, "[*]")
	assert.Contains(t, out, "[ ]")
}

func TestRunBriefingList_NoUser(t *testing.T) {
	setupTempWorkspace(t)

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	require.NoError(t, runBriefingList(c, nil))
	assert.Contains(t, buf.String(), "No current user set")
}

func TestRunBriefingList_NoBriefings(t *testing.T) {
	wsDir := setupTempWorkspace(t)

	dbPath := filepath.Join(wsDir, "watchtower.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec(`INSERT INTO workspace (id, name) VALUES ('T1', 'test')`)
	require.NoError(t, err)
	require.NoError(t, database.SetCurrentUserID("U123"))

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	require.NoError(t, runBriefingList(c, nil))
	assert.Contains(t, buf.String(), "No briefings found")
}

func TestRunBriefingList_WithBriefings(t *testing.T) {
	wsDir := setupTempWorkspace(t)

	dbPath := filepath.Join(wsDir, "watchtower.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	defer database.Close()

	_, err = database.Exec(`INSERT INTO workspace (id, name) VALUES ('T1', 'test')`)
	require.NoError(t, err)
	require.NoError(t, database.SetCurrentUserID("U123"))

	id, err := database.UpsertBriefing(db.Briefing{
		UserID:       "U123",
		Date:         "2026-04-02",
		Role:         "EM",
		Attention:    `[{"text":"x"}]`,
		YourDay:      "[]",
		WhatHappened: "[]",
		TeamPulse:    "[]",
		Coaching:     "[]",
		Model:        "haiku",
	})
	require.NoError(t, err)
	require.NotZero(t, id)

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)
	prev := briefingListFlagLimit
	briefingListFlagLimit = 10
	defer func() { briefingListFlagLimit = prev }()

	require.NoError(t, runBriefingList(c, nil))
	out := buf.String()
	assert.Contains(t, out, "2026-04-02")
	assert.Contains(t, out, "unread")
	assert.True(t, strings.Contains(out, "1 attention item"), "expected attention count, got: %s", out)
}

func TestRunCalendarSelect_TogglesSelection(t *testing.T) {
	wsDir := setupTempWorkspace(t)

	dbPath := filepath.Join(wsDir, "watchtower.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	defer database.Close()

	require.NoError(t, database.UpsertCalendar(db.CalendarCalendar{
		ID: "primary", Name: "Main", IsSelected: true, SyncedAt: "2026-04-01T00:00:00Z",
	}))

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	// First call: was selected → becomes deselected.
	require.NoError(t, runCalendarSelect(c, []string{"primary"}))
	assert.Contains(t, buf.String(), "deselected")

	// Reload from disk and toggle again.
	database.Close()
	database2, err := db.Open(dbPath)
	require.NoError(t, err)
	defer database2.Close()

	buf.Reset()
	require.NoError(t, runCalendarSelect(c, []string{"primary"}))
	assert.Contains(t, buf.String(), "selected")
	assert.NotContains(t, buf.String(), "deselected")
}

func TestRunCalendarSelect_NotFound(t *testing.T) {
	setupTempWorkspace(t)

	c := &cobra.Command{}
	var buf bytes.Buffer
	c.SetOut(&buf)

	err := runCalendarSelect(c, []string{"nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
