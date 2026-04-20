import Foundation
import GRDB

struct MeetingNote: Codable, FetchableRecord, PersistableRecord, Identifiable {
    static let databaseTableName = "meeting_notes"

    var id: Int64?
    var eventID: String
    var type: NoteType
    var text: String
    var isChecked: Bool
    var sortOrder: Int
    var taskID: Int64?
    var createdAt: String
    var updatedAt: String

    enum NoteType: String, Codable {
        case question
        case note
    }

    enum CodingKeys: String, CodingKey {
        case id, text, type
        case eventID = "event_id"
        case isChecked = "is_checked"
        case sortOrder = "sort_order"
        case taskID = "task_id"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    mutating func didInsert(_ inserted: InsertionSuccess) {
        id = inserted.rowID
    }
}
