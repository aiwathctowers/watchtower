import Foundation
import GRDB

// MARK: - DayPlan

struct DayPlan: FetchableRecord, Identifiable, Equatable {
    let id: Int64
    let userId: String
    let planDate: String          // "YYYY-MM-DD"
    let status: String            // "active", "archived"
    let hasConflicts: Bool
    let conflictSummary: String?
    let generatedAt: Date
    let lastRegeneratedAt: Date?
    let regenerateCount: Int
    let feedbackHistory: String   // JSON array of strings, e.g. "[]"
    let promptVersion: String?
    let briefingId: Int64?
    let readAt: Date?
    let createdAt: Date
    let updatedAt: Date

    init(row: Row) {
        id = row["id"]
        userId = row["user_id"] ?? ""
        planDate = row["plan_date"] ?? ""
        status = row["status"] ?? "active"
        hasConflicts = (row["has_conflicts"] as Int? ?? 0) != 0
        conflictSummary = row["conflict_summary"] as String?
        generatedAt = Self.parseDate(row["generated_at"] as String? ?? "") ?? Date()
        lastRegeneratedAt = Self.parseDate(row["last_regenerated_at"] as String? ?? "")
        regenerateCount = row["regenerate_count"] ?? 0
        feedbackHistory = row["feedback_history"] as String? ?? "[]"
        promptVersion = row["prompt_version"] as String?
        briefingId = row["briefing_id"] as Int64?
        readAt = Self.parseDate(row["read_at"] as String? ?? "")
        createdAt = Self.parseDate(row["created_at"] as String? ?? "") ?? Date()
        updatedAt = Self.parseDate(row["updated_at"] as String? ?? "") ?? Date()
    }

    // MARK: - Computed Properties

    /// True when planDate matches today's date in the local time zone.
    var isToday: Bool {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        let todayStr = fmt.string(from: Date())
        return planDate == todayStr
    }

    /// JSON-decoded feedback history strings. Returns [] on invalid JSON or empty value.
    var parsedFeedbackHistory: [String] {
        guard !feedbackHistory.isEmpty, feedbackHistory != "[]",
              let data = feedbackHistory.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }

    var isRead: Bool { readAt != nil }

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

    static func == (lhs: DayPlan, rhs: DayPlan) -> Bool {
        lhs.id == rhs.id &&
        lhs.userId == rhs.userId &&
        lhs.planDate == rhs.planDate &&
        lhs.status == rhs.status &&
        lhs.hasConflicts == rhs.hasConflicts
    }

#if DEBUG
    static func stub(
        id: Int64 = 1,
        userId: String = "U001",
        planDate: String = "2026-04-23",
        status: String = "active",
        hasConflicts: Bool = false,
        conflictSummary: String? = nil,
        generatedAt: Date = Date(),
        lastRegeneratedAt: Date? = nil,
        regenerateCount: Int = 0,
        feedbackHistory: String = "[]",
        promptVersion: String? = nil,
        briefingId: Int64? = nil,
        readAt: Date? = nil,
        createdAt: Date = Date(),
        updatedAt: Date = Date()
    ) -> DayPlan {
        // Build via GRDB Row to respect FetchableRecord init.
        let dict: [String: DatabaseValueConvertible?] = [
            "id": id,
            "user_id": userId,
            "plan_date": planDate,
            "status": status,
            "has_conflicts": hasConflicts ? 1 : 0,
            "conflict_summary": conflictSummary,
            "generated_at": iso8601Standard.string(from: generatedAt),
            "last_regenerated_at": lastRegeneratedAt.map { iso8601Standard.string(from: $0) },
            "regenerate_count": regenerateCount,
            "feedback_history": feedbackHistory,
            "prompt_version": promptVersion,
            "briefing_id": briefingId,
            "read_at": readAt.map { iso8601Standard.string(from: $0) },
            "created_at": iso8601Standard.string(from: createdAt),
            "updated_at": iso8601Standard.string(from: updatedAt)
        ]
        return DayPlan(row: Row(dict))
    }
#endif
}
