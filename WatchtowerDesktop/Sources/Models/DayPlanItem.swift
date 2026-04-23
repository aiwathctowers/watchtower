import Foundation
import GRDB

// MARK: - Enums

enum DayPlanItemKind: String, Codable {
    case timeblock
    case backlog
}

enum DayPlanItemSourceType: String, Codable {
    case task
    case briefingAttention = "briefing_attention"
    case jira
    case calendar
    case manual
    case focus
}

enum DayPlanItemStatus: String, Codable {
    case pending
    case done
    case skipped
}

// MARK: - DayPlanItem

struct DayPlanItem: FetchableRecord, Identifiable, Equatable {
    let id: Int64
    let dayPlanId: Int64
    let kind: DayPlanItemKind
    let sourceType: DayPlanItemSourceType
    let sourceId: String?
    let title: String
    let details: String?          // maps to "description" column (avoids conflict with CustomStringConvertible)
    let rationale: String?
    let startTime: Date?
    let endTime: Date?
    let durationMin: Int?
    let priority: String?         // "high", "medium", "low", or nil
    let status: DayPlanItemStatus
    let orderIndex: Int
    let tags: String              // JSON array, e.g. "[]"
    let createdAt: Date
    let updatedAt: Date

    init(row: Row) {
        id = row["id"]
        dayPlanId = row["day_plan_id"]
        kind = DayPlanItemKind(rawValue: row["kind"] ?? "timeblock") ?? .timeblock
        sourceType = DayPlanItemSourceType(rawValue: row["source_type"] ?? "manual") ?? .manual
        sourceId = row["source_id"] as String?
        title = row["title"] ?? ""
        details = row["description"] as String?
        rationale = row["rationale"] as String?
        startTime = Self.parseDate(row["start_time"] as String? ?? "")
        endTime = Self.parseDate(row["end_time"] as String? ?? "")
        durationMin = row["duration_min"] as Int?
        priority = row["priority"] as String?
        status = DayPlanItemStatus(rawValue: row["status"] ?? "pending") ?? .pending
        orderIndex = row["order_index"] ?? 0
        tags = row["tags"] as String? ?? "[]"
        createdAt = Self.parseDate(row["created_at"] as String? ?? "") ?? Date()
        updatedAt = Self.parseDate(row["updated_at"] as String? ?? "") ?? Date()
    }

    // MARK: - Computed Properties

    var isCalendarEvent: Bool { sourceType == .calendar }
    var isManual: Bool { sourceType == .manual }

    /// Calendar items are read-only (sourced from external calendar, not editable).
    var isReadOnly: Bool { isCalendarEvent }

    /// "HH:mm–HH:mm" formatted time range, nil if either start or end time is missing.
    var timeRange: String? {
        guard let start = startTime, let end = endTime else { return nil }
        let fmt = DateFormatter()
        fmt.dateFormat = "HH:mm"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return "\(fmt.string(from: start))–\(fmt.string(from: end))"
    }

    var isDone: Bool { status == .done }
    var isSkipped: Bool { status == .skipped }
    var isPending: Bool { status == .pending }

    /// JSON-decoded tags array. Returns [] on invalid JSON or empty value.
    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    // MARK: - Date Helpers

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

    private static func parseDate(_ s: String) -> Date? {
        guard !s.isEmpty else { return nil }
        if let d = iso8601WithFractional.date(from: s) { return d }
        if let d = iso8601Standard.date(from: s) { return d }
        return nil
    }

    // MARK: - Equatable

    static func == (lhs: DayPlanItem, rhs: DayPlanItem) -> Bool {
        lhs.id == rhs.id && lhs.dayPlanId == rhs.dayPlanId && lhs.title == rhs.title
    }

#if DEBUG
    static func stub(
        id: Int64 = 1,
        dayPlanId: Int64 = 1,
        kind: DayPlanItemKind = .timeblock,
        sourceType: DayPlanItemSourceType = .manual,
        sourceId: String? = nil,
        title: String = "Review PR",
        details: String? = nil,
        rationale: String? = nil,
        startTime: Date? = nil,
        endTime: Date? = nil,
        durationMin: Int? = nil,
        priority: String? = "medium",
        status: DayPlanItemStatus = .pending,
        orderIndex: Int = 0,
        tags: String = "[]",
        createdAt: Date = Date(),
        updatedAt: Date = Date()
    ) -> DayPlanItem {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        let dict: [String: DatabaseValueConvertible?] = [
            "id": id,
            "day_plan_id": dayPlanId,
            "kind": kind.rawValue,
            "source_type": sourceType.rawValue,
            "source_id": sourceId,
            "title": title,
            "description": details,
            "rationale": rationale,
            "start_time": startTime.map { fmt.string(from: $0) },
            "end_time": endTime.map { fmt.string(from: $0) },
            "duration_min": durationMin,
            "priority": priority,
            "status": status.rawValue,
            "order_index": orderIndex,
            "tags": tags,
            "created_at": fmt.string(from: createdAt),
            "updated_at": fmt.string(from: updatedAt)
        ]
        return DayPlanItem(row: Row(dict))
    }
#endif
}
