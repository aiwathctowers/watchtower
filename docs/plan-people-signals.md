# Plan: People Signals — Map-Reduce рефакторинг

Объединение 3 пайплайнов (digest + analysis + guide) в архитектуру Map-Reduce.
Контекст и мотивация: [docs/guide-analysis.md](guide-analysis.md)

---

## Этап 1: MAP — people_signals в channel digest

Добавить `people_signals` в output channel digest. Не ломает существующий функционал.

### 1.1 DB migration v18

**Файл:** `internal/db/schema.sql`

Добавить колонку в таблицу `digests`:
```sql
ALTER TABLE digests ADD COLUMN people_signals TEXT DEFAULT '[]';
```

Добавить в `migrations` map в `internal/db/db.go` запись `17 → 18`.

**Файл:** `internal/db/db.go` — добавить миграцию:
```go
17: "ALTER TABLE digests ADD COLUMN people_signals TEXT DEFAULT '[]'",
```

Также обновить CREATE TABLE в schema.sql — добавить `people_signals TEXT DEFAULT '[]'` после `action_items`.

### 1.2 Go structs

**Файл:** `internal/digest/pipeline.go`

Добавить новые типы рядом с существующими `DigestResult`, `Decision`, `ActionItem`:

```go
// PersonSignals holds signals for one person in a channel digest.
type PersonSignals struct {
    UserID  string   `json:"user_id"`
    Signals []Signal `json:"signals"`
}

// Signal is a typed observation about a person in channel context.
type Signal struct {
    Type       string `json:"type"`
    Detail     string `json:"detail"`
    EvidenceTS string `json:"evidence_ts,omitempty"`
}
```

Добавить поле в `DigestResult`:
```go
type DigestResult struct {
    Summary       string         `json:"summary"`
    Topics        []string       `json:"topics"`
    Decisions     []Decision     `json:"decisions"`
    ActionItems   []ActionItem   `json:"action_items"`
    KeyMessages   []string       `json:"key_messages"`
    PeopleSignals []PersonSignals `json:"people_signals"`  // NEW
}
```

### 1.3 Store signals in DB

**Файл:** `internal/digest/pipeline.go`, функция `storeDigest()`

Сейчас (строка ~734):
```go
topics, _ := json.Marshal(result.Topics)
decisions, _ := json.Marshal(result.Decisions)
actionItems, _ := json.Marshal(result.ActionItems)
```

Добавить:
```go
peopleSignals, _ := json.Marshal(result.PeopleSignals)
```

И передать в `db.Digest`:
```go
d := db.Digest{
    // ... existing fields ...
    PeopleSignals: string(peopleSignals),  // NEW
}
```

**Файл:** `internal/db/models.go`, struct `Digest`

Добавить поле:
```go
PeopleSignals string // JSON array of PersonSignals
```

**Файл:** `internal/db/digests.go`

Обновить SQL в `UpsertDigest()` — добавить `people_signals` в INSERT и UPDATE.
Обновить SQL в `scanDigest()` / scan helper — добавить поле в Scan().
Обновить `GetDigests()` SELECT — включить people_signals.

Backwards-compatible: если поле NULL/пустое, default '[]'.

### 1.4 Channel digest prompt

**Файл:** `internal/prompts/defaults.go`, const `defaultDigestChannel`

Добавить в JSON-схему после `key_messages`:
```
  "people_signals": [
    {
      "user_id": "@username",
      "signals": [
        {"type": "bottleneck|accomplishment|initiative|mediator|conflict|disengagement|dropped_ball|rubber_stamping|overloaded|after_hours|knowledge_hub|blocker", "detail": "specific observation with evidence", "evidence_ts": "1234567890.123456"}
      ]
    }
  ]
```

Добавить в Rules секцию:
```
- people_signals: For each person who STOOD OUT in this channel (max 5-7), emit typed signals based on their behavior IN CONTEXT of the conversation. Only notable behavior — skip routine participants. Each signal MUST cite specific evidence from messages.
  Signal types:
  * "bottleneck" — blocked a decision, process, or other people
  * "accomplishment" — delivered, resolved, shipped something concrete
  * "initiative" — proposed new ideas, started discussions, drove change
  * "mediator" — resolved conflicts, coordinated between parties
  * "conflict" — unconstructive behavior, tension with others
  * "disengagement" — ignored questions, went silent, minimal responses
  * "dropped_ball" — committed to something but didn't follow through
  * "rubber_stamping" — approved without meaningful review
  * "overloaded" — too many threads, fragmented responses
  * "after_hours" — significant activity outside business hours
  * "knowledge_hub" — multiple people asked them for help/answers
  * "blocker" — explicitly blocked without providing alternatives
  If no one stood out, return empty array [].
```

### 1.5 Parse signals (backwards-compatible)

**Файл:** `internal/digest/pipeline.go`, функция `parseDigestResult()`

Парсинг уже автоматический через `json.Unmarshal` — новое поле `PeopleSignals` будет nil/empty для старых дайджестов (JSON без этого поля). Никаких изменений в парсере не нужно.

### 1.6 Tests

**Файл:** `internal/digest/pipeline_test.go`

Добавить тест `TestChannelDigest_PeopleSignals`:
- Mock generator возвращает JSON с people_signals
- Проверить что signals сохранились в DB
- Проверить что пустые signals не ломают парсинг

Добавить тест `TestChannelDigest_BackwardsCompatible`:
- Mock generator возвращает JSON БЕЗ people_signals (старый формат)
- Проверить что PeopleSignals == nil/empty, остальное работает

### 1.7 Контрольная точка

После этого этапа: запустить `watchtower digest generate`, проверить что signals появляются в БД. Оценить качество сигналов на реальных данных перед продолжением.

---

## Этап 2: DB queries для агрегации сигналов

### 2.1 Новые query-функции

**Файл:** `internal/db/digests.go`

```go
// ChannelSignals groups signals for one user from one channel digest.
type ChannelSignals struct {
    ChannelID   string
    ChannelName string
    PeriodFrom  float64
    PeriodTo    float64
    Signals     []digest.Signal  // parsed from JSON
}

// GetPeopleSignalsForUser returns all signals for a specific user
// from channel digests within the given time window.
func (d *DB) GetPeopleSignalsForUser(userID string, from, to float64) ([]ChannelSignals, error)
```

SQL:
```sql
SELECT d.channel_id, c.name, d.period_from, d.period_to, d.people_signals
FROM digests d
LEFT JOIN channels c ON c.id = d.channel_id
WHERE d.type = 'channel'
  AND d.period_from >= ?
  AND d.period_to <= ?
  AND d.people_signals IS NOT NULL
  AND d.people_signals != '[]'
```

Затем в Go: парсить JSON, фильтровать по userID (user_id содержит @username, нужно маппить через userNames cache).

**Примечание:** user_id в signals — это @username (как AI видит). Нужен маппинг username→userID. Два варианта:
- (A) В промпте просить AI использовать Slack user_id (U123) вместо @username
- (B) Маппить при агрегации через userNames cache

**Выбор: (A)** — проще и надёжнее. Обновить промпт: "user_id: use the Slack user ID (e.g. U123456), NOT the display name". В formatMessages уже передаются user IDs, нужно убедиться что они видны AI.

**Проблема:** Текущий `formatMessages()` в digest pipeline форматирует как `@username`, не как `U123`. Нужно посмотреть формат — если AI видит `[@10:15 alice]`, он не знает user_id.

**Решение:** Добавить user_id в формат сообщений: `[10:15 @alice (U123)] message text`. Тогда AI сможет использовать U123 в people_signals. Это минимальное изменение в `formatMessages()`.

```go
// GetAllPeopleSignals returns signals for ALL users, grouped by user.
// Used to compute team signal norms for the reduce phase.
func (d *DB) GetAllPeopleSignals(from, to float64) (map[string][]ChannelSignals, error)
```

Та же SQL-выборка, но без фильтра по userID. Группировка в Go.

### 2.2 Tests

**Файл:** `internal/db/digests_test.go` (или новый файл `internal/db/people_signals_test.go`)

- `TestGetPeopleSignalsForUser` — seed digest с signals, проверить выборку
- `TestGetPeopleSignalsForUser_Empty` — digest без signals
- `TestGetAllPeopleSignals` — несколько дайджестов, проверить группировку
- `TestGetPeopleSignals_WindowFilter` — signals вне окна не попадают

---

## Этап 3: REDUCE pipeline — объединённый people card

### 3.1 Новая модель PeopleCard

**Файл:** `internal/db/models.go`

```go
// PeopleCard is a unified per-user card combining analysis + guide data.
// Replaces separate UserAnalysis and CommunicationGuide.
type PeopleCard struct {
    ID         int
    UserID     string
    PeriodFrom float64
    PeriodTo   float64

    // Computed stats (pure SQL, same as before)
    MessageCount     int
    ChannelsActive   int
    ThreadsInitiated int
    ThreadsReplied   int
    AvgMessageLength float64
    ActiveHoursJSON  string
    VolumeChangePct  float64

    // Analysis fields (from old UserAnalysis)
    Summary            string
    CommunicationStyle string // driver|collaborator|executor|observer|facilitator
    DecisionRole       string // decision-maker|approver|contributor|observer|blocker
    RedFlags           string // JSON array
    Highlights         string // JSON array
    Accomplishments    string // JSON array

    // Guide fields (from old CommunicationGuide)
    HowToCommunicate string // unified paragraph
    DecisionStyle    string // how they participate in decisions
    Tactics          string // JSON array of "If X, then Y"

    // Context
    RelationshipContext string

    // Metadata
    Model         string
    InputTokens   int
    OutputTokens  int
    CostUSD       float64
    PromptVersion int
    CreatedAt     string
}
```

### 3.2 DB table (migration v18, same as 1.1)

Объединить в одну миграцию v18. Два варианта:

**(A) Новая таблица `people_cards`** — чище, но ломает все существующие запросы.

**(B) Расширить `communication_guides`** — добавить analysis-поля, сохранить обратную совместимость.

**Выбор: (A)** — новая таблица. Старые таблицы `user_analyses` и `communication_guides` остаются для A/B сравнения. Удалим позже.

```sql
CREATE TABLE IF NOT EXISTS people_cards (
    id INTEGER PRIMARY KEY,
    user_id TEXT NOT NULL,
    period_from REAL NOT NULL,
    period_to REAL NOT NULL,

    -- Computed stats
    message_count INTEGER DEFAULT 0,
    channels_active INTEGER DEFAULT 0,
    threads_initiated INTEGER DEFAULT 0,
    threads_replied INTEGER DEFAULT 0,
    avg_message_length REAL DEFAULT 0,
    active_hours_json TEXT DEFAULT '{}',
    volume_change_pct REAL DEFAULT 0,

    -- Analysis
    summary TEXT DEFAULT '',
    communication_style TEXT DEFAULT '',
    decision_role TEXT DEFAULT '',
    red_flags TEXT DEFAULT '[]',
    highlights TEXT DEFAULT '[]',
    accomplishments TEXT DEFAULT '[]',

    -- Guide
    how_to_communicate TEXT DEFAULT '',
    decision_style TEXT DEFAULT '',
    tactics TEXT DEFAULT '[]',

    -- Context
    relationship_context TEXT DEFAULT '',

    -- Metadata
    model TEXT DEFAULT '',
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    prompt_version INTEGER DEFAULT 0,
    created_at TEXT DEFAULT '',

    UNIQUE(user_id, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_people_cards_user ON people_cards(user_id);
CREATE INDEX IF NOT EXISTS idx_people_cards_period ON people_cards(period_from, period_to);
```

Также `people_card_summaries`:
```sql
CREATE TABLE IF NOT EXISTS people_card_summaries (
    id INTEGER PRIMARY KEY,
    period_from REAL NOT NULL,
    period_to REAL NOT NULL,
    summary TEXT DEFAULT '',
    attention TEXT DEFAULT '[]',
    tips TEXT DEFAULT '[]',
    model TEXT DEFAULT '',
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    prompt_version INTEGER DEFAULT 0,
    created_at TEXT DEFAULT '',
    UNIQUE(period_from, period_to)
);
```

Миграция v18 (одна строка, multi-statement):
```go
17: `ALTER TABLE digests ADD COLUMN people_signals TEXT DEFAULT '[]';
CREATE TABLE IF NOT EXISTS people_cards (...);
CREATE INDEX IF NOT EXISTS idx_people_cards_user ON people_cards(user_id);
CREATE INDEX IF NOT EXISTS idx_people_cards_period ON people_cards(period_from, period_to);
CREATE TABLE IF NOT EXISTS people_card_summaries (...);`,
```

### 3.3 DB operations

**Новый файл:** `internal/db/people_cards.go`

Функции (по аналогии с `guides.go` и `user_analyses.go`):
```go
func (d *DB) UpsertPeopleCard(card PeopleCard) (int64, error)
func (d *DB) GetPeopleCardsForWindow(from, to float64) ([]PeopleCard, error)
func (d *DB) GetLatestPeopleCard(userID string) (*PeopleCard, error)
func (d *DB) GetPeopleCardHistory(userID string, limit int) ([]PeopleCard, error)
func (d *DB) UpsertPeopleCardSummary(s PeopleCardSummary) error
func (d *DB) GetLatestPeopleCardSummary() (*PeopleCardSummary, error)
```

### 3.4 Reduce prompt

**Файл:** `internal/prompts/defaults.go`

Новые prompt ID constants в `internal/prompts/store.go`:
```go
const (
    PeopleReduce = "people.reduce"
    PeopleTeam   = "people.team"
)
```

Добавить в `AllIDs`, `Defaults`, `Descriptions`.

**Промпт `people.reduce`:**
```
You are creating a unified profile card for @%s based on behavioral signals
observed across Slack channels over %s to %s.

%s  ← profile context

Below are TYPED SIGNALS observed in channel context (by the digest pipeline),
plus computed statistics and team norms. Your job is to synthesize these into
a single card that combines:
1. ANALYSIS — classify their communication style, role in decisions, flag concerns
2. COACHING — actionable advice for the viewer on how to work with this person

IMPORTANT: Focus on what makes this person DIFFERENT from team norms.
Do NOT describe typical behavior that matches the team average.

Return ONLY a JSON object:

{
  "summary": "1-2 sentences: what makes this person distinctive. Reference specific signals.",
  "communication_style": "driver|collaborator|executor|observer|facilitator",
  "decision_role": "decision-maker|approver|contributor|observer|blocker",
  "red_flags": ["Specific concerns backed by signals. Empty [] if none."],
  "highlights": ["Positive contributions backed by signals. Empty [] if none."],
  "accomplishments": ["Concrete things delivered/resolved this week from signals."],
  "how_to_communicate": "Paragraph: communication preferences, timing, format. ONLY what's specific to this person vs team norms. If they match the norm, say so briefly and focus on exceptions.",
  "decision_style": "How they participate in decisions — based on bottleneck/rubber_stamping/initiative/blocker signals. If no decision signals, say 'No notable decision patterns this period.'",
  "tactics": ["If X, then Y — specific actionable tactics based on observed signals. Max 3-4."]
}

%s  ← relationship context

Rules:
- Base ALL analysis on the signals provided. Do NOT invent patterns not supported by evidence.
- If a signal appears in multiple channels, it's a PATTERN — emphasize it.
- If conflicting signals exist (e.g., initiative in one channel, disengagement in another), note the CONTRAST.
- Compare stats to team norms: only mention stats that deviate significantly (>30%% from avg).
- Coaching framing: frame guide sections as advice FOR THE VIEWER, not judgments ABOUT the person.
- If relationship is manager→report: be more direct about concerns and accountability.
- If relationship is report→manager: frame tactically (managing up).
- If too few signals for meaningful analysis, say so in summary.
- %s  ← language instruction
- Return valid JSON only

=== SIGNALS FROM CHANNELS ===
%s

=== COMPUTED STATS ===
%s

=== TEAM NORMS ===
%s
```

**Промпт `people.team`:**
```
You are creating a team communication summary for %s to %s.

%s  ← profile context

Below are unified people cards for all team members. Create a summary that
a manager can quickly scan to understand what needs attention.

Return ONLY a JSON object:

{
  "summary": "3-5 sentences: team communication health, dynamics, decision flow.",
  "attention": ["Who needs attention and WHY — name names, cite specific signals and patterns. Be direct."],
  "tips": ["Actionable team-level communication tips based on patterns across people."]
}

Rules:
- Be direct — this is for a busy manager
- Reference specific people by @username
- Cross-reference: if multiple people have bottleneck signals, that's a systemic issue
- Look for signal clusters: multiple conflict signals = team friction
- %s  ← language instruction
- Return valid JSON only

=== PEOPLE CARDS ===
%s
```

### 3.5 Reduce pipeline

**Файл:** `internal/guide/pipeline.go` — рефакторинг

Переименовать пакет: нет, оставить `guide` чтобы не ломать импорты. Внутри переделать.

Ключевые изменения в `processUser()`:

**Было:**
```go
func (p *Pipeline) processUser(ctx context.Context, stats db.UserStats, from, to float64, hasThreadData bool) error {
    msgs, err := p.db.GetMessages(db.MessageOpts{UserID: stats.UserID, ...Limit: 5000...})
    userBlock := p.formatUser(stats, msgs)
    prompt := fmt.Sprintf(tmpl, userName, fromStr, toStr, profileCtx, relCtx, langInstr, userBlock)
    raw, usage, _, err := p.generator.Generate(...)
    result, err := parseGuideResult(raw)
    // store CommunicationGuide
}
```

**Стало:**
```go
func (p *Pipeline) processUser(ctx context.Context, stats db.UserStats, from, to float64, teamNorms *TeamNorms, signalNorms *SignalNorms) error {
    // Загрузить signals вместо сырых сообщений
    channelSignals, err := p.db.GetPeopleSignalsForUser(stats.UserID, from, to)
    signalsBlock := p.formatSignals(channelSignals)
    statsBlock := p.formatStats(stats)
    normsBlock := p.formatTeamNorms(teamNorms, signalNorms)
    relCtx := p.relationshipContext(stats.UserID)

    tmpl, pv := p.getPrompt(prompts.PeopleReduce, defaultPeopleReducePrompt)
    prompt := fmt.Sprintf(tmpl,
        p.userName(stats.UserID), fromStr, toStr,
        p.formatProfileContext(),
        relCtx,
        p.languageInstruction(),
        signalsBlock,
        statsBlock,
        normsBlock,
    )

    raw, usage, _, err := p.generator.Generate(...)
    result, err := parsePeopleCardResult(raw)

    // Store PeopleCard (вместо CommunicationGuide)
    card := db.PeopleCard{
        UserID: stats.UserID,
        PeriodFrom: from, PeriodTo: to,
        // stats...
        // analysis fields from result...
        // guide fields from result...
    }
    _, err = p.db.UpsertPeopleCard(card)
    return err
}
```

Новые helper-функции:

```go
// TeamNorms — средние stats по всем пользователям.
type TeamNorms struct {
    AvgMessages     float64
    AvgChannels     float64
    AvgMsgLength    float64
    AvgThreadsStart float64
    TotalUsers      int
}

// SignalNorms — частотность типов сигналов по команде.
type SignalNorms struct {
    TypeCounts map[string]int  // "bottleneck": 3, "accomplishment": 12, ...
    TotalUsers int
}

func (p *Pipeline) computeTeamNorms(allStats []db.UserStats) *TeamNorms
func (p *Pipeline) computeSignalNorms(allSignals map[string][]db.ChannelSignals) *SignalNorms
func (p *Pipeline) formatSignals(channelSignals []db.ChannelSignals) string
func (p *Pipeline) formatStats(stats db.UserStats) string
func (p *Pipeline) formatTeamNorms(tn *TeamNorms, sn *SignalNorms) string
```

**formatSignals output:**
```
#engineering:
  - [bottleneck] Решение о деплое ждали 4 часа её approve (ts: 1710345600.123)
  - [initiative] Инициировала рефакторинг auth модуля
#product:
  - [rubber_stamping] Одобрила 5 PRD подряд без комментариев
```

**formatTeamNorms output:**
```
Team averages (15 people): 32 msgs/person, 4 channels, 95 char avg msg, 5 threads started
Team active hours: 9-18
Signal distribution: bottleneck 2/15, accomplishment 8/15, after_hours 1/15, ...
```

### 3.6 RunForWindow flow update

```go
func (p *Pipeline) RunForWindow(ctx context.Context, from, to float64) (int, error) {
    p.loadCaches()

    // Check existing (now checks people_cards table)
    if !p.ForceRegenerate {
        existing, err := p.db.GetPeopleCardsForWindow(from, to)
        if len(existing) > 0 { return 0, nil }
    }

    // 1. Compute stats (same as before)
    allStats, err := p.db.ComputeAllUserStats(from, to, DefaultMinMessages)

    // 2. NEW: Load all signals for team norms
    allSignals, err := p.db.GetAllPeopleSignals(from, to)

    // 3. Compute norms
    teamNorms := p.computeTeamNorms(allStats)
    signalNorms := p.computeSignalNorms(allSignals)

    // 4. Filter: only process users who have signals OR enough messages
    // Users with 0 signals but enough messages still get a card (stats-only)

    // 5. Worker pool (same pattern)
    for stats := range tasks {
        p.processUser(ctx, stats, from, to, teamNorms, signalNorms)
    }

    // 6. Team summary (unified)
    p.generateTeamSummary(ctx, from, to)

    return total, nil
}
```

### 3.7 Team summary

```go
func (p *Pipeline) generateTeamSummary(ctx context.Context, from, to float64) error {
    cards, err := p.db.GetPeopleCardsForWindow(from, to)
    // Format all cards into text block
    // Generate via people.team prompt
    // Store in people_card_summaries
}
```

### 3.8 Result types

```go
type PeopleCardResult struct {
    Summary            string   `json:"summary"`
    CommunicationStyle string   `json:"communication_style"`
    DecisionRole       string   `json:"decision_role"`
    RedFlags           []string `json:"red_flags"`
    Highlights         []string `json:"highlights"`
    Accomplishments    []string `json:"accomplishments"`
    HowToCommunicate   string   `json:"how_to_communicate"`
    DecisionStyle      string   `json:"decision_style"`
    Tactics            []string `json:"tactics"`
}

type TeamSummaryResult struct {
    Summary   string   `json:"summary"`
    Attention []string `json:"attention"`
    Tips      []string `json:"tips"`
}
```

### 3.9 Tests

**Файл:** `internal/guide/pipeline_test.go` — обновить

- `TestReducePipeline_WithSignals` — mock signals в DB, mock generator, проверить PeopleCard
- `TestReducePipeline_NoSignals` — пользователь без signals, карточка на основе stats only
- `TestReducePipeline_TeamNorms` — проверить что norms правильно вычисляются
- `TestReducePipeline_SkipExisting` — ForceRegenerate=false пропускает
- `TestReducePipeline_TeamSummary` — проверить генерацию team summary
- `TestReducePipeline_RelationshipContext` — report/manager/peer влияет на промпт

---

## Этап 4: Daemon integration

### 4.1 Порядок выполнения в daemon

**Файл:** `internal/daemon/daemon.go`

Текущий порядок:
```
Phase 1 (parallel): channel digests + analysis + guide
Phase 2: chains
Phase 3: rollups
Phase 4: tracks
```

Новый порядок:
```
Phase 1: channel digests (генерируют people_signals)
Phase 2: chains
Phase 3: rollups
Phase 4: people reduce (читает signals из Phase 1, генерирует people_cards)
Phase 5: tracks
```

**Критично:** reduce ДОЛЖЕН идти ПОСЛЕ channel digests, потому что читает signals из них. Сейчас analysis и guide идут параллельно с digests — это больше нельзя.

Изменения в `daemon.go`:
- Убрать `analysisPipe` field и `SetAnalysisPipeline()`
- `guidePipe` переименовать в `peoplePipe` (или оставить для обратной совместимости)
- В `runSync()`: перенести people reduce после channel digests

```go
// Phase 1: channel digests
n, usage, err := d.digestPipe.RunChannelDigestsOnly(ctx)

// Phase 2: chains
d.chainsPipe.Run(ctx)

// Phase 3: rollups (chain-aware)
d.digestPipe.RunRollups(ctx)

// Phase 4: people reduce (reads signals from Phase 1)
if d.peoplePipe != nil && time.Since(d.lastPeople) > 24*time.Hour {
    d.peoplePipe.Run(ctx)
    d.lastPeople = time.Now()
}

// Phase 5: tracks
d.tracksPipe.Run(ctx)
```

### 4.2 Setup in cmd/sync.go

Обновить инициализацию:
```go
// OLD:
d.SetAnalysisPipeline(analysis.New(database, cfg, gen, logger))
d.SetGuidePipeline(guide.New(database, cfg, gen, logger))

// NEW:
d.SetPeoplePipeline(guide.New(database, cfg, gen, logger))
// analysis pipeline removed
```

### 4.3 Throttle guard

Объединить `lastAnalysis` и `lastGuide` в одну `lastPeople`:
- Файл: `internal/daemon/daemon.go`, поле `lastPeople time.Time`
- Файл для persistence: `~/.watchtower/last_people` (вместо отдельных last_analysis, last_guide)

---

## Этап 5: CLI updates

### 5.1 `cmd/people.go`

Текущий: читает из `user_analyses` таблицы.
Обновить: читать из `people_cards`.

```go
// watchtower people — list all people cards
// watchtower people @alice — detail for one person
// watchtower people generate — run reduce pipeline
// watchtower people --previous — previous week
// watchtower people --weeks N — last N weeks
```

Основные изменения:
- `getLatestAnalysis(userID)` → `getLatestPeopleCard(userID)`
- Отображение: добавить guide-секции (how_to_communicate, tactics)
- `people generate`: вызывать `guide.Pipeline.Run()` вместо `analysis.Pipeline.Run()`

### 5.2 `cmd/guide.go`

Два варианта:
- **(A)** Убрать `guide` command, `people` показывает всё
- **(B)** Оставить `guide` как alias/view, но данные из `people_cards`

**Выбор: (A)** — один command `people`, без путаницы. `guide` → deprecated, показывает подсказку.

---

## Этап 6: Desktop UI

### 6.1 Unified model

**Файл:** `WatchtowerDesktop/Sources/Models/CommunicationGuide.swift`

Переименовать/заменить на `PeopleCard.swift`:

```swift
struct PeopleCard: FetchableRecord, Decodable, Identifiable, Equatable {
    var id: Int64
    var userID: String
    var periodFrom: Double
    var periodTo: Double

    // Stats
    var messageCount: Int
    var channelsActive: Int
    var threadsInitiated: Int
    var threadsReplied: Int
    var avgMessageLength: Double
    var activeHoursJSON: String
    var volumeChangePct: Double

    // Analysis
    var summary: String
    var communicationStyle: String
    var decisionRole: String
    var redFlags: String      // JSON
    var highlights: String    // JSON
    var accomplishments: String // JSON

    // Guide
    var howToCommunicate: String
    var decisionStyle: String
    var tactics: String       // JSON

    var relationshipContext: String
    var model: String
    var inputTokens: Int
    var outputTokens: Int
    var costUSD: Double
    var promptVersion: Int
    var createdAt: String

    // Helpers
    var parsedRedFlags: [String] { ... }
    var parsedHighlights: [String] { ... }
    var parsedAccomplishments: [String] { ... }
    var parsedTactics: [String] { ... }
    var parsedActiveHours: [String: Int] { ... }
    var periodFromDate: Date { ... }
    var periodToDate: Date { ... }
}
```

### 6.2 Queries

**Файл:** `WatchtowerDesktop/Sources/Database/Queries/GuideQueries.swift`

Переименовать в `PeopleCardQueries.swift` или обновить:
- `fetchPeopleCards(db:from:to:)` — все карточки за окно
- `fetchLatestPeopleCard(db:userID:)` — последняя карточка
- `fetchPeopleCardHistory(db:userID:limit:)` — история
- `fetchPeopleCardSummary(db:from:to:)` — team summary

### 6.3 ViewModel

**Файл:** `WatchtowerDesktop/Sources/ViewModels/GuideViewModel.swift`

Обновить или заменить на `PeopleCardViewModel`:
- `cards: [PeopleCard]` вместо `guides: [CommunicationGuide]`
- Добавить `cardSummary: PeopleCardSummary?` (объединённый summary)

**Файл:** `WatchtowerDesktop/Sources/ViewModels/PeopleViewModel.swift`

Упростить: теперь данные из одной таблицы `people_cards` вместо `user_analyses`.

### 6.4 Views

**Файл:** `WatchtowerDesktop/Sources/Views/Guide/GuideDetailView.swift` → `PeopleCardDetailView.swift`

Объединённый UI:
```
┌─────────────────────────────────────────┐
│ @alice                    driver │ DM   │
│ Decision-maker                         │
├─────────────────────────────────────────┤
│ Summary                                │
│ "Alice is the primary decision-maker..."│
├─────────────────────────────────────────┤
│ 📊 Stats grid (msgs, channels, threads)│
├─────────────────────────────────────────┤
│ ✅ Highlights        │ ⚠️ Red Flags     │
│ - Closed 3 bugs      │ - Bottleneck in  │
│ - Initiated refactor  │   #engineering   │
├─────────────────────────────────────────┤
│ 🏆 Accomplishments                     │
│ - Shipped auth refactor                │
│ - Resolved CSS issues for 4 people     │
├─────────────────────────────────────────┤
│ 💬 How to Communicate                  │
│ "Unlike most of the team, Alice is..."  │
├─────────────────────────────────────────┤
│ 🎯 Decision Style                      │
│ "Quick decisions in #frontend, but..."  │
├─────────────────────────────────────────┤
│ ⚡ Tactics                              │
│ • If you need a quick approve → ...     │
│ • If she's blocking in #eng → ...       │
├─────────────────────────────────────────┤
│ 📈 Active Hours chart                  │
└─────────────────────────────────────────┘
```

**Файл:** `WatchtowerDesktop/Sources/Views/People/PersonDetailView.swift`

Перенаправить на PeopleCardDetailView, убрать дублирование.

**Файл:** `WatchtowerDesktop/Sources/Views/Guide/GuideListView.swift` → может быть объединён с PeopleListView

### 6.5 Navigation

Один таб "People" в sidebar. Убрать отдельный "Guide" таб (если он есть).

---

## Этап 7: Cleanup

### 7.1 Deprecated code (не удаляем сразу, помечаем)

После подтверждения что reduce-карточки качественнее:

- `internal/analysis/pipeline.go` → удалить
- `internal/analysis/pipeline_test.go` → удалить
- Промпты `analysis.user`, `analysis.period` → удалить из defaults
- Промпты `guide.user`, `guide.period` → удалить из defaults
- DB таблицы `user_analyses`, `communication_guides`, `guide_summaries`, `period_summaries` → оставить, не мигрировать (данные для истории)

### 7.2 Backward compatibility

- Desktop app: проверить что PeopleCard queries не ломаются если таблица пуста
- CLI: `people` command работает если нет карточек (показывает "run people generate")
- Digest pipeline: если people_signals отсутствует (старые дайджесты), reduce работает только на stats

---

## Порядок реализации (по коммитам)

1. **Этап 1** (MAP): DB migration + structs + prompt + store + tests
   - Можно деплоить независимо, не ломает ничего
   - После деплоя: запустить digest, проверить качество signals

2. **Этап 2** (Queries): DB query functions + tests
   - Чисто backend, не видно пользователю

3. **Этап 3** (REDUCE): Pipeline refactor + new prompts + tests
   - Ключевой этап, самый большой объём работы
   - Параллельно со старыми пайплайнами (оба работают)

4. **Этап 4** (Daemon): Интеграция в daemon
   - Переключение на новый порядок выполнения

5. **Этап 5** (CLI): Обновить commands
   - `people` показывает unified cards

6. **Этап 6** (Desktop): Новая модель + views
   - Unified PeopleCard UI

7. **Этап 7** (Cleanup): Удалить старый код
   - Только после A/B подтверждения качества

---

## Риски и митигации

| Риск | Митигация |
|------|-----------|
| AI не генерирует качественные signals в MAP | Итерировать промпт до этапа 3. Если signals плохие — не начинаем reduce |
| Reduce-карточки хуже старых | Параллельный запуск (этапы 1-3), A/B сравнение |
| Migration v18 ломает существующие данные | Только ADD COLUMN / CREATE TABLE, не ALTER существующее |
| formatMessages не показывает user_id для AI | Добавить user_id в формат на этапе 1 |
| Daemon ordering: reduce до digests | Явная последовательность в daemon, не параллель |
