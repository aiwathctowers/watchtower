// Package codex provides utilities for resolving and invoking the Codex CLI binary.
package codex

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	cachedBinary   string
	cachedBinaryMu sync.Once

	cachedPATH   string
	cachedPATHMu sync.Once
)

// FindBinary looks up the Codex CLI binary.
// If override is non-empty and points to an executable file, it is used directly
// (bypasses cache).
// Otherwise tries exec.LookPath, then falls back to a login shell lookup
// (needed when launched from a macOS .app bundle where PATH is minimal).
// The result (without override) is cached after the first call.
func FindBinary(override string) string {
	if override != "" {
		if info, err := os.Stat(override); err == nil && !info.IsDir() {
			return override
		}
	}

	cachedBinaryMu.Do(func() {
		// Fast path: already in PATH.
		if p, err := exec.LookPath("codex"); err == nil {
			cachedBinary = p
			return
		}
		// Login shell: loads .zshrc/.bash_profile → full PATH.
		if p := loginShellWhich("codex"); p != "" {
			cachedBinary = p
			return
		}
		cachedBinary = "codex" // fallback — let exec report the error
	})

	return cachedBinary
}

// RichPATH returns a PATH that includes common tool locations so that
// subprocess invocations can find node, npx, etc. even from a macOS .app bundle.
// The result is cached after the first call.
func RichPATH() string {
	cachedPATHMu.Do(func() {
		if shellPATH := loginShellPATH(); shellPATH != "" {
			cachedPATH = shellPATH
			return
		}
		cachedPATH = fallbackPATH()
	})

	return cachedPATH
}

func fallbackPATH() string {
	existing := os.Getenv("PATH")
	home, _ := os.UserHomeDir()
	extra := []string{
		"/usr/local/bin",
		"/opt/homebrew/bin",
	}
	if home != "" {
		nvmDir := filepath.Join(home, ".nvm", "versions", "node")
		if entries, err := os.ReadDir(nvmDir); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				extra = append(extra, filepath.Join(nvmDir, entries[i].Name(), "bin"))
			}
		}
		extra = append(extra,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".fnm", "current", "bin"),
		)
	}

	seen := make(map[string]bool)
	for _, p := range strings.Split(existing, ":") {
		seen[p] = true
	}
	var parts []string
	for _, p := range extra {
		if !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	if existing != "" {
		parts = append(parts, existing)
	}
	return strings.Join(parts, ":")
}

func loginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

func loginShellWhich(name string) string {
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return ""
		}
	}
	sh := loginShell()
	out, err := exec.Command(sh, "-l", "-c", "which "+name).Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return ""
	}
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

func loginShellPATH() string {
	sh := loginShell()
	out, err := exec.Command(sh, "-l", "-c", "echo $PATH").Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" || p == os.Getenv("PATH") {
		return ""
	}
	return p
}
