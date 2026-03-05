# Slack API Rate Limits

Reference: [docs.slack.dev/apis/web-api/rate-limits](https://docs.slack.dev/apis/web-api/rate-limits)

## Tiers

| Tier | Requests/min | Описание |
|------|-------------|----------|
| Tier 1 | 1+ | Минимальный, редкий доступ |
| Tier 2 | 20+ | Периодические burst'ы |
| Tier 3 | 50+ | Спорадические burst'ы |
| Tier 4 | 100+ | Щедрый burst |

Значения `N+` означают "не менее N" — реальный запас чуть больше, но Slack не публикует точные burst-лимиты.

## Методы, используемые Watchtower

| Метод | Tier | Где используем |
|-------|------|---------------|
| `team.info` | Tier 3 (~50/min) | Sync: метаданные workspace |
| `users.list` | Tier 2 (~20/min) | Sync: список пользователей |
| `conversations.list` | Tier 2 (~20/min) | Sync: список каналов |
| `conversations.history` | Tier 3 (~50/min) * | Sync: сообщения каналов |
| `conversations.replies` | Tier 3 (~50/min) * | Sync: треды |

### * Ограничение для non-Marketplace приложений (с мая 2025)

Источник: [changelog/2025-05-terms-rate-limit-update-and-faq](https://api.slack.com/changelog/2025-05-terms-rate-limit-update-and-faq)

С **29 мая 2025** для приложений, не одобренных в Slack Marketplace:
- `conversations.history` — понижен до **Tier 1 (1 req/min)**
- `conversations.replies` — понижен до **Tier 1 (1 req/min)**

С **2 сентября 2025** это распространяется на все существующие установки.

Watchtower — кастомное приложение (не из Marketplace), поэтому **действуют ограничения Tier 1** для history и replies.

## Как считаются лимиты

- **Per method, per workspace, per app** — лимит на каждый метод отдельно, на каждый workspace отдельно
- При превышении: HTTP `429 Too Many Requests` с заголовком `Retry-After` (секунды до повторного запроса)
- Рекомендация Slack: закладывать **1 запрос в секунду** как baseline

## Влияние на Watchtower

При Tier 1 для `conversations.history` и `conversations.replies` (1 req/min каждый):

| Операция | Оценка времени |
|----------|---------------|
| 100 каналов, history | ~100 минут |
| 1000 тредов, replies | ~1000 минут (~17 часов) |

Это делает thread sync основным bottleneck. Варианты:
1. Увеличить `Retry-After` backoff (уже реализовано в `internal/slack/ratelimit.go`)
2. Опубликовать приложение в Slack Marketplace (возвращает Tier 3)
3. Отключить sync тредов (`sync.sync_threads: false`) для быстрого первого sync
