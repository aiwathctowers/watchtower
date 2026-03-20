# Onboarding Flow — Watchtower Desktop

## Architecture: Unified State Machine

Onboarding is implemented as a **single linear flow** with the current step persisted in UserDefaults.
On app restart — returns to the unfinished step, not a restart from scratch.

### OnboardingStateMachine (OnboardingStateMachine.swift)

```swift
enum OnboardingStep: Int, CaseIterable, Comparable, Codable {
    case connect = 0      // Slack OAuth
    case settings = 1     // Language, model, history, sync frequency, notifications
    case claude = 2       // Claude CLI health check
    case chat = 3         // Role questionnaire + AI conversation (sync in background)
    case teamForm = 4     // Team form (reports, manager, peers)
    case generating = 5   // Profile generation via AI
    case complete = 6     // Done
}
```

**Persistence:** UserDefaults keys `onboarding_current_step`, `onboarding_sync_completed`.

**Methods:**
- `advance()` — transition to the next step
- `goTo(step)` — jump to a specific step
- `reset(to:)` — reset (for re-run from Settings)
- `markComplete()` — clear UserDefaults
- `skipCompleted()` — auto-skip completed steps (connect if config.yaml exists)
- `shouldSkip(step)` — check if step should be skipped

**Visual indicator:** The first 4 steps are displayed as dots in `stepsIndicator` (connect, settings, claude, chat). Steps teamForm/generating/complete are not shown in the indicator.

## Full Diagram

```
╔══════════════════════════════════════════════════════════════════════════╗
║                          APP LAUNCH                                     ║
║                      WatchtowerApp.swift                                ║
╚════════════════════════════════╤═════════════════════════════════════════╝
                                 │
                                 ▼
                    ┌────────────────────────┐
                    │   AppState.initialize() │
                    │                        │
                    │ 1. runCLIMigrations()  │
                    │ 2. resolveDBPath()     │
                    │ 3. DatabaseManager()   │
                    │ 4. Sync state machine  │
                    │    with DB state       │
                    └───────────┬────────────┘
                                │
               ┌────────────────┴─────────────────┐
               │                                  │
        onboarding.currentStep              onboarding.currentStep
            != .complete                        == .complete
               │                                  │
               ▼                                  ▼
    ╔═════════════════════╗            ╔══════════════════════╗
    ║  OnboardingView     ║            ║  MainNavigationView  ║
    ║  (unified flow)     ║            ╚══════════════════════╝
    ╚═════════╤═══════════╝
              │
              ▼  (resume from persisted step)
   ┌──────────────────────────────────────────────────────┐
   │                                                      │
   │  STEP 1: connect                                     │
   │  ─────────────────                                   │
   │  Auto-skip if config.yaml exists.                    │
   │                                                      │
   │  UI: Privacy notice + [Connect to Slack] button      │
   │  1. watchtower auth trust-cert                       │
   │  2. watchtower auth login (opens browser)            │
   │  3. OAuth callback → config.yaml created             │
   │                                                      │
   │  Success → goTo(.settings)                           │
   └──────────────────┬───────────────────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────────────────┐
   │                                                      │
   │  STEP 2: settings                                    │
   │  ────────────────                                    │
   │  Language, AI Model, History Depth, Sync Freq, Notifs│
   │                                                      │
   │  On "Continue": watchtower config set <key> <val>    │
   │    Keys: digest.language, sync.initial_history_days, │
   │          digest.model, ai.model, sync.poll_interval  │
   │  Success → goTo(.claude)                             │
   └──────────────────┬───────────────────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────────────────┐
   │                                                      │
   │  STEP 3: claude                                      │
   │  ──────────────                                      │
   │  Claude CLI health check:                            │
   │    claude -p "respond with: OK" --output-format text │
   │      --model <selected>                              │
   │                                                      │
   │  ┌─ Not found → install instructions + manual path   │
   │  │   + Browse... file picker                         │
   │  ├─ Found → auto health check                        │
   │  ├─ Passed → 1.5s delay → goTo(.chat) + runSync()   │
   │  └─ Failed → diagnoseClaudeError() + retry/back     │
   │                                                      │
   │  [Skip for now] → goTo(.chat) + runSync()            │
   └──────────────────┬───────────────────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────────────────┐
   │                                                      │
   │  STEP 4: chat (two parallel processes)               │
   │  ─────────────────────────────────────               │
   │                                                      │
   │  .task: creates OnboardingChatViewModel with         │
   │    language = ConfigService().digestLanguage          │
   │    ?? settingsLanguage                               │
   │  If sync not started and not completed → runSync()   │
   │                                                      │
   │  ┌─────────────────────┐  ┌────────────────────────┐ │
   │  │  BACKGROUND: Sync   │  │  FOREGROUND: Chat      │ │
   │  │                     │  │                        │ │
   │  │  runSync()          │  │  4a. Role Questionnaire│ │
   │  │  watchtower sync    │  │    Q1: Reports? [Y/N]  │ │
   │  │    --progress-json  │  │    Q2a/Q2b: Strategy?  │ │
   │  │                     │  │    Q3: Manage mgrs?    │ │
   │  │  Progress shown as  │  │    → 1 of 5 roles      │ │
   │  │  compact banner at  │  │                        │ │
   │  │  bottom of chat     │  │  4b. AI Conversation   │ │
   │  │                     │  │    4-6 questions about  │ │
   │  │  On complete:       │  │    team, domain, needs  │ │
   │  │  1. Open DB         │  │                        │ │
   │  │  2. vm.setDatabase()│  │  chatReady triggers:   │ │
   │  │  3. syncCompleted   │  │  1. [READY] marker     │ │
   │  │     = true          │  │  2. No "?" + ≥3 msgs   │ │
   │  │                     │  │  3. Fallback: ≥10 msgs  │ │
   │  │                     │  │  → [Continue] button    │ │
   │  └────────┬────────────┘  └───────────┬────────────┘ │
   │           │                           │              │
   │           │  User taps "Continue":                   │
   │           │  1. finishChat() cancels stream          │
   │           │  2. isExtractingProfile = true            │
   │           │  3. parseProfileFromChat() — LLM call    │
   │           │     extracts role/team/pain_points JSON   │
   │           │  4. isExtractingProfile = false           │
   │           │                           │              │
   │           ▼                           ▼              │
   │  ┌─────────────────────────────────────────────────┐ │
   │  │                                                 │ │
   │  │  CASE A: Sync finished before chat              │ │
   │  │    syncCompleted == true                        │ │
   │  │    → goTo(.teamForm) immediately                │ │
   │  │                                                 │ │
   │  │  CASE B: Chat finished before sync              │ │
   │  │    syncCompleted == false                       │ │
   │  │    → chatFinished = true                        │ │
   │  │    → UI switches to "Waiting for sync..." view  │ │
   │  │      with full progress bar                     │ │
   │  │    → when sync completes:                       │ │
   │  │      runSync() sees chatFinished == true         │ │
   │  │      → goTo(.teamForm) automatically            │ │
   │  │      Also: .onChange(of: syncCompleted) as       │ │
   │  │        reactive fallback for edge cases          │ │
   │  │                                                 │ │
   │  │  CASE C: Sync failed                            │ │
   │  │    → show error + [Retry Sync] button           │ │
   │  │                                                 │ │
   │  │  CASE D: Sync finished, chat not yet done       │ │
   │  │    → waiting view with [Continue] button        │ │
   │  │    → ensures DB available before transition     │ │
   │  │                                                 │ │
   │  └─────────────────────────────────────────────────┘ │
   └──────────────────┬───────────────────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────────────────┐
   │                                                      │
   │  STEP 5: teamForm                                    │
   │  ────────────────                                    │
   │  OnboardingTeamFormView:                             │
   │    Role (prefilled), Team (prefilled)                │
   │    My Reports [multi-select picker]                  │
   │    I Report To [single-select picker]                │
   │    Key Peers [multi-select picker]                   │
   │                                                      │
   │  User taps "Done":                                   │
   │    1. goTo(.generating)                              │
   │    2. generatePromptContext() → Claude generates      │
   │       custom_prompt_context (5-10 sentences)          │
   │    3. markOnboardingDone() → onboarding_done = 1     │
   │    4. On success:                                     │
   │       backgroundTaskManager.startPipelines()          │
   │       appState.completeOnboarding()                   │
   │       onRetry() → appState.initialize()               │
   │    5. On error: goTo(.teamForm) (retry)               │
   │                                                      │
   │  .task fallback: if VM == nil on restart,             │
   │    creates a new VM with DB. If DB unavailable →      │
   │    fallback to .chat for re-sync                      │
   └──────────────────┬───────────────────────────────────┘
                      │
                      ▼
   ┌──────────────────────────────────────────────────────┐
   │                                                      │
   │  STEP 6: generating                                  │
   │  ──────────────────                                  │
   │  Visual step — shows ProgressView +                   │
   │  "Setting up your personalized experience..."         │
   │  Actual work is performed in Task from teamForm       │
   │  (generatePromptContext + markOnboardingDone)          │
   │  Errors are displayed here, but handled in teamForm   │
   └──────────────────┬───────────────────────────────────┘
                      │
                      ▼
   ╔══════════════════════════════════════════════════════╗
   ║  MainNavigationView                                  ║
   ║  Sidebar: AI Chat, Tracks, Digests, People, Search,  ║
   ║           Training                                    ║
   ║                                                      ║
   ║  Background pipelines (SidebarProgressView):          ║
   ║  Phase 1: Digests (blocking)                          ║
   ║  Phase 2: Tracks + People Analytics (parallel)        ║
   ║  Phase 3: Daemon starts (sync --daemon --detach)      ║
   ╚══════════════════════════════════════════════════════╝
```

## Restart Behavior

| Interrupted at step | On restart |
|---|---|
| connect | Auto-skip if config.yaml exists, otherwise show connect |
| settings | Show settings (settings may not have been saved) |
| claude | Re-run health check (quick check). If already passed — auto-advance |
| chat | Restart questionnaire + AI chat from scratch (Claude session can't be restored). Sync restarts if not completed |
| teamForm | Show team form. If DB unavailable — fallback to chat for re-sync |
| generating | Visual placeholder — actual work from teamForm |
| **post-onboarding pipelines** | If `pipelines_completed == false` in UserDefaults → automatic restart of `startPipelines()`. Go pipelines safely skip already-generated data (digests by timestamp, tracks by day-window, people by window-level check) |

## Post-Onboarding Pipelines (BackgroundTaskManager)

```
startPipelines()
    │
    ▼
┌─────────────────────────────────────────────────────┐
│  Phase 1: Digests (blocking)                         │
│  watchtower digest generate --progress-json          │
│  Generates channel/daily/weekly digests              │
│  + decisions (JSON field in digests table)            │
└──────────────────────┬──────────────────────────────┘
                       │
          ┌────────────┴────────────┐
          │                         │
          ▼                         ▼
┌──────────────────────┐  ┌──────────────────────────┐
│  Phase 2a: Tracks    │  │  Phase 2b: People        │
│  (parallel)          │  │  (parallel)              │
│  watchtower tracks   │  │  watchtower people       │
│  generate            │  │  generate                │
│  --progress-json     │  │  --progress-json         │
│                      │  │                          │
│  Depends on digests  │  │  Does not depend on      │
│  (digest decisions)  │  │  digests (reads raw msgs) │
└──────────┬───────────┘  └──────────┬───────────────┘
           │                         │
           └────────────┬────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│  Phase 3: Daemon                                     │
│  watchtower sync --daemon --detach                   │
│  Starts only after all pipelines complete             │
│  Further updates: digests, tracks, people,           │
│  action items — on schedule with throttling           │
└─────────────────────────────────────────────────────┘
```

### Recovery on Interruption

`UserDefaults["pipelines_completed"]` — flag for all pipelines completion.
- Set to `true` after Phase 3 completes
- Reset on onboarding re-run (`startOnboarding()`)
- During `AppState.initialize()`: if onboarding completed but flag is `false` → `startPipelines()` automatically

Go pipelines are safe for re-execution:
- **Digests**: skips channels with digests newer than latest messages
- **Tracks**: day-aligned windows, delete+reinsert for current window
- **People**: window-level skip if analysis already exists

## Re-run from Settings (Path 3)

```
ProfileSettings.swift → "Re-run Onboarding" button
    │
    ▼
appState.startOnboarding()
    onboarding.reset(to: .chat)    // connect/settings/claude already passed
    needsOnboarding = true
    UserDefaults.removeObject(forKey: pipelinesCompletedKey)
    │
    ▼
NavigationRoot re-renders → OnboardingView
    Starts from step .chat (Role Questionnaire)
```

---

## File Details

### NavigationRoot — entry point (Navigation.swift)

```swift
if appState.isLoading        → Color.clear (blank)
if appState.needsOnboarding  → OnboardingView(onRetry:)    // Unified flow
else                         → MainNavigationView()         // Main app
```

### AppState — state management (AppState.swift)

| Method | Description |
|---|---|
| `initialize()` | Opens DB, syncs state machine with DB, loads emoji, starts digest watcher, checks updates, recovers pipelines |
| `onboarding` | `OnboardingStateMachine` — persistent state machine |
| `needsOnboarding` | `onboarding.currentStep != .complete` (after sync with DB) |
| `completeOnboarding()` | `onboarding.markComplete()` + `needsOnboarding = false` |
| `startOnboarding()` | `onboarding.reset(to: .chat)` + `needsOnboarding = true` + clear `pipelinesCompletedKey` |

### OnboardingChatViewModel — onboarding brain

| Method | When called | What it does |
|---|---|---|
| `startQuestionnaire()` | `.task` in OnboardingChatView | First question Q1 + quick-reply buttons |
| `answerRoleQ1(reportsToThem:)` | Yes/No button | Records answer, shows Q2a or Q2b |
| `answerRoleQ2a(setStrategy:)` | Yes/No button | If Yes → Q3, if No → finishQuestionnaire |
| `answerRoleQ2b(influenceType:)` | Expertise/Tasks button | → finishQuestionnaire |
| `answerRoleQ3(manageManagers:)` | Yes/No button | → finishQuestionnaire |
| `finishQuestionnaire()` | After last answer | Removes buttons, calls `initiateChat()` |
| `initiateChat()` | After questionnaire | Hidden prompt → Claude → streams first message |
| `send()` | User sends text | Streams Claude response, system prompt every time, checks `[READY]` marker |
| `stripReadyMarker(at:)` | After each AI response | Primary: removes `[READY]`, sets `chatReady=true`. Secondary: no `?` + `≥3` responses → chatReady |
| `finishChat()` | "Continue" button | Cancels stream, `isExtractingProfile=true`, calls `parseProfileFromChat()` |
| `parseProfileFromChat()` | From finishChat | **LLM extraction**: sends transcript to Claude, gets JSON `{role, team, pain_points}` |
| `generatePromptContext()` | Step generating (from teamForm closure) | Sends transcript + metadata to Claude → 5-10 sentences |
| `markOnboardingDone()` | After generatePromptContext | `UPDATE user_profile SET onboarding_done = 1` |

### chatReady — three mechanisms

1. **Primary**: AI sends `[READY]` marker (case-insensitive). `stripReadyMarker()` removes the marker from text.
2. **Secondary**: AI stopped asking questions (response without `?`) after `≥3` user messages (`minAnswersForNoQuestionHeuristic`).
3. **Fallback**: `≥10` user messages (`fallbackMessageCount`) — in case AI didn't send `[READY]` and keeps asking.

### Claude CLI Invocations

During onboarding, Claude CLI is called **4 times**:

1. **Health check** (Step claude): `claude -p "respond with: OK" --output-format text --model <model>` — verifies CLI works
2. **Onboarding conversation** (Step chat): multi-turn streaming via `claudeService.stream()` with `sessionID` for context. System prompt is sent with every message.
3. **Profile extraction** (Step chat → finishChat): one-shot LLM call — transcript → JSON `{role, team, pain_points}`. No sessionID.
4. **Profile generation** (Step generating): one-shot call — transcript + metadata → custom_prompt_context (5-10 sentences). No sessionID.

### Data Model: `user_profile` table

| Field | Source | Type | Downstream usage |
|---|---|---|---|
| `slack_user_id` | `workspace.current_user_id` | TEXT UNIQUE | PK, workspace link |
| `role` | LLM extraction from chat (parseProfileFromChat) | TEXT | `prompts.Store.GetForRole()` — role-specific prompts |
| `team` | LLM extraction from chat + team form | TEXT | Context in prompts |
| `reports` | Team form picker | JSON `[string]` | Tracks pipeline — ownership |
| `peers` | Team form picker | JSON `[string]` | Tracks pipeline — ownership |
| `manager` | Team form picker | TEXT | Tracks pipeline — ownership |
| `pain_points` | LLM extraction from chat | JSON `[string]` | Personalization context |
| `track_focus` | Chat parsing | JSON `[string]` | Personalization context |
| `onboarding_done` | `markOnboardingDone()` | INTEGER 0/1 | Onboarding display trigger |
| `custom_prompt_context` | AI-generated (5-10 sentences) | TEXT | Injected into ALL prompts: digest, tracks, analysis, action items, chat |
| `starred_channels` | Post-onboarding (Settings) | JSON `[string]` | Priority channels |
| `starred_people` | Post-onboarding (Settings) | JSON `[string]` | Priority people |

### Downstream Profile Usage

```
custom_prompt_context ─┬─▶ digest/pipeline.go    (channel/daily/weekly digests)
                       ├─▶ tracks/pipeline.go     (task extraction)
                       ├─▶ analysis/pipeline.go   (people analytics)
                       ├─▶ actionitems/pipeline.go (action items)
                       └─▶ ChatView system prompt  (AI chat)

role ─────────────────────▶ prompts/store.go GetForRole()
                             → role-specific prompt variants
                             → RoleInstructions[role] prefix

reports/manager/peers ────▶ tracks/pipeline.go
                             → ownership assignment
```

### Localization

All onboarding strings are localized in `OnboardingChatViewModel.strings` (3 languages: EN/RU/UA):
- Role questions (Q1-Q3)
- Button labels (Yes/No, Expertise/Tasks, Continue)
- Chat header and subtitle

System prompt for Claude switches language via `langRule` (English/Russian/Ukrainian).

### Status Bar (bottom panel during onboarding)

Shown only on steps connect/settings/claude (`currentStep <= .claude`).
Displays two indicators:
- 🟢/🔴 **Watchtower CLI** — `Constants.findCLIPath() != nil`
- 🟢/🟠 **Claude Code** — `Constants.findClaudePath() != nil`

---

## File Index

| File | Contents |
|---|---|
| `WatchtowerApp.swift` | Entry point, `onOpenURL` for `watchtower-auth://` |
| `OnboardingStateMachine.swift` | `OnboardingStep` enum (+ `indicatorTitle`, `indicatorSteps`), `OnboardingStateMachine` with UserDefaults persistence |
| `Navigation.swift` | `NavigationRoot`, `OnboardingView` (all steps: connect/settings/claude/chat/teamForm/generating), `MainNavigationView`, `ModelPreset`, `PollPreset`, sync/CLI helpers |
| `AppState.swift` | `onboarding: OnboardingStateMachine`, `needsOnboarding`, `completeOnboarding()`, `startOnboarding()`, `backgroundTaskManager`, `updateService` |
| `OnboardingChatView.swift` | Chat UI: role questions (quick-reply) + AI conversation + Continue button (with "Analyzing..." state) |
| `OnboardingChatViewModel.swift` | ViewModel: questionnaire, chat streaming, LLM profile extraction, prompt generation, localization |
| `OnboardingTeamFormView.swift` | Team form: role, team, reports, manager, peers |
| `UserProfile.swift` | `RoleLevel` enum, `RoleDetermination` struct |
| `ProfileQueries.swift` | `fetchCurrentProfile()`, `upsertProfile()` |
| `ProfileSettings.swift` | "Re-run Onboarding" button |
| `BackgroundTaskManager.swift` | `TaskKind` (.digests/.tracks/.people), pipeline orchestration, progress tracking, ETA, retry/dismiss |
| `internal/auth/oauth.go` | OAuth HTTPS server (port 18491), success/error pages |
| `internal/db/db.go` | Schema migration: `user_profile` table |
| `internal/db/profile.go` | Go-side `GetUserProfile()`, `UpsertUserProfile()` |
| `internal/prompts/store.go` | `GetForRole()` — role-aware prompt loading |
