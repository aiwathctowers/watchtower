import Foundation

// MARK: - Result

struct TargetExtractResult {
    var extracted: [ProposedTarget]
    var omittedCount: Int
    var notes: String
}

// MARK: - Service

/// Bridges the Desktop app to the `watchtower targets extract --json` subprocess.
/// Decodes the CLI's JSON output into existing Swift models (`ProposedTarget`/`ProposedLink`)
/// that the `ExtractPreviewSheet` already consumes.
struct TargetExtractService {
    let runner: CLIRunnerProtocol

    func extract(text: String, sourceRef: String = "") async throws -> TargetExtractResult {
        var args = ["targets", "extract", "--json", "--text", text]
        if !sourceRef.isEmpty {
            args.append(contentsOf: ["--source-ref", sourceRef])
        }
        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLIExtractResponse.self, from: data)

        let proposed = decoded.extracted.map { Self.proposedFrom($0) }

        return TargetExtractResult(
            extracted: proposed,
            omittedCount: decoded.omitted_count,
            notes: decoded.notes
        )
    }

    // Split out to keep Swift's type-checker happy (SE-0286 complexity limit).
    private static func proposedFrom(_ item: CLIExtractedItem) -> ProposedTarget {
        let links: [ProposedLink] = (item.secondary_links ?? []).map { l in
            ProposedLink(
                targetId: l.target_id.map { Int($0) },
                externalRef: l.external_ref,
                relation: l.relation
            )
        }
        let subs: [TargetSubItem] = (item.sub_items ?? []).map { s in
            TargetSubItem(text: s.text, done: false, dueDate: nil)
        }
        return ProposedTarget(
            text: item.text,
            intent: item.intent,
            level: item.level,
            customLabel: item.custom_label,
            levelConfidence: item.ai_level_confidence,
            periodStart: item.period_start,
            periodEnd: item.period_end,
            priority: item.priority.isEmpty ? "medium" : item.priority,
            parentId: item.parent_id.map { Int($0) },
            secondaryLinks: links,
            subItems: subs
        )
    }
}

// MARK: - Wire schema

/// Mirrors `{extracted, omitted_count, notes}` emitted by `watchtower targets extract --json`.
/// See `cmd/targets.go` `jsonProposedTarget`/`jsonProposedLink` for the Go side.
private struct CLIExtractResponse: Decodable {
    let extracted: [CLIExtractedItem]
    let omitted_count: Int
    let notes: String
}

private struct CLIExtractedItem: Decodable {
    let text: String
    let intent: String
    let level: String
    let custom_label: String
    let period_start: String
    let period_end: String
    let priority: String
    let parent_id: Int64?
    let ai_level_confidence: Double?
    let secondary_links: [CLISecondaryLink]?
    let sub_items: [CLISubItem]?
}

private struct CLISecondaryLink: Decodable {
    let target_id: Int64?
    let external_ref: String
    let relation: String
}

private struct CLISubItem: Decodable {
    let text: String
}
