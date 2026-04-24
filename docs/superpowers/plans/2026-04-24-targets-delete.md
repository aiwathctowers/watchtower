# Targets — Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire end-to-end delete for targets: new `watchtower targets delete <id>` CLI command, and a confirmation-gated Delete action in both the Desktop target list context menu and the target detail view.

**Architecture:** The data layer already supports delete (`DB.DeleteTarget` in `internal/db/targets.go` + FK constraints: `parent_id` → `SET NULL`, `target_links` → `CASCADE`). We add (a) a cobra subcommand in `cmd/targets.go` mirroring `targets done` / `targets dismiss`, (b) a `.confirmationDialog` around the existing Delete entry in `TargetsListView` plus selection cleanup, and (c) a new ellipsis menu in `TargetDetailView`'s tab bar containing a Delete action that also closes the detail pane.

**Tech Stack:** Go 1.25, cobra, `modernc.org/sqlite` via `database/sql`, testify (`require`/`assert`). SwiftUI on macOS 14+, GRDB.swift, XCTest.

**Reference spec:** `docs/superpowers/specs/2026-04-24-targets-delete-design.md`.

---

## Task 1: Go — add `watchtower targets delete <id>` CLI command

**Files:**
- Modify: `cmd/targets.go` (add `targetsDeleteCmd` variable, `runTargetsDelete` function, register in `init()`, add `--json` flag)
- Test: `cmd/targets_test.go` (three new test functions)

### Step 1.1: Write the failing happy-path test

- [ ] Open `cmd/targets_test.go` and append after `TestRunTargetsDismiss` (near line 339):

```go
// --- Delete ---

func TestRunTargetsDelete(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To delete", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsDeleteCmd.SetOut(buf)

	err := targetsDeleteCmd.RunE(targetsDeleteCmd, []string{"1"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Target #1 removed")

	database, err := openDBFromConfig()
	require.NoError(t, err)
	defer database.Close()
	target, _ := database.GetTargetByID(1)
	assert.Nil(t, target, "target should be gone after delete")
}
```

### Step 1.2: Run the test to verify it fails

- [ ] Run: `go test ./cmd/ -run TestRunTargetsDelete -v -count=1`
- [ ] Expected: compile error — `undefined: targetsDeleteCmd`.

### Step 1.3: Add the not-found test

- [ ] Append immediately after `TestRunTargetsDelete`:

```go
func TestRunTargetsDelete_NotFound(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	err := targetsDeleteCmd.RunE(targetsDeleteCmd, []string{"999999"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
```

### Step 1.4: Add the `--json` output test

- [ ] Append immediately after `TestRunTargetsDelete_NotFound`:

```go
func TestRunTargetsDelete_JSON(t *testing.T) {
	cleanup := setupTargetsTestEnv(t)
	defer cleanup()

	createTestTarget(t, "To delete json", "medium", "todo")

	buf := new(bytes.Buffer)
	targetsDeleteCmd.SetOut(buf)

	require.NoError(t, targetsDeleteCmd.Flags().Set("json", "true"))
	defer func() { _ = targetsDeleteCmd.Flags().Set("json", "false") }()

	err := targetsDeleteCmd.RunE(targetsDeleteCmd, []string{"1"})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"id":1`)
	assert.Contains(t, out, `"removed":true`)
}
```

### Step 1.5: Run all three tests to confirm they fail before implementation

- [ ] Run: `go test ./cmd/ -run 'TestRunTargetsDelete' -v -count=1`
- [ ] Expected: compile error (all three), since `targetsDeleteCmd` is still undefined.

### Step 1.6: Declare the cobra command + flag variable

- [ ] In `cmd/targets.go`, add a new flag variable in the existing `var (...)` block near the top (right after `targetsFlagSuggestLinksJSON`, ~line 52):

```go
	// delete subcommand flags
	targetsFlagDeleteJSON bool
```

- [ ] Add the command declaration immediately after `targetsDismissCmd` (~line 117, before `targetsSnoozeCmd`):

```go
var targetsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a target permanently",
	Long:  "Removes a target by ID. Children orphan to the root; linked rows cascade. This cannot be undone.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTargetsDelete,
}
```

### Step 1.7: Register the command and its flag in `init()`

- [ ] In the `targetsCmd.AddCommand(...)` list (~line 176), add `targetsDeleteCmd` after `targetsDismissCmd`:

```go
	targetsCmd.AddCommand(
		targetsShowCmd,
		targetsCreateCmd,
		targetsExtractCmd,
		targetsLinkCmd,
		targetsUnlinkCmd,
		targetsSuggestLinksCmd,
		targetsDoneCmd,
		targetsDismissCmd,
		targetsDeleteCmd,
		targetsSnoozeCmd,
		targetsUpdateCmd,
		targetsGenerateCmd,
		targetsNoteCmd,
		targetsAIUpdateCmd,
	)
```

- [ ] After the `// suggest-links flags` block in `init()`, add:

```go
	// delete flags
	targetsDeleteCmd.Flags().BoolVar(&targetsFlagDeleteJSON, "json", false, "output result as JSON")
```

### Step 1.8: Implement `runTargetsDelete`

- [ ] In `cmd/targets.go`, add the handler immediately after `runTargetsDismiss` (~line 970, before `runTargetsSnooze`):

```go
func runTargetsDelete(cmd *cobra.Command, args []string) error {
	id, err := strconv.Atoi(args[0])
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid target ID %q: must be a positive integer", args[0])
	}

	database, err := openDBFromConfig()
	if err != nil {
		return err
	}
	defer database.Close()

	if _, err := database.GetTargetByID(id); err != nil {
		return fmt.Errorf("target #%d not found: %w", id, err)
	}

	if err := database.DeleteTarget(id); err != nil {
		return fmt.Errorf("deleting target #%d: %w", id, err)
	}

	if targetsFlagDeleteJSON {
		payload := map[string]any{"id": id, "removed": true}
		enc := json.NewEncoder(cmd.OutOrStdout())
		if err := enc.Encode(payload); err != nil {
			return fmt.Errorf("encoding JSON: %w", err)
		}
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Target #%d removed\n", id)
	return nil
}
```

Note: `encoding/json` is already imported at the top of `cmd/targets.go` (confirmed by grep of the file).

### Step 1.9: Run the three new tests — expect all pass

- [ ] Run: `go test ./cmd/ -run 'TestRunTargetsDelete' -v -count=1`
- [ ] Expected: `PASS` for all three (`TestRunTargetsDelete`, `TestRunTargetsDelete_NotFound`, `TestRunTargetsDelete_JSON`).

### Step 1.10: Run the full `cmd` suite to catch registration side-effects

- [ ] Run: `go test ./cmd/ -count=1`
- [ ] Expected: all previously-passing tests still pass. In particular, `TestTargetsSubcommandsRegistered` should still pass because it does not enumerate every subcommand.

### Step 1.11: Manual smoke check

- [ ] Run: `go build -o /tmp/watchtower ./`
- [ ] In a throwaway workspace (or skip if no live DB handy), run `/tmp/watchtower targets delete 999999` — expect exit 1 with `target #999999 not found`.

### Step 1.12: Commit

```bash
git add cmd/targets.go cmd/targets_test.go
git commit -m "feat(cli): add 'targets delete <id>' command"
```

---

## Task 2: Swift — `TargetsViewModel.deleteTarget` regression test

Rationale: `TargetQueries.delete` already has `testDelete` at the query level, but the VM wrapper (`TargetsViewModel.deleteTarget`) has no direct coverage. Desktop UI changes in Tasks 3–4 wire both entry points to this VM method; a pinned regression test makes failures local.

**Files:**
- Test: `WatchtowerDesktop/Tests/ViewModelTests.swift` (append a new case)

### Step 2.1: Inspect the existing `ViewModelTests.swift` layout

- [ ] Run: `grep -n "^final class\|^func test\|^import" WatchtowerDesktop/Tests/ViewModelTests.swift | head -30`
- [ ] Note the test class name(s) and whether there is already a `TargetsViewModelTests` class or similar.

### Step 2.2: Add the test case

- [ ] If a `TargetsViewModelTests` class exists, append the test inside it. Otherwise, add a new `final class TargetsViewModelTests: XCTestCase { ... }` at the bottom of the file, using the same import pattern as other classes in the file (`import XCTest`, `import GRDB`, `@testable import WatchtowerDesktop`).
- [ ] Append this test:

```swift
func testDeleteTargetRemovesRow() throws {
    let db = try TestDatabase.create()
    try db.write { try TestDatabase.insertTarget($0) }

    let manager = DatabaseManager(dbPool: db)
    let vm = TargetsViewModel(dbManager: manager)

    let target = try XCTUnwrap(db.read { try TargetQueries.fetchByID($0, id: 1) })
    vm.deleteTarget(target)

    let gone = try db.read { try TargetQueries.fetchByID($0, id: 1) }
    XCTAssertNil(gone)
    XCTAssertNil(vm.errorMessage)
}
```

- [ ] If `DatabaseManager` has a different initialiser signature in this codebase, run `grep -n "class DatabaseManager\|init(" WatchtowerDesktop/Sources/Database/DatabaseManager.swift` and adjust the construction line accordingly. The test must end with a `DatabaseManager` whose `dbPool` is the one we wrote to.

### Step 2.3: Run the new test

- [ ] Run: `cd WatchtowerDesktop && swift test --filter TargetsViewModelTests.testDeleteTargetRemovesRow 2>&1 | tail -40`
- [ ] Expected: test compiles and passes. If construction of `TargetsViewModel` requires `@MainActor` context, wrap the body in `MainActor.run { ... }` or annotate the test with `@MainActor func testDeleteTargetRemovesRow() async throws`.

### Step 2.4: Run the full Desktop test suite

- [ ] Run: `cd WatchtowerDesktop && swift test 2>&1 | tail -20`
- [ ] Expected: all 490+ tests still pass.

### Step 2.5: Commit

```bash
git add WatchtowerDesktop/Tests/ViewModelTests.swift
git commit -m "test(desktop): regression test for TargetsViewModel.deleteTarget"
```

---

## Task 3: Desktop — confirmation dialog + selection cleanup in `TargetsListView`

**Files:**
- Modify: `WatchtowerDesktop/Sources/Views/Targets/TargetsListView.swift`

### Step 3.1: Add state for the pending delete

- [ ] Open `WatchtowerDesktop/Sources/Views/Targets/TargetsListView.swift`.
- [ ] In the `@State` declarations near the top (around lines 5–8), add:

```swift
    @State private var pendingDeleteTarget: Target?
```

### Step 3.2: Rewire the context-menu button to stash the target instead of deleting

- [ ] Locate the existing line (approximately `TargetsListView.swift:326`):

```swift
        Button("Delete", role: .destructive) { vm.deleteTarget(target) }
```

- [ ] Replace it with:

```swift
        Button("Delete…", role: .destructive) {
            pendingDeleteTarget = target
        }
```

### Step 3.3: Attach the confirmation dialog at view level

- [ ] In the outer `HStack` at the start of `body` (the one that contains the list panel and detail pane, around line 11), attach a modifier after the existing `.background { ... }` block at the end of the chain (around line 51), so that the dialog lives on the root view once:

```swift
        .confirmationDialog(
            pendingDeleteTarget.map { target in
                let label = target.text.count > 60
                    ? String(target.text.prefix(60)) + "…"
                    : target.text
                return "Delete \"\(label)\"?"
            } ?? "",
            isPresented: Binding(
                get: { pendingDeleteTarget != nil },
                set: { if !$0 { pendingDeleteTarget = nil } }
            ),
            titleVisibility: .visible,
            presenting: pendingDeleteTarget
        ) { target in
            Button("Delete", role: .destructive) {
                if selectedItemID == target.id { selectedItemID = nil }
                viewModel?.deleteTarget(target)
                pendingDeleteTarget = nil
            }
            Button("Cancel", role: .cancel) {
                pendingDeleteTarget = nil
            }
        } message: { _ in
            Text("This action cannot be undone.")
        }
```

- [ ] Note the two references to `viewModel` inside the dialog closure — `viewModel` is the `@State var viewModel: TargetsViewModel?` declared on `TargetsListView`. Use `viewModel?` because the closure does not have access to the local `vm` parameter from `contextMenu(_:vm:)`.

### Step 3.4: Build and verify

- [ ] Run: `cd WatchtowerDesktop && swift build 2>&1 | tail -30`
- [ ] Expected: clean build. If there is a type error around `Binding(get:set:)`, check that no other `.confirmationDialog` higher in the modifier chain swallows the binding.

### Step 3.5: Run the Desktop test suite

- [ ] Run: `cd WatchtowerDesktop && swift test 2>&1 | tail -20`
- [ ] Expected: all tests pass. No new tests for this step — the wiring is declarative and covered by the VM test from Task 2.

### Step 3.6: Manual UI smoke check

- [ ] Launch Watchtower Desktop (existing `run` command or open in Xcode).
- [ ] Right-click a target in the list, click `Delete…`, confirm in the dialog → target disappears; if the target was selected, the detail pane closes.
- [ ] Repeat and click `Cancel` in the dialog → target remains.

### Step 3.7: Commit

```bash
git add WatchtowerDesktop/Sources/Views/Targets/TargetsListView.swift
git commit -m "feat(desktop): confirm target deletion from list context menu"
```

---

## Task 4: Desktop — ellipsis menu with Delete in `TargetDetailView`

**Files:**
- Modify: `WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift`

### Step 4.1: Add local state for the confirmation dialog

- [ ] Open `WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift`.
- [ ] In the `@State` declarations near the top (around lines 9–31), add:

```swift
    @State private var showDeleteConfirm = false
```

### Step 4.2: Insert the ellipsis menu into the tab bar

- [ ] Locate the tab bar `HStack` at the top of `body` (~lines 42–55). Just before the `if let onClose { ... }` block, insert:

```swift
                Menu {
                    Button("Delete…", role: .destructive) {
                        showDeleteConfirm = true
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                        .foregroundStyle(.secondary)
                }
                .menuStyle(.borderlessButton)
                .fixedSize()
                .padding(.trailing, 8)
```

### Step 4.3: Attach the confirmation dialog to the view

- [ ] On the outer `VStack(spacing: 0)` at the top of `body`, after the existing `.sheet(isPresented: $showSuggestLinksSheet)` modifier (~line 85), add:

```swift
        .confirmationDialog(
            {
                let label = target.text.count > 60
                    ? String(target.text.prefix(60)) + "…"
                    : target.text
                return "Delete \"\(label)\"?"
            }(),
            isPresented: $showDeleteConfirm,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) {
                viewModel.deleteTarget(target)
                onClose?()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This action cannot be undone.")
        }
```

### Step 4.4: Build and verify

- [ ] Run: `cd WatchtowerDesktop && swift build 2>&1 | tail -30`
- [ ] Expected: clean build.

### Step 4.5: Run the Desktop test suite

- [ ] Run: `cd WatchtowerDesktop && swift test 2>&1 | tail -20`
- [ ] Expected: all tests pass.

### Step 4.6: Manual UI smoke check

- [ ] Launch Watchtower Desktop.
- [ ] Open a target in the detail pane.
- [ ] Click the ellipsis icon in the tab bar → `Delete…` appears.
- [ ] Click it → confirmation dialog appears.
- [ ] Confirm → detail pane closes and target is gone from the list.
- [ ] Repeat and click Cancel → target remains, detail pane stays open.
- [ ] Verify the menu is visible for targets with status `done`, `dismissed`, and `snoozed` (not just active).

### Step 4.7: Commit

```bash
git add WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift
git commit -m "feat(desktop): add Delete action to TargetDetailView tab bar"
```

---

## Task 5: Final verification

### Step 5.1: Run the whole Go suite

- [ ] Run: `go test ./... -count=1 2>&1 | tail -20`
- [ ] Expected: all green.

### Step 5.2: Run the whole Swift suite

- [ ] Run: `cd WatchtowerDesktop && swift test 2>&1 | tail -20`
- [ ] Expected: all green (previously 490 tests, now at least 491 after Task 2).

### Step 5.3: Sanity check — the four expected behaviours

- [ ] CLI happy path: `watchtower targets create --text "temp"` then `watchtower targets delete <id>` → exit 0, output `Target #<id> removed`.
- [ ] CLI not found: `watchtower targets delete 999999` → exit 1, stderr contains `not found`.
- [ ] Desktop list: context menu → Delete… → confirm → target removed; detail pane closes if it was open.
- [ ] Desktop detail: ellipsis → Delete… → confirm → target removed; detail pane auto-closes.

### Step 5.4: Branch note

- [ ] Current branch is `feature/targets-ai-ui`. These commits land on top of the existing feature branch — no new branch unless the user asks for one. Do not merge or push without explicit user instruction.

---

## Out of scope (do not implement)

- Bulk delete / multi-select.
- Soft delete or trash bin.
- Undo toast.
- Cascading delete of children.
- Warning dialog when the target has children or links.
- CLI flags `--yes`, `--cascade`, `--dry-run`.
