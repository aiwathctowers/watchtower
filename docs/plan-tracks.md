# Tracks: Action Items Reimagined

## Vision

Переосмысление action items в персонализированную систему отслеживания.
Watchtower становится персональным ассистентом, который знает кто ты, чем занимаешься,
и что для тебя важно — с первой минуты.

**Ключевые изменения:**
1. Rename: action items → **tracks**
2. Onboarding chat — AI узнаёт юзера пока Slack синкается
3. Profile — reports, peers, manager, роль, фокус
4. Ownership — mine / delegated / watching
5. Персонализированные промпты на основе профиля
6. Stars — приоритетные каналы и люди

---

## Onboarding Flow

```
1. User installs, enters Slack token

2. ПАРАЛЛЕЛЬНО:

   [Background: Slack sync]          [Foreground: Onboarding chat]
   channels, users, messages          "Привет! Чем занимаешься?"
   [████████░░] 80%                   → Pain points
                                      → Роль, ответственность
                                      → Что хочет отслеживать

   Прогресс синка — ненавязчиво (progress bar внизу чата)

3. Sync done →
   Chat заканчивается (или уже закончился, тогда форма сразу)
   → Team form открывается:

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

   Полный список людей из Slack (уже засинкан), поиск, picker.

4. Profile done →
   → LLM генерирует custom_prompt_context
   → Промпты персонализируются через Prompt Store
   → Первый tracks extraction запускается
   → Основное окно: tracks появляются
```

### Edge cases

- **Чат закончился раньше синка**: юзер ждёт, видит прогресс синка.
  Когда синк доехал — форма с людьми.
- **Синк закончился раньше чата**: не перебиваем. Юзер дообщается,
  потом форма.
- **Digests**: можно начать генерить до заполнения профиля
  (менее зависимы от персонализации).

### Onboarding chat: что выясняет AI

**Pain points** (мультиселект или свободный текст):
- Пропускаю важные сообщения пока AFK
- Решения теряются в тредах
- Теряю из виду кто кому что должен
- Не понимаю чем команда занята
- Дедлайны обсуждаются в чатах и забываются
- Не могу понять что горит а что может подождать

**Роль и контекст** (свободный диалог):
- Позиция (EM, IC, Tech Lead, PM...)
- Зона ответственности
- Руководит ли людьми

**Что отслеживать** (AI предлагает варианты под роль):
- EM: блокеры команды, решения, перегруз людей, дедлайны
- IC: мои review, вопросы ко мне, архитектурные решения
- TL: технические решения, tech debt, кто что делает
- PM: решения, approvals, follow-ups, дедлайны

---

## Tracks: Data Model

### Новые поля (поверх существующей модели)

```
Track {
  // всё что было в ActionItem, плюс:

  ownership TEXT DEFAULT 'mine'
    CHECK(ownership IN ('mine', 'delegated', 'watching'))
    -- mine:      мяч на мне, я должен действовать
    -- delegated: мяч на моём report, я слежу как руководитель
    -- watching:  не на мне, но затрагивает / важно знать

  ball_on TEXT DEFAULT ''
    -- user_id того, у кого сейчас мяч
    -- меняется автоматом при CheckForUpdates

  owner_user_id TEXT DEFAULT ''
    -- кто "владелец" track (может быть не текущий юзер)
    -- для delegated = report's user_id
}
```

### Статусы — без изменений

```
inbox → active → done / dismissed / snoozed
```

Работают, не трогаем. Ownership — ортогональное измерение.

### Ownership + Status = полная картина

```
Ownership: mine
  inbox   → новое, мне нужно посмотреть
  active  → взял в работу
  done    → сделал

Ownership: delegated
  inbox   → новое, мой report должен заняться
  active  → report работает над этим
  done    → report закрыл

Ownership: watching
  inbox   → появилось что-то важное
  active  → слежу за развитием
  done    → тема закрыта
```

### Как AI определяет ownership

На основе profile:
- Сообщение от моего report с просьбой к кому-то → `delegated`
- Вопрос/задача направлена мне → `mine`
- Решение в канале, которое affects мою зону → `watching`
- Сообщение от starred person → повышенный приоритет
- Активность в starred channel → больше tracks создаётся

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

LLM генерирует по итогам онбординга. Инжектится во все промпты.

Пример:
> "You are helping an Engineering Manager responsible for Platform team
> (infrastructure, API reliability). Direct reports: @alice, @bob, @charlie.
> Reports to: @dave. Key peers: @eve, @frank.
> Focus: team blockers, architectural decisions, missed deadlines.
> For reports' tasks → ownership=delegated.
> For decisions in starred channels → ownership=watching.
> Prioritize: decision_needed, follow_up, approval categories."

### Categories: персонализация по роли

8 категорий остаются, но AI **взвешивает** их по роли:

| Позиция | High priority categories      | Normal                    |
|---------|-------------------------------|---------------------------|
| EM      | decision_needed, follow_up    | code_review, bug_fix      |
| IC      | code_review, bug_fix, task    | decision_needed           |
| TL      | decision_needed, code_review  | approval, follow_up       |
| PM      | decision_needed, approval     | info_request, follow_up   |

Это не hardcode — AI решает на основе custom_prompt_context.

---

## Stars

### Концепция

Звёздочка = "обращай повышенное внимание". Хранится в profile.

**Starred channels**: анализируются тщательнее, пограничные вещи попадают в tracks.
**Starred people**: их сообщения получают доп. вес.

### UI

- В channel list (Digests tab): звёздочка рядом с каналом, toggle
- В people list (People tab): звёздочка рядом с человеком, toggle
- В Settings > Profile: полный список starred с управлением
- Quick action: нажал — сохранилось в user_profile

---

## Desktop UI Changes

### Tracks tab (бывший Actions)

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

Фильтрация по ownership — основной способ навигации.

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

Core backend complete, UI partially implemented:
- [x] Go API: AddStarredChannel, RemoveStarredChannel, AddStarredPerson, RemoveStarredPerson
- [x] Swift DatabaseManager methods for Add/Remove
- [x] StarToggleButton reusable component created
- [x] ProfileSettings UI for full management (already implemented)
- [x] Tests: 6 Go tests passing, all CRUD operations verified
- [ ] Quick toggles in DigestListView (requires state management refactoring)
- [ ] Quick toggles in People list (requires state management refactoring)
- [ ] Starred filter in tracks view (can be added later)

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
