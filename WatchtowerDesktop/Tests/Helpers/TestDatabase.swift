import Foundation
import GRDB
@testable import WatchtowerDesktop

/// In-memory GRDB database with the full watchtower schema for testing.
enum TestDatabase {
    static func create() throws -> DatabaseQueue {
        let dbQueue = try DatabaseQueue(path: ":memory:")
        try dbQueue.write { db in
            try db.execute(sql: schema)
            try db.execute(sql: "PRAGMA user_version = 5")
        }
        return dbQueue
    }

    /// Create a file-based DatabaseManager for ViewModel tests (DatabasePool requires a file).
    static func createDatabaseManager() throws -> (DatabaseManager, String) {
        let path = NSTemporaryDirectory() + "watchtower_test_\(UUID().uuidString).db"
        let pool = try DatabasePool(path: path)
        try pool.write { db in
            try db.execute(sql: schema)
            try db.execute(sql: "PRAGMA user_version = 5")
        }
        return (DatabaseManager(pool: pool), path)
    }

    /// Clean up temp DB files
    static func cleanup(path: String) {
        let fm = FileManager.default
        for suffix in ["", "-wal", "-shm"] {
            try? fm.removeItem(atPath: path + suffix)
        }
    }

    // MARK: - Fixture Insertion

    static func insertWorkspace(
        _ db: Database,
        id: String = "T001",
        name: String = "Test Workspace",
        domain: String = "test",
        syncedAt: String? = "2025-01-01T00:00:00Z",
        searchLastDate: String = "2025-01-01"
    ) throws {
        try db.execute(sql: """
            INSERT INTO workspace (id, name, domain, synced_at, search_last_date)
            VALUES (?, ?, ?, ?, ?)
            """, arguments: [id, name, domain, syncedAt, searchLastDate])
    }

    static func insertChannel(
        _ db: Database,
        id: String = "C001",
        name: String = "general",
        type: String = "public",
        topic: String = "",
        purpose: String = "",
        isArchived: Bool = false,
        isMember: Bool = true,
        dmUserID: String? = nil,
        numMembers: Int = 5
    ) throws {
        try db.execute(sql: """
            INSERT INTO channels (id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [id, name, type, topic, purpose, isArchived ? 1 : 0, isMember ? 1 : 0, dmUserID, numMembers])
    }

    static func insertUser(
        _ db: Database,
        id: String = "U001",
        name: String = "testuser",
        displayName: String = "Test User",
        realName: String = "Test Real Name",
        email: String = "test@example.com",
        isBot: Bool = false,
        isDeleted: Bool = false
    ) throws {
        try db.execute(sql: """
            INSERT INTO users (id, name, display_name, real_name, email, is_bot, is_deleted)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """, arguments: [id, name, displayName, realName, email, isBot ? 1 : 0, isDeleted ? 1 : 0])
    }

    static func insertMessage(
        _ db: Database,
        channelID: String = "C001",
        ts: String = "1700000000.000100",
        userID: String = "U001",
        text: String = "Hello world",
        threadTS: String? = nil,
        replyCount: Int = 0,
        isEdited: Bool = false,
        isDeleted: Bool = false,
        subtype: String = "",
        permalink: String = ""
    ) throws {
        try db.execute(sql: """
            INSERT INTO messages (channel_id, ts, user_id, text, thread_ts, reply_count, is_edited, is_deleted, subtype, permalink, raw_json)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}')
            """, arguments: [channelID, ts, userID, text, threadTS, replyCount, isEdited ? 1 : 0, isDeleted ? 1 : 0, subtype, permalink])
    }

    static func insertDigest(
        _ db: Database,
        channelID: String = "C001",
        periodFrom: Double = 1700000000,
        periodTo: Double = 1700086400,
        type: String = "channel",
        summary: String = "Test summary",
        topics: String = "[]",
        decisions: String = "[]",
        tracksJSON: String = "[]",
        messageCount: Int = 10,
        model: String = "haiku"
    ) throws {
        try db.execute(sql: """
            INSERT INTO digests (channel_id, period_from, period_to, type, summary, topics, decisions, action_items, message_count, model)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [channelID, periodFrom, periodTo, type, summary, topics, decisions, tracksJSON, messageCount, model])
    }

    static func insertWatchItem(
        _ db: Database,
        entityType: String = "channel",
        entityID: String = "C001",
        entityName: String = "general",
        priority: String = "normal"
    ) throws {
        try db.execute(sql: """
            INSERT INTO watch_list (entity_type, entity_id, entity_name, priority)
            VALUES (?, ?, ?, ?)
            """, arguments: [entityType, entityID, entityName, priority])
    }

    static func insertSyncState(
        _ db: Database,
        channelID: String = "C001",
        lastSyncedTS: String = "1700000000.000100",
        oldestSyncedTS: String = "1699900000.000100",
        isInitialSyncComplete: Bool = true,
        messagesSynced: Int = 50
    ) throws {
        try db.execute(sql: """
            INSERT INTO sync_state (channel_id, last_synced_ts, oldest_synced_ts, is_initial_sync_complete, messages_synced)
            VALUES (?, ?, ?, ?, ?)
            """, arguments: [channelID, lastSyncedTS, oldestSyncedTS, isInitialSyncComplete ? 1 : 0, messagesSynced])
    }

    static func insertUserAnalysis(
        _ db: Database,
        userID: String = "U001",
        periodFrom: Double = 1700000000,
        periodTo: Double = 1700604800,
        messageCount: Int = 100,
        channelsActive: Int = 5,
        threadsInitiated: Int = 10,
        threadsReplied: Int = 20,
        avgMessageLength: Double = 42.5,
        activeHoursJSON: String = #"{"9":12,"10":8,"14":15}"#,
        volumeChangePct: Double = 15.0,
        summary: String = "Active contributor",
        communicationStyle: String = "driver",
        decisionRole: String = "approver",
        redFlags: String = "[]",
        highlights: String = #"["Great leadership"]"#,
        styleDetails: String = "",
        recommendations: String = "[]",
        concerns: String = "[]",
        model: String = "haiku"
    ) throws {
        try db.execute(sql: """
            INSERT INTO user_analyses (user_id, period_from, period_to, message_count, channels_active,
                threads_initiated, threads_replied, avg_message_length, active_hours_json,
                volume_change_pct, summary, communication_style, decision_role, red_flags, highlights,
                style_details, recommendations, concerns, model)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [userID, periodFrom, periodTo, messageCount, channelsActive,
                             threadsInitiated, threadsReplied, avgMessageLength, activeHoursJSON,
                             volumeChangePct, summary, communicationStyle, decisionRole, redFlags, highlights,
                             styleDetails, recommendations, concerns, model])
    }

    static func insertTrack(
        _ db: Database,
        text: String = "Fix the bug",
        context: String = "Discussed in standup",
        category: String = "task",
        ownership: String = "mine",
        priority: String = "medium",
        tags: String = "[]",
        channelIDs: String = "[\"C001\"]",
        sourceRefs: String = "[]",
        hasUpdates: Bool = false,
        participants: String = "[]",
        requesterName: String = "",
        blocking: String = "",
        decisionSummary: String = "",
        decisionOptions: String = "[]",
        subItems: String = "[]",
        relatedDigestIDs: String = "[]",
        model: String = "haiku"
    ) throws {
        try db.execute(sql: """
            INSERT INTO tracks (text, context, category, ownership, priority, tags,
                channel_ids, source_refs, has_updates, participants, requester_name,
                blocking, decision_summary, decision_options, sub_items, related_digest_ids, model)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [text, context, category, ownership, priority, tags,
                             channelIDs, sourceRefs, hasUpdates ? 1 : 0, participants,
                             requesterName, blocking, decisionSummary, decisionOptions,
                             subItems, relatedDigestIDs, model])
    }

    // MARK: - Schema

    static let schema = """
    CREATE TABLE IF NOT EXISTS workspace (
        id                TEXT PRIMARY KEY,
        name              TEXT NOT NULL,
        domain            TEXT NOT NULL DEFAULT '',
        synced_at         TEXT,
        search_last_date  TEXT NOT NULL DEFAULT '',
        current_user_id   TEXT NOT NULL DEFAULT ''
    );
    CREATE TABLE IF NOT EXISTS users (
        id            TEXT PRIMARY KEY,
        name          TEXT NOT NULL,
        display_name  TEXT NOT NULL DEFAULT '',
        real_name     TEXT NOT NULL DEFAULT '',
        email         TEXT NOT NULL DEFAULT '',
        is_bot        INTEGER NOT NULL DEFAULT 0,
        is_deleted    INTEGER NOT NULL DEFAULT 0,
        is_bot_override INTEGER DEFAULT NULL,
        profile_json  TEXT NOT NULL DEFAULT '{}',
        updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE TABLE IF NOT EXISTS channels (
        id           TEXT PRIMARY KEY,
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
    CREATE TABLE IF NOT EXISTS messages (
        channel_id   TEXT NOT NULL,
        ts           TEXT NOT NULL,
        user_id      TEXT NOT NULL DEFAULT '',
        text         TEXT NOT NULL DEFAULT '',
        thread_ts    TEXT,
        reply_count  INTEGER NOT NULL DEFAULT 0,
        is_edited    INTEGER NOT NULL DEFAULT 0,
        is_deleted   INTEGER NOT NULL DEFAULT 0,
        subtype      TEXT NOT NULL DEFAULT '',
        permalink    TEXT NOT NULL DEFAULT '',
        ts_unix      REAL GENERATED ALWAYS AS (
            CASE WHEN INSTR(ts, '.') > 0
            THEN CAST(SUBSTR(ts, 1, INSTR(ts, '.') - 1) AS REAL)
            ELSE CAST(ts AS REAL) END) STORED,
        raw_json     TEXT NOT NULL DEFAULT '{}',
        PRIMARY KEY (channel_id, ts)
    );
    CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
        text,
        channel_id UNINDEXED,
        ts UNINDEXED,
        user_id UNINDEXED,
        tokenize='porter unicode61'
    );
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
    CREATE TABLE IF NOT EXISTS watch_list (
        entity_type TEXT NOT NULL CHECK(entity_type IN ('channel', 'user')),
        entity_id   TEXT NOT NULL,
        entity_name TEXT NOT NULL DEFAULT '',
        priority    TEXT NOT NULL DEFAULT 'normal' CHECK(priority IN ('high', 'normal', 'low')),
        created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        PRIMARY KEY (entity_type, entity_id)
    );
    CREATE TABLE IF NOT EXISTS digests (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        channel_id    TEXT NOT NULL DEFAULT '',
        period_from   REAL NOT NULL,
        period_to     REAL NOT NULL,
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
        read_at       TEXT,
        prompt_version INTEGER NOT NULL DEFAULT 0,
        people_signals TEXT NOT NULL DEFAULT '[]',
        situations     TEXT NOT NULL DEFAULT '[]',
        running_summary TEXT NOT NULL DEFAULT '',
        UNIQUE(channel_id, type, period_from, period_to)
    );
    CREATE TABLE IF NOT EXISTS digest_participants (
        digest_id      INTEGER NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
        user_id        TEXT NOT NULL,
        situation_idx  INTEGER NOT NULL DEFAULT 0,
        role           TEXT NOT NULL DEFAULT '',
        PRIMARY KEY (digest_id, user_id, situation_idx)
    );
    CREATE INDEX IF NOT EXISTS idx_digest_participants_user ON digest_participants(user_id);
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
    CREATE TABLE IF NOT EXISTS decision_reads (
        digest_id    INTEGER NOT NULL,
        decision_idx INTEGER NOT NULL,
        read_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        PRIMARY KEY (digest_id, decision_idx)
    );
    CREATE TABLE IF NOT EXISTS user_analyses (
        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id             TEXT NOT NULL,
        period_from         REAL NOT NULL,
        period_to           REAL NOT NULL,
        message_count       INTEGER NOT NULL DEFAULT 0,
        channels_active     INTEGER NOT NULL DEFAULT 0,
        threads_initiated   INTEGER NOT NULL DEFAULT 0,
        threads_replied     INTEGER NOT NULL DEFAULT 0,
        avg_message_length  REAL NOT NULL DEFAULT 0,
        active_hours_json   TEXT NOT NULL DEFAULT '{}',
        volume_change_pct   REAL NOT NULL DEFAULT 0,
        summary             TEXT NOT NULL DEFAULT '',
        communication_style TEXT NOT NULL DEFAULT '',
        decision_role       TEXT NOT NULL DEFAULT '',
        red_flags           TEXT NOT NULL DEFAULT '[]',
        highlights          TEXT NOT NULL DEFAULT '[]',
        style_details       TEXT NOT NULL DEFAULT '',
        recommendations     TEXT NOT NULL DEFAULT '[]',
        concerns            TEXT NOT NULL DEFAULT '[]',
        accomplishments     TEXT NOT NULL DEFAULT '[]',
        model               TEXT NOT NULL DEFAULT '',
        input_tokens        INTEGER NOT NULL DEFAULT 0,
        output_tokens       INTEGER NOT NULL DEFAULT 0,
        cost_usd            REAL NOT NULL DEFAULT 0,
        prompt_version      INTEGER NOT NULL DEFAULT 0,
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        UNIQUE(user_id, period_from, period_to)
    );
    CREATE TABLE IF NOT EXISTS period_summaries (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        period_from   REAL NOT NULL,
        period_to     REAL NOT NULL,
        summary       TEXT NOT NULL DEFAULT '',
        attention     TEXT NOT NULL DEFAULT '[]',
        model         TEXT NOT NULL DEFAULT '',
        input_tokens  INTEGER NOT NULL DEFAULT 0,
        output_tokens INTEGER NOT NULL DEFAULT 0,
        cost_usd      REAL NOT NULL DEFAULT 0,
        created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        UNIQUE(period_from, period_to)
    );
    CREATE TABLE IF NOT EXISTS custom_emojis (
        name       TEXT PRIMARY KEY,
        url        TEXT NOT NULL,
        alias_for  TEXT NOT NULL DEFAULT '',
        updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE TABLE IF NOT EXISTS tracks (
        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
        assignee_user_id    TEXT NOT NULL DEFAULT '',
        text                TEXT NOT NULL,
        context             TEXT NOT NULL DEFAULT '',
        category            TEXT NOT NULL DEFAULT 'task',
        ownership           TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
        ball_on             TEXT NOT NULL DEFAULT '',
        owner_user_id       TEXT NOT NULL DEFAULT '',
        requester_name      TEXT NOT NULL DEFAULT '',
        requester_user_id   TEXT NOT NULL DEFAULT '',
        blocking            TEXT NOT NULL DEFAULT '',
        decision_summary    TEXT NOT NULL DEFAULT '',
        decision_options    TEXT NOT NULL DEFAULT '[]',
        sub_items           TEXT NOT NULL DEFAULT '[]',
        participants        TEXT NOT NULL DEFAULT '[]',
        source_refs         TEXT NOT NULL DEFAULT '[]',
        tags                TEXT NOT NULL DEFAULT '[]',
        channel_ids         TEXT NOT NULL DEFAULT '[]',
        related_digest_ids  TEXT NOT NULL DEFAULT '[]',
        priority            TEXT NOT NULL DEFAULT 'medium',
        due_date            REAL,
        fingerprint         TEXT NOT NULL DEFAULT '[]',
        read_at             TEXT,
        has_updates         INTEGER NOT NULL DEFAULT 0,
        dismissed_at        TEXT NOT NULL DEFAULT '',
        model               TEXT NOT NULL DEFAULT '',
        input_tokens        INTEGER NOT NULL DEFAULT 0,
        output_tokens       INTEGER NOT NULL DEFAULT 0,
        cost_usd            REAL NOT NULL DEFAULT 0,
        prompt_version      INTEGER NOT NULL DEFAULT 0,
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE TABLE IF NOT EXISTS tasks (
        id              INTEGER PRIMARY KEY AUTOINCREMENT,
        text            TEXT NOT NULL,
        intent          TEXT NOT NULL DEFAULT '',
        status          TEXT NOT NULL DEFAULT 'todo' CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
        priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
        ownership       TEXT NOT NULL DEFAULT 'mine' CHECK(ownership IN ('mine','delegated','watching')),
        ball_on         TEXT NOT NULL DEFAULT '',
        due_date        TEXT NOT NULL DEFAULT '',
        snooze_until    TEXT NOT NULL DEFAULT '',
        blocking        TEXT NOT NULL DEFAULT '',
        tags            TEXT NOT NULL DEFAULT '[]',
        sub_items       TEXT NOT NULL DEFAULT '[]',
        source_type     TEXT NOT NULL DEFAULT 'manual' CHECK(source_type IN ('track','digest','briefing','manual','chat','inbox')),
        source_id       TEXT NOT NULL DEFAULT '',
        created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
    CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
    CREATE INDEX IF NOT EXISTS idx_tasks_due_date ON tasks(due_date);
    CREATE INDEX IF NOT EXISTS idx_tasks_source ON tasks(source_type, source_id);
    CREATE INDEX IF NOT EXISTS idx_tasks_updated ON tasks(updated_at DESC);

    CREATE TABLE IF NOT EXISTS inbox_items (
        id              INTEGER PRIMARY KEY AUTOINCREMENT,
        channel_id      TEXT NOT NULL,
        message_ts      TEXT NOT NULL,
        thread_ts       TEXT NOT NULL DEFAULT '',
        sender_user_id  TEXT NOT NULL,
        trigger_type    TEXT NOT NULL CHECK(trigger_type IN ('mention','dm')),
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
        target_id       INTEGER,
        read_at         TEXT,
        item_class      TEXT NOT NULL DEFAULT 'ambient',
        pinned          INTEGER NOT NULL DEFAULT 0,
        archived_at     TEXT,
        archive_reason  TEXT NOT NULL DEFAULT '',
        created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        UNIQUE(channel_id, message_ts)
    );
    CREATE INDEX IF NOT EXISTS idx_inbox_status ON inbox_items(status);
    CREATE INDEX IF NOT EXISTS idx_inbox_priority ON inbox_items(priority);
    CREATE INDEX IF NOT EXISTS idx_inbox_updated ON inbox_items(updated_at DESC);
    CREATE INDEX IF NOT EXISTS idx_inbox_sender ON inbox_items(sender_user_id);
    CREATE INDEX IF NOT EXISTS idx_inbox_snooze ON inbox_items(snooze_until);

    CREATE TABLE IF NOT EXISTS inbox_learned_rules (
        id             INTEGER PRIMARY KEY AUTOINCREMENT,
        rule_type      TEXT NOT NULL CHECK(rule_type IN ('source_mute','source_boost','trigger_downgrade','trigger_boost')),
        scope_key      TEXT NOT NULL,
        weight         REAL NOT NULL,
        source         TEXT NOT NULL CHECK(source IN ('implicit','explicit_feedback','user_rule')),
        evidence_count INTEGER NOT NULL DEFAULT 0,
        last_updated   TEXT NOT NULL,
        UNIQUE(rule_type, scope_key)
    );
    CREATE INDEX IF NOT EXISTS idx_inbox_learned_rules_scope ON inbox_learned_rules(rule_type, scope_key);

    CREATE TABLE IF NOT EXISTS inbox_feedback (
        id            INTEGER PRIMARY KEY AUTOINCREMENT,
        inbox_item_id INTEGER NOT NULL,
        rating        INTEGER NOT NULL CHECK(rating IN (-1,1)),
        reason        TEXT DEFAULT '',
        created_at    TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_inbox_feedback_item ON inbox_feedback(inbox_item_id);

    CREATE TABLE IF NOT EXISTS calendar_calendars (
        id          TEXT PRIMARY KEY,
        name        TEXT NOT NULL,
        is_primary  INTEGER NOT NULL DEFAULT 0,
        is_selected INTEGER NOT NULL DEFAULT 1,
        color       TEXT NOT NULL DEFAULT '',
        synced_at   TEXT NOT NULL DEFAULT ''
    );

    CREATE TABLE IF NOT EXISTS calendar_events (
        id              TEXT PRIMARY KEY,
        calendar_id     TEXT NOT NULL REFERENCES calendar_calendars(id),
        title           TEXT NOT NULL DEFAULT '',
        description     TEXT NOT NULL DEFAULT '',
        location        TEXT NOT NULL DEFAULT '',
        start_time      TEXT NOT NULL,
        end_time        TEXT NOT NULL,
        organizer_email TEXT NOT NULL DEFAULT '',
        attendees       TEXT NOT NULL DEFAULT '[]',
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

    CREATE TABLE IF NOT EXISTS calendar_attendee_map (
        email          TEXT PRIMARY KEY,
        slack_user_id  TEXT NOT NULL DEFAULT '',
        resolved_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );

    CREATE TABLE IF NOT EXISTS feedback (
        id          INTEGER PRIMARY KEY AUTOINCREMENT,
        entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis', 'briefing', 'task', 'inbox')),
        entity_id   TEXT NOT NULL,
        rating      INTEGER NOT NULL CHECK(rating IN (-1, 1)),
        comment     TEXT NOT NULL DEFAULT '',
        created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE INDEX IF NOT EXISTS idx_feedback_entity ON feedback(entity_type, entity_id);
    CREATE INDEX IF NOT EXISTS idx_feedback_rating ON feedback(entity_type, rating);
    CREATE TABLE IF NOT EXISTS prompts (
        id         TEXT PRIMARY KEY,
        template   TEXT NOT NULL,
        version    INTEGER NOT NULL DEFAULT 1,
        language   TEXT NOT NULL DEFAULT '',
        updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE TABLE IF NOT EXISTS prompt_history (
        id         INTEGER PRIMARY KEY AUTOINCREMENT,
        prompt_id  TEXT NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
        version    INTEGER NOT NULL,
        template   TEXT NOT NULL,
        reason     TEXT NOT NULL DEFAULT '',
        created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE INDEX IF NOT EXISTS idx_prompt_history_prompt ON prompt_history(prompt_id);
    CREATE INDEX IF NOT EXISTS idx_prompt_history_version ON prompt_history(prompt_id, version);
    CREATE TABLE IF NOT EXISTS user_interactions (
        user_a              TEXT NOT NULL,
        user_b              TEXT NOT NULL,
        period_from         REAL NOT NULL,
        period_to           REAL NOT NULL,
        messages_to         INTEGER NOT NULL DEFAULT 0,
        messages_from       INTEGER NOT NULL DEFAULT 0,
        shared_channels     INTEGER NOT NULL DEFAULT 0,
        thread_replies_to   INTEGER NOT NULL DEFAULT 0,
        thread_replies_from INTEGER NOT NULL DEFAULT 0,
        shared_channel_ids  TEXT NOT NULL DEFAULT '[]',
        dm_messages_to      INTEGER NOT NULL DEFAULT 0,
        dm_messages_from    INTEGER NOT NULL DEFAULT 0,
        mentions_to         INTEGER NOT NULL DEFAULT 0,
        mentions_from       INTEGER NOT NULL DEFAULT 0,
        reactions_to        INTEGER NOT NULL DEFAULT 0,
        reactions_from      INTEGER NOT NULL DEFAULT 0,
        interaction_score   REAL NOT NULL DEFAULT 0,
        connection_type     TEXT NOT NULL DEFAULT '',
        PRIMARY KEY (user_a, user_b, period_from, period_to)
    );
    CREATE INDEX IF NOT EXISTS idx_user_interactions_a ON user_interactions(user_a, period_from, period_to);

    CREATE TABLE IF NOT EXISTS people_cards (
        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id             TEXT NOT NULL,
        period_from         REAL NOT NULL,
        period_to           REAL NOT NULL,
        message_count       INTEGER NOT NULL DEFAULT 0,
        channels_active     INTEGER NOT NULL DEFAULT 0,
        threads_initiated   INTEGER NOT NULL DEFAULT 0,
        threads_replied     INTEGER NOT NULL DEFAULT 0,
        avg_message_length  REAL NOT NULL DEFAULT 0,
        active_hours_json   TEXT NOT NULL DEFAULT '{}',
        volume_change_pct   REAL NOT NULL DEFAULT 0,
        summary             TEXT NOT NULL DEFAULT '',
        communication_style TEXT NOT NULL DEFAULT '',
        decision_role       TEXT NOT NULL DEFAULT '',
        red_flags           TEXT NOT NULL DEFAULT '[]',
        highlights          TEXT NOT NULL DEFAULT '[]',
        accomplishments     TEXT NOT NULL DEFAULT '[]',
        communication_guide TEXT NOT NULL DEFAULT '',
        decision_style      TEXT NOT NULL DEFAULT '',
        tactics             TEXT NOT NULL DEFAULT '[]',
        relationship_context TEXT NOT NULL DEFAULT '',
        status              TEXT NOT NULL DEFAULT 'active',
        model               TEXT NOT NULL DEFAULT '',
        input_tokens        INTEGER NOT NULL DEFAULT 0,
        output_tokens       INTEGER NOT NULL DEFAULT 0,
        cost_usd            REAL NOT NULL DEFAULT 0,
        prompt_version      INTEGER NOT NULL DEFAULT 0,
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        UNIQUE(user_id, period_from, period_to)
    );
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

    CREATE TABLE IF NOT EXISTS briefings (
        id               INTEGER PRIMARY KEY AUTOINCREMENT,
        workspace_id     TEXT NOT NULL DEFAULT '',
        user_id          TEXT NOT NULL,
        date             TEXT NOT NULL,
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

    CREATE TABLE IF NOT EXISTS pipeline_runs (
        id               INTEGER PRIMARY KEY AUTOINCREMENT,
        pipeline         TEXT NOT NULL,
        source           TEXT NOT NULL DEFAULT 'cli',
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
    CREATE TABLE IF NOT EXISTS channel_settings (
        channel_id         TEXT PRIMARY KEY,
        is_muted_for_llm   INTEGER NOT NULL DEFAULT 0,
        is_favorite        INTEGER NOT NULL DEFAULT 0
    );
    CREATE TABLE IF NOT EXISTS user_profile (
        id                    INTEGER PRIMARY KEY,
        slack_user_id         TEXT NOT NULL UNIQUE,
        role                  TEXT NOT NULL DEFAULT '',
        team                  TEXT NOT NULL DEFAULT '',
        responsibilities      TEXT NOT NULL DEFAULT '[]',
        reports               TEXT NOT NULL DEFAULT '[]',
        peers                 TEXT NOT NULL DEFAULT '[]',
        manager               TEXT NOT NULL DEFAULT '',
        starred_channels      TEXT NOT NULL DEFAULT '[]',
        starred_people        TEXT NOT NULL DEFAULT '[]',
        pain_points           TEXT NOT NULL DEFAULT '[]',
        track_focus           TEXT NOT NULL DEFAULT '[]',
        onboarding_done       INTEGER NOT NULL DEFAULT 0,
        custom_prompt_context TEXT NOT NULL DEFAULT '',
        created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE TABLE IF NOT EXISTS day_plans (
        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id             TEXT NOT NULL,
        plan_date           TEXT NOT NULL,
        status              TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
        has_conflicts       INTEGER NOT NULL DEFAULT 0,
        conflict_summary    TEXT,
        generated_at        TEXT NOT NULL,
        last_regenerated_at TEXT,
        regenerate_count    INTEGER NOT NULL DEFAULT 0,
        feedback_history    TEXT,
        prompt_version      TEXT,
        briefing_id         INTEGER,
        read_at             TEXT,
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        UNIQUE (user_id, plan_date)
    );
    CREATE TABLE IF NOT EXISTS day_plan_items (
        id           INTEGER PRIMARY KEY AUTOINCREMENT,
        day_plan_id  INTEGER NOT NULL REFERENCES day_plans(id) ON DELETE CASCADE,
        kind         TEXT NOT NULL CHECK (kind IN ('timeblock','backlog')),
        source_type  TEXT NOT NULL CHECK (source_type IN ('task','briefing_attention','jira','calendar','manual','focus')),
        source_id    TEXT,
        title        TEXT NOT NULL,
        description  TEXT,
        rationale    TEXT,
        start_time   TEXT,
        end_time     TEXT,
        duration_min INTEGER,
        priority     TEXT CHECK (priority IS NULL OR priority IN ('high','medium','low')),
        status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','skipped')),
        order_index  INTEGER NOT NULL DEFAULT 0,
        tags         TEXT,
        created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );

    CREATE TABLE IF NOT EXISTS targets (
        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
        text                TEXT NOT NULL,
        intent              TEXT NOT NULL DEFAULT '',
        level               TEXT NOT NULL DEFAULT 'day'
                            CHECK(level IN ('quarter','month','week','day','custom')),
        custom_label        TEXT NOT NULL DEFAULT '',
        period_start        TEXT NOT NULL DEFAULT '',
        period_end          TEXT NOT NULL DEFAULT '',
        parent_id           INTEGER REFERENCES targets(id) ON DELETE SET NULL,
        status              TEXT NOT NULL DEFAULT 'todo'
                            CHECK(status IN ('todo','in_progress','blocked','done','dismissed','snoozed')),
        priority            TEXT NOT NULL DEFAULT 'medium'
                            CHECK(priority IN ('high','medium','low')),
        ownership           TEXT NOT NULL DEFAULT 'mine'
                            CHECK(ownership IN ('mine','delegated','watching')),
        ball_on             TEXT NOT NULL DEFAULT '',
        due_date            TEXT NOT NULL DEFAULT '',
        snooze_until        TEXT NOT NULL DEFAULT '',
        blocking            TEXT NOT NULL DEFAULT '',
        tags                TEXT NOT NULL DEFAULT '[]',
        sub_items           TEXT NOT NULL DEFAULT '[]',
        notes               TEXT NOT NULL DEFAULT '[]',
        progress            REAL NOT NULL DEFAULT 0.0,
        source_type         TEXT NOT NULL DEFAULT 'manual'
                            CHECK(source_type IN ('extract','briefing','manual','chat','inbox','jira','slack')),
        source_id           TEXT NOT NULL DEFAULT '',
        ai_level_confidence REAL DEFAULT NULL,
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
        updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
    );
    CREATE INDEX IF NOT EXISTS idx_targets_level    ON targets(level);
    CREATE INDEX IF NOT EXISTS idx_targets_parent   ON targets(parent_id);
    CREATE INDEX IF NOT EXISTS idx_targets_status   ON targets(status);
    CREATE INDEX IF NOT EXISTS idx_targets_priority ON targets(priority);
    CREATE INDEX IF NOT EXISTS idx_targets_source   ON targets(source_type, source_id);

    CREATE TABLE IF NOT EXISTS target_links (
        id                  INTEGER PRIMARY KEY AUTOINCREMENT,
        source_target_id    INTEGER NOT NULL REFERENCES targets(id) ON DELETE CASCADE,
        target_target_id    INTEGER REFERENCES targets(id) ON DELETE CASCADE,
        external_ref        TEXT NOT NULL DEFAULT '',
        relation            TEXT NOT NULL
                            CHECK(relation IN ('contributes_to','blocks','related','duplicates')),
        confidence          REAL DEFAULT NULL,
        created_by          TEXT NOT NULL DEFAULT 'ai'
                            CHECK(created_by IN ('ai','user')),
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
    );
    CREATE INDEX IF NOT EXISTS idx_target_links_source ON target_links(source_target_id);
    CREATE INDEX IF NOT EXISTS idx_target_links_target ON target_links(target_target_id);
    """

    // MARK: - Briefing Fixtures

    static func insertBriefing(
        _ db: Database,
        userID: String = "U001",
        date: String = "2024-01-15",
        role: String = "engineer",
        attention: String = "[]",
        yourDay: String = "[]",
        whatHappened: String = "[]",
        teamPulse: String = "[]",
        coaching: String = "[]",
        model: String = "haiku",
        readAt: String? = nil
    ) throws {
        try db.execute(sql: """
            INSERT INTO briefings (user_id, date, role, attention, your_day,
                what_happened, team_pulse, coaching, model, read_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [userID, date, role, attention, yourDay,
                             whatHappened, teamPulse, coaching, model, readAt])
    }

    // MARK: - Profile Fixtures

    static func insertProfile(
        _ db: Database,
        slackUserID: String = "U001",
        role: String = "",
        team: String = "",
        responsibilities: String = "[]",
        reports: String = "[]",
        peers: String = "[]",
        manager: String = "",
        starredChannels: String = "[]",
        starredPeople: String = "[]",
        painPoints: String = "[]",
        trackFocus: String = "[]",
        onboardingDone: Bool = false,
        customPromptContext: String = ""
    ) throws {
        try db.execute(sql: """
            INSERT INTO user_profile
                (slack_user_id, role, team, responsibilities, reports, peers, manager,
                 starred_channels, starred_people, pain_points, track_focus,
                 onboarding_done, custom_prompt_context)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [
                slackUserID, role, team, responsibilities, reports, peers, manager,
                starredChannels, starredPeople, painPoints, trackFocus,
                onboardingDone ? 1 : 0, customPromptContext
            ])
    }

    // MARK: - People Card Fixtures

    static func insertPeopleCard(
        _ db: Database,
        userID: String = "U001",
        periodFrom: Double = 1700000000,
        periodTo: Double = 1700604800,
        messageCount: Int = 100,
        channelsActive: Int = 5,
        threadsInitiated: Int = 10,
        threadsReplied: Int = 20,
        avgMessageLength: Double = 42.5,
        activeHoursJSON: String = #"{"9":12,"10":8,"14":15}"#,
        volumeChangePct: Double = 15.0,
        summary: String = "Active contributor",
        communicationStyle: String = "driver",
        decisionRole: String = "approver",
        redFlags: String = "[]",
        highlights: String = #"["Great leadership"]"#,
        accomplishments: String = "[]",
        communicationGuide: String = "",
        decisionStyle: String = "",
        tactics: String = "[]",
        relationshipContext: String = "",
        status: String = "ok",
        model: String = "haiku"
    ) throws {
        try db.execute(sql: """
            INSERT INTO people_cards (user_id, period_from, period_to, message_count, channels_active,
                threads_initiated, threads_replied, avg_message_length, active_hours_json,
                volume_change_pct, summary, communication_style, decision_role, red_flags, highlights,
                accomplishments, communication_guide, decision_style, tactics, relationship_context, status, model)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [userID, periodFrom, periodTo, messageCount, channelsActive,
                             threadsInitiated, threadsReplied, avgMessageLength, activeHoursJSON,
                             volumeChangePct, summary, communicationStyle, decisionRole, redFlags, highlights,
                             accomplishments, communicationGuide, decisionStyle, tactics, relationshipContext, status, model])
    }

    // MARK: - People Card Summary Fixtures

    static func insertPeopleCardSummary(
        _ db: Database,
        periodFrom: Double = 1700000000,
        periodTo: Double = 1700604800,
        summary: String = "Team is collaborating well",
        attention: String = #"["Alice is overloaded"]"#,
        tips: String = #"["Consider redistributing tasks"]"#,
        model: String = "haiku",
        inputTokens: Int = 500,
        outputTokens: Int = 200,
        costUSD: Double = 0.001,
        promptVersion: Int = 1
    ) throws {
        try db.execute(sql: """
            INSERT INTO people_card_summaries (period_from, period_to, summary, attention, tips,
                model, input_tokens, output_tokens, cost_usd, prompt_version)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [periodFrom, periodTo, summary, attention, tips,
                             model, inputTokens, outputTokens, costUSD, promptVersion])
    }

    // MARK: - Task Fixtures

    static func insertTask(
        _ db: Database,
        text: String = "Review PR",
        intent: String = "",
        status: String = "todo",
        priority: String = "medium",
        ownership: String = "mine",
        ballOn: String = "",
        dueDate: String = "",
        snoozeUntil: String = "",
        blocking: String = "",
        tags: String = "[]",
        subItems: String = "[]",
        sourceType: String = "manual",
        sourceID: String = ""
    ) throws {
        try db.execute(sql: """
            INSERT INTO tasks (text, intent, status, priority, ownership, ball_on,
                due_date, snooze_until, blocking, tags, sub_items, source_type, source_id)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [text, intent, status, priority, ownership, ballOn,
                             dueDate, snoozeUntil, blocking, tags, subItems, sourceType, sourceID])
    }

    // MARK: - Target Fixtures

    @discardableResult
    static func insertTarget(
        _ db: Database,
        text: String = "Ship the feature",
        intent: String = "",
        level: String = "week",
        customLabel: String = "",
        periodStart: String = "2026-04-20",
        periodEnd: String = "2026-04-26",
        parentId: Int? = nil,
        status: String = "todo",
        priority: String = "medium",
        ownership: String = "mine",
        ballOn: String = "",
        dueDate: String = "",
        snoozeUntil: String = "",
        blocking: String = "",
        tags: String = "[]",
        subItems: String = "[]",
        notes: String = "[]",
        progress: Double = 0.0,
        sourceType: String = "manual",
        sourceID: String = "",
        aiLevelConfidence: Double? = nil
    ) throws -> Int64 {
        try db.execute(sql: """
            INSERT INTO targets (text, intent, level, custom_label, period_start, period_end,
                parent_id, status, priority, ownership, ball_on, due_date, snooze_until,
                blocking, tags, sub_items, notes, progress, source_type, source_id, ai_level_confidence)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [text, intent, level, customLabel, periodStart, periodEnd,
                             parentId, status, priority, ownership, ballOn, dueDate, snoozeUntil,
                             blocking, tags, subItems, notes, progress, sourceType, sourceID, aiLevelConfidence])
        return db.lastInsertedRowID
    }

    @discardableResult
    static func insertTargetLink(
        _ db: Database,
        sourceTargetId: Int,
        targetTargetId: Int? = nil,
        externalRef: String = "",
        relation: String = "contributes_to",
        confidence: Double? = nil,
        createdBy: String = "ai"
    ) throws -> Int64 {
        try db.execute(sql: """
            INSERT INTO target_links (source_target_id, target_target_id, external_ref, relation, confidence, created_by)
            VALUES (?, ?, ?, ?, ?, ?)
            """, arguments: [sourceTargetId, targetTargetId, externalRef, relation, confidence, createdBy])
        return db.lastInsertedRowID
    }

    // MARK: - Inbox Fixtures

    static func insertInboxItem(
        _ db: Database,
        channelID: String = "C001",
        messageTS: String = "1700000000.000100",
        threadTS: String = "",
        senderUserID: String = "U002",
        triggerType: String = "mention",
        snippet: String = "Hey, can you review this?",
        permalink: String = "",
        status: String = "pending",
        priority: String = "medium",
        aiReason: String = "",
        resolvedReason: String = "",
        snoozeUntil: String = "",
        taskID: Int? = nil,       // kept for call-site compat; maps to target_id column
        readAt: String? = nil
    ) throws {
        try db.execute(sql: """
            INSERT INTO inbox_items (channel_id, message_ts, thread_ts, sender_user_id,
                trigger_type, snippet, permalink, status, priority, ai_reason,
                resolved_reason, snooze_until, target_id, read_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [channelID, messageTS, threadTS, senderUserID,
                             triggerType, snippet, permalink, status, priority, aiReason,
                             resolvedReason, snoozeUntil, taskID, readAt])
    }

    // MARK: - Inbox Learned Rules Fixtures

    static func insertLearnedRule(
        _ db: Database,
        scopeKey: String = "sender:U1",
        weight: Double = -0.5,
        source: String = "implicit",
        evidenceCount: Int = 3,
        lastUpdated: String = "2026-04-23T10:00:00Z",
        ruleType: String = "source_mute"
    ) throws {
        try db.execute(
            sql: """
                INSERT INTO inbox_learned_rules (rule_type, scope_key, weight, source, evidence_count, last_updated)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
            arguments: [ruleType, scopeKey, weight, source, evidenceCount, lastUpdated]
        )
    }

    // MARK: - Inbox Feedback Fixtures

    static func insertFeedbackRecord(
        _ db: Database,
        inboxItemId: Int = 1,
        rating: Int = 1,
        reason: String = "useful",
        createdAt: String = "2026-04-23T10:00:00Z"
    ) throws {
        try db.execute(
            sql: """
                INSERT INTO inbox_feedback (inbox_item_id, rating, reason, created_at)
                VALUES (?, ?, ?, ?)
                """,
            arguments: [inboxItemId, rating, reason, createdAt]
        )
    }

    // MARK: - Calendar Fixtures

    static func ensureCalendar(
        _ db: Database,
        id: String = "primary",
        name: String = "Primary",
        isPrimary: Bool = true,
        isSelected: Bool = true
    ) throws {
        try db.execute(sql: """
            INSERT OR IGNORE INTO calendar_calendars (id, name, is_primary, is_selected)
            VALUES (?, ?, ?, ?)
            """, arguments: [id, name, isPrimary ? 1 : 0, isSelected ? 1 : 0])
    }

    static func insertCalendarEvent(
        _ db: Database,
        id: String = "evt_001",
        calendarID: String = "primary",
        title: String = "Team Standup",
        description: String = "",
        startTime: String = "2023-11-14T22:13:20Z",
        endTime: String = "2023-11-14T23:13:20Z",
        isAllDay: Bool = false,
        location: String = "",
        organizerEmail: String = "alice@example.com",
        attendees: String = "[]",
        isRecurring: Bool = false,
        eventStatus: String = "confirmed",
        eventType: String = "",
        htmlLink: String = "",
        updatedAt: String = ""
    ) throws {
        try ensureCalendar(db, id: calendarID)
        try db.execute(sql: """
            INSERT INTO calendar_events (id, calendar_id, title, description, location,
                start_time, end_time, organizer_email, attendees, is_recurring,
                is_all_day, event_status, event_type, html_link, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [id, calendarID, title, description, location,
                             startTime, endTime, organizerEmail, attendees,
                             isRecurring ? 1 : 0, isAllDay ? 1 : 0, eventStatus,
                             eventType, htmlLink, updatedAt])
    }

    // MARK: - Day Plan Fixtures

    @discardableResult
    static func insertDayPlan(
        _ db: Database,
        userID: String = "U001",
        planDate: String = "2026-04-23",
        status: String = "active",
        hasConflicts: Bool = false,
        conflictSummary: String? = nil,
        generatedAt: String = "2026-04-23T08:00:00Z",
        lastRegeneratedAt: String? = nil,
        regenerateCount: Int = 0,
        feedbackHistory: String? = nil,
        promptVersion: String? = nil,
        briefingID: Int? = nil,
        readAt: String? = nil
    ) throws -> Int64 {
        try db.execute(sql: """
            INSERT INTO day_plans (user_id, plan_date, status, has_conflicts, conflict_summary,
                generated_at, last_regenerated_at, regenerate_count, feedback_history,
                prompt_version, briefing_id, read_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [userID, planDate, status, hasConflicts ? 1 : 0, conflictSummary,
                             generatedAt, lastRegeneratedAt, regenerateCount, feedbackHistory,
                             promptVersion, briefingID, readAt])
        return db.lastInsertedRowID
    }

    @discardableResult
    static func insertDayPlanItem(
        _ db: Database,
        dayPlanID: Int64 = 1,
        kind: String = "timeblock",
        sourceType: String = "manual",
        sourceID: String? = nil,
        title: String = "Review PR",
        description: String? = nil,
        rationale: String? = nil,
        startTime: String? = nil,
        endTime: String? = nil,
        durationMin: Int? = nil,
        priority: String? = "medium",
        status: String = "pending",
        orderIndex: Int = 0,
        tags: String? = nil
    ) throws -> Int64 {
        try db.execute(sql: """
            INSERT INTO day_plan_items (day_plan_id, kind, source_type, source_id, title,
                description, rationale, start_time, end_time, duration_min, priority,
                status, order_index, tags)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, arguments: [dayPlanID, kind, sourceType, sourceID, title,
                             description, rationale, startTime, endTime, durationMin,
                             priority, status, orderIndex, tags])
        return db.lastInsertedRowID
    }
}
