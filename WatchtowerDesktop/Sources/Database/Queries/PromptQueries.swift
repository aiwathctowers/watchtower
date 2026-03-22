import GRDB

enum PromptQueries {

    static func fetchAll(_ db: Database) throws -> [PromptTemplate] {
        guard try db.tableExists("prompts") else { return [] }
        return try PromptTemplate.fetchAll(db, sql: "SELECT * FROM prompts ORDER BY id")
    }

    static func fetchByID(_ db: Database, id: String) throws -> PromptTemplate? {
        guard try db.tableExists("prompts") else { return nil }
        return try PromptTemplate.fetchOne(db, sql: "SELECT * FROM prompts WHERE id = ?", arguments: [id])
    }

    static func fetchHistory(_ db: Database, promptID: String) throws -> [PromptHistoryEntry] {
        guard try db.tableExists("prompt_history") else { return [] }
        return try PromptHistoryEntry.fetchAll(
            db,
            sql: """
                SELECT * FROM prompt_history WHERE prompt_id = ? ORDER BY version DESC
                """,
            arguments: [promptID]
        )
    }
}
