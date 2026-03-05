package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAndReadPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	err := WritePID(path)
	require.NoError(t, err)

	pid, err := ReadPID(path)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestWritePID_CreatesDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "daemon.pid")

	err := WritePID(path)
	require.NoError(t, err)

	pid, err := ReadPID(path)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestReadPID_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.pid")

	pid, err := ReadPID(path)
	require.NoError(t, err)
	assert.Equal(t, 0, pid)
}

func TestReadPID_InvalidContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	require.NoError(t, os.WriteFile(path, []byte("not-a-number"), 0o644))

	_, err := ReadPID(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing pid file")
}

func TestFindProcess_LiveProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Write our own PID — we know this process is alive.
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644))

	pid, err := FindProcess(path)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestFindProcess_StalePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Use a PID that almost certainly doesn't exist.
	require.NoError(t, os.WriteFile(path, []byte("999999999"), 0o644))

	pid, err := FindProcess(path)
	require.NoError(t, err)
	assert.Equal(t, 0, pid, "stale PID should return 0")

	// Stale file should be cleaned up.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "stale PID file should be removed")
}

func TestFindProcess_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.pid")

	pid, err := FindProcess(path)
	require.NoError(t, err)
	assert.Equal(t, 0, pid)
}

func TestRemovePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o644))

	RemovePID(path)

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestRemovePID_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.pid")
	// Should not panic or error.
	RemovePID(path)
}
