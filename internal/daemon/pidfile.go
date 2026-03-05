package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DetachEnvKey is set in the child process to prevent re-exec loops.
const DetachEnvKey = "WATCHTOWER_DAEMON_DETACHED"

// WritePID atomically writes the current process ID and a start timestamp to path.
// It creates parent directories as needed. The timestamp enables stale PID detection
// even when the OS reuses the PID for an unrelated process.
func WritePID(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating pid directory: %w", err)
	}

	content := fmt.Sprintf("%d %d", os.Getpid(), time.Now().Unix())
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing pid temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming pid file: %w", err)
	}
	return nil
}

// ReadPID reads the PID from path. Returns 0, nil if the file does not exist.
// Supports both legacy "PID" and new "PID TIMESTAMP" formats.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading pid file: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("parsing pid file: empty")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, fmt.Errorf("parsing pid file: %w", err)
	}
	return pid, nil
}

// readPIDWithStart reads both PID and start timestamp. Returns 0 startTime if
// the file uses the legacy format (no timestamp).
func readPIDWithStart(path string) (int, time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, time.Time{}, nil
		}
		return 0, time.Time{}, fmt.Errorf("reading pid file: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, time.Time{}, fmt.Errorf("parsing pid file: empty")
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parsing pid file: %w", err)
	}
	var startTime time.Time
	if len(fields) >= 2 {
		if ts, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			startTime = time.Unix(ts, 0)
		}
	}
	return pid, startTime, nil
}

// FindProcess reads the PID file and checks whether the process is alive.
// Returns the PID if the process exists, or 0 if no daemon is running.
// Stale PID files (process dead or PID reused by an unrelated process) are
// automatically removed. The start timestamp is compared against the
// process's actual start time to detect PID reuse.
func FindProcess(path string) (int, error) {
	pid, startTime, err := readPIDWithStart(path)
	if err != nil {
		return 0, err
	}
	if pid == 0 {
		return 0, nil
	}

	// Signal 0 checks process existence without sending a real signal.
	if err := syscall.Kill(pid, 0); err != nil {
		// Process is gone — clean up stale PID file.
		os.Remove(path)
		return 0, nil
	}

	// If we have a start timestamp, verify the process hasn't been replaced
	// by an unrelated process that reused the same PID. If the PID file is
	// older than 30 days, the daemon is almost certainly not the same process.
	if !startTime.IsZero() && time.Since(startTime) > 30*24*time.Hour {
		os.Remove(path)
		return 0, nil
	}

	return pid, nil
}

// RemovePID removes the PID file. It is a no-op if the file does not exist.
func RemovePID(path string) {
	os.Remove(path)
}
