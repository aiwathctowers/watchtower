import GRDB

enum MeetingNoteQueries {

    // MARK: - Fetch

    static func fetchForEvent(_ db: Database, eventID: String) throws -> [MeetingNote] {
        try MeetingNote
            .filter(Column("event_id") == eventID)
            .order(Column("sort_order"), Column("created_at"))
            .fetchAll(db)
    }

    // MARK: - Create

    @discardableResult
    static func create(
        _ db: Database,
        eventID: String,
        type: MeetingNote.NoteType,
        text: String,
        sortOrder: Int
    ) throws -> MeetingNote {
        try db.execute(
            sql: """
                INSERT INTO meeting_notes (event_id, type, text, is_checked, sort_order)
                VALUES (?, ?, ?, 0, ?)
                """,
            arguments: [eventID, type.rawValue, text, sortOrder]
        )
        let rowID = db.lastInsertedRowID
        return MeetingNote(
            id: rowID,
            eventID: eventID,
            type: type,
            text: text,
            isChecked: false,
            sortOrder: sortOrder,
            taskID: nil,
            createdAt: "",  // Will be filled by DB default
            updatedAt: ""
        )
    }

    // MARK: - Update

    static func update(_ db: Database, id: Int64, text: String) throws {
        try db.execute(
            sql: "UPDATE meeting_notes SET text = ?, updated_at = datetime('now') WHERE id = ?",
            arguments: [text, id]
        )
    }

    static func toggleChecked(_ db: Database, id: Int64) throws {
        try db.execute(
            sql: "UPDATE meeting_notes SET is_checked = NOT is_checked, updated_at = datetime('now') WHERE id = ?",
            arguments: [id]
        )
    }

    static func setTaskID(_ db: Database, noteID: Int64, taskID: Int64) throws {
        try db.execute(
            sql: "UPDATE meeting_notes SET task_id = ?, updated_at = datetime('now') WHERE id = ?",
            arguments: [taskID, noteID]
        )
    }

    // MARK: - Delete

    static func delete(_ db: Database, id: Int64) throws {
        try db.execute(
            sql: "DELETE FROM meeting_notes WHERE id = ?",
            arguments: [id]
        )
    }

    // MARK: - Count

    static func countForEvent(_ db: Database, eventID: String) throws -> Int {
        try MeetingNote.filter(Column("event_id") == eventID).fetchCount(db)
    }
}
