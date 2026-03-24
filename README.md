<p align="center">
  <img src="assets/banner.png" alt="Watchtower" width="400" />
</p>

<p align="center">
  AI-powered Slack intelligence for macOS.<br/>
  Syncs your workspace locally, generates briefings, tracks action items, and analyzes team dynamics.
</p>

## What is Watchtower?

Watchtower is a native macOS app that turns your Slack workspace into an actionable knowledge base. A background daemon syncs messages into a local SQLite database, then AI pipelines distill them into briefings, digests, tracks, and people analytics — all without leaving your desktop.

```
[Slack API] → [Local SQLite] → [AI Pipelines] → [Desktop App]
                                      ↓
                              Briefings · Digests
                              Tracks · People · Chains
```

**Key principles:** all data stays on your machine, read-only Slack access, AI runs via Claude CLI.

## Features

- **Daily Briefings** — personalized morning overview: what needs attention, your tasks for the day, what happened, team pulse, coaching tips
- **AI Chat** — ask questions about your workspace in natural language, with multi-turn conversations and model selection
- **Tracks** — action items extracted from conversations: tasks, reviews, approvals, follow-ups with priority, status, and ownership
- **Digests** — channel summaries, daily rollups, weekly trends with running context that preserves topic continuity
- **Chains** — cross-channel discussion threads automatically linked by AI
- **People Analytics** — communication styles, decision roles, activity patterns, team health metrics
- **Full-text Search** — FTS5 search across all synced messages
- **Self-improving AI** — feedback loop with prompt tuning based on your ratings
- **Native Notifications** — alerts for new briefings, tracks, and digests

## Install

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/aiwathctowers/watchtower/main/scripts/install.sh | bash
```

Installs the desktop app to `/Applications` and the `watchtower` CLI to your PATH.

### From source

Requires Go 1.25+, Swift 5.10+, macOS 14+.

```bash
git clone https://github.com/aiwathctowers/watchtower.git
cd watchtower
make app          # Full release build → build/Watchtower.app
# or
make app-dev      # Fast dev build
```

### Pre-built binaries

Download from [Releases](https://github.com/aiwathctowers/watchtower/releases) (macOS Apple Silicon).

## Getting Started

```bash
# 1. Login via Slack OAuth (opens browser)
watchtower auth login

# 2. Start the background daemon
watchtower sync --daemon

# 3. Open Watchtower.app — data appears automatically
```

**Prerequisites:**
- **Slack** — OAuth login handled automatically
- **Claude CLI** — install [Claude Code](https://docs.anthropic.com/en/docs/claude-code) for AI features (or set `ANTHROPIC_API_KEY`)

## How It Works

The daemon (`watchtower sync --daemon`) polls Slack and runs five AI pipelines in sequence after each sync:

1. **Digests** — channel summaries with running context
2. **Tracks** — personal action items for the current user
3. **Chains** — cross-channel discussion linking
4. **People** — team member profiles from interaction patterns
5. **Briefings** — daily aggregation of all above (once per day)

The desktop app reads the same SQLite database via GRDB and updates in real-time.

## Configuration

Config file: `~/.config/watchtower/config.yaml`

```yaml
sync:
  poll_interval: "15m"
  workers: 5
  initial_history_days: 30
digest:
  enabled: true
  model: "claude-haiku-4-5-20251001"
briefing:
  enabled: true
  hour: 8
```

Settings are also editable from the desktop app (Settings tab).

## Data Storage

| What | Where |
|------|-------|
| Database | `~/.local/share/watchtower/<workspace>/watchtower.db` |
| Config | `~/.config/watchtower/config.yaml` |
| Logs | `~/.local/share/watchtower/<workspace>/watchtower.log` |

All data is local. SQLite with WAL mode for concurrent access. The desktop app and daemon share the same database.

## CLI Reference

The CLI provides full access to all features and is required for the daemon:

```bash
watchtower sync [--daemon|--full]   # Sync Slack data
watchtower ask "<question>"         # AI query
watchtower digest                   # View digests
watchtower tracks                   # View action items
watchtower briefing                 # View daily briefing
watchtower people [@user]           # People analytics
watchtower chains                   # Discussion chains
watchtower config set <key> <val>   # Configure
watchtower feedback <good|bad> ...  # Rate AI output
watchtower tune [--apply]           # Improve prompts via AI
```

## Development

```bash
make build        # Build Go CLI only
make test         # Go tests
make test-swift   # Swift tests (395 tests)
make lint-all     # Go + Swift linting
make app-dev      # Fast dev build (CLI + desktop)
make app          # Release build with notarization
```

## License

MIT — see [LICENSE](LICENSE).
