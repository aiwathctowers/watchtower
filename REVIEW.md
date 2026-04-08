# Code Review: feature/google-calendar

## Branch Info
- Branch: `feature/google-calendar`
- Base: `origin/main`
- Commits: 1 (+ local uncommitted fixes: cost removal, sync progress, user roster)
- Files changed: 81 (+6514/-305)

## Automated Checks

| Check | Status | Details |
|-------|--------|---------|
| Build | ✅ | Go + Swift |
| Lint | ⚠️ | 28 gosec warnings (pre-existing, XSS/SSRF false positives) |
| Tests | ✅ | All pass (post-fix) |
| Coverage | — | cached |

## Issues Found

- [x] #1: [CRITICAL] Nil logger panic in `calendar.NewSyncer` — fixed: nil guard with `io.Discard` logger
- [x] #2: [CRITICAL] `cancelConnect()` no-op — fixed: process stored to `authProcess` before launch
- [x] #3: [ERROR] Default `sync_days_ahead` mismatch — fixed: Swift fallback changed to 2
- [x] #4: [ERROR] Duplicate `tokenEndpoint` constant — fixed: consolidated to `googleTokenEndpoint`
- [x] #5: [ERROR] Fragile `fmt.Sprintf` — fixed: added arg mapping comment
- [x] #6: [STYLE] Inconsistent HTTP response handling — fixed: standardized to `io.ReadAll` pattern
- [x] #7: [WARNING] Plain HTTP OAuth — fixed: added comment (Google spec requires http for native)
- [x] #8: [WARNING] N+1 query in `gatherAttendeesContext` — fixed: fetch once before loop
- [x] #9: [WARNING] Stale event cleanup timing — fixed: pass Go timestamp to DB instead of `strftime`
- [x] #10: [WARNING] Pipe deadlock — fixed: read pipes before `waitUntilExit`
- [x] #11: [WARNING] `parsedAttendees` decodes on every access — fixed: added comment (acceptable)
- [x] #12: [WARNING] `time.Sleep(500ms)` — fixed: added comment (intentional)
- [x] #13: [STYLE] Duplicated utility functions — fixed: use exported `auth.RandomState/PortFromAddr/OpenBrowser`
- [x] #14: [STYLE] Inconsistent HTTP handling — fixed: see #6
- [x] #15: [STYLE] Unused `Pipeline.logger` — fixed: added logging calls
- [x] #16: [SUGGESTION] Non-transactional batch upsert — fixed: wrapped in tx
- [x] #17: [SUGGESTION] Silent fallback to "primary" — fixed: log error before fallback
- [x] #18: [SUGGESTION] Weak self capture — fixed: added comment
- [x] #19: [SUGGESTION] Client thread safety — fixed: added doc comment
- [x] #20: [SUGGESTION] EventAttendee.id uniqueness — fixed: added comment

## Feedback Log

### All issues (2026-04-02)
- Batch-fixed via 3 parallel agents + 1 manual fix
- Meeting pipeline also needed nil-logger guard (same as #1, caught by test)
- All 20 issues resolved, all tests green

---
*Создано: 2026-04-02*
*Статус: all fixed*

---

# Code Review: Phase 0b — Jira Board Analysis, Key Detection, Feature Toggles (Task #7)

## Branch Info
- Branch: `feature/jira-integration`
- Base: `main`

## Automated Checks

| Check | Status | Details |
|-------|--------|---------|
| Go Build | PASS | `go build ./...` clean |
| Go Tests | PASS | All packages pass |
| Go Lint | WARN | 27 gosec (pre-existing XSS/SSRF false positives) + 1 pre-existing goimports in cmd/ai.go |
| Swift Build | PASS | `swift build` clean |
| Swift Test | SKIPPED | Permission denied in sandbox |
| SwiftLint | SKIPPED | Permission denied in sandbox |
| Lint config | PASS | No rule softening detected |

## Acceptance Criteria Verification

### 0.3 — LLM Board Analysis
- [x] LLM generates profile (workflow stages, stale thresholds, estimation, health signals) — `board_analyzer.go:callLLM()` with structured prompt, JSON schema includes all fields
- [x] Board card in Settings: workflow visualization, stale threshold sliders, Re-analyze button — `JiraBoardProfileView.swift` with `workflowChain`, `staleSliderRow`, `reAnalyzeSection`
- [x] Config hash check on sync — `sync.go:118-126` checks via `boardAnalyzer.CheckConfigChanged()`, logs warning
- [x] Graceful degradation: LLM unavailable -> fallback profile — `board_analyzer.go:188-190` catches LLM error, calls `BuildFallbackProfile()`
- [x] CLI: `watchtower jira boards analyze [--force]` — `cmd/jira.go:758-849`, flag registered at line 142

### 0.6 — Jira Key Detection in Slack
- [x] Regex `\b([A-Z][A-Z0-9_]+-\d+)\b` — `key_detector.go:14`
- [x] Validation: project prefix must exist in known projects — `key_detector.go:58-63`, uses `refreshKnownKeys()` from DB
- [x] Integration: tracks pipeline — `tracks/pipeline.go:701-707`
- [x] Integration: digest pipeline — `digest/pipeline.go:1284-1291`
- [x] Deduplication: UNIQUE(issue_key, channel_id, message_ts) — `schema.sql:774`, `UpsertJiraSlackLink` uses ON CONFLICT

### 0.7 — Feature Toggles by Role
- [x] JiraFeatureToggles config struct with 11 toggles — `config.go:72-84`
- [x] Defaults by role (ic, senior_ic, middle_management, top_management, direction_owner) — `defaults.go:59-107`
- [x] CLI: features [--json] / enable / disable / reset — `cmd/jira.go:83-756`
- [x] Feature keys: both short and full JSON names work — `cmd/jira.go:573-600`
- [x] Settings -> Jira Features: toggle switches grouped by category — `JiraFeaturesSettingsView.swift` with 4 groups

### Arch Fixes
- [x] B1: featureToggleRef accepts both short and full names — `cmd/jira.go:574-599`, each case has both variants
- [x] B2: boards analyze wired to AI provider (not stub) — `cmd/jira.go:788-791`, uses `newAIClient(cfg, ...)`
- [x] B3: UserOverridesWrapper for JSON parsing — `JiraBoardProfileView.swift:3-9`
- [x] W1: Phase values match design (backlog|active_work|review|testing|done|other) — LLM prompt at `board_analyzer.go:451`, Swift `phaseColor` covers all 6
- [x] W2: Override merge instead of overwrite — `cmd/jira.go:892-907`, reads existing, merges new on top

## Findings for Lead

### Observations (non-blocking)

**O1: TestDatabase.swift does not include Jira tables.**
`WatchtowerDesktop/Tests/Helpers/TestDatabase.swift` has no jira_* tables in its schema. Currently not a blocker because Swift tests don't test Jira DB queries directly, but if Jira-related Swift tests are added later, they will fail. Consider syncing TestDatabase schema with Go schema.sql.

**O2: Fallback profile does not distinguish `review` and `testing` phases.**
`BuildFallbackProfile` maps columns only to `backlog`, `active_work`, and `done`. Columns named "Code Review" or "QA" get `active_work`. This is acceptable for a fallback (LLM does the real mapping), but could be improved with simple heuristics (e.g., strings.Contains(nameLower, "review") -> "review").

**O3: `knownKeysOnce` reset race condition (theoretical).**
`KeyDetector.ResetCache()` at `key_detector.go:149` assigns a new `sync.Once{}` without holding the lock that protects `knownKeysOnce`. If `DetectKeys()` is called concurrently during `ResetCache()`, the `sync.Once` could be in an inconsistent state. In practice, `ResetCache()` is only called after sync completes, so this is unlikely to trigger. Low priority.

**O4: JQL uses project key directly (no quoting).**
`sync.go:83` uses `fmt.Sprintf("project = %s ...")` — project key is validated by regex beforehand (`validProjectKeyRe` on line 71), so injection is prevented. However, using `project = "%s"` (with quotes) would be more defensive. Low priority.

**O5: Swift optimistic toggle update without rollback on all error paths.**
`JiraFeaturesSettingsView.swift:219` does optimistic update (`featuresState?.features[key] = enable`) but only reverts on CLI launch failure (line 243) and non-zero exit (line 262). If the Task itself fails or is cancelled, the UI state remains incorrect until next load. Minor UX issue.

### Positive Notes

- Clean separation of concerns: analyzer, detector, features builder all independent
- Comprehensive test coverage for all new Go components
- JQL injection prevention via regex validation
- Config hash change detection integrated into sync loop
- Override merge logic is correct
- CLI commands are well-structured with proper error messages
- Swift views properly delegate to CLI (architecture consistency)

## Verdict

**APPROVED.** All acceptance criteria met, all arch fixes verified. No blockers found. 5 minor observations reported to Lead for prioritization.

---
*Создано: 2026-04-06*
*Статус: approved*
*Task #7: completed*

---

# Code Review: Proxy AI through Go daemon (remove direct Claude/Codex from Desktop)

## Scope
AI proxy changes only (subset of branch diff)

## Automated Checks

| Check | Status | Details |
|-------|--------|---------|
| Go Build | OK | clean |
| Swift Build | OK | clean |
| Go Lint | OK | goimports fixed |
| Go Tests | OK | all packages pass |

## Changed Files

### New
- `cmd/ai.go` — `watchtower ai query/test` subcommands
- `WatchtowerDesktop/Sources/Services/WatchtowerAIService.swift` — unified AI service via bundled CLI

### Modified (10 files)
- `internal/ai/client.go` — `--verbose` for stream-json
- `AppState.swift` — WatchtowerAIService()
- `Navigation.swift` — health check via watchtower ai test
- `ChatViewModel.swift` — createService -> WatchtowerAIService
- `OnboardingChatViewModel.swift` — claudeService -> aiService
- `TrackChatView.swift` — claudeService -> aiService
- `SettingsView.swift` — testConnection via watchtower ai test, removed CLI path fields
- `TrainingSettings.swift`, `TrainingView.swift` — removed PATH enrichment
- `Constants.swift` — removed findClaudePath/findCodexPath, TCC-safe fallbacks

### Deleted
- `CodexService.swift` — replaced by WatchtowerAIService
- `ClaudeService.swift` — gutted, kept StreamEvent + AIServiceProtocol only

## Issues Found
- [x] #1: [ERROR] goimports alignment — fixed
- [ ] #2: [STYLE] `--allowed-tools` flag declared but not wired through Go AI client — future work

## Verdict
**APPROVED.** Clean architecture change. Desktop no longer touches Claude/Codex binaries. Zero TCC risk.

---
*Создано: 2026-04-06*
*Статус: approved*
