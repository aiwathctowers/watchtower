# Watchtower

Slack intelligence platform: syncs your workspace into a local SQLite database, generates AI-powered digests, tracks action items and decisions, analyzes team dynamics — all from the terminal or a native macOS app.

**What it does:**
- Syncs channels, users, messages, and threads from Slack into a local database
- AI-generated digests — channel summaries, daily rollups, weekly trends
- Action items extraction — finds tasks, decisions, and follow-ups assigned to you
- People analytics — communication patterns, decision roles, activity trends
- Natural-language Q&A about your workspace via Claude
- "Catchup" summaries of what happened while you were away
- Full-text search across all messages (FTS5 with stemming)
- Watch list for high-priority channels and people
- Native macOS desktop app with real-time updates
- Self-improving prompts via feedback loop
- All data stays on your machine. Read-only Slack access.

```
[Slack API] → [SQLite DB] → [AI Pipeline] → [CLI / Desktop App]
                                ↓
                          Digests, Actions,
                          People, Decisions
```

## Quick Start

```bash
# Install CLI + desktop app
curl -fsSL https://raw.githubusercontent.com/vadimtrunov/watchtower/main/scripts/install.sh | bash

# Login via Slack OAuth (opens browser)
watchtower auth login

# Sync your workspace
watchtower sync

# Ask questions
watchtower ask "what did the team discuss today?"

# See AI-generated digests
watchtower digest

# Check your action items
watchtower actions
```

## Installation

### One-liner (recommended)

Downloads the latest release, installs the desktop app to `/Applications`, and sets up the `watchtower` CLI.

```bash
curl -fsSL https://raw.githubusercontent.com/vadimtrunov/watchtower/main/scripts/install.sh | bash
```

### Homebrew

```bash
brew tap vadimtrunov/tap
brew install watchtower
```

### From source (Go 1.25+)

```bash
go install github.com/vadimtrunov/watchtower@latest
```

### From source (clone)

```bash
git clone https://github.com/vadimtrunov/watchtower.git
cd watchtower
make install
```

### Pre-built binaries

Download from [Releases](https://github.com/vadimtrunov/watchtower/releases). Available for macOS (Apple Silicon).

## Prerequisites

1. **Slack** — `watchtower auth login` handles OAuth automatically. No manual token setup needed.

2. **Claude CLI** (for AI features) — install [Claude Code](https://docs.anthropic.com/en/docs/claude-code) or set an API key:
   ```bash
   watchtower config set ai.api_key "sk-ant-..."
   # or: export ANTHROPIC_API_KEY="sk-ant-..."
   ```

For manual token setup or custom Slack apps, see [docs/usage.md](docs/usage.md#manual-token-setup).

## Commands

### Core

| Command | Description |
|---------|-------------|
| `watchtower` | Interactive REPL mode |
| `watchtower ask "<question>"` | One-shot AI query about your workspace |
| `watchtower catchup` | Summary since last check |
| `watchtower sync` | Incremental sync from Slack |
| `watchtower sync --daemon` | Run as background daemon (polls every 15m) |
| `watchtower status` | Database stats and sync health |

### Intelligence

| Command | Description |
|---------|-------------|
| `watchtower digest` | Channel and daily AI digests |
| `watchtower actions` | Action items assigned to you |
| `watchtower decisions` | Decisions extracted from conversations |
| `watchtower trends` | Weekly trending topics and team pulse |
| `watchtower people` | Team communication analysis |
| `watchtower people @user` | Detailed profile for a specific user |

### Management

| Command | Description |
|---------|-------------|
| `watchtower auth login` | OAuth login via browser |
| `watchtower config init` | Setup wizard |
| `watchtower config set <key> <val>` | Set config value |
| `watchtower config show` | Show current config |
| `watchtower watch add <target>` | Add channel/user to watch list |
| `watchtower watch remove <target>` | Remove from watch list |
| `watchtower watch list` | Show watch list |
| `watchtower channels` | List synced channels |
| `watchtower users` | List synced users |
| `watchtower logs` | View sync log output |
| `watchtower version` | Print version |

### Prompt tuning

| Command | Description |
|---------|-------------|
| `watchtower feedback <good\|bad> <type> <id>` | Rate AI output quality |
| `watchtower prompts list` | List all prompt templates |
| `watchtower prompts show <id>` | View a prompt template |
| `watchtower prompts history <id>` | Prompt version history |
| `watchtower prompts reset <id>` | Reset prompt to default |
| `watchtower tune [id]` | AI-suggested prompt improvements |

### Examples

```bash
# Sync and get a channel summary
watchtower sync
watchtower ask "summarize #engineering for today"

# Catchup on watched channels only, last 4 hours
watchtower catchup --since 4h --watched-only

# Full re-sync (re-fetches history within configured window)
watchtower sync --full

# Run sync as a daemon (polls every 15m by default)
watchtower sync --daemon

# View digests for a specific channel
watchtower digest --channel general

# See your action items
watchtower actions --status inbox

# Check team decisions from this week
watchtower decisions --since 7d

# Analyze a team member
watchtower people @alice

# Add high-priority watch
watchtower watch add #incidents --priority high

# Rate a digest and improve prompts
watchtower feedback good digest abc123
watchtower tune --apply

# Interactive mode
watchtower
> what were the key decisions made this week?
> /catchup
> /status
> /quit
```

## Desktop App

Native macOS app (SwiftUI) that provides a visual interface over the same SQLite database. Real-time updates via GRDB observation when the daemon writes new data.

Features:
- **Dashboard** — sync status, activity feed, quick stats
- **AI Chat** — conversational interface powered by Claude CLI subprocess
- **Digests** — browse channel/daily/weekly summaries with feedback buttons
- **Actions** — action items with priority/status filters and Slack deep links
- **Decisions** — extracted decisions with context and participants
- **People** — communication analytics, activity charts, role analysis
- **Search** — full-text search across all synced messages
- **Notifications** — native macOS alerts for new action items and digests
- **Training** — prompt editor, feedback stats, version history

The desktop app is a read-only UI layer — sync, digest generation, and Slack API interaction run in the Go CLI daemon. See [docs/desktop-app.md](docs/desktop-app.md) for architecture details.

### Building the desktop app

```bash
# Build everything (Go CLI + Swift desktop + .app bundle)
bash scripts/build-app.sh

# Or build Swift app separately
cd WatchtowerDesktop && swift build
```

## First Sync

The initial sync uses Slack's search API for fast bootstrapping — no need to enumerate every channel. Subsequent syncs are incremental and typically take seconds.

For a full history sync (`watchtower sync --full`), timing depends on workspace size due to Slack API rate limits (~50 requests/minute for message history):

| Workspace size | Estimated full sync |
|----------------|---------------------|
| Small (<50 channels) | 2-5 minutes |
| Medium (~200 channels) | 10-20 minutes |
| Large (500+ channels) | 1-2 hours |

To limit history depth:

```bash
watchtower config set sync.initial_history_days 7
```

## Configuration

Config file: `~/.config/watchtower/config.yaml`

```yaml
active_workspace: my-company
workspaces:
  my-company:
    slack_token: "xoxp-..."       # or env: WATCHTOWER_SLACK_TOKEN
ai:
  api_key: ""                     # or env: ANTHROPIC_API_KEY
  model: "claude-sonnet-4-6"
sync:
  workers: 5
  initial_history_days: 30
  poll_interval: "15m"
  sync_threads: true
digest:
  enabled: true
  model: "claude-haiku-4-5-20251001"
  min_messages: 5
```

Environment variables override config file values. See [docs/usage.md](docs/usage.md#configuration-reference) for the full schema.

## Data Storage

- Database: `~/.local/share/watchtower/<workspace>/watchtower.db`
- Sync log: `~/.local/share/watchtower/<workspace>/watchtower.log`
- Config: `~/.config/watchtower/config.yaml`

All data is local. Messages are stored in SQLite with WAL mode for concurrent read/write. Full-text search uses FTS5 with porter stemming. The desktop app reads the same database via GRDB.

## License

MIT — see [LICENSE](LICENSE).
