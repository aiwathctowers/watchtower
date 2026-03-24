# Tracks: Action Items Reimagined

## Vision

Reimagining action items as a personalized tracking system.
Watchtower becomes a personal assistant that knows who you are, what you do,
and what matters to you — from the very first minute.

**Key changes:**
1. Rename: action items → **tracks**
2. Onboarding chat — AI learns about the user while Slack syncs
3. Profile — reports, peers, manager, role, focus
4. Ownership — mine / delegated / watching
5. Personalized prompts based on profile
6. Stars — priority channels and people

---

## Onboarding Flow

```
1. User installs, enters Slack token

2. IN PARALLEL:

   [Background: Slack sync]          [Foreground: Onboarding chat]
   channels, users, messages          "Hi! What do you do?"
   [████████░░] 80%                   → Pain points
                                      → Role, responsibilities
                                      → What to track

   Sync progress — unobtrusive (progress bar at bottom of chat)

3. Sync done →
   Chat ends (or already ended, then form appears immediately)
   → Team form opens:

   ┌─────────────────────────────────┐
   │  My Reports        [search ▼]  │
   │  • @alice           x          │
   │  • @bob             x          │
   │                                │
   │  I Report To       [search ▼]  │
   │  • @dave            x          │
   │                                │
   │  Key Peers          [search ▼]  │
   │  • @eve             x          │
   │  • @frank           x          │
   │                                │
   │         [Done]                 │
   └─────────────────────────────────┘

   Full list of people from Slack (already synced), search, picker.

4. Profile done →
   → LLM generates custom_prompt_context
   → Prompts are personalized via Prompt Store
   → First tracks extraction runs
   → Main window: tracks appear
```

### Edge cases

- **Chat finishes before sync**: user waits, sees sync progress.
  When sync completes — people form appears.
- **Sync finishes before chat**: don't interrupt. User finishes chatting,
  then the form appears.
- **Digests**: can start generating before profile is filled
  (less dependent on personalization).

### Onboarding chat: what AI discovers

**Pain points** (multi-select or free text):
- Missing important messages while AFK
- Decisions get lost in threads
- Losing track of who owes what to whom
- Don't understand what the team is working on
- Deadlines are discussed in chats and forgotten
- Can't tell what's urgent vs what can wait

**Role and context** (free conversation):
- Position (EM, IC, Tech Lead, PM...)
- Area of responsibility
- Whether they manage people

**What to track** (AI suggests options based on role):
- EM: team blockers, decisions, people overload, deadlines
- IC: my reviews, questions directed at me, architectural decisions
- TL: technical decisions, tech debt, who's doing what
- PM: decisions, approvals, follow-ups, deadlines

---

## Tracks: Data Model

### New fields (on top of existing model)

```
Track {
  // everything from ActionItem, plus:

  ownership TEXT DEFAULT 'mine'
    CHECK(ownership IN ('mine', 'delegated', 'watching'))
    -- mine:      ball is on me, I need to act
    -- delegated: ball is on my report, I'm overseeing as manager
    -- watching:  not on me, but affects me / important to know

  ball_on TEXT DEFAULT ''
    -- user_id of the person who currently has the ball
    -- changes automatically during CheckForUpdates

  owner_user_id TEXT DEFAULT ''
    -- who "owns" the track (may not be the current user)
    -- for delegated = report's user_id
}
```

### Statuses — unchanged

```
inbox → active → done / dismissed / snoozed
```

Working fine, no changes. Ownership is an orthogonal dimension.

### Ownership + Status = complete picture

```
Ownership: mine
  inbox   → new, I need to look at it
  active  → picked up, working on it
  done    → completed

Ownership: delegated
  inbox   → new, my report should handle it
  active  → report is working on it
  done    → report closed it

Ownership: watching
  inbox   → something important appeared
  active  → monitoring progress
  done    → topic closed
```

### How AI determines ownership

Based on profile:
- Message from my report requesting something from someone → `delegated`
- Question/task directed at me → `mine`
- Decision in a channel that affects my area → `watching`
- Message from starred person → increased priority
- Activity in starred channel → more tracks created

---

## Profile

### DB Schema

```sql
CREATE TABLE user_profile (
  id INTEGER PRIMARY KEY,
  slack_user_id TEXT NOT NULL UNIQUE,
  role TEXT DEFAULT '',
  team TEXT DEFAULT '',
  responsibilities TEXT DEFAULT '',     -- JSON array
  reports TEXT DEFAULT '',              -- JSON array of user_ids
  peers TEXT DEFAULT '',                -- JSON array of user_ids
  manager TEXT DEFAULT '',              -- user_id
  starred_channels TEXT DEFAULT '',     -- JSON array of channel_ids
  starred_people TEXT DEFAULT '',       -- JSON array of user_ids
  pain_points TEXT DEFAULT '',          -- JSON array from onboarding
  track_focus TEXT DEFAULT '',          -- JSON array
  onboarding_done INTEGER DEFAULT 0,
  custom_prompt_context TEXT DEFAULT '',
  created_at TEXT DEFAULT (datetime('now')),
  updated_at TEXT DEFAULT (datetime('now'))
);
```

### custom_prompt_context

Generated by LLM based on onboarding results. Injected into all prompts.

Example:
> "You are helping an Engineering Manager responsible for Platform team
> (infrastructure, API reliability). Direct reports: @alice, @bob, @charlie.
> Reports to: @dave. Key peers: @eve, @frank.
> Focus: team blockers, architectural decisions, missed deadlines.
> For reports' tasks → ownership=delegated.
> For decisions in starred channels → ownership=watching.
> Prioritize: decision_needed, follow_up, approval categories."

### Categories: role-based personalization

8 categories remain, but AI **weights** them by role:

| Role    | High priority categories      | Normal                    |
|---------|-------------------------------|---------------------------|
| EM      | decision_needed, follow_up    | code_review, bug_fix      |
| IC      | code_review, bug_fix, task    | decision_needed           |
| TL      | decision_needed, code_review  | approval, follow_up       |
| PM      | decision_needed, approval     | info_request, follow_up   |

This is not hardcoded — AI decides based on custom_prompt_context.

---

## Stars

### Concept

Star = "pay extra attention". Stored in profile.

**Starred channels**: analyzed more thoroughly, borderline items make it into tracks.
**Starred people**: their messages get additional weight.

### UI

- In channel list (Digests tab): star next to channel, toggle
- In people list (People tab): star next to person, toggle
- In Settings > Profile: full starred list with management
- Quick action: tap — saved to user_profile

---

## Desktop UI Changes

### Tracks tab (formerly Actions)

```
┌────────────────────────────────────────────────┐
│ Tracks                                          │
├──────────┬─────────────────────────────────────┤
│          │                                      │
│ [Mine]      │  Track detail...                 │
│ [Delegated] │                                  │
│ [Watching]  │                                  │
│             │                                  │
│ Priority: [All ▼]                              │
│             │                                  │
│ 🔴 Review PR    mine     │                     │
│ 🟡 Deploy API   delegated│                     │
│ 🟢 RFC discuss  watching │                     │
│ ...            │                                │
└──────────┴─────────────────────────────────────┘
```

Filtering by ownership is the primary navigation method.

### Settings > Profile tab

```
┌─────────────────────────────────────────────┐
│ Settings                                     │
│ [General] [Profile] [Prompts] [Advanced]     │
├─────────────────────────────────────────────┤
│                                              │
│  About You                                   │
│  Role: [Engineering Manager]                 │
│  Team: [Platform]                            │
│                                              │
│  My Reports              [+ Add]             │
│  @alice    x                                 │
│  @bob      x                                 │
│                                              │
│  I Report To                                 │
│  @dave     x                                 │
│                                              │
│  Key Peers               [+ Add]             │
│  @eve      x                                 │
│  @frank    x                                 │
│                                              │
│  Starred Channels        [+ Add]             │
│  #platform-eng   x                           │
│  #incidents      x                           │
│                                              │
│  Starred People          [+ Add]             │
│  @cto            x                           │
│                                              │
│  [Re-run onboarding]                         │
└─────────────────────────────────────────────┘
```

---

## Implementation Phases

**Summary**: All 5 phases complete. Tracks system fully operational with personalization and starred items management.

### Phase 0: Rename action_items → tracks ✅

Pure refactoring, no new features.

- [x] DB migration v19: rename `action_items` → `tracks`, `action_item_history` → `track_history`
- [x] Go: rename package `internal/actionitems/` → `internal/tracks/`
- [x] Go: update all imports and references across codebase
- [x] Prompts: rename `actionitems.*` → `tracks.*` in defaults.go and Prompt Store
- [x] CLI: `watchtower actions` → `watchtower tracks` (cmd/actions.go → cmd/tracks.go)
- [x] Desktop: rename views, view models, queries (Actions* → Tracks*)
- [x] Desktop: tab label "Actions" → "Tracks"
- [x] Update daemon hooks: SetActionItemsPipeline → SetTracksPipeline
- [x] All Go tests pass
- [x] All Swift tests pass

### Phase 1: Profile ✅

- [x] DB migration v20: CREATE TABLE `user_profile`
- [x] Go: model + CRUD in `internal/db/profile.go`
- [x] Go: profile available (loaded via GetUserProfile, pipeline injection in Phase 4)
- [x] Desktop: `ProfileSettingsView` in Settings (Profile tab)
- [x] Desktop: SlackUserPicker component (reusable, searchable, multi-select)
- [x] Desktop: ChannelPicker component (reusable, searchable, multi-select)
- [x] CLI: `watchtower profile` — show current profile
- [x] Tests (Go: 4 profile tests, Swift: 11 new tests, total 232)

### Phase 2: Ownership & Ball ✅

- [x] DB migration v21: add `ownership`, `ball_on`, `owner_user_id` to tracks
- [x] Go: update Track model and DB operations (UpsertTrack, GetTracks filter, UpdateTrackFromExtraction, UpdateTrackBallOn)
- [x] Prompt: AI uses profile context to determine ownership per track (formatProfileContext with reports/peers/manager/stars)
- [x] Prompt: AI determines ball_on based on conversation flow (tracks.extract + tracks.update prompts)
- [x] Desktop: ownership filter in Tracks tab (mine/delegated/watching) + picker + sidebar
- [x] Desktop: ownership badge on track items (Delegated/Watching capsule badges)
- [x] CLI: `watchtower tracks --ownership mine|delegated|watching` filter + ownership badges in output
- [x] CheckForUpdates: detect ball_on changes, log to history (ball_on_changed event)
- [x] Tests (Go: all pass, Swift: 232 tests pass)

### Phase 3: Onboarding Chat ✅

- [x] Onboarding detection: first sync + onboarding_done == 0
- [x] Desktop: OnboardingChatView (Sheet or dedicated window)
- [x] Onboarding LLM prompt (discover role, pain points, track focus)
- [x] Progress bar for sync status within chat view (existing OnboardingView handles sync)
- [x] Flow: chat completes → wait for sync if needed → team form
- [x] Desktop: TeamFormView (reports/manager/peers pickers)
- [x] Parse chat results → populate user_profile
- [x] Generate custom_prompt_context via LLM
- [x] Personalize prompts via Prompt Store (custom_prompt_context injected into all prompts)
- [x] Set onboarding_done = 1
- [x] "Re-run onboarding" button in Settings
- [x] Tests (242 Swift tests pass, 10 new onboarding tests)

### Phase 4: Prompt Personalization ✅

- [x] Inject custom_prompt_context into all prompts:
  - tracks.extract (main extraction)
  - tracks.update (update check)
  - digest.channel, digest.daily, digest.weekly, digest.period
  - analysis.user, analysis.period
- [x] Category weight adjustment based on role
- [x] Starred channels/people → extra attention in prompts
- [x] Reports → automatic delegated ownership
- [x] Fallback to base templates when profile is empty
- [x] Tests: extraction with/without profile produces different results

### Phase 5: Stars UI ✅

All star functionality implemented:
- [x] Go API: AddStarredChannel, RemoveStarredChannel, AddStarredPerson, RemoveStarredPerson
- [x] Swift DatabaseManager methods for Add/Remove
- [x] StarToggleButton reusable component created
- [x] ProfileSettings UI for full management (already implemented)
- [x] Tests: 6 Go tests passing, all CRUD operations verified
- [x] Quick toggles in DigestListView (star per channel, optimistic updates)
- [x] Quick toggles in People list (star per person, optimistic updates)
- [x] Starred filter in tracks view (toggle button in toolbar)

---

## Technical Notes

### Prompt injection pattern

```go
func (p *Pipeline) buildPrompt(channel string, messages []Message) string {
    profile := p.profile // loaded once at pipeline start

    basePrompt := p.promptStore.Get("tracks.extract")

    // Inject profile context before the main prompt
    if profile.CustomPromptContext != "" {
        return profile.CustomPromptContext + "\n\n" + fmt.Sprintf(basePrompt, ...)
    }
    return fmt.Sprintf(basePrompt, ...)
}
```

### Ownership determination in prompt

```
OWNERSHIP RULES (based on user profile):
- If the track is a task/question/request directed at ME → ownership: "mine"
- If the track involves one of MY REPORTS ({reports_list}) as the responsible person → ownership: "delegated"
- If the track is a decision/discussion that affects my area but I'm not the actor → ownership: "watching"
- If unsure → ownership: "mine" (better to surface than miss)

BALL RULES:
- ball_on = user_id of the person who needs to act next
- If I asked a question and am waiting for reply → ball_on: other person
- If someone asked me something → ball_on: my user_id
```

### Migration strategy

Phase 0 (rename) is a clean break:
- New migration renames tables, no data loss
- All code references updated atomically
- Old `watchtower actions` command shows deprecation message for 1 version
