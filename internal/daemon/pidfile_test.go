package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	require.NoError(t, os.WriteFile(path, []byte("not-a-number"), 0o600))

	_, err := ReadPID(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing pid file")
}

func TestFindProcess_LiveProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Write our own PID — we know this process is alive.
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600))

	pid, err := FindProcess(path)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)
}

func TestFindProcess_StalePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Use a PID that almost certainly doesn't exist.
	require.NoError(t, os.WriteFile(path, []byte("999999999"), 0o600))

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
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o600))

	RemovePID(path)

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestRemovePID_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.pid")
	// Should not panic or error.
	RemovePID(path)
}

func TestReadPID_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	_, err := ReadPID(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestReadPIDWithStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// New format: PID + timestamp
	require.NoError(t, os.WriteFile(path, []byte("12345 1700000000"), 0o600))

	pid, startTime, err := readPIDWithStart(path)
	require.NoError(t, err)
	assert.Equal(t, 12345, pid)
	assert.False(t, startTime.IsZero())
	assert.Equal(t, int64(1700000000), startTime.Unix())
}

func TestReadPIDWithStart_LegacyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Legacy format: just PID
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o600))

	pid, startTime, err := readPIDWithStart(path)
	require.NoError(t, err)
	assert.Equal(t, 12345, pid)
	assert.True(t, startTime.IsZero())
}

func TestReadPIDWithStart_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.pid")

	pid, startTime, err := readPIDWithStart(path)
	require.NoError(t, err)
	assert.Equal(t, 0, pid)
	assert.True(t, startTime.IsZero())
}

func TestReadPIDWithStart_InvalidPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	require.NoError(t, os.WriteFile(path, []byte("abc"), 0o600))

	_, _, err := readPIDWithStart(path)
	assert.Error(t, err)
}

func TestReadPIDWithStart_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	require.NoError(t, os.WriteFile(path, []byte("   "), 0o600))

	_, _, err := readPIDWithStart(path)
	assert.Error(t, err)
}

func TestFindProcess_WithTimestamp_OwnProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Write our PID with a timestamp — this should find us (not reused since we ARE watchtower test)
	require.NoError(t, WritePID(path))

	pid, err := FindProcess(path)
	require.NoError(t, err)
	// Our process is running, but isReusedPID may detect we're "go test" not "watchtower".
	// Either way, no error should occur.
	_ = pid
}

func TestIsReusedPID_OwnProcess(t *testing.T) {
	// Our own PID — ps will show "go" or something test-related, not "watchtower",
	// so isReusedPID should return true (it's not watchtower).
	reused := isReusedPID(os.Getpid())
	// We expect true since our process name contains "go" not "watchtower"
	assert.True(t, reused)
}

func TestIsReusedPID_NonexistentProcess(t *testing.T) {
	// PID that doesn't exist — ps will fail, function returns false (conservative)
	reused := isReusedPID(999999999)
	assert.False(t, reused)
}

func TestWritePID_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.pid")

	err := WritePID(path)
	require.NoError(t, err)

	// Verify content format: "PID TIMESTAMP"
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	parts := strings.Fields(string(data))
	require.Len(t, parts, 2)

	pid, err := strconv.Atoi(parts[0])
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)

	ts, err := strconv.ParseInt(parts[1], 10, 64)
	require.NoError(t, err)
	assert.InDelta(t, time.Now().Unix(), ts, 5)

	// Temp file should not remain
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err))
}

func TestFindProcess_StalePIDWithTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// PID that doesn't exist, with timestamp
	require.NoError(t, os.WriteFile(path, []byte("999999999 1700000000"), 0o600))

	pid, err := FindProcess(path)
	require.NoError(t, err)
	assert.Equal(t, 0, pid, "stale PID should return 0")

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestFindProcess_LiveProcessWithTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")

	// Write our PID with current timestamp
	require.NoError(t, WritePID(path))

	pid, err := FindProcess(path)
	require.NoError(t, err)
	// Our process is "go test", not "watchtower", so isReusedPID returns true.
	// FindProcess should therefore return 0 (detected as reused PID).
	assert.Equal(t, 0, pid)
}

func TestFindProcess_EmptyPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	_, err := FindProcess(path)
	assert.Error(t, err)
}
