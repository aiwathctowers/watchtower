# Targets AI in Desktop UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore AI-driven target extraction ("Paste and extract") and link suggestion ("Suggest links") in the Desktop app by bridging Swift → Go CLI.

**Architecture:** Go CLI already has `watchtower targets extract` and `watchtower targets suggest-links` working, but they are interactive (stdin prompts). We add a `--json` flag to both that emits the result as JSON and skips prompts. Swift layer adds `TargetExtractService` and `TargetSuggestLinksService` using the existing `ProcessCLIRunner` pattern, decodes JSON into existing `ProposedTarget`/`ProposedLink` models, and wires two hidden UI buttons in `CreateTargetSheet.swift:63` and `TargetDetailView.swift:524`.

**Tech Stack:** Go 1.25 (cobra), Swift 5.10 (SwiftUI, async/await, JSONDecoder), macOS 14+.

---

## Out of Scope

- Changes to `internal/targets/` Go code (pipeline, extractor, linker) — already work end-to-end via CLI.
- New DB columns or migrations.
- Alternative entry points beyond `CreateTargetSheet` and `TargetDetailView` (inbox → targets flow, chat tool-call stay V2).
- Unit tests for new Swift views' SwiftUI bodies (follow repo convention — test services + view models only).

## File Structure

**Go — modify:**
- `cmd/targets.go` — add `--json` flag + JSON output path to `targetsExtractCmd` and `targetsSuggestLinksCmd`.
- `cmd/targets_test.go` — new tests for `--json` output (non-interactive, schema shape).

**Swift — create:**
- `WatchtowerDesktop/Sources/Services/TargetExtractService.swift` — `ProcessCLIRunner` wrapper that runs `watchtower targets extract --json` and decodes the result into `[ProposedTarget]` + omittedCount + notes.
- `WatchtowerDesktop/Sources/Services/TargetSuggestLinksService.swift` — same pattern for `watchtower targets suggest-links --json`.
- `WatchtowerDesktop/Sources/Views/Targets/SuggestLinksSheet.swift` — mini-dialog to review & apply AI-proposed parent + secondary links.
- `WatchtowerDesktop/Tests/TargetExtractServiceTests.swift` — happy path + malformed JSON + CLI failure.
- `WatchtowerDesktop/Tests/TargetSuggestLinksServiceTests.swift` — same shape.

**Swift — modify:**
- `WatchtowerDesktop/Sources/Views/Targets/CreateTargetSheet.swift` — add "Paste and extract" button (uncomment + wire), spinner state, sheet presentation of `ExtractPreviewSheet`.
- `WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift` — add "Suggest links" button, spinner state, sheet presentation of `SuggestLinksSheet`.
- `WatchtowerDesktop/Sources/Views/Targets/ExtractPreviewSheet.swift` — minor: add toast for `notes` if too long (already renders, just verify).

---

## Task 1: Go — `targets extract --json` non-interactive mode

**Files:**
- Modify: `cmd/targets.go:76-81` (flag registration), `cmd/targets.go:513-629` (`runTargetsExtract`)
- Test: `cmd/targets_test.go`

**Current behavior:** Command runs AI extraction, then prompts `[y/N]` per target on stdin and writes confirmed ones to DB.

**New behavior with `--json`:** Run AI extraction, print extracted result as JSON to stdout, do NOT prompt, do NOT write to DB. Caller (Desktop) writes via GRDB after user confirms in the sheet.

- [ ] **Step 1.1: Write the failing test**

Add to `cmd/targets_test.go` (create if absent). The test uses a mocked generator to verify JSON output shape — look at existing patterns in `cmd/targets_test.go` or `cmd/briefing_test.go` for how generator is injected. If injection is impractical, use a smaller assertion: verify the command has a `--json` flag registered.

```go
func TestTargetsExtractCmdHasJSONFlag(t *testing.T) {
    flag := targetsExtractCmd.Flags().Lookup("json")
    if flag == nil {
        t.Fatal("targets extract should have --json flag")
    }
    if flag.DefValue != "false" {
        t.Errorf("--json default should be false, got %q", flag.DefValue)
    }
}
```

- [ ] **Step 1.2: Run the test, verify fail**

```bash
go test ./cmd/ -run TestTargetsExtractCmdHasJSONFlag -v
```

Expected: FAIL — flag not defined.

- [ ] **Step 1.3: Add `--json` flag**

In `cmd/targets.go`, near line 55 add a new var:
```go
targetsFlagExtractJSON bool
```

In `init()` near line 213 add:
```go
targetsExtractCmd.Flags().BoolVar(&targetsFlagExtractJSON, "json", false, "output extracted targets as JSON (non-interactive; caller is responsible for persistence)")
```

- [ ] **Step 1.4: Run the test, verify pass**

```bash
go test ./cmd/ -run TestTargetsExtractCmdHasJSONFlag -v
```

Expected: PASS.

- [ ] **Step 1.5: Wire JSON output into `runTargetsExtract`**

After the call to `pipe.Extract(...)` returns `result` (line ~569), before the current interactive block (`if len(result.Extracted) == 0 { ... }`), add an early return:

```go
if targetsFlagExtractJSON {
    jsonOut := struct {
        Extracted    []jsonProposedTarget `json:"extracted"`
        OmittedCount int                  `json:"omitted_count"`
        Notes        string               `json:"notes"`
    }{
        Extracted:    toJSONProposedTargets(result.Extracted),
        OmittedCount: result.OmittedCount,
        Notes:        result.Notes,
    }
    enc := json.NewEncoder(out)
    enc.SetIndent("", "  ")
    return enc.Encode(jsonOut)
}
```

Below the existing `runTargetsExtract` function, add helpers:

```go
type jsonProposedTarget struct {
    Text              string              `json:"text"`
    Intent            string              `json:"intent"`
    Level             string              `json:"level"`
    CustomLabel       string              `json:"custom_label"`
    PeriodStart       string              `json:"period_start"`
    PeriodEnd         string              `json:"period_end"`
    Priority          string              `json:"priority"`
    DueDate           string              `json:"due_date"`
    ParentID          *int64              `json:"parent_id"`
    AILevelConfidence *float64            `json:"ai_level_confidence"`
    SecondaryLinks    []jsonProposedLink  `json:"secondary_links"`
}

type jsonProposedLink struct {
    TargetID    *int64   `json:"target_id"`
    ExternalRef string   `json:"external_ref"`
    Relation    string   `json:"relation"`
    Confidence  *float64 `json:"confidence"`
}

func toJSONProposedTargets(items []targets.ProposedTarget) []jsonProposedTarget {
    out := make([]jsonProposedTarget, 0, len(items))
    for _, pt := range items {
        j := jsonProposedTarget{
            Text:        pt.Text,
            Intent:      pt.Intent,
            Level:       pt.Level,
            CustomLabel: pt.CustomLabel,
            PeriodStart: pt.PeriodStart,
            PeriodEnd:   pt.PeriodEnd,
            Priority:    pt.Priority,
            DueDate:     pt.DueDate,
        }
        if pt.ParentID.Valid {
            pid := pt.ParentID.Int64
            j.ParentID = &pid
        }
        if pt.AILevelConfidence.Valid {
            c := pt.AILevelConfidence.Float64
            j.AILevelConfidence = &c
        }
        for _, l := range pt.SecondaryLinks {
            jl := jsonProposedLink{
                ExternalRef: l.ExternalRef,
                Relation:    l.Relation,
            }
            if l.TargetID.Valid {
                tid := l.TargetID.Int64
                jl.TargetID = &tid
            }
            if l.Confidence.Valid {
                c := l.Confidence.Float64
                jl.Confidence = &c
            }
            j.SecondaryLinks = append(j.SecondaryLinks, jl)
        }
        out = append(out, j)
    }
    return out
}
```

- [ ] **Step 1.6: Build**

```bash
make build
```

Expected: build succeeds.

- [ ] **Step 1.7: Smoke-test manually**

```bash
./watchtower targets extract --text "hello world" --json 2>/dev/null | head -5
```

Expected: JSON on stdout, no prompt, exit 0 (even if AI returns empty extracted).

- [ ] **Step 1.8: Commit**

```bash
git add cmd/targets.go cmd/targets_test.go
git commit -m "feat(cmd): add --json flag to targets extract for Desktop bridge"
```

---

## Task 2: Go — `targets suggest-links --json` non-interactive mode

**Files:**
- Modify: `cmd/targets.go:97-102` (flag registration), `cmd/targets.go:710-810` (`runTargetsSuggestLinks`)
- Test: `cmd/targets_test.go`

**Current behavior:** Runs `pipe.LinkExisting(id)`, prints proposed parent + links, then prompts `[y/N]` and applies to DB on yes.

**New behavior with `--json`:** Print the LinkResult as JSON to stdout, do NOT prompt, do NOT apply. Caller persists the chosen subset via GRDB.

- [ ] **Step 2.1: Write the failing test**

```go
func TestTargetsSuggestLinksCmdHasJSONFlag(t *testing.T) {
    flag := targetsSuggestLinksCmd.Flags().Lookup("json")
    if flag == nil {
        t.Fatal("targets suggest-links should have --json flag")
    }
}
```

- [ ] **Step 2.2: Run test, verify fail**

```bash
go test ./cmd/ -run TestTargetsSuggestLinksCmdHasJSONFlag -v
```

Expected: FAIL.

- [ ] **Step 2.3: Add `--json` flag**

Near the other `targetsFlag*` declarations:
```go
targetsFlagSuggestLinksJSON bool
```

In `init()`:
```go
targetsSuggestLinksCmd.Flags().BoolVar(&targetsFlagSuggestLinksJSON, "json", false, "output suggested links as JSON (non-interactive)")
```

- [ ] **Step 2.4: Verify test passes**

```bash
go test ./cmd/ -run TestTargetsSuggestLinksCmdHasJSONFlag -v
```

Expected: PASS.

- [ ] **Step 2.5: Wire JSON output into `runTargetsSuggestLinks`**

In `runTargetsSuggestLinks`, after the `pipe.LinkExisting(ctx, int64(id))` returns `result` (line ~740), before the text-printing and prompting block, add:

```go
if targetsFlagSuggestLinksJSON {
    jsonOut := struct {
        ParentID       *int64             `json:"parent_id"`
        SecondaryLinks []jsonProposedLink `json:"secondary_links"`
    }{
        SecondaryLinks: make([]jsonProposedLink, 0, len(result.SecondaryLinks)),
    }
    if result.ParentID.Valid {
        pid := result.ParentID.Int64
        jsonOut.ParentID = &pid
    }
    for _, l := range result.SecondaryLinks {
        jl := jsonProposedLink{
            ExternalRef: l.ExternalRef,
            Relation:    l.Relation,
        }
        if l.TargetID.Valid {
            tid := l.TargetID.Int64
            jl.TargetID = &tid
        }
        if l.Confidence.Valid {
            c := l.Confidence.Float64
            jl.Confidence = &c
        }
        jsonOut.SecondaryLinks = append(jsonOut.SecondaryLinks, jl)
    }
    enc := json.NewEncoder(cmd.OutOrStdout())
    enc.SetIndent("", "  ")
    return enc.Encode(jsonOut)
}
```

- [ ] **Step 2.6: Build**

```bash
make build
```

Expected: succeeds.

- [ ] **Step 2.7: Commit**

```bash
git add cmd/targets.go cmd/targets_test.go
git commit -m "feat(cmd): add --json flag to targets suggest-links for Desktop bridge"
```

---

## Task 3: Swift — `TargetExtractService`

**Files:**
- Create: `WatchtowerDesktop/Sources/Services/TargetExtractService.swift`
- Test: `WatchtowerDesktop/Tests/TargetExtractServiceTests.swift`

- [ ] **Step 3.1: Write the failing test**

```swift
// WatchtowerDesktop/Tests/TargetExtractServiceTests.swift
import XCTest
@testable import WatchtowerDesktop

final class TargetExtractServiceTests: XCTestCase {
    // MARK: - Happy path

    func testExtractParsesJSONIntoProposedTargets() async throws {
        let json = """
        {
          "extracted": [
            {
              "text": "Write onboarding docs",
              "intent": "",
              "level": "week",
              "custom_label": "",
              "period_start": "2026-04-24",
              "period_end": "2026-04-30",
              "priority": "high",
              "due_date": "",
              "parent_id": null,
              "ai_level_confidence": 0.82,
              "secondary_links": []
            }
          ],
          "omitted_count": 2,
          "notes": "skipped duplicates"
        }
        """
        let runner = StubCLIRunner(stdout: Data(json.utf8))
        let service = TargetExtractService(runner: runner)

        let result = try await service.extract(text: "sample")

        XCTAssertEqual(result.extracted.count, 1)
        XCTAssertEqual(result.extracted[0].text, "Write onboarding docs")
        XCTAssertEqual(result.extracted[0].level, "week")
        XCTAssertEqual(result.extracted[0].levelConfidence, 0.82, accuracy: 0.001)
        XCTAssertEqual(result.omittedCount, 2)
        XCTAssertEqual(result.notes, "skipped duplicates")
    }

    // MARK: - CLI failure

    func testExtractPropagatesCLIError() async {
        let runner = StubCLIRunner(
            error: CLIRunnerError.nonZeroExit(code: 1, stderr: "boom")
        )
        let service = TargetExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "sample")
            XCTFail("expected error")
        } catch {
            // OK
        }
    }

    // MARK: - Malformed JSON

    func testExtractThrowsOnMalformedJSON() async {
        let runner = StubCLIRunner(stdout: Data("not json".utf8))
        let service = TargetExtractService(runner: runner)
        do {
            _ = try await service.extract(text: "sample")
            XCTFail("expected decoding error")
        } catch {
            // OK
        }
    }
}

// MARK: - Stub runner

private final class StubCLIRunner: CLIRunnerProtocol {
    let stdout: Data
    let error: Error?
    var capturedArgs: [String] = []

    init(stdout: Data = Data(), error: Error? = nil) {
        self.stdout = stdout
        self.error = error
    }

    func run(args: [String]) async throws -> Data {
        capturedArgs = args
        if let error { throw error }
        return stdout
    }
}
```

- [ ] **Step 3.2: Run tests, verify compile failure**

```bash
cd WatchtowerDesktop && swift test --filter TargetExtractServiceTests 2>&1 | tail -20
```

Expected: compile error — `TargetExtractService` undefined.

- [ ] **Step 3.3: Implement the service**

```swift
// WatchtowerDesktop/Sources/Services/TargetExtractService.swift
import Foundation

struct TargetExtractResult {
    var extracted: [ProposedTarget]
    var omittedCount: Int
    var notes: String
}

/// Bridges the Desktop app to `watchtower targets extract --json` subprocess.
struct TargetExtractService {
    let runner: CLIRunnerProtocol

    func extract(text: String, sourceRef: String = "") async throws -> TargetExtractResult {
        var args = ["targets", "extract", "--json", "--text", text]
        if !sourceRef.isEmpty {
            args.append(contentsOf: ["--source-ref", sourceRef])
        }
        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLIExtractResponse.self, from: data)

        let proposed = decoded.extracted.map { item in
            ProposedTarget(
                text: item.text,
                intent: item.intent,
                level: item.level,
                customLabel: item.custom_label,
                levelConfidence: item.ai_level_confidence,
                periodStart: item.period_start,
                periodEnd: item.period_end,
                priority: item.priority.isEmpty ? "medium" : item.priority,
                parentId: item.parent_id.map { Int($0) },
                secondaryLinks: (item.secondary_links ?? []).map { l in
                    ProposedLink(
                        targetId: l.target_id.map { Int($0) },
                        externalRef: l.external_ref,
                        relation: l.relation
                    )
                }
            )
        }

        return TargetExtractResult(
            extracted: proposed,
            omittedCount: decoded.omitted_count,
            notes: decoded.notes
        )
    }
}

// MARK: - Wire JSON schema

// Mirrors the JSON shape emitted by `watchtower targets extract --json` (see cmd/targets.go).
private struct CLIExtractResponse: Decodable {
    let extracted: [CLIExtractedItem]
    let omitted_count: Int
    let notes: String
}

private struct CLIExtractedItem: Decodable {
    let text: String
    let intent: String
    let level: String
    let custom_label: String
    let period_start: String
    let period_end: String
    let priority: String
    let due_date: String
    let parent_id: Int64?
    let ai_level_confidence: Double?
    let secondary_links: [CLISecondaryLink]?
}

private struct CLISecondaryLink: Decodable {
    let target_id: Int64?
    let external_ref: String
    let relation: String
    let confidence: Double?
}
```

- [ ] **Step 3.4: Run tests, verify pass**

```bash
cd WatchtowerDesktop && swift test --filter TargetExtractServiceTests 2>&1 | tail -10
```

Expected: all PASS.

- [ ] **Step 3.5: Commit**

```bash
git add WatchtowerDesktop/Sources/Services/TargetExtractService.swift \
        WatchtowerDesktop/Tests/TargetExtractServiceTests.swift
git commit -m "feat(desktop): add TargetExtractService bridging CLI targets extract --json"
```

---

## Task 4: Swift — `TargetSuggestLinksService`

**Files:**
- Create: `WatchtowerDesktop/Sources/Services/TargetSuggestLinksService.swift`
- Test: `WatchtowerDesktop/Tests/TargetSuggestLinksServiceTests.swift`

- [ ] **Step 4.1: Write the failing test**

```swift
// WatchtowerDesktop/Tests/TargetSuggestLinksServiceTests.swift
import XCTest
@testable import WatchtowerDesktop

final class TargetSuggestLinksServiceTests: XCTestCase {
    func testParsesParentAndSecondaryLinks() async throws {
        let json = """
        {
          "parent_id": 12,
          "secondary_links": [
            {"target_id": null, "external_ref": "jira:PROJ-7", "relation": "blocks", "confidence": 0.65},
            {"target_id": 33, "external_ref": "", "relation": "related", "confidence": null}
          ]
        }
        """
        let runner = StubCLIRunner(stdout: Data(json.utf8))
        let service = TargetSuggestLinksService(runner: runner)

        let result = try await service.suggest(targetID: 99)

        XCTAssertEqual(result.parentID, 12)
        XCTAssertEqual(result.secondaryLinks.count, 2)
        XCTAssertEqual(result.secondaryLinks[0].relation, "blocks")
        XCTAssertEqual(result.secondaryLinks[0].externalRef, "jira:PROJ-7")
        XCTAssertEqual(result.secondaryLinks[1].targetID, 33)
    }

    func testHandlesEmptyResult() async throws {
        let json = "{\"parent_id\": null, \"secondary_links\": []}"
        let runner = StubCLIRunner(stdout: Data(json.utf8))
        let service = TargetSuggestLinksService(runner: runner)

        let result = try await service.suggest(targetID: 1)

        XCTAssertNil(result.parentID)
        XCTAssertTrue(result.secondaryLinks.isEmpty)
    }
}

// Reuse `StubCLIRunner` from TargetExtractServiceTests.swift if tests share a target;
// otherwise duplicate the small stub here.
```

- [ ] **Step 4.2: Run tests, verify fail**

```bash
cd WatchtowerDesktop && swift test --filter TargetSuggestLinksServiceTests 2>&1 | tail -10
```

Expected: compile error — type undefined.

- [ ] **Step 4.3: Implement the service**

```swift
// WatchtowerDesktop/Sources/Services/TargetSuggestLinksService.swift
import Foundation

struct SuggestedLinksResult {
    var parentID: Int?
    var secondaryLinks: [ProposedLink]
}

struct TargetSuggestLinksService {
    let runner: CLIRunnerProtocol

    func suggest(targetID: Int) async throws -> SuggestedLinksResult {
        let args = ["targets", "suggest-links", "\(targetID)", "--json"]
        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLISuggestLinksResponse.self, from: data)
        let links = (decoded.secondary_links ?? []).map { l in
            ProposedLink(
                targetId: l.target_id.map { Int($0) },
                externalRef: l.external_ref,
                relation: l.relation
            )
        }
        return SuggestedLinksResult(
            parentID: decoded.parent_id.map { Int($0) },
            secondaryLinks: links
        )
    }
}

private struct CLISuggestLinksResponse: Decodable {
    let parent_id: Int64?
    let secondary_links: [CLISuggestedLink]?
}

private struct CLISuggestedLink: Decodable {
    let target_id: Int64?
    let external_ref: String
    let relation: String
    let confidence: Double?
}
```

- [ ] **Step 4.4: Run tests, verify pass**

```bash
cd WatchtowerDesktop && swift test --filter TargetSuggestLinksServiceTests 2>&1 | tail -10
```

Expected: PASS.

- [ ] **Step 4.5: Commit**

```bash
git add WatchtowerDesktop/Sources/Services/TargetSuggestLinksService.swift \
        WatchtowerDesktop/Tests/TargetSuggestLinksServiceTests.swift
git commit -m "feat(desktop): add TargetSuggestLinksService bridging CLI suggest-links --json"
```

---

## Task 5: Wire "Paste and extract" into `CreateTargetSheet`

**Files:**
- Modify: `WatchtowerDesktop/Sources/Views/Targets/CreateTargetSheet.swift` (lines 22-23, 63, add spinner + sheet state + callback)

- [ ] **Step 5.1: Replace commented-out state with live state**

Change lines 22-24 from:
```swift
// V1: hidden — pending Swift→Go CLI bridge (see spec "Out of Scope V2")
// @State private var showExtractSheet = false
// @State private var highlightExtract: Bool = false
```

to:
```swift
@State private var showExtractSheet = false
@State private var extractedResult: TargetExtractResult?
@State private var isExtracting = false
```

- [ ] **Step 5.2: Replace the hidden-button comment with the actual button**

In `formContent` (around line 63), replace:
```swift
// V1: "Paste and extract" button hidden — pending Swift→Go CLI bridge (see spec "Out of Scope V2")
```
with:
```swift
extractButton
```

Add below `textField`:

```swift
@ViewBuilder
private var extractButton: some View {
    HStack {
        Button {
            Task { await runExtract() }
        } label: {
            if isExtracting {
                HStack(spacing: 6) {
                    ProgressView().controlSize(.small)
                    Text("Extracting…")
                }
            } else {
                Label("Paste and extract", systemImage: "sparkles")
            }
        }
        .disabled(isExtracting || text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        Spacer()
    }
}
```

- [ ] **Step 5.3: Add the async extract method + sheet presentation**

Append in the MARK: - Create section (below `createTarget()`):

```swift
private func runExtract() async {
    guard let runner = ProcessCLIRunner.makeDefault() else {
        errorMessage = "watchtower CLI not found in PATH"
        return
    }
    isExtracting = true
    errorMessage = nil
    defer { isExtracting = false }
    do {
        let service = TargetExtractService(runner: runner)
        let result = try await service.extract(text: text)
        extractedResult = result
        showExtractSheet = true
    } catch {
        errorMessage = error.localizedDescription
    }
}
```

Attach a `.sheet` to the root `VStack` (after `.onAppear`):

```swift
.sheet(isPresented: $showExtractSheet) {
    if let result = extractedResult {
        ExtractPreviewSheet(
            proposed: result.extracted,
            omittedCount: result.omittedCount,
            notes: result.notes,
            onCreateSelected: { _ in
                // Sheet writes to DB itself; close the create sheet too.
                dismiss()
            }
        )
    }
}
```

- [ ] **Step 5.4: Build**

```bash
cd WatchtowerDesktop && swift build 2>&1 | tail -20
```

Expected: succeeds with no errors.

- [ ] **Step 5.5: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Targets/CreateTargetSheet.swift
git commit -m "feat(desktop): wire 'Paste and extract' in CreateTargetSheet to TargetExtractService"
```

---

## Task 6: Wire "Suggest links" into `TargetDetailView`

**Files:**
- Modify: `WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift` (around line 28 and line 524)
- Create: `WatchtowerDesktop/Sources/Views/Targets/SuggestLinksSheet.swift`

- [ ] **Step 6.1: Read current state of TargetDetailView**

Examine lines 20-40 for existing state vars and lines 520-560 for the comment + where to place the button.

- [ ] **Step 6.2: Add state vars and unhide the button**

Near other `@State` vars (around line 28), add:
```swift
@State private var showSuggestLinksSheet = false
@State private var suggestedLinks: SuggestedLinksResult?
@State private var isSuggestingLinks = false
@State private var suggestLinksError: String?
```

Around line 524, replace the comment `// V1: "Suggest links" button hidden — pending Swift→Go CLI bridge (see spec "Out of Scope V2")` with:

```swift
HStack {
    Button {
        Task { await runSuggestLinks() }
    } label: {
        if isSuggestingLinks {
            HStack(spacing: 6) {
                ProgressView().controlSize(.small)
                Text("Suggesting…")
            }
        } else {
            Label("Suggest links", systemImage: "sparkles")
        }
    }
    .disabled(isSuggestingLinks)
    if let suggestLinksError {
        Text(suggestLinksError)
            .font(.caption)
            .foregroundStyle(.red)
    }
    Spacer()
}
```

- [ ] **Step 6.3: Add the async method**

In the body of `TargetDetailView`, add (near `navigate...` helpers):

```swift
private func runSuggestLinks() async {
    guard let runner = ProcessCLIRunner.makeDefault() else {
        suggestLinksError = "watchtower CLI not found in PATH"
        return
    }
    isSuggestingLinks = true
    suggestLinksError = nil
    defer { isSuggestingLinks = false }
    do {
        let service = TargetSuggestLinksService(runner: runner)
        let result = try await service.suggest(targetID: target.id)
        if result.parentID == nil && result.secondaryLinks.isEmpty {
            suggestLinksError = "AI had no suggestions"
            return
        }
        suggestedLinks = result
        showSuggestLinksSheet = true
    } catch {
        suggestLinksError = error.localizedDescription
    }
}
```

Attach the sheet at the view root:
```swift
.sheet(isPresented: $showSuggestLinksSheet) {
    if let suggestedLinks {
        SuggestLinksSheet(
            targetID: target.id,
            suggestions: suggestedLinks,
            onApplied: { /* no-op: GRDB ValueObservation refreshes links tab */ }
        )
    }
}
```

- [ ] **Step 6.4: Create `SuggestLinksSheet`**

```swift
// WatchtowerDesktop/Sources/Views/Targets/SuggestLinksSheet.swift
import SwiftUI

struct SuggestLinksSheet: View {
    @Environment(AppState.self) private var appState
    @Environment(\.dismiss) private var dismiss

    let targetID: Int
    @State var suggestions: SuggestedLinksResult
    var onApplied: () -> Void = {}

    @State private var applyParent: Bool = true
    @State private var selectedLinks: Set<Int> = []
    @State private var errorMessage: String?

    init(targetID: Int, suggestions: SuggestedLinksResult, onApplied: @escaping () -> Void = {}) {
        self.targetID = targetID
        self._suggestions = State(initialValue: suggestions)
        self.onApplied = onApplied
        self._selectedLinks = State(initialValue: Set(suggestions.secondaryLinks.indices))
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            ScrollView { content.padding() }
            Divider()
            footer
        }
        .frame(width: 480, height: 420)
    }

    private var header: some View {
        HStack {
            Text("Suggested links")
                .font(.headline)
            Spacer()
            Button("Cancel") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .padding()
    }

    @ViewBuilder
    private var content: some View {
        if let parentID = suggestions.parentID {
            VStack(alignment: .leading, spacing: 6) {
                Toggle(isOn: $applyParent) {
                    Text("Set parent to target #\(parentID)")
                        .font(.callout)
                }
            }
        }

        if !suggestions.secondaryLinks.isEmpty {
            Divider().padding(.vertical, 4)
            Text("Secondary links")
                .font(.subheadline)
                .fontWeight(.medium)
            ForEach(Array(suggestions.secondaryLinks.enumerated()), id: \.offset) { idx, link in
                HStack(spacing: 8) {
                    Toggle("", isOn: Binding(
                        get: { selectedLinks.contains(idx) },
                        set: { on in
                            if on { selectedLinks.insert(idx) } else { selectedLinks.remove(idx) }
                        }
                    ))
                    .labelsHidden()
                    Text(link.relation.replacingOccurrences(of: "_", with: " "))
                        .font(.caption)
                    if let tid = link.targetId {
                        Text("→ target #\(tid)")
                            .font(.caption)
                            .fontWeight(.semibold)
                    } else if !link.externalRef.isEmpty {
                        Text("→ \(link.externalRef)")
                            .font(.caption)
                            .fontWeight(.semibold)
                    }
                    Spacer()
                }
            }
        }

        if let errorMessage {
            Text(errorMessage).foregroundStyle(.red).font(.caption)
        }
    }

    private var footer: some View {
        HStack {
            Spacer()
            Button("Apply") { apply() }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
                .disabled(!hasAnythingToApply)
        }
        .padding()
    }

    private var hasAnythingToApply: Bool {
        (suggestions.parentID != nil && applyParent) || !selectedLinks.isEmpty
    }

    private func apply() {
        guard let db = appState.databaseManager else {
            errorMessage = "Database not available"
            return
        }
        do {
            try db.dbPool.write { dbConn in
                if applyParent, let parentID = suggestions.parentID {
                    try dbConn.execute(
                        sql: "UPDATE targets SET parent_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id = ?",
                        arguments: [parentID, targetID]
                    )
                }
                for idx in selectedLinks.sorted() {
                    let link = suggestions.secondaryLinks[idx]
                    try dbConn.execute(
                        sql: """
                            INSERT OR IGNORE INTO target_links
                              (source_target_id, target_target_id, external_ref, relation, created_by)
                            VALUES (?, ?, ?, ?, 'ai')
                            """,
                        arguments: [targetID, link.targetId, link.externalRef, link.relation]
                    )
                }
            }
            onApplied()
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
```

- [ ] **Step 6.5: Build**

```bash
cd WatchtowerDesktop && swift build 2>&1 | tail -20
```

Expected: succeeds.

- [ ] **Step 6.6: Commit**

```bash
git add WatchtowerDesktop/Sources/Views/Targets/TargetDetailView.swift \
        WatchtowerDesktop/Sources/Views/Targets/SuggestLinksSheet.swift
git commit -m "feat(desktop): wire 'Suggest links' in TargetDetailView via CLI bridge"
```

---

## Task 7: Full build + test sweep

- [ ] **Step 7.1: Go tests**

```bash
make test
```

Expected: all pass.

- [ ] **Step 7.2: Swift tests**

```bash
cd WatchtowerDesktop && swift test
```

Expected: all pass (499+ existing + new service tests).

- [ ] **Step 7.3: Lint**

```bash
make lint-all
```

Expected: no new warnings.

- [ ] **Step 7.4: Manual smoke test**

1. `make build && make install` (or use existing built binary).
2. Launch the Desktop app (`./WatchtowerDesktop/.build/debug/WatchtowerDesktop` or via Xcode).
3. Open Targets → "New Target" → paste multi-line text with action items → click "Paste and extract" → verify spinner, preview sheet, create selected.
4. Open an existing target → Links tab → click "Suggest links" → verify spinner, suggestion sheet, apply.
5. Verify failures surface clearly: disconnect network / kill `claude` CLI — confirm an error message appears in-sheet, no crash.

---

## Self-Review Checklist

- Spec alignment: restores V1 out-of-scope AI UI paths without touching migration, DB, or AI prompts.
- No placeholders — every step has concrete code or commands.
- Type names consistent: `TargetExtractResult`, `SuggestedLinksResult`, `TargetExtractService`, `TargetSuggestLinksService`, `ProposedTarget` (existing), `ProposedLink` (existing).
- JSON schema mirror: Go `jsonProposedTarget` fields ↔ Swift `CLIExtractedItem` Decodable — snake_case everywhere.
- No DB schema changes. `target_links` insert uses existing `created_by='ai'` path already used by `ExtractPreviewSheet` (see `ExtractPreviewSheet.swift:186`).

## Rollback

If anything regresses in the target CRUD flow:
```bash
git revert <commit-range-start>..<commit-range-end>
```
No migrations touched, so revert is clean.
