-- Watchtower database schema
-- All tables for Slack workspace data storage

-- Workspace metadata
CREATE TABLE IF NOT EXISTS workspace (
    id                TEXT PRIMARY KEY,  -- Slack team_id
    name              TEXT NOT NULL,
    domain            TEXT NOT NULL DEFAULT '',
    synced_at         TEXT,              -- ISO8601 timestamp of last sync
    search_last_date  TEXT NOT NULL DEFAULT '',  -- YYYY-MM-DD of last search sync
    current_user_id   TEXT NOT NULL DEFAULT ''   -- Slack user_id of the token owner (from auth.test)
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
    last_read    TEXT NOT NULL DEFAULT '',  -- Slack conversations.mark cursor (message ts)
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name);
CREATE INDEX IF NOT EXISTS idx_channels_type ON channels(type);
CREATE INDEX IF NOT EXISTS idx_channels_is_archived ON channels(is_archived);
CREATE INDEX IF NOT EXISTS idx_channels_is_member ON channels(is_member);

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
    ts_unix      REAL GENERATED ALWAYS AS (CASE WHEN INSTR(ts, '.') > 0 THEN CAST(SUBSTR(ts, 1, INSTR(ts, '.') - 1) AS REAL) ELSE CAST(ts AS REAL) END) STORED,
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
    DELETE FROM messages_fts WHERE channel_id = NEW.channel_id AND ts = NEW.ts;
    INSERT INTO messages_fts(text, channel_id, ts, user_id)
    VALUES (NEW.text, NEW.channel_id, NEW.ts, NEW.user_id);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages
BEGIN
    DELETE FROM messages_fts WHERE channel_id = OLD.channel_id AND ts = OLD.ts;
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE OF text, is_deleted ON messages
WHEN OLD.text != NEW.text OR OLD.is_deleted != NEW.is_deleted
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

-- AI-generated digests (summaries of channel activity)
CREATE TABLE IF NOT EXISTS digests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id    TEXT NOT NULL DEFAULT '',  -- '' for cross-channel digests
    period_from   REAL NOT NULL,             -- Unix timestamp
    period_to     REAL NOT NULL,             -- Unix timestamp
    type          TEXT NOT NULL CHECK(type IN ('channel', 'daily', 'weekly')),
    summary       TEXT NOT NULL,
    topics        TEXT NOT NULL DEFAULT '[]',
    decisions     TEXT NOT NULL DEFAULT '[]',
    action_items  TEXT NOT NULL DEFAULT '[]',
    message_count INTEGER NOT NULL DEFAULT 0,
    model         TEXT NOT NULL DEFAULT '',
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    read_at         TEXT,  -- NULL = unread, ISO8601 = when read (local-only)
    prompt_version  INTEGER NOT NULL DEFAULT 0,  -- version of prompt used for generation
    people_signals  TEXT NOT NULL DEFAULT '[]',   -- JSON array of PersonSignals from MAP phase
    UNIQUE(channel_id, type, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_digests_channel ON digests(channel_id);
CREATE INDEX IF NOT EXISTS idx_digests_type ON digests(type);
CREATE INDEX IF NOT EXISTS idx_digests_period ON digests(period_from, period_to);

-- Per-decision read tracking (local-only, Desktop app)
CREATE TABLE IF NOT EXISTS decision_reads (
    digest_id    INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
    decision_idx INTEGER NOT NULL,  -- index in the decisions JSON array
    read_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (digest_id, decision_idx)
);

-- User communication analyses (people analytics with sliding window)
CREATE TABLE IF NOT EXISTS user_analyses (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id             TEXT NOT NULL,
    period_from         REAL NOT NULL,             -- Unix timestamp (window start)
    period_to           REAL NOT NULL,             -- Unix timestamp (window end)
    -- Computed stats (pure SQL, no AI)
    message_count       INTEGER NOT NULL DEFAULT 0,
    channels_active     INTEGER NOT NULL DEFAULT 0,
    threads_initiated   INTEGER NOT NULL DEFAULT 0,
    threads_replied     INTEGER NOT NULL DEFAULT 0,
    avg_message_length  REAL NOT NULL DEFAULT 0,
    active_hours_json   TEXT NOT NULL DEFAULT '{}',  -- {"9":12,"10":8,...}
    volume_change_pct   REAL NOT NULL DEFAULT 0,     -- vs previous window
    -- AI-generated analysis
    summary             TEXT NOT NULL DEFAULT '',
    communication_style TEXT NOT NULL DEFAULT '',
    decision_role       TEXT NOT NULL DEFAULT '',     -- "driver","approver","observer",...
    red_flags           TEXT NOT NULL DEFAULT '[]',   -- JSON array
    highlights          TEXT NOT NULL DEFAULT '[]',   -- JSON array (positive contributions)
    style_details       TEXT NOT NULL DEFAULT '',     -- detailed communication style evaluation
    recommendations     TEXT NOT NULL DEFAULT '[]',   -- JSON array of improvement suggestions
    concerns            TEXT NOT NULL DEFAULT '[]',   -- JSON array of specific issues with examples
    accomplishments     TEXT NOT NULL DEFAULT '[]',   -- JSON array of what was delivered/completed
    -- Metadata
    model               TEXT NOT NULL DEFAULT '',
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL NOT NULL DEFAULT 0,
    prompt_version      INTEGER NOT NULL DEFAULT 0,  -- version of prompt used for generation
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(user_id, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_user_analyses_user ON user_analyses(user_id);
CREATE INDEX IF NOT EXISTS idx_user_analyses_period ON user_analyses(period_from, period_to);

-- Period summaries (cross-user team summary for a time window)
CREATE TABLE IF NOT EXISTS period_summaries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    period_from   REAL NOT NULL,
    period_to     REAL NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    attention     TEXT NOT NULL DEFAULT '[]',  -- JSON array of things to pay attention to
    model         TEXT NOT NULL DEFAULT '',
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_period_summaries_period ON period_summaries(period_from, period_to);

-- Custom workspace emojis (synced via emoji.list API)
CREATE TABLE IF NOT EXISTS custom_emojis (
    name       TEXT PRIMARY KEY,       -- Emoji shortcode (without colons)
    url        TEXT NOT NULL,           -- URL to emoji image (or "alias:other_name")
    alias_for  TEXT NOT NULL DEFAULT '', -- If this is an alias, the target emoji name
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Tracks extracted by AI for the current user (cross-channel)
CREATE TABLE IF NOT EXISTS tracks (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id          TEXT NOT NULL,
    assignee_user_id    TEXT NOT NULL,           -- users.id of the assigned user
    assignee_raw        TEXT NOT NULL DEFAULT '', -- how AI wrote it ("@ivan", "Иван")
    text                TEXT NOT NULL,
    context             TEXT NOT NULL DEFAULT '', -- brief context from the conversation
    source_message_ts   TEXT NOT NULL DEFAULT '', -- Slack timestamp of source message
    source_channel_name TEXT NOT NULL DEFAULT '', -- channel name for display
    status              TEXT NOT NULL DEFAULT 'inbox' CHECK(status IN ('inbox','active','done','dismissed','snoozed')),
    priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    due_date            REAL,                    -- Unix timestamp if AI extracted a deadline
    period_from         REAL NOT NULL,           -- analysis window start
    period_to           REAL NOT NULL,           -- analysis window end
    model               TEXT NOT NULL DEFAULT '',
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    completed_at        TEXT,
    has_updates         INTEGER NOT NULL DEFAULT 0,  -- 1 if source thread has new activity
    last_checked_ts     TEXT NOT NULL DEFAULT '',     -- Slack ts of last checked reply
    snooze_until        REAL,                        -- Unix timestamp when snooze expires
    pre_snooze_status   TEXT NOT NULL DEFAULT '',     -- status to restore after snooze
    participants        TEXT NOT NULL DEFAULT '[]',    -- JSON: participants with stances
    source_refs         TEXT NOT NULL DEFAULT '[]',   -- JSON: key source message references
    requester_name      TEXT NOT NULL DEFAULT '',     -- who made the request (@username)
    requester_user_id   TEXT NOT NULL DEFAULT '',     -- Slack user_id of the requester
    category            TEXT NOT NULL DEFAULT '',     -- code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
    blocking            TEXT NOT NULL DEFAULT '',     -- who/what is blocked if not done
    tags                TEXT NOT NULL DEFAULT '[]',   -- JSON array of project/topic tags
    decision_summary    TEXT NOT NULL DEFAULT '',     -- how the group arrived at the decision
    decision_options    TEXT NOT NULL DEFAULT '[]',   -- JSON array of options if decision pending
    related_digest_ids  TEXT NOT NULL DEFAULT '[]',   -- JSON array of related digest IDs
    sub_items           TEXT NOT NULL DEFAULT '[]',   -- JSON array of sub-tasks with statuses
    prompt_version      INTEGER NOT NULL DEFAULT 0,  -- version of prompt used for generation
    ownership           TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine', 'delegated', 'watching')),
    ball_on             TEXT NOT NULL DEFAULT '',     -- user_id of the person who needs to act next
    owner_user_id       TEXT NOT NULL DEFAULT ''      -- owner of the track (for delegated = report's user_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tracks_dedup ON tracks(channel_id, assignee_user_id, source_message_ts, text);
CREATE INDEX IF NOT EXISTS idx_tracks_assignee ON tracks(assignee_user_id);
CREATE INDEX IF NOT EXISTS idx_tracks_status ON tracks(status);
CREATE INDEX IF NOT EXISTS idx_tracks_period ON tracks(period_from, period_to);

-- Feedback on AI-generated content (thumbs up/down)
CREATE TABLE IF NOT EXISTS feedback (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis')),
    entity_id   TEXT NOT NULL,       -- digest.id, tracks.id, or "digest_id:decision_idx"
    rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),  -- -1 = bad, +1 = good
    comment     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating);

-- Editable AI prompt templates with versioning
CREATE TABLE IF NOT EXISTS prompts (
    id         TEXT PRIMARY KEY,  -- 'digest.channel', 'digest.daily', 'tracks.extract', etc.
    template   TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    language   TEXT NOT NULL DEFAULT '',  -- '' = auto-detect, 'en', 'ru', etc.
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Prompt version history for rollback and audit
CREATE TABLE IF NOT EXISTS prompt_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    prompt_id  TEXT NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    template   TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',  -- "tuned: 12 negative feedbacks on decisions", "manual edit", "rollback to v3"
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_prompt_history_prompt ON prompt_history(prompt_id);
CREATE INDEX IF NOT EXISTS idx_prompt_history_version ON prompt_history(prompt_id, version);

-- Track change history
CREATE TABLE IF NOT EXISTS track_history (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    track_id        INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    event           TEXT NOT NULL,       -- 'created', 'priority_changed', 'context_updated', 'status_changed', 'due_date_changed', 'reopened'
    field           TEXT NOT NULL DEFAULT '',
    old_value       TEXT NOT NULL DEFAULT '',
    new_value       TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_track_history_track ON track_history(track_id);

-- Decision importance corrections (training signal for prompt tuning)
CREATE TABLE IF NOT EXISTS decision_importance_corrections (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    digest_id            INTEGER NOT NULL,
    decision_idx         INTEGER NOT NULL,
    decision_text        TEXT NOT NULL DEFAULT '',
    original_importance  TEXT NOT NULL CHECK(original_importance IN ('high', 'medium', 'low')),
    new_importance       TEXT NOT NULL CHECK(new_importance IN ('high', 'medium', 'low')),
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_dic_dedup ON decision_importance_corrections(digest_id, decision_idx);
CREATE INDEX IF NOT EXISTS idx_dic_created ON decision_importance_corrections(created_at);

-- User profile for personalization (role, team, reports, starred items)
CREATE TABLE IF NOT EXISTS user_profile (
    id                    INTEGER PRIMARY KEY,
    slack_user_id         TEXT NOT NULL UNIQUE,
    role                  TEXT NOT NULL DEFAULT '',
    team                  TEXT NOT NULL DEFAULT '',
    responsibilities      TEXT NOT NULL DEFAULT '[]',    -- JSON array of strings
    reports               TEXT NOT NULL DEFAULT '[]',    -- JSON array of Slack user_ids
    peers                 TEXT NOT NULL DEFAULT '[]',    -- JSON array of Slack user_ids
    manager               TEXT NOT NULL DEFAULT '',      -- Slack user_id
    starred_channels      TEXT NOT NULL DEFAULT '[]',    -- JSON array of channel_ids
    starred_people        TEXT NOT NULL DEFAULT '[]',    -- JSON array of Slack user_ids
    pain_points           TEXT NOT NULL DEFAULT '[]',    -- JSON array from onboarding
    track_focus           TEXT NOT NULL DEFAULT '[]',    -- JSON array of focus areas
    onboarding_done       INTEGER NOT NULL DEFAULT 0,
    custom_prompt_context TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Chains: thematic threads grouping related decisions and tracks over time
CREATE TABLE IF NOT EXISTS chains (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id   INTEGER REFERENCES chains(id) ON DELETE SET NULL,
    title       TEXT NOT NULL,
    slug        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'resolved', 'stale')),
    summary     TEXT NOT NULL DEFAULT '',
    channel_ids TEXT NOT NULL DEFAULT '[]',
    first_seen  REAL NOT NULL,
    last_seen   REAL NOT NULL,
    item_count  INTEGER NOT NULL DEFAULT 0,
    read_at     TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_chains_status ON chains(status);
CREATE INDEX IF NOT EXISTS idx_chains_last_seen ON chains(last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_chains_parent ON chains(parent_id);

-- Chain refs: links chains to decisions (in digests), tracks, and digests themselves
CREATE TABLE IF NOT EXISTS chain_refs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    chain_id      INTEGER NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
    ref_type      TEXT NOT NULL CHECK(ref_type IN ('decision', 'track', 'digest')),
    digest_id     INTEGER NOT NULL DEFAULT 0,
    decision_idx  INTEGER NOT NULL DEFAULT 0,
    track_id      INTEGER NOT NULL DEFAULT 0,
    channel_id    TEXT NOT NULL DEFAULT '',
    timestamp     REAL NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(chain_id, ref_type, digest_id, decision_idx, track_id)
);
CREATE INDEX IF NOT EXISTS idx_chain_refs_chain ON chain_refs(chain_id);
CREATE INDEX IF NOT EXISTS idx_chain_refs_digest ON chain_refs(digest_id);
CREATE INDEX IF NOT EXISTS idx_chain_refs_track ON chain_refs(track_id);

-- User interaction edges (social graph) — computed per analysis window
CREATE TABLE IF NOT EXISTS user_interactions (
    user_a              TEXT NOT NULL,              -- current user ("me")
    user_b              TEXT NOT NULL,              -- the other person
    period_from         REAL NOT NULL,              -- analysis window start (Unix ts)
    period_to           REAL NOT NULL,              -- analysis window end (Unix ts)
    messages_to         INTEGER NOT NULL DEFAULT 0, -- A's messages in channels where B is active
    messages_from       INTEGER NOT NULL DEFAULT 0, -- B's messages in channels where A is active
    shared_channels     INTEGER NOT NULL DEFAULT 0, -- channels where both posted
    thread_replies_to   INTEGER NOT NULL DEFAULT 0, -- A replied to B's threads
    thread_replies_from INTEGER NOT NULL DEFAULT 0, -- B replied to A's threads
    shared_channel_ids  TEXT NOT NULL DEFAULT '[]', -- JSON array of shared channel IDs
    dm_messages_to      INTEGER NOT NULL DEFAULT 0, -- A's DM messages to B
    dm_messages_from    INTEGER NOT NULL DEFAULT 0, -- B's DM messages to A
    mentions_to         INTEGER NOT NULL DEFAULT 0, -- A @-mentioned B
    mentions_from       INTEGER NOT NULL DEFAULT 0, -- B @-mentioned A
    reactions_to        INTEGER NOT NULL DEFAULT 0, -- A reacted to B's messages
    reactions_from      INTEGER NOT NULL DEFAULT 0, -- B reacted to A's messages
    interaction_score   REAL NOT NULL DEFAULT 0,    -- weighted composite score
    connection_type     TEXT NOT NULL DEFAULT '',    -- peer, i_depend, depends_on_me, weak
    PRIMARY KEY (user_a, user_b, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_user_interactions_a ON user_interactions(user_a, period_from, period_to);

-- Communication guides (per-user, per-window coach-style insights)
CREATE TABLE IF NOT EXISTS communication_guides (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id                 TEXT NOT NULL,
    period_from             REAL NOT NULL,             -- Unix timestamp (window start)
    period_to               REAL NOT NULL,             -- Unix timestamp (window end)
    -- Computed stats (pure SQL, no AI)
    message_count           INTEGER NOT NULL DEFAULT 0,
    channels_active         INTEGER NOT NULL DEFAULT 0,
    threads_initiated       INTEGER NOT NULL DEFAULT 0,
    threads_replied         INTEGER NOT NULL DEFAULT 0,
    avg_message_length      REAL NOT NULL DEFAULT 0,
    active_hours_json       TEXT NOT NULL DEFAULT '{}',
    volume_change_pct       REAL NOT NULL DEFAULT 0,
    -- AI-generated guide (coach framing)
    summary                 TEXT NOT NULL DEFAULT '',     -- how to communicate effectively with this person
    communication_preferences TEXT NOT NULL DEFAULT '',   -- preferred style, format, timing
    availability_patterns   TEXT NOT NULL DEFAULT '',     -- when they are most responsive
    decision_process        TEXT NOT NULL DEFAULT '',     -- how they make/participate in decisions
    situational_tactics     TEXT NOT NULL DEFAULT '[]',   -- JSON array: if X happens, do Y
    effective_approaches    TEXT NOT NULL DEFAULT '[]',   -- JSON array: what works well
    recommendations         TEXT NOT NULL DEFAULT '[]',   -- JSON array: actionable tips
    relationship_context    TEXT NOT NULL DEFAULT '',     -- peer/report/manager/cross-team dynamics
    -- Metadata
    model                   TEXT NOT NULL DEFAULT '',
    input_tokens            INTEGER NOT NULL DEFAULT 0,
    output_tokens           INTEGER NOT NULL DEFAULT 0,
    cost_usd                REAL NOT NULL DEFAULT 0,
    prompt_version          INTEGER NOT NULL DEFAULT 0,
    created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(user_id, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_communication_guides_user ON communication_guides(user_id);
CREATE INDEX IF NOT EXISTS idx_communication_guides_period ON communication_guides(period_from, period_to);

-- Guide summaries (cross-user team communication health for a time window)
CREATE TABLE IF NOT EXISTS guide_summaries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    period_from   REAL NOT NULL,
    period_to     REAL NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',     -- team communication health overview
    tips          TEXT NOT NULL DEFAULT '[]',   -- JSON array: team-level communication tips
    model         TEXT NOT NULL DEFAULT '',
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL NOT NULL DEFAULT 0,
    prompt_version INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(period_from, period_to)
);

-- Unified people cards (per-user, per-window — combines analysis + guide)
CREATE TABLE IF NOT EXISTS people_cards (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id             TEXT NOT NULL,
    period_from         REAL NOT NULL,
    period_to           REAL NOT NULL,
    -- Computed stats (pure SQL, no AI)
    message_count       INTEGER NOT NULL DEFAULT 0,
    channels_active     INTEGER NOT NULL DEFAULT 0,
    threads_initiated   INTEGER NOT NULL DEFAULT 0,
    threads_replied     INTEGER NOT NULL DEFAULT 0,
    avg_message_length  REAL NOT NULL DEFAULT 0,
    active_hours_json   TEXT NOT NULL DEFAULT '{}',
    volume_change_pct   REAL NOT NULL DEFAULT 0,
    -- Analysis (from signals reduce)
    summary             TEXT NOT NULL DEFAULT '',
    communication_style TEXT NOT NULL DEFAULT '',
    decision_role       TEXT NOT NULL DEFAULT '',
    red_flags           TEXT NOT NULL DEFAULT '[]',
    highlights          TEXT NOT NULL DEFAULT '[]',
    accomplishments     TEXT NOT NULL DEFAULT '[]',
    -- Guide (coaching framing)
    how_to_communicate  TEXT NOT NULL DEFAULT '',
    decision_style      TEXT NOT NULL DEFAULT '',
    tactics             TEXT NOT NULL DEFAULT '[]',
    -- Context
    relationship_context TEXT NOT NULL DEFAULT '',
    -- Metadata
    model               TEXT NOT NULL DEFAULT '',
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL NOT NULL DEFAULT 0,
    prompt_version      INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(user_id, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_people_cards_user ON people_cards(user_id);
CREATE INDEX IF NOT EXISTS idx_people_cards_period ON people_cards(period_from, period_to);

-- People card summaries (cross-user team health for a time window)
CREATE TABLE IF NOT EXISTS people_card_summaries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    period_from   REAL NOT NULL,
    period_to     REAL NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    attention     TEXT NOT NULL DEFAULT '[]',
    tips          TEXT NOT NULL DEFAULT '[]',
    model         TEXT NOT NULL DEFAULT '',
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL NOT NULL DEFAULT 0,
    prompt_version INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(period_from, period_to)
);
