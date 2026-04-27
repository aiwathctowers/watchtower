import Foundation

// MARK: - Overrides

/// Optional overrides applied when promoting a sub-item to a child target.
/// `nil` means "inherit the default" (parent fields, or sub-item text/due_date).
struct PromoteSubItemOverrides {
    var text: String?
    var intent: String?
    var level: String?
    var priority: String?
    var ownership: String?
    var dueDate: String?       // "YYYY-MM-DDTHH:MM"
    var periodStart: String?   // "YYYY-MM-DD"
    var periodEnd: String?     // "YYYY-MM-DD"
    /// Tag list. `nil` inherits parent tags; an empty array clears them.
    var tags: [String]?
}

// MARK: - Result

struct PromoteSubItemResult {
    var id: Int
    var text: String
    var level: String
    var priority: String
    var status: String
    var dueDate: String
    var periodStart: String
    var periodEnd: String
    var parentID: Int
    var sourceType: String
    var sourceID: String
}

// MARK: - Service

/// Bridges the Desktop app to the `watchtower targets promote-subitem --json` subprocess.
/// Promotes the sub-item at `index` of `parentID` to a standalone child target with parent_id set.
struct TargetPromoteSubItemService {
    let runner: CLIRunnerProtocol

    func promote(
        parentID: Int,
        index: Int,
        overrides: PromoteSubItemOverrides = PromoteSubItemOverrides()
    ) async throws -> PromoteSubItemResult {
        var args: [String] = [
            "targets", "promote-subitem",
            String(parentID), String(index),
            "--json",
        ]
        if let v = overrides.text {
            args.append(contentsOf: ["--text", v])
        }
        if let v = overrides.intent {
            args.append(contentsOf: ["--intent", v])
        }
        if let v = overrides.level {
            args.append(contentsOf: ["--level", v])
        }
        if let v = overrides.priority {
            args.append(contentsOf: ["--priority", v])
        }
        if let v = overrides.ownership {
            args.append(contentsOf: ["--ownership", v])
        }
        if let v = overrides.dueDate {
            args.append(contentsOf: ["--due", v])
        }
        if let v = overrides.periodStart {
            args.append(contentsOf: ["--period-start", v])
        }
        if let v = overrides.periodEnd {
            args.append(contentsOf: ["--period-end", v])
        }
        if let tags = overrides.tags {
            // Empty array → empty CLI value, which the Go side reads as "clear tags".
            args.append(contentsOf: ["--tags", tags.joined(separator: ",")])
        }

        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLIPromoteResponse.self, from: data)

        return PromoteSubItemResult(
            id: decoded.id,
            text: decoded.text,
            level: decoded.level,
            priority: decoded.priority,
            status: decoded.status,
            dueDate: decoded.dueDate ?? "",
            periodStart: decoded.periodStart ?? "",
            periodEnd: decoded.periodEnd ?? "",
            parentID: decoded.parentID,
            sourceType: decoded.sourceType,
            sourceID: decoded.sourceID
        )
    }
}

// MARK: - Wire schema

/// Mirrors the JSON emitted by `watchtower targets promote-subitem --json`.
/// See `cmd/targets.go` `runTargetsPromoteSubItem` for the Go side.
/// String fields that the Go side may render as `""` are decoded as `String?`
/// to harden against future schema drift (e.g. `omitempty` on the Go side).
private struct CLIPromoteResponse: Decodable {
    let id: Int
    let text: String
    let level: String
    let priority: String
    let status: String
    let dueDate: String?
    let periodStart: String?
    let periodEnd: String?
    let parentID: Int
    let sourceType: String
    let sourceID: String

    enum CodingKeys: String, CodingKey {
        case id, text, level, priority, status
        case dueDate     = "due_date"
        case periodStart = "period_start"
        case periodEnd   = "period_end"
        case parentID    = "parent_id"
        case sourceType  = "source_type"
        case sourceID    = "source_id"
    }
}
