import Foundation
import GRDB
import Testing
@testable import WatchtowerDesktop

@Suite("DigestWatcher")
@MainActor
struct DigestWatcherTests {
    private func makePool() throws -> DatabasePool {
        let (manager, _) = try TestDatabase.createDatabaseManager()
        return manager.dbPool
    }

    private func insertChannel(
        _ pool: DatabasePool,
        id: String,
        name: String,
        type: String,
        dmUserID: String? = nil
    ) throws {
        try pool.write { db in
            try db.execute(sql: """
                INSERT INTO channels (id, name, type, topic, purpose, is_archived, is_member, dm_user_id, num_members, last_read, updated_at)
                VALUES (?, ?, ?, '', '', 0, 1, ?, 0, '', strftime('%Y-%m-%dT%H:%M:%SZ','now'))
                """, arguments: [id, name, type, dmUserID ?? ""])
        }
    }

    private func insertUser(
        _ pool: DatabasePool,
        id: String,
        name: String,
        displayName: String = ""
    ) throws {
        try pool.write { db in
            try db.execute(sql: """
                INSERT INTO users (id, name, display_name, real_name, email, is_bot, is_deleted, is_stub, profile_json, updated_at)
                VALUES (?, ?, ?, '', '', 0, 0, 0, '{}', strftime('%Y-%m-%dT%H:%M:%SZ','now'))
                """, arguments: [id, name, displayName])
        }
    }

    @Test("init seeds lastChecked from UserDefaults defaults")
    func initSeedsLastChecked() throws {
        let pool = try makePool()
        // Wipe defaults so we get a fresh init.
        UserDefaults.standard.removeObject(forKey: "lastCheckedDigestID")
        UserDefaults.standard.removeObject(forKey: "lastCheckedBriefingID")

        let watcher = DigestWatcher(dbPool: pool)
        // Init alone doesn't trigger DB lookups; that happens on start().
        _ = watcher
    }

    @Test("start initializes lastCheckedDigestID from max id when first run")
    func startSeedsFromDB() throws {
        UserDefaults.standard.removeObject(forKey: "lastCheckedDigestID")
        UserDefaults.standard.removeObject(forKey: "lastCheckedBriefingID")

        let pool = try makePool()
        // Seed an existing digest so max id is non-zero.
        try pool.write { db in
            try db.execute(sql: """
                INSERT INTO digests (channel_id, period_from, period_to, type, summary, topics, decisions, action_items, message_count, model)
                VALUES ('C1', 100, 200, 'channel', 'x', '[]', '[]', '[]', 1, 'm')
                """)
        }

        let watcher = DigestWatcher(dbPool: pool)
        watcher.start()
        // Stop quickly to avoid the 60s polling loop racing in tests.
        watcher.stop()

        let stored = UserDefaults.standard.integer(forKey: "lastCheckedDigestID")
        #expect(stored > 0, "lastCheckedDigestID should be seeded from max digest id, got \(stored)")
    }

    @Test("stop cancels watch task safely when called before start")
    func stopBeforeStart() throws {
        let pool = try makePool()
        let watcher = DigestWatcher(dbPool: pool)
        watcher.stop() // should not crash
    }

    @Test("stop is idempotent")
    func stopIdempotent() throws {
        let pool = try makePool()
        let watcher = DigestWatcher(dbPool: pool)
        watcher.start()
        watcher.stop()
        watcher.stop()
    }
}
