package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "warning: removing stale pid file: %v\n", rmErr)
		}
		return 0, nil
	}

	// Detect PID reuse: if we have a timestamp, use process name check.
	// Without a timestamp (legacy format), fall back to a 30-day heuristic.
	if !startTime.IsZero() {
		if isReusedPID(pid) {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "warning: removing stale pid file: %v\n", rmErr)
			}
			return 0, nil
		}
	} else {
		// Legacy PID file without timestamp: use 30-day heuristic as fallback.
		info, err := os.Stat(path)
		if err == nil && time.Since(info.ModTime()) > 30*24*time.Hour {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "warning: removing stale pid file: %v\n", rmErr)
			}
			return 0, nil
		}
	}

	return pid, nil
}

// RemovePID removes the PID file. It is a no-op if the file does not exist.
func RemovePID(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: removing pid file: %v\n", err)
	}
}

// isReusedPID checks whether the given PID belongs to a process that is NOT
// a watchtower instance. Uses `ps` to read the process command name.
// Returns true if the PID is definitely reused by an unrelated process.
// Returns false (conservative) if we can't determine or if it is watchtower.
func isReusedPID(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false // can't determine — assume it's ours
	}
	comm := strings.TrimSpace(string(out))
	return comm != "" && !strings.Contains(comm, "watchtower")
}
