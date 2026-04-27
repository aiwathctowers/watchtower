import Foundation

// MARK: - Result

struct SuggestedLinksResult {
    var parentID: Int?
    var secondaryLinks: [ProposedLink]
}

// MARK: - Service

/// Bridges the Desktop app to the `watchtower targets suggest-links <id> --json` subprocess.
/// Decodes the CLI's JSON output into a domain result that the `SuggestLinksSheet`
/// (Task 6) consumes.
struct TargetSuggestLinksService {
    let runner: CLIRunnerProtocol

    func suggest(targetID: Int) async throws -> SuggestedLinksResult {
        let args = ["targets", "suggest-links", "\(targetID)", "--json"]
        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLISuggestLinksResponse.self, from: data)
        let links = (decoded.secondary_links ?? []).map { l in
            ProposedLink(
                targetId: l.target_id.map { Int($0) },
                externalRef: l.external_ref,
                relation: l.relation
            )
        }
        return SuggestedLinksResult(
            parentID: decoded.parent_id.map { Int($0) },
            secondaryLinks: links
        )
    }
}

// MARK: - Wire schema

/// Mirrors `{parent_id, secondary_links}` emitted by `watchtower targets suggest-links --json`.
/// See `cmd/targets.go` `runTargetsSuggestLinks` for the Go side.
private struct CLISuggestLinksResponse: Decodable {
    let parent_id: Int64?
    let secondary_links: [CLISuggestedLink]?
}

private struct CLISuggestedLink: Decodable {
    let target_id: Int64?
    let external_ref: String
    let relation: String
}
