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
        channelID: String = "C001",
        assigneeUserID: String = "U001",
        assigneeRaw: String = "alice",
        text: String = "Fix the bug",
        context: String = "Discussed in standup",
        sourceMessageTS: String = "1700000000.000100",
        sourceChannelName: String = "general",
        status: String = "inbox",
        priority: String = "medium",
        dueDate: Double? = nil,
        periodFrom: Double = 1700000000,
        periodTo: Double = 1700086400,
        model: String = "haiku",
        ownership: String = "mine",
        ballOn: String = "",
        ownerUserID: String = ""
    ) throws {
        var cols = "channel_id, assignee_user_id, assignee_raw, text, context, source_message_ts, source_channel_name, status, priority, period_from, period_to, model, ownership, ball_on, owner_user_id"
        var placeholders = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
        var args: [any DatabaseValueConvertible] = [
            channelID, assigneeUserID, assigneeRaw, text, context,
            sourceMessageTS, sourceChannelName, status, priority, periodFrom, periodTo, model,
            ownership, ballOn, ownerUserID
        ]
        if let dueDate {
            cols += ", due_date"
            placeholders += ", ?"
            args.append(dueDate)
        }
        try db.execute(
            sql: "INSERT INTO tracks (\(cols)) VALUES (\(placeholders))",
            arguments: StatementArguments(args)
        )
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
        ts_unix      REAL GENERATED ALWAYS AS (CASE WHEN INSTR(ts, '.') > 0 THEN CAST(SUBSTR(ts, 1, INSTR(ts, '.') - 1) AS REAL) ELSE CAST(ts AS REAL) END) STORED,
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
        UNIQUE(channel_id, type, period_from, period_to)
    );
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
        channel_id          TEXT NOT NULL,
        assignee_user_id    TEXT NOT NULL,
        assignee_raw        TEXT NOT NULL DEFAULT '',
        text                TEXT NOT NULL,
        context             TEXT NOT NULL DEFAULT '',
        source_message_ts   TEXT NOT NULL DEFAULT '',
        source_channel_name TEXT NOT NULL DEFAULT '',
        status              TEXT NOT NULL DEFAULT 'inbox' CHECK(status IN ('inbox','active','done','dismissed','snoozed')),
        priority            TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('high','medium','low')),
        due_date            REAL,
        has_updates         INTEGER NOT NULL DEFAULT 0,
        last_checked_ts     TEXT NOT NULL DEFAULT '',
        snooze_until        REAL,
        pre_snooze_status   TEXT NOT NULL DEFAULT '',
        period_from         REAL NOT NULL,
        period_to           REAL NOT NULL,
        model               TEXT NOT NULL DEFAULT '',
        input_tokens        INTEGER NOT NULL DEFAULT 0,
        output_tokens       INTEGER NOT NULL DEFAULT 0,
        cost_usd            REAL NOT NULL DEFAULT 0,
        created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
        completed_at        TEXT,
        participants        TEXT NOT NULL DEFAULT '[]',
        source_refs         TEXT NOT NULL DEFAULT '[]',
        requester_name      TEXT NOT NULL DEFAULT '',
        requester_user_id   TEXT NOT NULL DEFAULT '',
        category            TEXT NOT NULL DEFAULT '',
        blocking            TEXT NOT NULL DEFAULT '',
        tags                TEXT NOT NULL DEFAULT '[]',
        decision_summary    TEXT NOT NULL DEFAULT '',
        decision_options    TEXT NOT NULL DEFAULT '[]',
        related_digest_ids  TEXT NOT NULL DEFAULT '[]',
        sub_items           TEXT NOT NULL DEFAULT '[]',
        prompt_version      INTEGER NOT NULL DEFAULT 0,
        ownership           TEXT NOT NULL DEFAULT 'mine',
        ball_on             TEXT NOT NULL DEFAULT '',
        owner_user_id       TEXT NOT NULL DEFAULT ''
    );
    CREATE TABLE IF NOT EXISTS track_history (
        id              INTEGER PRIMARY KEY AUTOINCREMENT,
        track_id        INTEGER NOT NULL,
        event           TEXT NOT NULL,
        field           TEXT NOT NULL DEFAULT '',
        old_value       TEXT NOT NULL DEFAULT '',
        new_value       TEXT NOT NULL DEFAULT '',
        created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
    );
    CREATE TABLE IF NOT EXISTS feedback (
        id          INTEGER PRIMARY KEY AUTOINCREMENT,
        entity_type TEXT NOT NULL CHECK(entity_type IN ('digest', 'track', 'decision', 'user_analysis')),
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
    """

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
                onboardingDone ? 1 : 0, customPromptContext,
            ])
    }
}
