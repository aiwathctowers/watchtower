import Foundation
import GRDB

// MARK: - TaskSubItem

struct TaskSubItem: Codable, Identifiable, Equatable {
    let id = UUID()
    var text: String
    var done: Bool

    enum CodingKeys: String, CodingKey {
        case text, done
    }

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.text == rhs.text && lhs.done == rhs.done
    }
}

// MARK: - TaskItem

struct TaskItem: FetchableRecord, Identifiable, Equatable {
    let id: Int
    let text: String
    let intent: String
    let status: String          // "todo", "in_progress", "blocked", "done", "dismissed", "snoozed"
    let priority: String        // "high", "medium", "low"
    let ownership: String       // "mine", "delegated", "watching"
    let ballOn: String
    let dueDate: String         // "YYYY-MM-DD" or ""
    let snoozeUntil: String     // "YYYY-MM-DD" or ""
    let blocking: String
    let tags: String            // JSON
    let subItems: String        // JSON
    let sourceType: String      // "track", "digest", "briefing", "manual", "chat"
    let sourceID: String
    let createdAt: String
    let updatedAt: String

    init(row: Row) {
        id = row["id"]
        text = row["text"] ?? ""
        intent = row["intent"] ?? ""
        status = row["status"] ?? "todo"
        priority = row["priority"] ?? "medium"
        ownership = row["ownership"] ?? "mine"
        ballOn = row["ball_on"] ?? ""
        dueDate = row["due_date"] ?? ""
        snoozeUntil = row["snooze_until"] ?? ""
        blocking = row["blocking"] ?? ""
        tags = row["tags"] ?? "[]"
        subItems = row["sub_items"] ?? "[]"
        sourceType = row["source_type"] ?? "manual"
        sourceID = row["source_id"] ?? ""
        createdAt = row["created_at"] ?? ""
        updatedAt = row["updated_at"] ?? ""
    }

    // MARK: - Status Predicates

    var isActive: Bool {
        ["todo", "in_progress", "blocked"].contains(status)
    }

    var isOverdue: Bool {
        guard isActive, !dueDate.isEmpty else { return false }
        guard let due = Self.dateFormatter.date(from: dueDate) else { return false }
        return due < Calendar.current.startOfDay(for: Date())
    }

    var isDueToday: Bool {
        guard !dueDate.isEmpty else { return false }
        guard let due = Self.dateFormatter.date(from: dueDate) else { return false }
        return Calendar.current.isDateInToday(due)
    }

    // MARK: - Priority

    var priorityOrder: Int {
        switch priority {
        case "high": return 0
        case "medium": return 1
        case "low": return 2
        default: return 1
        }
    }

    // MARK: - Status Display

    var statusIcon: String {
        switch status {
        case "todo": return "circle"
        case "in_progress": return "circle.dotted.circle"
        case "blocked": return "exclamationmark.circle"
        case "done": return "checkmark.circle.fill"
        case "dismissed": return "xmark.circle"
        case "snoozed": return "moon.circle"
        default: return "circle"
        }
    }

    var statusColor: String {
        switch status {
        case "todo": return "secondary"
        case "in_progress": return "blue"
        case "blocked": return "red"
        case "done": return "green"
        case "dismissed": return "gray"
        case "snoozed": return "purple"
        default: return "secondary"
        }
    }

    // MARK: - JSON Decoders

    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var decodedSubItems: [TaskSubItem] {
        guard !subItems.isEmpty, subItems != "[]",
              let data = subItems.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TaskSubItem].self, from: data)) ?? []
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
              let date = Self.dateFormatter.date(from: dueDate) else { return nil }
        let display = DateFormatter()
        display.dateStyle = .medium
        display.timeStyle = .none
        return display.string(from: date)
    }
}
