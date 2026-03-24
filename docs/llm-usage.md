# LLM: расход токенов и точки вызова

## Сводка: все AI-вызовы системы

```mermaid
graph TB
    subgraph DAEMON["Демон (каждые 15 мин)"]
        direction TB
        SYNC["Slack Sync<br/>(нет AI)"] --> P1

        subgraph P1["Фаза 1 — параллельно"]
            CD["Channel Digests<br/>🔥 ГЛАВНЫЙ ПОТРЕБИТЕЛЬ<br/>N каналов × 1 вызов<br/>model: haiku<br/>workers: 2"]
            PA["People Analysis<br/>M юзеров × 1 вызов<br/>model: haiku<br/>workers: 10<br/>⏱️ 1×/24ч"]
        end

        P1 --> P2["Фаза 2: Chains<br/>1 вызов на все решения<br/>model: haiku"]
        P2 --> P3

        subgraph P3["Фаза 3: Rollups"]
            DR["Daily Rollup<br/>1 вызов/день"]
            WR["Weekly Trends<br/>1 вызов/нед"]
        end

        P3 --> P4["Фаза 4: Tracks<br/>N каналов × 1 вызов<br/>model: haiku<br/>workers: 3<br/>⏱️ 1×/час"]
        P4 --> P4U["Tracks Update<br/>N каналов × 1 вызов<br/>workers: 2<br/>⏱️ каждый sync"]
    end

    subgraph INTERACTIVE["Интерактивные (по запросу юзера)"]
        CHAT["Desktop Chat / CLI Ask<br/>model: sonnet<br/>бюджет: 150K токенов"]
        OB["Onboarding<br/>model: sonnet<br/>4-6 оборотов + 2 вызова"]
    end

    style CD fill:#ff6b6b,color:#fff
    style P4U fill:#ffa07a
    style PA fill:#ffa07a
```

---

## Детальная разбивка по пайплайнам

### 1. Channel Digests — главный расход

**Частота:** каждый sync (по умолчанию каждые 15 мин)

**Количество вызовов = количество каналов** с ≥5 новых сообщений с последнего дайджеста.

**Что уходит в промпт (input):**

| Компонент | Размер |
|-----------|--------|
| Шаблон промпта + инструкции | ~1-2K tokens |
| Profile context пользователя | ~200-800 tokens |
| Сообщения канала (макс 500 шт) | **~5K-15K tokens** |
| Language / role instructions | ~100-200 tokens |
| **Итого на 1 канал** | **~6K-18K tokens input** |

**Output:** JSON (summary, topics, decisions, action_items, key_messages) — ~500-2K tokens.

```mermaid
graph LR
    subgraph "Масштабирование Channel Digests"
        A["100 активных каналов<br/>× 15K tokens input<br/>× 1K tokens output"]
        A --> B["= 1.6M tokens/sync<br/>= ~6.4M tokens/час<br/>(4 sync × 15 мин)"]
        B --> C["Haiku: ~$0.64/час<br/>~$15/день"]
    end
```

**Лимиты и пороги:**
- `DefaultDigestMinMsgs = 5` — канал пропускается если < 5 сообщений
- `DefaultTimeRangeLimit = 500` — макс сообщений на канал
- `DefaultDigestWorkers = 2` — параллельные вызовы Claude

---

### 2. Tracks Extract — второй по расходу

**Частота:** throttled, **1 раз в час** (DefaultTracksInterval)

**Количество вызовов = количество каналов** с сообщениями в окне.

**Что уходит в промпт:**

| Компонент | Размер |
|-----------|--------|
| Шаблон промпта | ~2-3K tokens |
| Сообщения канала | **~5K-15K tokens** |
| Существующие tracks (дедупликация) | ~1-3K tokens |
| Chain context | ~0.5-2K tokens |
| Profile context | ~200-800 tokens |
| **Итого на 1 канал** | **~9K-24K tokens input** |

**Особенности:**
- Первый запуск: обрабатывает **30 дней** по-дневно (DefaultInitialHistDays) — огромный разовый расход
- `msgLimit = 50000` — абсолютный потолок сообщений за run
- `DefaultWorkers = 3` — параллельных вызовов
- Token cost делится поровну на кол-во извлечённых items

```mermaid
graph LR
    subgraph "Первый запуск Tracks"
        A["30 дней × 100 каналов<br/>= до 3000 AI вызовов"] --> B["Потенциально<br/>50-70M tokens"]
        B --> C["Haiku: ~$5-7<br/>единоразово"]
    end

    subgraph "Штатный режим"
        D["100 каналов × 1×/час<br/>~20K tokens/канал"] --> E["~2M tokens/час"]
        E --> F["Haiku: ~$0.20/час<br/>~$5/день"]
    end
```

---

### 3. Tracks Update — частый, но лёгкий

**Частота:** **каждый sync** (каждые 15 мин) — без throttle!

**Количество вызовов:** по 1 на канал с активными tracks.

**Что уходит в промпт:**

| Компонент | Размер |
|-----------|--------|
| Список active tracks | ~1-3K tokens |
| Новые сообщения (макс 200) | ~2-5K tokens |
| Thread replies | ~1-3K tokens |
| **Итого на 1 канал** | **~4K-11K tokens** |

**Workers:** 2 параллельных.

```mermaid
graph LR
    subgraph "Tracks Update"
        A["50 каналов с tracks<br/>× 8K tokens<br/>× 4 раза/час"] --> B["~1.6M tokens/час"]
        B --> C["Haiku: ~$0.16/час<br/>~$4/день"]
    end
```

---

### 4. People Analysis — редкий, но тяжёлый

**Частота:** **1 раз в 24 часа** (жёсткий throttle, персистируется на диск).

**Количество вызовов = количество юзеров** с ≥3 сообщениями за 7 дней.

**Что уходит в промпт:**

| Компонент | Размер |
|-----------|--------|
| Шаблон промпта | ~1-2K tokens |
| Статистика юзера (SQL) | ~500-1K tokens |
| Сообщения юзера (макс 5000!) | **~15K-50K tokens** |
| Profile context | ~200-800 tokens |
| **Итого на 1 юзера** | **~17K-54K tokens** |

**Workers:** 10 параллельных (!) + 1 period summary в конце.

```mermaid
graph LR
    subgraph "People Analysis"
        A["200 юзеров<br/>× 30K tokens avg"] --> B["~6M tokens<br/>1 раз в день"]
        B --> C["Haiku: ~$0.60/день"]
    end
```

---

### 5. Chains — лёгкий, 1 вызов

**Частота:** каждый sync, но только если есть unlinked decisions.

**Количество вызовов: 1** (все решения одним запросом) + N обновлений summaries.

| Компонент | Размер |
|-----------|--------|
| System prompt (chainsSystemPrompt) | ~1K tokens |
| Список active chains | ~1-5K tokens |
| Unlinked decisions (14 дней) | ~2-10K tokens |
| **Итого** | **~4K-16K tokens** |

Дёшево. ~$0.01-0.05/день.

---

### 6. Rollups — лёгкие, на основе дайджестов

**Daily:** 1 вызов/день. Input = summaries канальных дайджестов (~3-8K tokens).
**Weekly:** 1 вызов/неделю. Input = daily summaries (~5-15K tokens).

Дёшево. ~$0.01-0.03/день.

---

### 7. Interactive Chat (Desktop / CLI Ask)

**Частота:** по запросу юзера.

**Бюджет контекста: 150K tokens** (DefaultAIContextBudget).

| Tier | Бюджет | Содержимое |
|------|--------|-----------|
| 1. Workspace summary | ~1K | Статистика, watched каналы |
| 2. Priority context | 40% (60K) | Сообщения из watched сущностей |
| 3. Relevant context | 50% (75K) | FTS-результаты по запросу |
| 4. Broad context | 10% (15K) | Обзор активности |

**Model:** sonnet (по умолчанию) — **дороже haiku в ~5 раз**.

Multi-turn: sessionID переиспользуется, system prompt не отправляется повторно.

---

### 8. Onboarding — разовый

5 вызовов суммарно:
1. Health check: ~10 tokens
2. AI-интервью: 4-6 оборотов × ~2K tokens = ~12K
3. Profile extraction: ~3K tokens
4. Context generation: ~3K tokens

**Итого: ~20K tokens, один раз** при первом запуске.

---

## Итоговая таблица расхода (workspace ~100 каналов, ~200 юзеров)

```mermaid
graph TD
    subgraph "Ежедневный расход токенов"
        CD["Channel Digests<br/>~100M tokens/день<br/>💰 ~$10"]
        TU["Tracks Update<br/>~38M tokens/день<br/>💰 ~$4"]
        TE["Tracks Extract<br/>~48M tokens/день<br/>💰 ~$5"]
        PA["People Analysis<br/>~6M tokens/день<br/>💰 ~$0.60"]
        CH["Chains<br/>~0.5M tokens/день<br/>💰 ~$0.05"]
        RU["Rollups<br/>~0.1M tokens/день<br/>💰 ~$0.01"]
    end

    CD --> TOTAL["ИТОГО<br/>~190M tokens/день<br/>~$20/день (haiku)"]
    TU --> TOTAL
    TE --> TOTAL
    PA --> TOTAL
    CH --> TOTAL
    RU --> TOTAL
```

| Пайплайн | Вызовов/день | Tokens input/вызов | Tokens/день | Модель | Доля расхода |
|----------|-------------|-------------------|-------------|--------|-------------|
| **Channel Digests** | ~400 (100 × 4/ч) | ~15K | ~100M | haiku | **52%** |
| **Tracks Extract** | ~2400 (100 × 24/ч) | ~20K | ~48M | haiku | **25%** |
| **Tracks Update** | ~9600 (50 × 4/ч × 48/д) | ~8K | ~38M | haiku | **20%** |
| **People Analysis** | ~200 (1×/день) | ~30K | ~6M | haiku | **3%** |
| **Chains** | ~96 | ~10K | ~0.5M | haiku | <1% |
| **Rollups** | ~2 | ~10K | ~0.1M | haiku | <1% |

---

## Точки оптимизации

```mermaid
graph TB
    subgraph "🔴 Высокий импакт"
        O1["Channel Digests: увеличить<br/>min_messages порог (5→20?)"]
        O2["Tracks Update: добавить<br/>throttle (сейчас КАЖДЫЙ sync!)"]
        O3["Digest: skip каналы без<br/>новых сообщений с прошлого дайджеста"]
    end

    subgraph "🟡 Средний импакт"
        O4["Tracks Extract: увеличить<br/>interval (1ч → 4ч?)"]
        O5["People Analysis: batch<br/>нескольких юзеров в 1 вызов"]
        O6["Сообщения: обрезать до<br/>200-300 вместо 500"]
    end

    subgraph "🟢 Уже оптимизировано"
        O7["People: 1×/24ч throttle ✅"]
        O8["Tracks: 1×/час throttle ✅"]
        O9["Chains: 1 вызов на все ✅"]
        O10["Rollups: на summaries, не сырые данные ✅"]
    end
```

### Ключевые проблемы:

1. **Tracks Update не throttled** — работает КАЖДЫЙ sync (каждые 15 мин). При 50 каналах с tracks = 50 AI вызовов × 4 раза/час = **200 вызовов/час** просто на проверку обновлений.

2. **Channel Digests переобрабатывают** — если в канале 6 сообщений за 15 мин, дайджест создаётся. Через 15 мин ещё 3 сообщения — ещё один дайджест. Можно копить до значимого объёма.

3. **DefaultTimeRangeLimit = 500** — слишком много для каналов с мелкими сообщениями. 200-300 хватило бы.

4. **Первый запуск Tracks = 30 дней** — огромный spike. Можно ограничить до 7 дней.
