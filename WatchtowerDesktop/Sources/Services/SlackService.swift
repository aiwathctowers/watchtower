import Foundation
import Yams

enum SlackService {
    enum SlackError: LocalizedError {
        case noToken
        case apiError(String)
        case networkError(Error)

        var errorDescription: String? {
            switch self {
            case .noToken: "Slack token not found in config"
            case .apiError(let msg): "Slack API: \(msg)"
            case .networkError(let err): err.localizedDescription
            }
        }
    }

    /// Mark a channel as read up to the given timestamp.
    /// If `ts` is nil, marks the channel as fully read using current time.
    static func markRead(channelID: String, ts: String? = nil) async throws {
        let token = try readToken()
        let markTS = ts ?? String(format: "%.6f", Date().timeIntervalSince1970)

        guard let url = URL(string: "https://slack.com/api/conversations.mark") else { return }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json; charset=utf-8", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.httpBody = try JSONEncoder().encode([
            "channel": channelID,
            "ts": markTS
        ])

        let (data, _) = try await URLSession.shared.data(for: request)
        let resp = try JSONDecoder().decode(SlackResponse.self, from: data)
        if !resp.ok {
            // M14: conversations.mark requires channels:write/groups:write/im:write/mpim:write scopes
            let error = resp.error ?? "unknown"
            if error == "missing_scope" {
                let msg = "missing_scope — conversations.mark requires write scopes "
                    + "(channels:write, etc.) which may not be granted to this token"
                throw SlackError.apiError(msg)
            }
            throw SlackError.apiError(error)
        }
    }

    private struct SlackResponse: Decodable {
        let ok: Bool
        let error: String?
    }

    private static func readToken() throws -> String {
        let configPath = Constants.configPath
        guard let data = FileManager.default.contents(atPath: configPath),
              let str = String(data: data, encoding: .utf8),
              let yaml = try Yams.load(yaml: str) as? [String: Any] else {
            throw SlackError.noToken
        }

        let workspace = yaml["active_workspace"] as? String ?? ""
        if let workspaces = yaml["workspaces"] as? [String: Any],
           let ws = workspaces[workspace] as? [String: Any],
           let token = ws["slack_token"] as? String, !token.isEmpty {
            return token
        }

        throw SlackError.noToken
    }
}
