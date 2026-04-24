import Foundation
import GRDB

// MARK: - TargetSubItem

struct TargetSubItem: Codable, Identifiable, Equatable {
    let id = UUID()
    var text: String
    var done: Bool
    var dueDate: String?

    enum CodingKeys: String, CodingKey {
        case text, done
        case dueDate = "due_date"
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.done == rhs.done && lhs.dueDate == rhs.dueDate
    }

    var dueDateParsed: Date? {
        guard let dueDate, !dueDate.isEmpty else { return nil }
        return Target.parseDueDate(dueDate)
    }

    var isOverdue: Bool {
        guard !done, let date = dueDateParsed else { return false }
        return date < Date()
    }
}

// MARK: - TargetNote

struct TargetNote: Codable, Identifiable, Equatable {
    let id = UUID()
    var text: String
    var createdAt: String

    enum CodingKeys: String, CodingKey {
        case text
        case createdAt = "created_at"
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.createdAt == rhs.createdAt
    }

    private static let iso8601Formatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    var createdDate: Date? {
        Self.iso8601Formatter.date(from: createdAt)
    }
}

// MARK: - Target

struct Target: FetchableRecord, TableRecord, Codable, Identifiable, Equatable, Hashable {
    static var databaseTableName = "targets"

    let id: Int
    let text: String
    let intent: String
    let level: String           // "quarter", "month", "week", "day", "custom"
    let customLabel: String     // free text only when level='custom'
    let periodStart: String     // YYYY-MM-DD
    let periodEnd: String       // YYYY-MM-DD
    let parentId: Int?
    let status: String          // "todo", "in_progress", "blocked", "done", "dismissed", "snoozed"
    let priority: String        // "high", "medium", "low"
    let ownership: String       // "mine", "delegated", "watching"
    let ballOn: String
    let dueDate: String         // "YYYY-MM-DDTHH:MM" or ""
    let snoozeUntil: String
    let blocking: String
    let tags: String            // JSON []
    let subItems: String        // JSON []
    let notes: String           // JSON []
    let progress: Double        // 0.0..1.0
    let sourceType: String      // "extract","briefing","manual","chat","inbox","jira","slack"
    let sourceID: String
    let aiLevelConfidence: Double?
    let createdAt: String
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case text
        case intent
        case level
        case customLabel      = "custom_label"
        case periodStart      = "period_start"
        case periodEnd        = "period_end"
        case parentId         = "parent_id"
        case status
        case priority
        case ownership
        case ballOn           = "ball_on"
        case dueDate          = "due_date"
        case snoozeUntil      = "snooze_until"
        case blocking
        case tags
        case subItems         = "sub_items"
        case notes
        case progress
        case sourceType       = "source_type"
        case sourceID         = "source_id"
        case aiLevelConfidence = "ai_level_confidence"
        case createdAt        = "created_at"
        case updatedAt        = "updated_at"
    }

    init(row: Row) {
        id               = row["id"]
        text             = row["text"] ?? ""
        intent           = row["intent"] ?? ""
        level            = row["level"] ?? "day"
        customLabel      = row["custom_label"] ?? ""
        periodStart      = row["period_start"] ?? ""
        periodEnd        = row["period_end"] ?? ""
        parentId         = row["parent_id"]
        status           = row["status"] ?? "todo"
        priority         = row["priority"] ?? "medium"
        ownership        = row["ownership"] ?? "mine"
        ballOn           = row["ball_on"] ?? ""
        dueDate          = row["due_date"] ?? ""
        snoozeUntil      = row["snooze_until"] ?? ""
        blocking         = row["blocking"] ?? ""
        tags             = row["tags"] ?? "[]"
        subItems         = row["sub_items"] ?? "[]"
        notes            = row["notes"] ?? "[]"
        progress         = row["progress"] ?? 0.0
        sourceType       = row["source_type"] ?? "manual"
        sourceID         = row["source_id"] ?? ""
        aiLevelConfidence = row["ai_level_confidence"]
        createdAt        = row["created_at"] ?? ""
        updatedAt        = row["updated_at"] ?? ""
    }

    // MARK: - Hashable (id-based)

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.id == rhs.id
    }

    // MARK: - Status Predicates

    var isActive: Bool {
        ["todo", "in_progress", "blocked"].contains(status)
    }

    var isOverdue: Bool {
        guard isActive, !dueDate.isEmpty else { return false }
        guard let due = Self.parseDueDate(dueDate) else { return false }
        return due < Date()
    }

    var isDueToday: Bool {
        guard !dueDate.isEmpty else { return false }
        guard let due = Self.parseDueDate(dueDate) else { return false }
        return Calendar.current.isDateInToday(due)
    }

    // MARK: - Level

    var levelOrder: Int {
        switch level {
        case "quarter": return 0
        case "month":   return 1
        case "week":    return 2
        case "day":     return 3
        default:        return 4
        }
    }

    // MARK: - Priority

    var priorityOrder: Int {
        switch priority {
        case "high":   return 0
        case "medium": return 1
        case "low":    return 2
        default:       return 1
        }
    }

    // MARK: - Status Display

    var statusIcon: String {
        switch status {
        case "todo":       return "circle"
        case "in_progress": return "circle.dotted.circle"
        case "blocked":    return "exclamationmark.circle"
        case "done":       return "checkmark.circle.fill"
        case "dismissed":  return "xmark.circle"
        case "snoozed":    return "moon.circle"
        default:           return "circle"
        }
    }

    var statusColor: String {
        switch status {
        case "todo":       return "secondary"
        case "in_progress": return "blue"
        case "blocked":    return "red"
        case "done":       return "green"
        case "dismissed":  return "gray"
        case "snoozed":    return "purple"
        default:           return "secondary"
        }
    }

    // MARK: - JSON Decoders

    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var decodedSubItems: [TargetSubItem] {
        guard !subItems.isEmpty, subItems != "[]",
              let data = subItems.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TargetSubItem].self, from: data)) ?? []
    }

    var decodedNotes: [TargetNote] {
        guard !notes.isEmpty, notes != "[]",
              let data = notes.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TargetNote].self, from: data)) ?? []
    }

    /// Progress as "2/5" format, nil if no sub-items.
    var subItemsProgress: String? {
        let items = decodedSubItems
        guard !items.isEmpty else { return nil }
        let done = items.filter(\.done).count
        return "\(done)/\(items.count)"
    }

    // MARK: - Source Helpers

    var sourceTrackID: Int? {
        guard sourceType == "track" else { return nil }
        return Int(sourceID)
    }

    var sourceDigestID: Int? {
        guard sourceType == "digest" else { return nil }
        return Int(sourceID)
    }

    var sourceBriefingID: Int? {
        guard sourceType == "briefing" else { return nil }
        return Int(sourceID)
    }

    // MARK: - Date Helpers

    private static let dateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    private static let datetimeFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd'T'HH:mm"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    /// Parses due date string supporting both "YYYY-MM-DDTHH:MM" and "YYYY-MM-DD" formats.
    static func parseDueDate(_ str: String) -> Date? {
        if let d = datetimeFormatter.date(from: str) { return d }
        if let d = dateFormatter.date(from: str) { return d }
        return nil
    }

    /// Formats due date for storage: "yyyy-MM-dd'T'HH:mm".
    static func formatDueDate(_ date: Date) -> String {
        datetimeFormatter.string(from: date)
    }

    private static let iso8601WithFractional: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    private static let iso8601Standard: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    var createdDate: Date {
        if let date = Self.iso8601WithFractional.date(from: createdAt) { return date }
        return Self.iso8601Standard.date(from: createdAt) ?? Date()
    }

    var updatedDate: Date {
        if let date = Self.iso8601WithFractional.date(from: updatedAt) { return date }
        return Self.iso8601Standard.date(from: updatedAt) ?? Date()
    }

    var dueDateFormatted: String? {
        guard !dueDate.isEmpty,
              let date = Self.parseDueDate(dueDate) else { return nil }
        let display = DateFormatter()
        display.dateStyle = .medium
        let comps = Calendar.current.dateComponents([.hour, .minute], from: date)
        if comps.hour == 0 && comps.minute == 0 {
            display.timeStyle = .none
        } else {
            display.timeStyle = .short
        }
        return display.string(from: date)
    }

    var dueDateParsed: Date? {
        guard !dueDate.isEmpty else { return nil }
        return Self.parseDueDate(dueDate)
    }
}
