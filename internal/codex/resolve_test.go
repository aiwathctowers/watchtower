package codex

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetCache() {
	cachedBinary = ""
	cachedBinaryMu = sync.Once{}
	cachedPATH = ""
	cachedPATHMu = sync.Once{}
}

func TestFindBinary_Override(t *testing.T) {
	resetCache()

	// Create a temporary file to act as our "codex" binary.
	tmpDir := t.TempDir()
	fakeBin := filepath.Join(tmpDir, "codex")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindBinary(fakeBin)
	if got != fakeBin {
		t.Errorf("FindBinary(%q) = %q, want %q", fakeBin, got, fakeBin)
	}
}

func TestFindBinary_OverrideNonExistent(t *testing.T) {
	resetCache()

	// Non-existent override should fall through to lookup.
	got := FindBinary("/nonexistent/codex")
	// Should return something (either found in PATH or fallback "codex").
	if got == "/nonexistent/codex" {
		t.Error("FindBinary should not return non-existent override path")
	}
}

func TestFindBinary_OverrideDirectory(t *testing.T) {
	resetCache()

	// Directory override should be rejected.
	tmpDir := t.TempDir()
	got := FindBinary(tmpDir)
	if got == tmpDir {
		t.Error("FindBinary should not return a directory as binary")
	}
}

func TestFindBinary_Fallback(t *testing.T) {
	resetCache()

	// With no override and likely no codex in PATH, should return "codex" fallback.
	got := FindBinary("")
	if got == "" {
		t.Error("FindBinary should never return empty string")
	}
}

func TestRichPATH_NotEmpty(t *testing.T) {
	resetCache()

	got := RichPATH()
	if got == "" {
		t.Error("RichPATH should not return empty string")
	}
}

func TestRichPATH_Cached(t *testing.T) {
	resetCache()

	first := RichPATH()
	second := RichPATH()
	if first != second {
		t.Error("RichPATH should return cached value on second call")
	}
}
