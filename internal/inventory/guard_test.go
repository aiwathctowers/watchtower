// Package inventory hosts the guard test for docs/inventory/.
//
// docs/inventory/*.md catalogs behavioral contracts (INBOX-01, TRACKS-03, ...)
// each protected by named test guards. The contract is informal: contributors
// must point each contract at real, runnable test functions. This test fails
// the build if any of those references rot — file moved, test renamed, etc.
package inventory

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// guardRef is a `path/to/file.go::TestName` reference parsed from inventory.
type guardRef struct {
	inventoryFile string
	relPath       string
	testName      string
}

// TestInventoryGuardsResolveGo verifies every Go "Test guards:" reference in
// docs/inventory/*.md points to an existing func TestName(...) in the named
// file. Swift references are checked separately (file existence only).
func TestInventoryGuardsResolveGo(t *testing.T) {
	root := mustRepoRoot(t)
	refs := mustParseGuardRefs(t, root, ".go")

	if len(refs) == 0 {
		t.Fatalf("no Go guard references parsed — regex broken or inventory empty?")
	}

	var failures []string
	for _, ref := range refs {
		abs := filepath.Join(root, ref.relPath)
		body, err := os.ReadFile(abs)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s → %s::%s: %v", ref.inventoryFile, ref.relPath, ref.testName, err))
			continue
		}
		marker := []byte("func " + ref.testName + "(")
		if !bytes.Contains(body, marker) {
			failures = append(failures, fmt.Sprintf("%s → %s::%s: test function not found", ref.inventoryFile, ref.relPath, ref.testName))
		}
	}

	if len(failures) > 0 {
		t.Fatalf("inventory Go-guard mismatch (%d):\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
}

// TestInventoryGuardsResolveSwift checks Swift test files referenced by
// inventory exist. We don't parse Swift to find the function symbol — `swift
// test` will catch a renamed XCTest at its own gate.
func TestInventoryGuardsResolveSwift(t *testing.T) {
	root := mustRepoRoot(t)
	refs := mustParseGuardRefs(t, root, ".swift")

	var failures []string
	for _, ref := range refs {
		abs := filepath.Join(root, ref.relPath)
		if _, err := os.Stat(abs); err != nil {
			failures = append(failures, fmt.Sprintf("%s → %s: %v", ref.inventoryFile, ref.relPath, err))
		}
	}

	if len(failures) > 0 {
		t.Fatalf("inventory Swift-guard file mismatch (%d):\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
}

// mustParseGuardRefs scans docs/inventory/*.md (excluding README.md) for
// `relPath::TestName` entries whose relPath ends with the given suffix.
func mustParseGuardRefs(t *testing.T, root, suffix string) []guardRef {
	t.Helper()
	dir := filepath.Join(root, "docs", "inventory")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	// Backtick-delimited markdown reference: `path/to/file.go::TestFoo`
	// Path may include unicode but not backticks/colons/whitespace.
	refRe := regexp.MustCompile("`([^`\\s:]+)::([A-Za-z_][A-Za-z0-9_]+)`")

	var refs []guardRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "README.md" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range refRe.FindAllStringSubmatch(string(body), -1) {
			rel := m[1]
			if !strings.HasSuffix(rel, suffix) {
				continue
			}
			refs = append(refs, guardRef{
				inventoryFile: e.Name(),
				relPath:       rel,
				testName:      m[2],
			})
		}
	}
	return refs
}

// mustRepoRoot returns the absolute path to the repository root. The test runs
// from internal/inventory/, so the root is two parents up.
func mustRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
