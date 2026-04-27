# Targets — Delete

Date: 2026-04-24
Status: Approved (awaiting implementation plan)
Branch: `feature/targets-ai-ui` (continuation)

## Summary

Add the ability to delete targets end-to-end: a new `watchtower targets delete <id>` CLI command, and a confirmation-gated Delete action in the Desktop UI (both in the list context menu and in the detail view). Behaviour is symmetric across CLI and UI; the data-layer hard-delete is already implemented.

## Motivation

`targets done` and `targets dismiss` soft-complete a target but leave it in the database. Users who created a target in error, or who want to permanently discard auto-extracted entries, have no way to remove it. Today the Desktop UI has a half-wired `Delete` button in the list context menu that fires without confirmation and leaves stale `selectedItemID` behind. CLI has no delete command at all.

## Current State (audit)

- **Go DB layer** — `DB.DeleteTarget(id)` exists in `internal/db/targets.go:235`. It recomputes parent progress and relies on schema constraints for children/links.
  - `targets.parent_id` uses `ON DELETE SET NULL` — confirmed by `TestDeleteTarget_ParentIDSetNull` in `internal/db/targets_test.go:193`.
  - `target_links` uses `ON DELETE CASCADE` — confirmed by the `target_links ON DELETE CASCADE` block in `internal/db/targets_test.go:266`.
- **Go CLI** — `cmd/targets.go` has `unlink` (link removal) but no `delete` for the target itself. Only `done` / `dismiss` soft-completion.
- **Swift queries / VM** — `TargetQueries.delete(_:id:)` and `TargetsViewModel.deleteTarget(_:)` already exist.
- **Swift UI** — `TargetsListView.swift:326` already wires `Button("Delete", role: .destructive) { vm.deleteTarget(target) }` inside the context menu, but without a confirmation dialog and without clearing `selectedItemID`. `TargetDetailView` has no Delete affordance.

## Scope

In scope:
- Shared behaviour: hard-delete by id; children's `parent_id` is set to `NULL`; linked rows cascade; parent progress recomputed. All of this is already in the DB layer — no schema or Go-layer changes required.
- CLI: new `watchtower targets delete <id>` command (no prompt, optional `--json`).
- Desktop: confirmation dialog wrapped around existing context-menu action; new Delete action in `TargetDetailView` via an ellipsis menu in the tab bar; detail pane auto-closes after deletion.

Out of scope:
- Bulk delete, soft delete, undo, trash / recycle-bin semantics.
- Cascading delete of child targets (children orphan to root — existing DB behaviour).
- Warning dialog when a target has children or links (explicit product decision).
- CLI flags `--yes`, `--cascade`, `--dry-run`.

## Design

### 1. Semantics (unchanged data-layer contract)

Delete is a hard-delete driven by `DB.DeleteTarget(id)`. For a target `T`:

- `DELETE FROM targets WHERE id = T`.
- Every child `C` with `C.parent_id = T` gets `C.parent_id = NULL` via FK `ON DELETE SET NULL`. Children remain as root-level targets.
- Every row in `target_links` referencing `T` (as source or target) is removed via FK `ON DELETE CASCADE`.
- If `T.parent_id` was non-null, the parent's `progress` is recomputed by `RecomputeParentProgress` (inside `DeleteTarget`).
- No soft-delete flag is introduced. No undo.

This is the existing behaviour — the spec only surfaces it as the contract both UIs rely on.

### 2. CLI: `watchtower targets delete <id>`

Location: `cmd/targets.go` (alongside `targetsDoneCmd`, `targetsDismissCmd`).

Shape:

```
watchtower targets delete <id> [--json]
```

- `<id>` is a required positional integer (mirrors `done`/`dismiss`).
- No interactive prompt; no `--yes` flag. The command runs the DB delete and prints a single success line — same UX as `targets done <id>`.
- Errors:
  - Non-integer id → `cobra` ArgsError equivalent used elsewhere.
  - `id` not found → return `fmt.Errorf("target %d not found", id)`. Implementation fetches the target first via `GetTargetByID` (or equivalent) so the error is explicit instead of silently succeeding on a no-op DELETE.
- Output:
  - Default: `Target #<id> removed\n` to stdout.
  - `--json`: `{"id": <id>, "removed": true}` to stdout.
- Command registration: added to `init()` via `targetsCmd.AddCommand(targetsDeleteCmd)`.

### 3. Desktop UI — `TargetsListView` context menu

File: `WatchtowerDesktop/Sources/Views/Targets/TargetsListView.swift`.

Change the existing destructive button in `contextMenu(_:vm:)` so that it only requests deletion; the confirmation dialog is attached at the view level and performs the actual call.

- Add `@State private var pendingDeleteTarget: Target?` to `TargetsListView`.
- The context-menu button sets `pendingDeleteTarget = target` instead of calling `vm.deleteTarget` directly.
- Attach `.confirmationDialog(...)` to the outer `HStack` (or a dedicated view modifier) bound to `$pendingDeleteTarget` using the `isPresented` + `presenting:` pattern supplied by `confirmationDialog(_:presenting:actions:message:)`.
- On confirm:
  1. Call `vm.deleteTarget(pendingDeleteTarget)`.
  2. If `selectedItemID == pendingDeleteTarget.id`, set `selectedItemID = nil`.
  3. Clear `pendingDeleteTarget = nil`.
- On cancel: simply clear `pendingDeleteTarget`.

### 4. Desktop UI — `TargetDetailView` delete affordance

File: `WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift`.

- In the existing tab-bar `HStack` (lines ~42–55), insert a `Menu` with label `Image(systemName: "ellipsis.circle")` just before the `onClose` close button.
- The menu has one item: `Button("Delete…", role: .destructive) { showDeleteConfirm = true }`.
- Add `@State private var showDeleteConfirm = false`.
- Attach `.confirmationDialog(...)` bound to `$showDeleteConfirm` with the same text as the list variant.
- On confirm:
  1. `viewModel.deleteTarget(target)`.
  2. `onClose?()` — this is the existing close-callback provided by `TargetsListView` which already clears `selectedItemID`, so we reuse it instead of re-implementing state cleanup.

The delete action is visible for all statuses (active, done, dismissed, snoozed, blocked), unlike the `actionsSection` (Done/Dismiss/Snooze) which is gated on `target.isActive`. That asymmetry is intentional: deletion is valid at any time.

### 5. Confirmation dialog — shared copy

Both dialogs use identical text so behaviour is predictable:

- Title: `Delete "\(target.text.truncated(60))"?` (truncation helper — if a clean helper isn't already present, use a simple `String(target.text.prefix(60))` concatenation with an ellipsis when longer).
- Message: `This action cannot be undone.`
- Buttons:
  - `Delete` — `.destructive`, triggers the delete.
  - `Cancel` — `.cancel`.

Copy is English to match the rest of the Desktop UI.

### 6. Testing

Go:

- `cmd/targets_test.go` — add two cases:
  - `TestTargetsDeleteCmd_HappyPath`: create a target via `db.CreateTarget`, run `targets delete <id>`, assert exit 0, stdout matches `Target #<id> removed`, row no longer present in `targets`.
  - `TestTargetsDeleteCmd_NotFound`: run `targets delete 999999` on an empty DB, assert non-zero exit and the `not found` error message.
- The existing `DB.DeleteTarget` tests in `internal/db/targets_test.go` (parent_id set-null and link cascade) already cover the data-layer contract — no additional DB-level test needed.

Swift:

- `WatchtowerDesktop/Tests/ViewModelTests.swift` or `TargetTests.swift` (both already exist) — one test that inserts a target into an in-memory GRDB pool, calls `TargetsViewModel.deleteTarget(_:)`, and asserts the row is gone.
- No UI tests for the confirmation dialog; it is a thin presentation layer over the already-tested VM method.

### 7. Non-functional notes

- No schema changes. No migrations.
- No breaking change to `DB.DeleteTarget`'s signature.
- No new config flags.
- Localization: Desktop already ships English-only strings in this area; new strings follow the same convention.

## Risks & mitigations

- **Stale `selectedItemID` after context-menu delete** — fixed by the explicit `selectedItemID = nil` check in the confirm handler.
- **Double-click / double-tap in dialog** — SwiftUI's `confirmationDialog` is already debounced; the destructive handler runs at most once per presentation.
- **CLI scripts expecting silent success on missing id** — none known. New explicit not-found error is a minor UX upgrade consistent with other `targets` subcommands.
- **Inbox / briefing references to deleted targets** — targets are not referenced by inbox/briefing via FK; any cached mention by id will gracefully miss on lookup. Not a regression.

## Open questions

None.

## Deliverables

1. New CLI command `watchtower targets delete` with tests.
2. `TargetsListView` confirmation dialog + selection cleanup.
3. `TargetDetailView` ellipsis menu with `Delete…` and confirmation dialog.
4. Swift VM-level test covering delete.
5. No DB or schema changes; existing `DB.DeleteTarget` behaviour documented but unchanged.
