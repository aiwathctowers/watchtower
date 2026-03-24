import Foundation
import GRDB

enum ChatConversationQueries {
    static func ensureTable(_ db: Database) throws {
        try db.execute(sql: """
            CREATE TABLE IF NOT EXISTS chat_conversations (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                title TEXT NOT NULL DEFAULT '',
                session_id TEXT,
                context_type TEXT,
                context_id TEXT,
                created_at REAL NOT NULL,
                updated_at REAL NOT NULL
            )
        """)
    }

    static func fetchAll(_ db: Database) throws -> [ChatConversation] {
        try ChatConversation.fetchAll(db, sql: """
            SELECT * FROM chat_conversations ORDER BY updated_at DESC
        """)
    }

    static func fetchStandalone(_ db: Database) throws -> [ChatConversation] {
        try ChatConversation.fetchAll(db, sql: """
            SELECT * FROM chat_conversations WHERE context_type IS NULL ORDER BY updated_at DESC
        """)
    }

    static func search(_ db: Database, query: String) throws -> [ChatConversation] {
        let pattern = "%\(query)%"
        return try ChatConversation.fetchAll(
            db,
            sql: """
                SELECT * FROM chat_conversations WHERE context_type IS NULL AND title LIKE ? ORDER BY updated_at DESC
                """,
            arguments: [pattern]
        )
    }

    static func ensureContextColumns(_ db: Database) throws {
        let columns = try db.columns(in: "chat_conversations").map(\.name)
        if !columns.contains("context_type") {
            try db.execute(sql: "ALTER TABLE chat_conversations ADD COLUMN context_type TEXT")
            // Fresh column — no migration needed.
            if !columns.contains("context_id") {
                try db.execute(sql: "ALTER TABLE chat_conversations ADD COLUMN context_id TEXT")
            }
            return
        }
        if !columns.contains("context_id") {
            try db.execute(sql: "ALTER TABLE chat_conversations ADD COLUMN context_id TEXT")
        }
        // One-time migration: rename old "action_item" context type to "track".
        try db.execute(sql: "UPDATE chat_conversations SET context_type = 'track' WHERE context_type = 'action_item'")
    }

    @discardableResult
    static func create(_ db: Database, title: String = "", contextType: String? = nil, contextID: String? = nil) throws -> ChatConversation {
        let now = Date().timeIntervalSince1970
        try db.execute(sql: """
            INSERT INTO chat_conversations (title, context_type, context_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
        """, arguments: [title, contextType, contextID, now, now])
        let rowID = db.lastInsertedRowID
        guard let conversation = try ChatConversation.fetchOne(db, sql: "SELECT * FROM chat_conversations WHERE id = ?", arguments: [rowID]) else {
            throw DatabaseError(message: "Failed to fetch newly created chat conversation")
        }
        return conversation
    }

    static func fetchByContext(_ db: Database, type: String, id: String) throws -> ChatConversation? {
        try ChatConversation.fetchOne(
            db,
            sql: """
                SELECT * FROM chat_conversations WHERE context_type = ? AND context_id = ? ORDER BY updated_at DESC LIMIT 1
                """,
            arguments: [type, id]
        )
    }

    static func updateTitle(_ db: Database, id: Int64, title: String) throws {
        let now = Date().timeIntervalSince1970
        try db.execute(sql: """
            UPDATE chat_conversations SET title = ?, updated_at = ? WHERE id = ?
        """, arguments: [title, now, id])
    }

    static func updateSessionID(_ db: Database, id: Int64, sessionID: String) throws {
        let now = Date().timeIntervalSince1970
        try db.execute(sql: """
            UPDATE chat_conversations SET session_id = ?, updated_at = ? WHERE id = ?
        """, arguments: [sessionID, now, id])
    }

    static func touch(_ db: Database, id: Int64) throws {
        let now = Date().timeIntervalSince1970
        try db.execute(sql: """
            UPDATE chat_conversations SET updated_at = ? WHERE id = ?
        """, arguments: [now, id])
    }

    static func delete(_ db: Database, id: Int64) throws {
        try db.execute(sql: "DELETE FROM chat_conversations WHERE id = ?", arguments: [id])
    }

    static func fetchByID(_ db: Database, id: Int64) throws -> ChatConversation? {
        try ChatConversation.fetchOne(db, sql: "SELECT * FROM chat_conversations WHERE id = ?", arguments: [id])
    }
}
