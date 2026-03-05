# Watchtower Usage Guide

## Table of Contents

- [Authentication](#authentication)
- [Initial Configuration](#initial-configuration)
- [Manual Token Setup](#manual-token-setup)
- [Syncing](#syncing)
- [AI Queries](#ai-queries)
- [Catchup](#catchup)
- [Watch List](#watch-list)
- [REPL Mode](#repl-mode)
- [Viewing Logs](#viewing-logs)
- [Listing Channels and Users](#listing-channels-and-users)
- [Configuration Reference](#configuration-reference)
- [Environment Variables](#environment-variables)
- [Database](#database)
- [Daemon Mode](#daemon-mode)
- [Troubleshooting](#troubleshooting)

---

## Authentication

The easiest way to connect Watchtower to your Slack workspace:

```bash
watchtower auth login
```

This opens your browser for Slack OAuth authorization. After you approve, Watchtower automatically:
- Obtains a user token with all required scopes
- Saves it to `~/.config/watchtower/config.yaml` (permissions `600`)
- Sets the workspace name from your Slack team name
- Creates the database directory

Then you're ready to sync:

```bash
watchtower sync
```

### Custom OAuth credentials

By default, Watchtower uses built-in OAuth credentials. To use your own Slack app:

```bash
export WATCHTOWER_OAUTH_CLIENT_ID="your-client-id"
export WATCHTOWER_OAUTH_CLIENT_SECRET="your-client-secret"
watchtower auth login
```

---

## Initial Configuration

After `auth login`, add your Anthropic API key for AI features:

```bash
watchtower config set ai.api_key "sk-ant-..."
# or: export ANTHROPIC_API_KEY="sk-ant-..."
```

View your current config:

```bash
watchtower config show
```

Tokens are masked in the output for safety.

The interactive wizard is also available for manual setup:

```bash
watchtower config init
```

---

## Manual Token Setup

If you prefer to create your own Slack app and manage tokens manually:

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App**
2. Choose **From scratch**, give it a name (e.g., "Watchtower"), select your workspace
3. Go to **OAuth & Permissions** in the sidebar
4. Under **User Token Scopes**, add these 13 scopes:

   | Scope | Purpose |
   |-------|---------|
   | `channels:history` | Read public channel messages |
   | `channels:read` | List public channels |
   | `groups:history` | Read private channel messages |
   | `groups:read` | List private channels |
   | `im:history` | Read direct messages |
   | `im:read` | List DM channels |
   | `mpim:history` | Read group DM messages |
   | `mpim:read` | List group DM channels |
   | `users:read` | List workspace users |
   | `users:read.email` | Read user email addresses |
   | `files:read` | Read file metadata |
   | `reactions:read` | Read message reactions |
   | `team:read` | Read workspace info |

5. Click **Install to Workspace**, authorize the app
6. Copy the **User OAuth Token** (starts with `xoxp-`)
7. Configure Watchtower:

```bash
watchtower config set active_workspace my-company
watchtower config set workspaces.my-company.slack_token "xoxp-..."
```

This is a **read-only** token. Watchtower cannot post, edit, or delete anything in your Slack workspace.

---

## Syncing

### First sync

```bash
watchtower sync
```

This fetches:
1. **Metadata** — workspace info, all users, all channels
2. **Messages** — conversation history for each channel (last 30 days by default)
3. **Threads** — replies for messages with threads

Progress is displayed in real-time. The first sync may take a while depending on workspace size.

### Incremental sync

Running `watchtower sync` again only fetches new messages since the last sync. This is fast (minutes, not hours).

### Sync options

```bash
# Full re-sync (re-fetches within initial_history_days window)
watchtower sync --full

# Sync only specific channels
watchtower sync --channels engineering,general

# Use more workers for faster sync
watchtower sync --workers 10

# Skip DMs and group DMs
watchtower sync --skip-dms

# Override history window for this run (in days)
watchtower sync --days 7
```

### Check sync status

```bash
watchtower status
```

Shows workspace name, database size, last sync time, and counts for channels, users, messages, and threads.

---

## AI Queries

Ask natural-language questions about your Slack workspace:

```bash
watchtower ask "what did the team discuss about the migration?"
watchtower ask "what decisions were made in #engineering this week?"
watchtower ask "summarize what @alice has been working on"
```

### How it works

1. Your question is parsed to extract channels, users, time ranges, and keywords
2. Relevant messages are pulled from the local SQLite database
3. Messages are formatted and sent to Claude as context
4. Claude's response is streamed to your terminal with Slack links

### Options

```bash
# Scope to a specific channel
watchtower ask "what's new?" --channel general

# Limit time range
watchtower ask "any incidents?" --since 4h

# Use a different model
watchtower ask "summarize today" --model claude-sonnet-4-20250514

# Wait for full response (no streaming)
watchtower ask "summarize today" --no-stream
```

### Requirements

- Anthropic API key must be configured (`ai.api_key` or `ANTHROPIC_API_KEY` env var)
- Database must have synced messages (run `watchtower sync` first)

---

## Catchup

Get a summary of what happened since your last check:

```bash
watchtower catchup
```

Watchtower remembers when you last ran catchup and summarizes everything since then.

### Options

```bash
# Override time range
watchtower catchup --since 8h
watchtower catchup --since 2d

# Only watched channels/users
watchtower catchup --watched-only

# Specific channel
watchtower catchup --channel engineering
```

### How it prioritizes

1. **High-priority** watch list channels/users get the most context
2. **Normal-priority** watch list items come next
3. **Other channels** fill remaining context budget
4. Noise is filtered: bot messages, join/leave, short "ok"/"thanks" messages

---

## Watch List

Mark channels and users as important for sync priority and catchup focus:

```bash
# Add to watch list
watchtower watch add #incidents --priority high
watchtower watch add #engineering --priority normal
watchtower watch add @alice --priority high

# Remove
watchtower watch remove #random

# View watch list
watchtower watch list
```

**Priority levels:**
- `high` — synced first, gets 40% of AI context budget
- `normal` — synced after high, gets shared context budget (default)
- `low` — synced after normal, minimal context allocation

---

## REPL Mode

Launch interactive mode by running `watchtower` with no arguments:

```bash
watchtower
```

Type questions in natural language. Special commands:

| Command | Action |
|---------|--------|
| `/sync` | Trigger incremental sync |
| `/status` | Show database stats |
| `/catchup` | Run catchup summary |
| `/help` | List available commands |
| `/quit` or `/exit` | Exit REPL |

---

## Viewing Logs

Every sync run writes logs to `~/.local/share/watchtower/<workspace>/watchtower.log`, regardless of whether `--verbose` is used.

```bash
# Show last 50 lines (default)
watchtower logs

# Show last 100 lines
watchtower logs -n 100

# Follow log output in real-time (like tail -f)
watchtower logs -f
```

This is useful for diagnosing sync issues or monitoring a running daemon without `--verbose`.

---

## Listing Channels and Users

### Channels

```bash
# All channels
watchtower channels

# Filter by type
watchtower channels --type public
watchtower channels --type private
watchtower channels --type dm

# Sort by message count or recent activity
watchtower channels --sort messages
watchtower channels --sort recent
```

### Users

```bash
# All users
watchtower users

# Only active (non-deleted, non-bot) users
watchtower users --active
```

---

## Configuration Reference

Config file location: `~/.config/watchtower/config.yaml`

```yaml
# Which workspace to use by default
active_workspace: my-company

# Workspace definitions (supports multiple)
workspaces:
  my-company:
    slack_token: "xoxp-..."

# AI settings
ai:
  api_key: "sk-ant-..."              # Anthropic API key
  model: "claude-sonnet-4-20250514"  # Claude model
  max_tokens: 4096                   # Max response tokens
  context_budget: 150000             # Context window budget (tokens)

# Sync settings
sync:
  workers: 5                  # Parallel sync workers
  initial_history_days: 30    # Days of history on first sync
  poll_interval: "15m"        # Daemon mode interval
  sync_threads: true          # Sync thread replies
  sync_on_wake: true          # Sync on system wake (daemon mode)

# Watch list (can also be managed via CLI)
watch:
  channels:
    - name: "engineering"
      priority: "high"
    - name: "general"
      priority: "normal"
  users:
    - name: "alice"
      priority: "high"
```

### Setting values via CLI

Use dot-notation for nested keys:

```bash
watchtower config set ai.model claude-sonnet-4-20250514
watchtower config set sync.workers 10
watchtower config set sync.initial_history_days 60
watchtower config set sync.sync_threads false
```

---

## Environment Variables

Environment variables override config file values:

| Variable | Config Key | Description |
|----------|-----------|-------------|
| `WATCHTOWER_SLACK_TOKEN` | `workspaces.*.slack_token` | Slack user token |
| `ANTHROPIC_API_KEY` | `ai.api_key` | Anthropic API key |
| `WATCHTOWER_AI_MODEL` | `ai.model` | Claude model ID |
| `WATCHTOWER_SYNC_WORKERS` | `sync.workers` | Number of sync workers |

Example:

```bash
export WATCHTOWER_SLACK_TOKEN="xoxp-..."
export ANTHROPIC_API_KEY="sk-ant-..."
watchtower sync
```

This is useful for CI environments or if you don't want tokens in a config file.

---

## Database

### Location

```
~/.local/share/watchtower/<workspace-name>/watchtower.db
```

Each workspace gets its own SQLite database file.

### What's stored

- **Workspace** metadata (name, domain)
- **Users** (name, display name, email, bot/deleted status)
- **Channels** (name, type, topic, purpose, member count)
- **Messages** (text, author, timestamp, thread info, reactions)
- **Files** (metadata only — name, type, size, permalink. No file contents.)
- **Sync state** (per-channel progress for resumable sync)
- **Watch list** and **user checkpoints** (for catchup)

### Full-text search

Messages are indexed using SQLite FTS5 with porter stemming. This means:
- "deploy" matches "deployed", "deploying", "deployment"
- "error" matches "errors"
- Searches are fast even with millions of messages

### Storage estimates

| Workspace size | Estimated DB size |
|---------------|-------------------|
| Small (50 channels, 6 months) | 200-500 MB |
| Medium (200 channels, 1 year) | 1-2 GB |
| Large (500 channels, 2 years) | 3-5 GB |

---

## Daemon Mode

Run Watchtower as a persistent syncer:

```bash
# Foreground (Ctrl+C to stop)
watchtower sync --daemon

# Background (detaches from terminal)
watchtower sync --daemon --detach

# Stop a background daemon
watchtower sync --stop
```

The daemon will:
- Run an initial sync immediately on startup
- Poll for new messages at the configured interval (default: every 15 minutes)
- Sync on system wake from sleep (if `sync.sync_on_wake: true`)

When running with `--detach`:
- Logs are written to `~/.local/share/watchtower/<workspace>/daemon.log`
- PID file: `~/.local/share/watchtower/<workspace>/daemon.pid`
- Use `watchtower sync --stop` to gracefully shut it down

The sync state is always saved, so the next run continues where it left off.

### Adjusting poll interval

```bash
watchtower config set sync.poll_interval "5m"   # every 5 minutes
watchtower config set sync.poll_interval "1h"   # every hour
```

---

## Troubleshooting

### "Slack token not configured"

Run `watchtower auth login` to authenticate via browser, or set the token manually:
```bash
watchtower config set workspaces.my-company.slack_token "xoxp-..."
```

### "Anthropic API key not configured"

Only needed for AI features (`ask`, `catchup`, REPL). Set it:
```bash
watchtower config set ai.api_key "sk-ant-..."
```
Or use the environment variable: `export ANTHROPIC_API_KEY="sk-ant-..."`

### Sync is slow

- Disable thread sync for faster initial sync: `watchtower config set sync.sync_threads false`
- Reduce history window: `watchtower config set sync.initial_history_days 7`
- Increase workers: `watchtower sync --workers 10`

Thread sync is the bottleneck (~98% of API calls). Disabling it reduces first sync from hours to ~1 hour for large workspaces.

### "channel_not_found" warnings during sync

Normal. This happens for channels the token doesn't have access to (e.g., you left the channel). Watchtower skips them and continues.

### Database is too large

Reduce history window and re-sync:
```bash
watchtower config set sync.initial_history_days 14
watchtower sync --full
```

### Verbose output

Add `--verbose` to any command for detailed logging:
```bash
watchtower sync --verbose
watchtower ask "test" --verbose
```

### Global flags

These work with any command:

| Flag | Description |
|------|-------------|
| `--workspace <name>` | Use a specific workspace (overrides `active_workspace`) |
| `--config <path>` | Use a custom config file path |
| `--verbose` | Enable detailed logging |
