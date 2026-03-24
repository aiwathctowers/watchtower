# Анализ системы Communication Guides

## Что хорошо

1. **Философия "коучинг, а не оценка"** — промпт запрещает оценочную лексику и формулирует всё как советы для пользователя ("responds best to...", "when you need X, try..."). Снижает токсичность, повышает полезность.

2. **Учёт отношений (relationship context)** — система определяет тип отношений (менеджер→подчинённый, подчинённый→менеджер, пир) и адаптирует совет. Реально полезная персонализация.

3. **Структура вывода из 7 секций** — summary, preferences, availability, decision process, tactics, approaches, recommendations. Situational tactics в формате "If X, then Y" — особенно удачны.

4. **Двухуровневая архитектура** — per-user guides + team-level summary. Командный саммари ловит cross-team паттерны (таймзоны, bottleneck'и).

5. **Техническая реализация** — worker pool с atomic счётчиками, skip existing, sanitize() от инъекций, extractJSON() устойчив к markdown-обёрткам.

## Что плохо / можно улучшить

1. **Слишком много сообщений в контекст (до 5000)** — при ~100 символов средней длине это ~500K символов. Качество анализа может страдать на больших контекстах. Нужна стратифицированная выборка: 100-200 репрезентативных сообщений (разные каналы, время, темы).

2. **Нет анализа контекста коммуникации** — гайд видит только сообщения одного человека в изоляции. Не видит, на что человек отвечает, как его сообщения воспринимаются другими. Без thread context теряется: скорость реакции, качество ответов, конфликтные паттерны.

3. **`relationshipContext` — хрупкая логика** — `strings.Contains(p.profile.Reports, targetName)` — текстовый поиск по строке. "Alex" найдётся в "Alexander" — false positive. Нужно парсить JSON-массив и сравнивать точно.

4. **Team summary видит урезанную информацию** — в командный контекст попадают только summary, preferences и availability. Recommendations и situational_tactics — самые ценные секции — теряются. Командный гайд не может найти паттерны.

5. **Нет temporal evolution** — гайд генерируется для 7-дневного окна в вакууме. Нет сравнения "раньше отвечал быстро, теперь стал медленнее". VolumeChangePct есть, но больше никакого diff'а.

6. **Промпт перегружен инструкциями** — ~60 строк, дублирует одни и те же идеи в разных формах (JSON-схема + отдельные секции "to analyze"). Расходует токены и может сбивать модель.

7. **Нет валидации качества вывода** — если LLM вернёт пустые строки или generic советы, система сохранит как гайд. Нужна проверка: summary < 30 символов или все массивы пусты → low-quality/отбросить.

8. **Один вызов на человека — дорого** — при 100+ пользователях это 100+ вызовов AI. Можно батчить по 3-5 пользователей, аналогично people analytics (10 за раз).

9. **Карточки людей слишком похожи** — если у людей схожий рабочий паттерн (9-18, треды, короткие сообщения), LLM генерирует одинаковые карточки с разными словами. Причина: AI видит одного человека за раз и не знает, что "отвечает в течение часа" — это норма для всей команды. Полезны только ОТКЛОНЕНИЯ от нормы.

---

## План рефакторинга: Map-Reduce через дайджест-пайплайн

### Проблема

Текущий guide pipeline берёт сырые сообщения одного человека, вырванные из контекста. AI видит:
```
[10:15 #engineering] alice: I think we should wait
[10:32 #engineering] alice: Let me check with the team
[14:05 #product] alice: Approved
```
И пишет generic "Alice is thoughtful and collaborative". Но он не видит, что между 10:15 и 14:05 вся команда ждала её решение 4 часа.

### Решение: piggyback на дайджест-пайплайн

Дайджест-пайплайн **уже видит полный контекст** канала — все сообщения, все участники, все треды. Мы уже платим за этот AI-вызов.

#### Фаза 1: MAP (в дайджест-пайплайне, почти бесплатно)

Добавить секцию `people_signals` в output `DigestResult`. Каждый сигнал — типизированное наблюдение о человеке В КОНТЕКСТЕ канала:

```json
{
  "summary": "...",
  "topics": [...],
  "decisions": [...],
  "action_items": [...],
  "key_messages": [...],
  "people_signals": [
    {
      "user_id": "@alice",
      "signals": [
        {"type": "bottleneck", "detail": "Решение о деплое ждали 4 часа её approve", "evidence_ts": "1234567890.123456"},
        {"type": "accomplishment", "detail": "Инициировала и довела рефакторинг auth до PR"}
      ]
    },
    {
      "user_id": "@bob",
      "signals": [
        {"type": "mediator", "detail": "Разрулил конфликт между фронтом и бэком, предложил компромисс"},
        {"type": "after_hours", "detail": "Писал в 23:00-01:00, 3 дня подряд"}
      ]
    }
  ]
}
```

**Типы сигналов** (объединение из analysis + guide + новые):

| Сигнал | Что ловит | Пример |
|--------|-----------|--------|
| `bottleneck` | Блокирует решения/процессы | "Решение ждали 4 часа её approve" |
| `accomplishment` | Что сделал/решил/продвинул | "Закрыла 3 критических бага за день" |
| `initiative` | Инициирует обсуждения, предлагает | "Предложила новый подход к кешированию" |
| `mediator` | Разруливает конфликты, координирует | "Свёл frontend и backend к компромиссу" |
| `conflict` | Трение, неконструктивность | "Отклонил PR без объяснения, автор переспрашивал дважды" |
| `disengagement` | Молчит, короткие ответы, уход | "3 вопроса к нему остались без ответа" |
| `dropped_ball` | Обещал — не сделал | "Обещал ревью к среде, к пятнице не сделал" |
| `rubber_stamping` | Апрувит без вопросов | "Одобрил 5 PRD подряд без единого комментария" |
| `overloaded` | Слишком много тредов/задач | "Упомянут в 12 тредах за день, отвечает фрагментарно" |
| `after_hours` | Активность вне рабочих часов | "Писал в 23:00-01:00, 3 дня подряд" |
| `knowledge_hub` | К нему идут за ответами | "4 разных человека спрашивали его про auth" |
| `blocker` | Явно блокирует без обоснования | "Заветировал без альтернативы, обсуждение заглохло" |

Промпт-инструкция для MAP:
```
people_signals: For each person who STOOD OUT in this channel (max 5-7 people),
emit typed signals. Only emit signals for NOTABLE behavior — skip people who
just sent routine messages. Each signal must cite specific evidence from messages.
Signal types: bottleneck, accomplishment, initiative, mediator, conflict,
disengagement, dropped_ball, rubber_stamping, overloaded, after_hours,
knowledge_hub, blocker. If no one stood out, return empty array.
```

Доплата: ~150 tok input (инструкция) + ~300 tok output (сигналы) на канал. При 30 каналах = ~13.5K tok ≈ **$0.003-0.005 на haiku**. Ничтожно.

#### Фаза 2: REDUCE (объединённый guide + analysis пайплайн)

Заменяет ОБА текущих пайплайна (analysis + guide) одним лёгким reduce-вызовом на человека.

**Input reduce-вызова @alice:**
```
=== SIGNALS FROM CHANNELS ===
#engineering (window 2026-03-13 — 2026-03-20):
  - [bottleneck] Решение о деплое ждали 4 часа её approve (evidence: 1710345600.123)
  - [initiative] Инициировала рефакторинг auth модуля
  - [after_hours] Единственная кто писала после 20:00 (3 дня)
#product:
  - [rubber_stamping] Одобрила 5 PRD подряд без комментариев
#frontend:
  - [accomplishment] Закрыла 3 критических бага за день
  - [knowledge_hub] К ней обращались 4 человека за советом по CSS

=== COMPUTED STATS ===
Messages: 45 | Channels: 3 | Threads started: 8 | Threads replied: 12
Avg message length: 120 chars | Volume change: +15% vs prev week
Active hours: {"9":5, "10":8, "11":6, "14":4, "20":3, "21":2, "22":1}

=== TEAM NORMS (for comparison) ===
Avg messages/person: 32 | Avg channels: 4 | Avg msg length: 95
Avg threads started: 5 | Team active hours: 9-18
People with after_hours signals: 1/15 (only this person)
People with bottleneck signals: 2/15

=== RELATIONSHIP ===
This person is YOUR DIRECT REPORT.
```

**Output reduce (объединённая карточка guide + analysis):**
```json
{
  "summary": "1-2 предложения: ключевое отличие этого человека от остальных",

  "communication_style": "driver|collaborator|executor|observer|facilitator",
  "decision_role": "decision-maker|approver|contributor|observer|blocker",

  "red_flags": ["Конкретные проблемы с цитатами из сигналов"],
  "highlights": ["Конкретные достижения из сигналов"],
  "accomplishments": ["Что сделал на этой неделе — из accomplishment-сигналов"],

  "how_to_communicate": "Параграф: стиль, формат, тайминг. ТОЛЬКО то что специфично для этого человека относительно team norms",
  "decision_style": "Как участвует в решениях — на основе bottleneck/rubber_stamping/initiative сигналов",
  "tactics": [
    "If X, then Y — конкретные, основанные на signals"
  ]
}
```

Это ОДИН output вместо двух отдельных (analysis + guide). Данные те же, вызов один.

#### Фаза 3: Team summary (объединённый)

Один team summary вместо двух (analysis period + guide period):

```json
{
  "summary": "3-5 предложений: здоровье коммуникации команды",
  "attention": [
    "Кто требует внимания менеджера и почему — с конкретикой"
  ],
  "tips": [
    "Командные советы по коммуникации"
  ]
}
```

### Сравнение стоимости: 3 пайплайна → 1

| | Сейчас (3 пайплайна) | Map-Reduce (1 пайплайн) |
|---|---|---|
| **Digest** | N вызовов × ~240 tok output | N вызовов × ~540 tok output (+signals) |
| **Analysis** | M вызовов × ~150K tok input | **убран** |
| **Guide** | M вызовов × ~150K tok input | **убран** |
| **Reduce** | — | M вызовов × ~2-3K tok input (signals + stats) |
| **Team summaries** | 2 вызова (analysis + guide) | 1 вызов |
| **Итого input (50 юзеров, 30 каналов)** | **~15M tok** | **~175K tok** (~85x дешевле) |
| **AI-вызовов** | 30 + 50 + 50 + 2 = **132** | 30 + 50 + 1 = **81** |
| **Качество** | Сырые сообщения без контекста | Контекстные типизированные сигналы |

### Что нужно изменить

#### 1. MAP: digest pipeline (`internal/digest/`)

**`internal/digest/pipeline.go`**:
- `DigestResult` struct: `+ PeopleSignals []PersonSignals`
- `PersonSignals` struct: `UserID string, Signals []Signal`
- `Signal` struct: `Type string, Detail string, EvidenceTS string`
- `storeDigest()`: сериализовать signals в JSON
- `parseDigestResult()`: парсить новое поле (backwards-compatible: пустой массив если нет)

**`internal/prompts/defaults.go`**:
- `defaultDigestChannel`: добавить `people_signals` в JSON-схему + инструкцию с типами сигналов

**`internal/db/schema.sql`** (migration v18):
- Таблица `digests`: `+ people_signals TEXT DEFAULT '[]'`

**`internal/db/digests.go`**:
- `GetPeopleSignalsForUser(userID, from, to) []ChannelSignals` — собрать сигналы из всех каналов
- `GetAllPeopleSignals(from, to) map[string][]ChannelSignals` — все сигналы всех юзеров (для team norms)

#### 2. REDUCE: объединённый guide+analysis pipeline (`internal/guide/`)

Переделать `internal/guide/pipeline.go`:
- `processUser()`: вместо 5000 сырых сообщений → загрузить signals из digests
- Вычислить team norms: средние stats + частотность каждого типа сигнала
- Передать в reduce-промпт: signals + stats + relationship + team norms
- Парсить объединённый output (guide + analysis поля)

**`internal/prompts/defaults.go`**:
- Новый промпт `PeopleReduce` (заменяет `GuideUser` и `AnalysisUser`)
- Новый промпт `PeopleTeam` (заменяет `GuidePeriod` и `AnalysisPeriod`)

**`internal/db/schema.sql`**:
- Таблица `communication_guides`: добавить analysis-поля (`communication_style`, `decision_role`, `red_flags`, `highlights`, `accomplishments`)
- ИЛИ: новая таблица `people_cards` с объединённой структурой (чище)

**`internal/db/models.go`**:
- `PeopleCard` struct — объединённая модель

#### 3. Удалить старый analysis pipeline

- `internal/analysis/pipeline.go` → удалить (или оставить как fallback)
- `internal/analysis/pipeline_test.go` → удалить
- `cmd/people.go` → перенаправить на новый reduce pipeline
- Daemon: убрать `d.SetAnalysisPipeline()`, guide pipeline теперь выдаёт оба результата

#### 4. Desktop UI

- `CommunicationGuide.swift` → `PeopleCard.swift` (объединённая модель)
- `GuideDetailView.swift` → показывать и guide-секции, и analysis-секции
- `PersonDetailView.swift` → брать данные из того же PeopleCard вместо отдельного UserAnalysis
- Один таб "People" вместо потенциального разделения

### Порядок реализации

1. **MAP**: добавить `people_signals` в digest — минимальные изменения, не ломает существующее
2. **Запустить, собрать сигналы** — посмотреть качество на реальных данных
3. **REDUCE**: новый объединённый reduce pipeline — параллельно со старыми
4. **Сравнить** старые analysis+guide карточки с новыми reduce-карточками
5. **Убрать старое** когда reduce докажет качество
