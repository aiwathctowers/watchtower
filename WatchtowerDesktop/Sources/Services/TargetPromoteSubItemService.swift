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
    /// Comma-separated tag list. Empty string clears the parent tags.
    var tagsCSV: String?
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
        if let v = overrides.tagsCSV {
            args.append(contentsOf: ["--tags", v])
        }

        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLIPromoteResponse.self, from: data)

        return PromoteSubItemResult(
            id: decoded.id,
            text: decoded.text,
            level: decoded.level,
            priority: decoded.priority,
            status: decoded.status,
            dueDate: decoded.due_date,
            periodStart: decoded.period_start,
            periodEnd: decoded.period_end,
            parentID: decoded.parent_id,
            sourceType: decoded.source_type,
            sourceID: decoded.source_id
        )
    }
}

// MARK: - Wire schema

/// Mirrors the JSON emitted by `watchtower targets promote-subitem --json`.
/// See `cmd/targets.go` `runTargetsPromoteSubItem` for the Go side.
private struct CLIPromoteResponse: Decodable {
    let id: Int
    let text: String
    let level: String
    let priority: String
    let status: String
    let due_date: String
    let period_start: String
    let period_end: String
    let parent_id: Int
    let source_type: String
    let source_id: String
}
