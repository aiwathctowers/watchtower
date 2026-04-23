-- Watchtower database schema
-- All tables for Slack workspace data storage

-- Workspace metadata
CREATE TABLE IF NOT EXISTS workspace (
    id                TEXT PRIMARY KEY,  -- Slack team_id
    name              TEXT NOT NULL,
    domain            TEXT NOT NULL DEFAULT '',
    synced_at         TEXT,              -- ISO8601 timestamp of last sync
    search_last_date  TEXT NOT NULL DEFAULT '',  -- YYYY-MM-DD of last search sync
    current_user_id   TEXT NOT NULL DEFAULT '',  -- Slack user_id of the token owner (from auth.test)
    inbox_last_processed_ts REAL NOT NULL DEFAULT 0  -- Unix timestamp of last inbox pipeline run
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
    is_stub       INTEGER NOT NULL DEFAULT 0,
    is_bot_override INTEGER DEFAULT NULL,  -- NULL=use Slack value, 1=force bot, 0=force not-bot
    is_muted_for_llm INTEGER NOT NULL DEFAULT 0,  -- 1=exclude this user's messages from AI analysis
    profile_json  TEXT NOT NULL DEFAULT '{}',
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_users_name ON users(name);
CREATE INDEX IF NOT EXISTS idx_users_is_bot ON users(is_bot);
CREATE INDEX IF NOT EXISTS idx_users_is_stub ON users(is_stub);

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
    people_signals  TEXT NOT NULL DEFAULT '[]',   -- JSON array of PersonSignals from MAP phase (legacy)
    situations      TEXT NOT NULL DEFAULT '[]',   -- JSON array of Situation objects from channel digest
    running_summary TEXT NOT NULL DEFAULT '',     -- JSON running context for next digest (channel memory)
    UNIQUE(channel_id, type, period_from, period_to)
);
CREATE INDEX IF NOT EXISTS idx_digests_channel ON digests(channel_id);
CREATE INDEX IF NOT EXISTS idx_digests_type ON digests(type);
CREATE INDEX IF NOT EXISTS idx_digests_period ON digests(period_from, period_to);

-- Digest participants: which users were mentioned in each digest's situations
CREATE TABLE IF NOT EXISTS digest_participants (
    digest_id      INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
    user_id        TEXT NOT NULL,
    situation_idx  INTEGER NOT NULL DEFAULT 0,
    role           TEXT NOT NULL DEFAULT '',
    topic_id       INTEGER NOT NULL DEFAULT 0,  -- 0 = legacy (pre-v39), >0 = digest_topics.id
    PRIMARY KEY (digest_id, user_id, situation_idx)
);
CREATE INDEX IF NOT EXISTS idx_digest_participants_user ON digest_participants(user_id);

-- Digest topics: each digest is decomposed into granular, self-contained topics.
-- Each topic carries its own decisions, action_items, situations, key_messages.
CREATE TABLE IF NOT EXISTS digest_topics (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    digest_id     INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
    idx           INTEGER NOT NULL DEFAULT 0,
    title         TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    decisions     TEXT NOT NULL DEFAULT '[]',
    action_items  TEXT NOT NULL DEFAULT '[]',
    situations    TEXT NOT NULL DEFAULT '[]',
    key_messages  TEXT NOT NULL DEFAULT '[]',
    UNIQUE(digest_id, idx)
);
CREATE INDEX IF NOT EXISTS idx_digest_topics_digest ON digest_topics(digest_id);

-- Per-decision read tracking (local-only, Desktop app)
CREATE TABLE IF NOT EXISTS decision_reads (
    digest_id    INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
    decision_idx INTEGER NOT NULL,  -- index in the decisions JSON array within a topic
    topic_id     INTEGER NOT NULL DEFAULT 0,  -- 0 = legacy (pre-v39), >0 = digest_topics.id
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

-- Action-item tracks (hybrid v2 extraction + cross-channel merge)
CREATE TABLE IF NOT EXISTS tracks (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    assignee_user_id    TEXT NOT NULL DEFAULT '',           -- user the track is for
    text                TEXT NOT NULL,                      -- actionable description
    context             TEXT NOT NULL DEFAULT '',           -- 3-5 sentence explanation
    category            TEXT NOT NULL DEFAULT 'task',       -- code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
    ownership           TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
    ball_on             TEXT NOT NULL DEFAULT '',           -- user_id of next actor
    owner_user_id       TEXT NOT NULL DEFAULT '',           -- for delegated: report's user_id
    requester_name      TEXT NOT NULL DEFAULT '',           -- who made the request
    requester_user_id   TEXT NOT NULL DEFAULT '',           -- requester's Slack user_id
    blocking            TEXT NOT NULL DEFAULT '',           -- who/what is blocked
    decision_summary    TEXT NOT NULL DEFAULT '',           -- how group arrived at decision
    decision_options    TEXT NOT NULL DEFAULT '[]',         -- JSON: [{option, supporters, pros, cons}]
    sub_items           TEXT NOT NULL DEFAULT '[]',         -- JSON: [{text, status}]
    participants        TEXT NOT NULL DEFAULT '[]',         -- JSON: [{name, user_id, stance}]
    source_refs         TEXT NOT NULL DEFAULT '[]',         -- JSON: [{ts, author, text}] key message quotes
    tags                TEXT NOT NULL DEFAULT '[]',         -- JSON: ["tag1","tag2"]
    channel_ids         TEXT NOT NULL DEFAULT '[]',         -- JSON: ["C1","C2"] cross-channel
    related_digest_ids  TEXT NOT NULL DEFAULT '[]',         -- JSON: [1,2,3]
    priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    due_date            REAL,                               -- Unix timestamp if deadline extracted
    fingerprint         TEXT NOT NULL DEFAULT '[]',         -- JSON: extracted entities for dedup
    read_at             TEXT,                               -- NULL=unread, ISO8601=when read
    has_updates         INTEGER NOT NULL DEFAULT 0,
    dismissed_at        TEXT NOT NULL DEFAULT '',           -- ''=active, ISO8601=when dismissed
    model               TEXT NOT NULL DEFAULT '',
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL NOT NULL DEFAULT 0,
    prompt_version      INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_tracks_priority ON tracks(priority);
CREATE INDEX IF NOT EXISTS idx_tracks_has_updates ON tracks(has_updates);
CREATE INDEX IF NOT EXISTS idx_tracks_updated ON tracks(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracks_ownership ON tracks(ownership);
CREATE INDEX IF NOT EXISTS idx_tracks_assignee ON tracks(assignee_user_id);

-- Personal action items (tasks)
CREATE TABLE IF NOT EXISTS tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    text            TEXT NOT NULL,
    intent          TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
    priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    ownership       TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
    ball_on         TEXT NOT NULL DEFAULT '',
    due_date        TEXT NOT NULL DEFAULT '',           -- YYYY-MM-DDTHH:MM or ""
    snooze_until    TEXT NOT NULL DEFAULT '',           -- YYYY-MM-DDTHH:MM or ""
    blocking        TEXT NOT NULL DEFAULT '',
    tags            TEXT NOT NULL DEFAULT '[]',
    sub_items       TEXT NOT NULL DEFAULT '[]',
    notes           TEXT NOT NULL DEFAULT '[]',
    source_type     TEXT NOT NULL DEFAULT 'manual' CHECK(source_type IN ('track','digest','briefing','manual','chat','inbox','jira')),
    source_id       TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_due_date ON tasks(due_date);
CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at DESC);

-- Inbox items — messages awaiting user response (@mentions, DMs)
CREATE TABLE IF NOT EXISTS inbox_items (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id      TEXT NOT NULL,
    message_ts      TEXT NOT NULL,
    thread_ts       TEXT NOT NULL DEFAULT '',
    sender_user_id  TEXT NOT NULL,
    trigger_type    TEXT NOT NULL CHECK(trigger_type IN ('mention','dm','thread_reply','reaction')),
    snippet         TEXT NOT NULL DEFAULT '',
    context         TEXT NOT NULL DEFAULT '',
    raw_text        TEXT NOT NULL DEFAULT '',
    permalink       TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
    priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    ai_reason       TEXT NOT NULL DEFAULT '',
    resolved_reason TEXT NOT NULL DEFAULT '',
    snooze_until    TEXT NOT NULL DEFAULT '',
    waiting_user_ids TEXT NOT NULL DEFAULT '',
    task_id         INTEGER,
    read_at         TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(channel_id, message_ts)
);
CREATE INDEX IF NOT EXISTS idx_inbox_items_status ON inbox_items(status);
CREATE INDEX IF NOT EXISTS idx_inbox_items_priority ON inbox_items(priority);
CREATE INDEX IF NOT EXISTS idx_inbox_items_updated ON inbox_items(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_inbox_items_sender ON inbox_items(sender_user_id);
CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze ON inbox_items(snooze_until);

-- Feedback on AI-generated content (thumbs up/down)
CREATE TABLE IF NOT EXISTS feedback (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis', 'briefing', 'task', 'inbox')),
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

-- Decision importance corrections (training signal for prompt tuning)
CREATE TABLE IF NOT EXISTS decision_importance_corrections (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    digest_id            INTEGER NOT NULL,
    decision_idx         INTEGER NOT NULL,
    topic_id             INTEGER NOT NULL DEFAULT 0,  -- 0 = legacy (pre-v39), >0 = digest_topics.id
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
    communication_guide TEXT NOT NULL DEFAULT '',
    decision_style      TEXT NOT NULL DEFAULT '',
    tactics             TEXT NOT NULL DEFAULT '[]',
    -- Context
    relationship_context TEXT NOT NULL DEFAULT '',
    -- Status
    status              TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'insufficient_data')),
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

-- Daily personalized briefings
CREATE TABLE IF NOT EXISTS briefings (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id     TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL,
    date             TEXT NOT NULL,              -- YYYY-MM-DD
    role             TEXT NOT NULL DEFAULT '',
    attention        TEXT NOT NULL DEFAULT '[]',
    your_day         TEXT NOT NULL DEFAULT '[]',
    what_happened    TEXT NOT NULL DEFAULT '[]',
    team_pulse       TEXT NOT NULL DEFAULT '[]',
    coaching         TEXT NOT NULL DEFAULT '[]',
    model            TEXT NOT NULL DEFAULT '',
    input_tokens     INTEGER NOT NULL DEFAULT 0,
    output_tokens    INTEGER NOT NULL DEFAULT 0,
    cost_usd         REAL NOT NULL DEFAULT 0,
    prompt_version   INTEGER NOT NULL DEFAULT 0,
    read_at          TEXT,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(user_id, date)
);
CREATE INDEX IF NOT EXISTS idx_briefings_user_date ON briefings(user_id, date DESC);

-- Per-channel user settings (mute for AI, favorite)
CREATE TABLE IF NOT EXISTS channel_settings (
    channel_id       TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    is_muted_for_llm INTEGER NOT NULL DEFAULT 0,
    is_favorite      INTEGER NOT NULL DEFAULT 0,
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Pipeline run history — logs every pipeline invocation (CLI, daemon, desktop)
CREATE TABLE IF NOT EXISTS pipeline_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    pipeline         TEXT NOT NULL,                          -- 'digests', 'tracks', 'people', 'briefing'
    source           TEXT NOT NULL DEFAULT 'cli',            -- 'cli', 'daemon'
    model            TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'done', 'error')),
    error_msg        TEXT NOT NULL DEFAULT '',
    items_found      INTEGER NOT NULL DEFAULT 0,
    input_tokens     INTEGER NOT NULL DEFAULT 0,
    output_tokens    INTEGER NOT NULL DEFAULT 0,
    cost_usd         REAL NOT NULL DEFAULT 0,
    total_api_tokens INTEGER NOT NULL DEFAULT 0,
    period_from      REAL,
    period_to        REAL,
    started_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    finished_at      TEXT,
    duration_seconds REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_pipeline ON pipeline_runs(pipeline);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_started ON pipeline_runs(started_at DESC);

-- Pipeline steps — per-step detail within a run
CREATE TABLE IF NOT EXISTS pipeline_steps (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id           INTEGER NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    step             INTEGER NOT NULL,
    total            INTEGER NOT NULL,
    status           TEXT NOT NULL DEFAULT '',
    channel_id       TEXT NOT NULL DEFAULT '',
    channel_name     TEXT NOT NULL DEFAULT '',
    input_tokens     INTEGER NOT NULL DEFAULT 0,
    output_tokens    INTEGER NOT NULL DEFAULT 0,
    cost_usd         REAL NOT NULL DEFAULT 0,
    total_api_tokens INTEGER NOT NULL DEFAULT 0,
    message_count    INTEGER NOT NULL DEFAULT 0,
    period_from      REAL,
    period_to        REAL,
    duration_seconds REAL NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_pipeline_steps_run ON pipeline_steps(run_id);

-- Google Calendar calendars
CREATE TABLE IF NOT EXISTS calendar_calendars (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    is_primary  INTEGER NOT NULL DEFAULT 0,
    is_selected INTEGER NOT NULL DEFAULT 1,
    color       TEXT NOT NULL DEFAULT '',
    synced_at   TEXT NOT NULL DEFAULT ''
);

-- Calendar events (synced from Google Calendar)
CREATE TABLE IF NOT EXISTS calendar_events (
    id              TEXT PRIMARY KEY,
    calendar_id     TEXT NOT NULL REFERENCES calendar_calendars(id),
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    location        TEXT NOT NULL DEFAULT '',
    start_time      TEXT NOT NULL,           -- ISO8601
    end_time        TEXT NOT NULL,           -- ISO8601
    organizer_email TEXT NOT NULL DEFAULT '',
    attendees       TEXT NOT NULL DEFAULT '[]',  -- JSON array
    is_recurring    INTEGER NOT NULL DEFAULT 0,
    is_all_day      INTEGER NOT NULL DEFAULT 0,
    event_status    TEXT NOT NULL DEFAULT 'confirmed',
    event_type      TEXT NOT NULL DEFAULT '',
    html_link       TEXT NOT NULL DEFAULT '',
    raw_json        TEXT NOT NULL DEFAULT '{}',
    synced_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_calendar_events_calendar ON calendar_events(calendar_id);
CREATE INDEX IF NOT EXISTS idx_calendar_events_start ON calendar_events(start_time);
CREATE INDEX IF NOT EXISTS idx_calendar_events_end ON calendar_events(end_time);

-- Calendar attendee email to Slack user_id mapping cache
CREATE TABLE IF NOT EXISTS calendar_attendee_map (
    email          TEXT PRIMARY KEY,
    slack_user_id  TEXT NOT NULL DEFAULT '',
    resolved_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS meeting_prep_cache (
    event_id      TEXT PRIMARY KEY,
    result_json   TEXT NOT NULL DEFAULT '',
    user_notes    TEXT NOT NULL DEFAULT '',
    generated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_meeting_prep_cache_generated ON meeting_prep_cache(generated_at);

-- Jira boards
CREATE TABLE IF NOT EXISTS jira_boards (
    id INTEGER PRIMARY KEY, name TEXT NOT NULL, project_key TEXT NOT NULL DEFAULT '',
    board_type TEXT NOT NULL DEFAULT '', is_selected INTEGER NOT NULL DEFAULT 0,
    issue_count INTEGER NOT NULL DEFAULT 0, synced_at TEXT NOT NULL DEFAULT '',
    raw_columns_json TEXT NOT NULL DEFAULT '',
    raw_config_json TEXT NOT NULL DEFAULT '',
    llm_profile_json TEXT NOT NULL DEFAULT '',
    workflow_summary TEXT NOT NULL DEFAULT '',
    user_overrides_json TEXT NOT NULL DEFAULT '',
    config_hash TEXT NOT NULL DEFAULT '',
    profile_generated_at TEXT NOT NULL DEFAULT ''
);

-- Jira custom fields (discovered from API, classified by LLM)
CREATE TABLE IF NOT EXISTS jira_custom_fields (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    field_type TEXT NOT NULL,
    items_type TEXT NOT NULL DEFAULT '',
    is_useful INTEGER NOT NULL DEFAULT 0,
    usage_hint TEXT NOT NULL DEFAULT '',
    synced_at TEXT NOT NULL DEFAULT ''
);

-- Per-board custom field mapping
CREATE TABLE IF NOT EXISTS jira_board_field_map (
    board_id INTEGER NOT NULL,
    field_id TEXT NOT NULL,
    role TEXT NOT NULL,
    PRIMARY KEY (board_id, field_id)
);

-- Jira issues
CREATE TABLE IF NOT EXISTS jira_issues (
    key TEXT PRIMARY KEY, id TEXT NOT NULL DEFAULT '', project_key TEXT NOT NULL,
    board_id INTEGER,
    summary TEXT NOT NULL, description_text TEXT NOT NULL DEFAULT '',
    issue_type TEXT NOT NULL DEFAULT '', issue_type_category TEXT NOT NULL DEFAULT '',
    is_bug INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL, status_category TEXT NOT NULL,
    status_category_changed_at TEXT NOT NULL DEFAULT '',
    assignee_account_id TEXT NOT NULL DEFAULT '', assignee_email TEXT NOT NULL DEFAULT '',
    assignee_display_name TEXT NOT NULL DEFAULT '', assignee_slack_id TEXT NOT NULL DEFAULT '',
    reporter_account_id TEXT NOT NULL DEFAULT '', reporter_email TEXT NOT NULL DEFAULT '',
    reporter_display_name TEXT NOT NULL DEFAULT '', reporter_slack_id TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT '', story_points REAL,
    due_date TEXT NOT NULL DEFAULT '', sprint_id INTEGER, sprint_name TEXT NOT NULL DEFAULT '',
    epic_key TEXT NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '[]', components TEXT NOT NULL DEFAULT '[]',
    fix_versions TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL, updated_at TEXT NOT NULL, resolved_at TEXT NOT NULL DEFAULT '',
    raw_json TEXT NOT NULL DEFAULT '', custom_fields_json TEXT NOT NULL DEFAULT '',
    synced_at TEXT NOT NULL, is_deleted INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_jira_issues_project ON jira_issues(project_key);
CREATE INDEX IF NOT EXISTS idx_jira_issues_assignee ON jira_issues(assignee_account_id);
CREATE INDEX IF NOT EXISTS idx_jira_issues_status_cat ON jira_issues(status_category);
CREATE INDEX IF NOT EXISTS idx_jira_issues_sprint ON jira_issues(sprint_id);
CREATE INDEX IF NOT EXISTS idx_jira_issues_epic ON jira_issues(epic_key);
CREATE INDEX IF NOT EXISTS idx_jira_issues_updated ON jira_issues(updated_at);
CREATE INDEX IF NOT EXISTS idx_jira_issues_due ON jira_issues(due_date);
CREATE INDEX IF NOT EXISTS idx_jira_issues_board ON jira_issues(board_id);

-- Jira sprints
CREATE TABLE IF NOT EXISTS jira_sprints (
    id INTEGER PRIMARY KEY, board_id INTEGER NOT NULL, name TEXT NOT NULL,
    state TEXT NOT NULL, goal TEXT NOT NULL DEFAULT '',
    start_date TEXT NOT NULL DEFAULT '', end_date TEXT NOT NULL DEFAULT '',
    complete_date TEXT NOT NULL DEFAULT '', synced_at TEXT NOT NULL DEFAULT ''
);

-- Jira issue links
CREATE TABLE IF NOT EXISTS jira_issue_links (
    id TEXT PRIMARY KEY, source_key TEXT NOT NULL, target_key TEXT NOT NULL,
    link_type TEXT NOT NULL, synced_at TEXT NOT NULL DEFAULT ''
);

-- Jira user mapping
CREATE TABLE IF NOT EXISTS jira_user_map (
    jira_account_id TEXT PRIMARY KEY, email TEXT NOT NULL DEFAULT '',
    slack_user_id TEXT NOT NULL DEFAULT '', display_name TEXT NOT NULL DEFAULT '',
    match_method TEXT NOT NULL DEFAULT '', match_confidence REAL NOT NULL DEFAULT 0,
    resolved_at TEXT NOT NULL DEFAULT ''
);

-- Jira Slack links (key detection)
CREATE TABLE IF NOT EXISTS jira_slack_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_key TEXT NOT NULL,
    channel_id TEXT NOT NULL DEFAULT '',
    message_ts TEXT NOT NULL DEFAULT '',
    track_id INTEGER,
    digest_id INTEGER,
    link_type TEXT NOT NULL DEFAULT 'mention',
    detected_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(issue_key, channel_id, message_ts)
);
CREATE INDEX IF NOT EXISTS idx_jira_slack_links_issue ON jira_slack_links(issue_key);
CREATE INDEX IF NOT EXISTS idx_jira_slack_links_channel ON jira_slack_links(channel_id, message_ts);
CREATE INDEX IF NOT EXISTS idx_jira_slack_links_track ON jira_slack_links(track_id);
CREATE INDEX IF NOT EXISTS idx_jira_slack_links_digest ON jira_slack_links(digest_id);

CREATE INDEX IF NOT EXISTS idx_jira_issues_assignee_slack ON jira_issues(assignee_slack_id);
CREATE INDEX IF NOT EXISTS idx_jira_issues_assignee_status ON jira_issues(assignee_slack_id, status_category);

-- Jira sync state
CREATE TABLE IF NOT EXISTS jira_sync_state (
    project_key TEXT PRIMARY KEY, last_synced_at TEXT NOT NULL DEFAULT '',
    issues_synced INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
    last_error_at TEXT NOT NULL DEFAULT ''
);

-- Meeting notes (questions + freeform notes linked to calendar events)
CREATE TABLE IF NOT EXISTS meeting_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('question', 'note')),
    text TEXT NOT NULL DEFAULT '',
    is_checked INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    task_id INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_meeting_notes_event ON meeting_notes(event_id);

-- Calendar auth state (tracks whether the Google refresh token is still valid)
CREATE TABLE IF NOT EXISTS calendar_auth_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    status TEXT NOT NULL DEFAULT 'ok',
    error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT OR IGNORE INTO calendar_auth_state (id, status, error) VALUES (1, 'ok', '');

-- Jira releases (fix versions)
CREATE TABLE IF NOT EXISTS jira_releases (
    id INTEGER NOT NULL,
    project_key TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    release_date TEXT NOT NULL DEFAULT '',
    released INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0,
    synced_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    UNIQUE(project_key, name)
);
