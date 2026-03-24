# Slack API Rate Limits

Reference: [docs.slack.dev/apis/web-api/rate-limits](https://docs.slack.dev/apis/web-api/rate-limits)

## Tiers

| Tier | Requests/min | Description |
|------|-------------|-------------|
| Tier 1 | 1+ | Minimal, rare access |
| Tier 2 | 20+ | Periodic bursts |
| Tier 3 | 50+ | Sporadic bursts |
| Tier 4 | 100+ | Generous burst |

Values `N+` mean "at least N" — the actual allowance is slightly higher, but Slack does not publish exact burst limits.

## Methods Used by Watchtower

| Method | Tier | Usage |
|--------|------|-------|
| `team.info` | Tier 3 (~50/min) | Sync: workspace metadata |
| `users.list` | Tier 2 (~20/min) | Sync: user list |
| `conversations.list` | Tier 2 (~20/min) | Sync: channel list |
| `conversations.history` | Tier 3 (~50/min) * | Sync: channel messages |
| `conversations.replies` | Tier 3 (~50/min) * | Sync: threads |

### * Restriction for Non-Marketplace Apps (since May 2025)

Source: [changelog/2025-05-terms-rate-limit-update-and-faq](https://api.slack.com/changelog/2025-05-terms-rate-limit-update-and-faq)

Starting **May 29, 2025**, for apps not approved in the Slack Marketplace:
- `conversations.history` — downgraded to **Tier 1 (1 req/min)**
- `conversations.replies` — downgraded to **Tier 1 (1 req/min)**

Starting **September 2, 2025**, this applies to all existing installations.

Watchtower is a custom app (not from the Marketplace), so **Tier 1 restrictions apply** for history and replies.

## How Limits Are Counted

- **Per method, per workspace, per app** — each method has a separate limit per workspace
- When exceeded: HTTP `429 Too Many Requests` with `Retry-After` header (seconds until next request)
- Slack recommendation: budget **1 request per second** as baseline

## Impact on Watchtower

With Tier 1 for `conversations.history` and `conversations.replies` (1 req/min each):

| Operation | Estimated Time |
|-----------|---------------|
| 100 channels, history | ~100 minutes |
| 1000 threads, replies | ~1000 minutes (~17 hours) |

This makes thread sync the main bottleneck. Options:
1. Increase `Retry-After` backoff (already implemented in `internal/slack/ratelimit.go`)
2. Publish the app on Slack Marketplace (restores Tier 3)
3. Disable thread sync (`sync.sync_threads: false`) for a faster initial sync
