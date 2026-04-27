import Foundation

/// Bridges the Desktop app to `watchtower meeting-prep recap --json`.
/// CLI is the sole writer to `meeting_recaps`; on success the row is upserted
/// before returning. Caller refetches via `MeetingRecapQueries.fetch` to get
/// authoritative content + timestamps.
struct MeetingRecapService {
    let runner: CLIRunnerProtocol

    func generate(eventID: String, text: String) async throws {
        let args = [
            "meeting-prep", "recap",
            "--event-id", eventID,
            "--text", text,
            "--json"
        ]
        // Discard stdout — caller refetches from DB.
        _ = try await runner.run(args: args)
    }
}
