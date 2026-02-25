-- Watchtower database schema
-- All tables for Slack workspace data storage

-- Workspace metadata
CREATE TABLE IF NOT EXISTS workspace (
    id          TEXT PRIMARY KEY,  -- Slack team_id
    name        TEXT NOT NULL,
    domain      TEXT NOT NULL DEFAULT '',
    synced_at   TEXT              -- ISO8601 timestamp of last sync
);

-- Users
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,  -- Slack user ID
    name          TEXT NOT NULL,
    display_name  TEXT NOT NULL DEFAULT '',
    real_name     TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL DEFAULT '',
    is_bot        INTEGER NOT NULL DEFAULT 0,
    is_deleted    INTEGER NOT NULL DEFAULT 0,
    profile_json  TEXT NOT NULL DEFAULT '{}',
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_users_name ON users(name);
CREATE INDEX IF NOT EXISTS idx_users_is_bot ON users(is_bot);

-- Channels
CREATE TABLE IF NOT EXISTS channels (
    id           TEXT PRIMARY KEY,  -- Slack channel ID
    name         TEXT NOT NULL,
    type         TEXT NOT NULL CHECK(type IN ('public', 'private', 'dm', 'group_dm')),
    topic        TEXT NOT NULL DEFAULT '',
    purpose      TEXT NOT NULL DEFAULT '',
    is_archived  INTEGER NOT NULL DEFAULT 0,
    is_member    INTEGER NOT NULL DEFAULT 0,
    dm_user_id   TEXT,
    num_members  INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name);
CREATE INDEX IF NOT EXISTS idx_channels_type ON channels(type);
CREATE INDEX IF NOT EXISTS idx_channels_is_archived ON channels(is_archived);

-- Messages
CREATE TABLE IF NOT EXISTS messages (
    channel_id   TEXT NOT NULL,
    ts           TEXT NOT NULL,       -- Slack timestamp (unique message ID)
    user_id      TEXT NOT NULL DEFAULT '',
    text         TEXT NOT NULL DEFAULT '',
    thread_ts    TEXT,
    reply_count  INTEGER NOT NULL DEFAULT 0,
    is_edited    INTEGER NOT NULL DEFAULT 0,
    is_deleted   INTEGER NOT NULL DEFAULT 0,
    subtype      TEXT NOT NULL DEFAULT '',
    permalink    TEXT NOT NULL DEFAULT '',
    ts_unix      REAL GENERATED ALWAYS AS (CAST(SUBSTR(ts, 1, INSTR(ts, '.') - 1) AS REAL)) STORED,
    raw_json     TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (channel_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(channel_id, thread_ts);
CREATE INDEX IF NOT EXISTS idx_messages_ts_unix ON messages(ts_unix);
CREATE INDEX IF NOT EXISTS idx_messages_channel_ts_unix ON messages(channel_id, ts_unix);

-- FTS5 virtual table for full-text search on messages
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    text,
    channel_id UNINDEXED,
    ts UNINDEXED,
    user_id UNINDEXED,
    tokenize='porter unicode61'
);

-- Triggers to keep FTS index in sync with messages table
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages
WHEN NEW.text != '' AND NEW.is_deleted = 0
BEGIN
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    VALUES (NEW.text, NEW.channel_id, NEW.ts, NEW.user_id);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages
BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF text, is_deleted ON messages
BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    SELECT NEW.text, NEW.channel_id, NEW.ts, NEW.user_id
    WHERE NEW.text != '' AND NEW.is_deleted = 0;
END;

-- Reactions
CREATE TABLE IF NOT EXISTS reactions (
    channel_id  TEXT NOT NULL,
    message_ts  TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    emoji       TEXT NOT NULL,
    PRIMARY KEY (channel_id, message_ts, user_id, emoji)
);
CREATE INDEX IF NOT EXISTS idx_reactions_message ON reactions(channel_id, message_ts);

-- Files
CREATE TABLE IF NOT EXISTS files (
    id                 TEXT PRIMARY KEY,  -- Slack file ID
    message_channel_id TEXT NOT NULL DEFAULT '',
    message_ts         TEXT NOT NULL DEFAULT '',
    name               TEXT NOT NULL DEFAULT '',
    mimetype           TEXT NOT NULL DEFAULT '',
    size               INTEGER NOT NULL DEFAULT 0,
    permalink          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_files_message ON files(message_channel_id, message_ts);

-- Sync state tracking per channel
CREATE TABLE IF NOT EXISTS sync_state (
    channel_id              TEXT PRIMARY KEY,
    last_synced_ts          TEXT NOT NULL DEFAULT '',
    oldest_synced_ts        TEXT NOT NULL DEFAULT '',
    is_initial_sync_complete INTEGER NOT NULL DEFAULT 0,
    cursor                  TEXT NOT NULL DEFAULT '',
    messages_synced         INTEGER NOT NULL DEFAULT 0,
    last_sync_at            TEXT,
    error                   TEXT NOT NULL DEFAULT ''
);

-- Watch list for priority tracking
CREATE TABLE IF NOT EXISTS watch_list (
    entity_type TEXT NOT NULL CHECK(entity_type IN ('channel', 'user')),
    entity_id   TEXT NOT NULL,
    entity_name TEXT NOT NULL DEFAULT '',
    priority    TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('high', 'normal', 'low')),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (entity_type, entity_id)
);

-- User checkpoints (singleton table for last catchup time)
CREATE TABLE IF NOT EXISTS user_checkpoints (
    id              INTEGER PRIMARY KEY CHECK(id = 1),
    last_checked_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
