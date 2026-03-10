import Foundation
import GRDB

enum ChatMessageQueries {
    static func ensureTable(_ db: Database) throws {
        try db.execute(sql: """
            CREATE TABLE IF NOT EXISTS chat_messages (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                conversation_id INTEGER NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
                role TEXT NOT NULL,
                text TEXT NOT NULL,
                created_at REAL NOT NULL
            )
        """)
        try db.execute(sql: """
            CREATE INDEX IF NOT EXISTS idx_chat_messages_conversation ON chat_messages(conversation_id)
        """)
    }

    static func fetchByConversation(_ db: Database, conversationID: Int64) throws -> [ChatMessageRecord] {
        try ChatMessageRecord.fetchAll(db, sql: """
            SELECT * FROM chat_messages WHERE conversation_id = ? ORDER BY created_at ASC
        """, arguments: [conversationID])
    }

    @discardableResult
    static func insert(_ db: Database, conversationID: Int64, role: String, text: String) throws -> Int64 {
        let now = Date().timeIntervalSince1970
        try db.execute(sql: """
            INSERT INTO chat_messages (conversation_id, role, text, created_at) VALUES (?, ?, ?, ?)
        """, arguments: [conversationID, role, text, now])
        return db.lastInsertedRowID
    }

    static func deleteByConversation(_ db: Database, conversationID: Int64) throws {
        try db.execute(sql: "DELETE FROM chat_messages WHERE conversation_id = ?", arguments: [conversationID])
    }
}
