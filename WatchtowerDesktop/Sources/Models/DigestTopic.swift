import GRDB
import Foundation

/// A self-contained thematic unit within a digest.
/// Each topic carries its own decisions, action items, situations, and key messages.
struct DigestTopic: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let digestID: Int
    let idx: Int
    let title: String
    let summary: String
    let decisions: String
    let actionItems: String
    let situations: String
    let keyMessages: String

    enum CodingKeys: String, CodingKey {
        case id, idx, title, summary, decisions, situations
        case digestID = "digest_id"
        case actionItems = "action_items"
        case keyMessages = "key_messages"
    }

    private static let decoder = JSONDecoder()

    var parsedDecisions: [Decision] {
        guard let data = decisions.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([Decision].self, from: data)) ?? []
    }

    var parsedActionItems: [DigestTrack] {
        guard let data = actionItems.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([DigestTrack].self, from: data)) ?? []
    }

    var parsedKeyMessages: [String] {
        guard let data = keyMessages.data(using: .utf8) else { return [] }
        return (try? Self.decoder.decode([String].self, from: data)) ?? []
    }
}
