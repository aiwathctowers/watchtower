import Foundation
import GRDB

// TrackState is a snapshot of a track's narrative fields at a point in time,
// captured BEFORE a mutating call (extraction or manual edit) overwrites
// those fields. See docs/inventory/tracks.md TRACKS-06.
struct TrackState: FetchableRecord, Identifiable, Equatable {
    let id: Int
    let trackID: Int
    let text: String
    let context: String
    let category: String
    let ownership: String
    let ballOn: String
    let ownerUserID: String
    let requesterName: String
    let requesterUserID: String
    let blocking: String
    let decisionSummary: String
    let decisionOptions: String
    let subItems: String
    let participants: String
    let tags: String
    let priority: String
    let dueDate: Double?
    let source: String   // 'extraction' | 'manual'
    let model: String
    let promptVersion: Int
    let createdAt: String

    init(row: Row) {
        id = row["id"]
        trackID = row["track_id"]
        text = row["text"] ?? ""
        context = row["context"] ?? ""
        category = row["category"] ?? "task"
        ownership = row["ownership"] ?? "mine"
        ballOn = row["ball_on"] ?? ""
        ownerUserID = row["owner_user_id"] ?? ""
        requesterName = row["requester_name"] ?? ""
        requesterUserID = row["requester_user_id"] ?? ""
        blocking = row["blocking"] ?? ""
        decisionSummary = row["decision_summary"] ?? ""
        decisionOptions = row["decision_options"] ?? "[]"
        subItems = row["sub_items"] ?? "[]"
        participants = row["participants"] ?? "[]"
        tags = row["tags"] ?? "[]"
        priority = row["priority"] ?? "medium"
        dueDate = row["due_date"]
        source = row["source"] ?? "extraction"
        model = row["model"] ?? ""
        promptVersion = row["prompt_version"] ?? 0
        createdAt = row["created_at"] ?? ""
    }

    // MARK: - Source helpers

    var isExtraction: Bool { source == "extraction" }
    var isManual: Bool { source == "manual" }

    var sourceLabel: String {
        switch source {
        case "extraction":
            return model.isEmpty ? "AI extraction" : "AI extraction (\(model))"
        case "manual": return "Manual edit"
        default: return source.capitalized
        }
    }

    // MARK: - Date

    private static let iso8601: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    var createdDate: Date {
        Self.iso8601.date(from: createdAt) ?? Date()
    }

    var createdAgo: String {
        let interval = Date().timeIntervalSince(createdDate)
        if interval < 60 { return "just now" }
        if interval < 3600 { return "\(Int(interval / 60))m ago" }
        if interval < 86400 { return "\(Int(interval / 3600))h ago" }
        let days = Int(interval / 86400)
        return days == 1 ? "1d ago" : "\(days)d ago"
    }

    // MARK: - JSON decoders (mirror Track)

    var decodedSubItems: [TrackSubItem] {
        guard !subItems.isEmpty, subItems != "[]",
              let data = subItems.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([TrackSubItem].self, from: data)) ?? []
    }

    var decodedTags: [String] {
        guard !tags.isEmpty, tags != "[]",
              let data = tags.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }
}
