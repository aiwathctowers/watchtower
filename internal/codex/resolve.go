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
		// Use RichPATH so version-manager binaries (nvm, volta, fnm)
		// are found before stale system-wide installs (e.g. /usr/local/bin).
		richPATH := RichPATH()
		if p, err := lookPathIn("codex", richPATH); err == nil {
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
// It combines the login shell PATH with well-known tool directories (nvm, volta,
// fnm, Homebrew) prepended so that the correct node version is used for tools
// installed via version managers.
// The result is cached after the first call.
func RichPATH() string {
	cachedPATHMu.Do(func() {
		base := loginShellPATH()
		if base == "" {
			base = os.Getenv("PATH")
		}
		cachedPATH = enrichPATH(base)
	})

	return cachedPATH
}

// enrichPATH prepends well-known tool directories (nvm, volta, fnm, Homebrew)
// to the given base PATH, deduplicating entries. nvm/volta/fnm paths come first
// so that version-manager-installed node is preferred over system node.
func enrichPATH(base string) string {
	home, _ := os.UserHomeDir()

	// Paths prepended in order — version managers first, then system locations.
	var extra []string
	if home != "" {
		// nvm — newest version last so it appears first after reversal.
		nvmDir := filepath.Join(home, ".nvm", "versions", "node")
		if entries, err := os.ReadDir(nvmDir); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				extra = append(extra, filepath.Join(nvmDir, entries[i].Name(), "bin"))
			}
		}
		extra = append(extra,
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".fnm", "current", "bin"),
			filepath.Join(home, ".local", "bin"),
		)
	}
	extra = append(extra,
		"/opt/homebrew/bin",
		"/usr/local/bin",
	)

	// Build result: extra paths first (deduplicated), then base paths (deduplicated).
	seen := make(map[string]bool)
	var parts []string
	for _, p := range extra {
		if !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	for _, p := range strings.Split(base, ":") {
		if !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ":")
}

// lookPathIn searches for an executable in the given PATH string
// (colon-separated directories), similar to exec.LookPath but using
// a custom PATH instead of the process environment.
func lookPathIn(name, pathEnv string) (string, error) {
	for _, dir := range strings.Split(pathEnv, ":") {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
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
