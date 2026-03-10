# Watchtower

CLI tool that syncs your Slack workspace into a local SQLite database and lets you query it with AI (Claude).

**What it does:**
- Syncs channels, users, messages, and threads from Slack into a local database
- Full-text search across all messages (FTS5 with stemming)
- Ask natural-language questions about your workspace via Claude API
- Get "catchup" summaries of what happened while you were away
- Watch list for high-priority channels and people
- All data stays on your machine. Read-only Slack access.

```
[Slack API] --> [SQLite DB] --> [Claude API] --> [Terminal]
```

## Quick Start

```bash
# 1. Install
go install github.com/vadimtrunov/watchtower@latest

# 2. Login via Slack OAuth (opens browser)
watchtower auth login

# 3. Sync your workspace (first sync may take a while — see below)
watchtower sync

# 4. Ask questions
watchtower ask "what did the team discuss today?"

# 5. Or get a catchup summary
watchtower catchup --since 8h
```

## Installation

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

### Homebrew (after release)

```bash
brew tap vadimtrunov/tap
brew install watchtower
```

### Pre-built binaries

Download from [Releases](https://github.com/vadimtrunov/watchtower/releases). Available for macOS (Intel/Apple Silicon) and Linux (amd64/arm64).

## Prerequisites

1. **Slack** — `watchtower auth login` handles OAuth automatically. No manual token setup needed.

2. **Anthropic API Key** (for AI features) — get one at [console.anthropic.com](https://console.anthropic.com/)
   ```bash
   watchtower config set ai.api_key "sk-ant-..."
   # or: export ANTHROPIC_API_KEY="sk-ant-..."
   ```

For manual token setup or custom Slack apps, see [docs/usage.md](docs/usage.md#manual-token-setup).

## Commands

| Command | Description |
|---------|-------------|
| `watchtower` | Interactive REPL mode |
| `watchtower ask "<question>"` | One-shot AI query |
| `watchtower catchup` | Summary since last check |
| `watchtower sync` | Incremental sync from Slack |
| `watchtower status` | Database stats and sync health |
| `watchtower auth login` | OAuth login via browser |
| `watchtower config init` | Setup wizard |
| `watchtower config set <key> <val>` | Set config value |
| `watchtower config show` | Show current config |
| `watchtower watch add <target>` | Add channel/user to watch list |
| `watchtower watch remove <target>` | Remove from watch list |
| `watchtower watch list` | Show watch list |
| `watchtower logs` | View sync log output |
| `watchtower channels` | List synced channels |
| `watchtower users` | List synced users |
| `watchtower version` | Print version |

### Examples

```bash
# Sync everything, then ask about a specific channel
watchtower sync
watchtower ask "summarize #engineering for today"

# Catchup on watched channels only, last 4 hours
watchtower catchup --since 4h --watched-only

# Full re-sync (re-fetches history within configured window)
watchtower sync --full

# Run sync as a daemon (polls every 15m by default)
watchtower sync --daemon

# Add high-priority watch
watchtower watch add #incidents --priority high
watchtower watch add @alice --priority normal

# List channels sorted by message count
watchtower channels --sort messages

# Interactive mode
watchtower
> what were the key decisions made this week?
> /catchup
> /status
> /quit
```

## First Sync

The initial sync downloads message history for the configured window (default: 30 days). Depending on the workspace size, this can take a while due to Slack API rate limits (~50 requests/minute for message history).

| Workspace size | Estimated first sync |
|----------------|---------------------|
| Small (<50 channels) | 2-5 minutes |
| Medium (~200 channels) | 10-20 minutes |
| Large (500+ channels, thousands of users) | 1-2 hours |

To speed up the first sync, reduce the history window:

```bash
watchtower config set sync.initial_history_days 7
```

Subsequent syncs are incremental and typically take seconds.

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
```

Environment variables override config file values. See [docs/usage.md](docs/usage.md#configuration-reference) for the full schema.

## Data Storage

- Database: `~/.local/share/watchtower/<workspace>/watchtower.db`
- Sync log: `~/.local/share/watchtower/<workspace>/watchtower.log`
- Config: `~/.config/watchtower/config.yaml`

All data is local. Messages are stored in SQLite with WAL mode for concurrent read/write. Full-text search uses FTS5 with porter stemming.

## License

MIT -- see [LICENSE](LICENSE).
