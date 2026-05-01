-- +goose Up
-- Adds the `target_due` inbox trigger and `targets.notified_at` so the daemon
-- can surface overdue targets into the inbox exactly once per due transition.

-- Defer FK checks so the inbox_feedback → inbox_items reference survives the
-- DROP/RENAME dance below; SQLite re-validates at COMMIT, by which point the
-- table exists again with the same name and rowids.
PRAGMA defer_foreign_keys = ON;

ALTER TABLE targets ADD COLUMN notified_at TEXT NOT NULL DEFAULT '';

CREATE TABLE inbox_items_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id      TEXT NOT NULL,
    message_ts      TEXT NOT NULL,
    thread_ts       TEXT NOT NULL DEFAULT '',
    sender_user_id  TEXT NOT NULL,
    trigger_type    TEXT NOT NULL CHECK(trigger_type IN (
        'mention','dm','thread_reply','reaction',
        'jira_assigned','jira_comment_mention','jira_comment_watching','jira_status_change','jira_priority_change',
        'calendar_invite','calendar_time_change','calendar_cancelled',
        'decision_made','briefing_ready',
        'target_due'
    )),
    snippet         TEXT NOT NULL DEFAULT '',
    context         TEXT NOT NULL DEFAULT '',
    raw_text        TEXT NOT NULL DEFAULT '',
    permalink       TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
    priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    ai_reason       TEXT NOT NULL DEFAULT '',
    resolved_reason TEXT NOT NULL DEFAULT '',
    snooze_until    TEXT NOT NULL DEFAULT '',
    waiting_user_ids TEXT NOT NULL DEFAULT '[]',
    target_id       INTEGER,
    read_at         TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    item_class      TEXT NOT NULL DEFAULT 'actionable' CHECK(item_class IN ('actionable','ambient')),
    pinned          INTEGER NOT NULL DEFAULT 0,
    archived_at     TEXT,
    archive_reason  TEXT DEFAULT '' CHECK(archive_reason IN ('','resolved','seen_expired','stale','dismissed')),
    UNIQUE(channel_id, message_ts)
);

INSERT INTO inbox_items_new SELECT * FROM inbox_items;

DROP TABLE inbox_items;

ALTER TABLE inbox_items_new RENAME TO inbox_items;

CREATE INDEX IF NOT EXISTS idx_inbox_items_status ON inbox_items(status);
CREATE INDEX IF NOT EXISTS idx_inbox_items_priority ON inbox_items(priority);
CREATE INDEX IF NOT EXISTS idx_inbox_items_updated ON inbox_items(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_inbox_items_sender ON inbox_items(sender_user_id);
CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze ON inbox_items(snooze_until);
CREATE INDEX IF NOT EXISTS idx_inbox_items_class_status ON inbox_items(item_class, status);
CREATE INDEX IF NOT EXISTS idx_inbox_items_pinned ON inbox_items(pinned) WHERE pinned = 1;
CREATE INDEX IF NOT EXISTS idx_inbox_items_archived ON inbox_items(archived_at);

-- Targets that are due but unfired live in this hot path; partial index keeps
-- the daemon's NotifyDueTargets scan cheap as the targets table grows.
CREATE INDEX IF NOT EXISTS idx_targets_due_unfired ON targets(due_date)
    WHERE notified_at = '' AND due_date != '';

-- +goose Down
PRAGMA defer_foreign_keys = ON;

CREATE TABLE inbox_items_old (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id      TEXT NOT NULL,
    message_ts      TEXT NOT NULL,
    thread_ts       TEXT NOT NULL DEFAULT '',
    sender_user_id  TEXT NOT NULL,
    trigger_type    TEXT NOT NULL CHECK(trigger_type IN (
        'mention','dm','thread_reply','reaction',
        'jira_assigned','jira_comment_mention','jira_comment_watching','jira_status_change','jira_priority_change',
        'calendar_invite','calendar_time_change','calendar_cancelled',
        'decision_made','briefing_ready'
    )),
    snippet         TEXT NOT NULL DEFAULT '',
    context         TEXT NOT NULL DEFAULT '',
    raw_text        TEXT NOT NULL DEFAULT '',
    permalink       TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','resolved','dismissed','snoozed')),
    priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
    ai_reason       TEXT NOT NULL DEFAULT '',
    resolved_reason TEXT NOT NULL DEFAULT '',
    snooze_until    TEXT NOT NULL DEFAULT '',
    waiting_user_ids TEXT NOT NULL DEFAULT '[]',
    target_id       INTEGER,
    read_at         TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    item_class      TEXT NOT NULL DEFAULT 'actionable' CHECK(item_class IN ('actionable','ambient')),
    pinned          INTEGER NOT NULL DEFAULT 0,
    archived_at     TEXT,
    archive_reason  TEXT DEFAULT '' CHECK(archive_reason IN ('','resolved','seen_expired','stale','dismissed')),
    UNIQUE(channel_id, message_ts)
);

INSERT INTO inbox_items_old SELECT * FROM inbox_items WHERE trigger_type != 'target_due';

DROP TABLE inbox_items;

ALTER TABLE inbox_items_old RENAME TO inbox_items;

CREATE INDEX IF NOT EXISTS idx_inbox_items_status ON inbox_items(status);
CREATE INDEX IF NOT EXISTS idx_inbox_items_priority ON inbox_items(priority);
CREATE INDEX IF NOT EXISTS idx_inbox_items_updated ON inbox_items(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_inbox_items_sender ON inbox_items(sender_user_id);
CREATE INDEX IF NOT EXISTS idx_inbox_items_snooze ON inbox_items(snooze_until);
CREATE INDEX IF NOT EXISTS idx_inbox_items_class_status ON inbox_items(item_class, status);
CREATE INDEX IF NOT EXISTS idx_inbox_items_pinned ON inbox_items(pinned) WHERE pinned = 1;
CREATE INDEX IF NOT EXISTS idx_inbox_items_archived ON inbox_items(archived_at);

DROP INDEX IF EXISTS idx_targets_due_unfired;
ALTER TABLE targets DROP COLUMN notified_at;
