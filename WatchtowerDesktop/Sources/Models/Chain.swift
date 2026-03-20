import Foundation
import GRDB

/// A thematic chain grouping related decisions, digests, and tracks over time across channels.
struct Chain: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let parentID: Int?       // nil if top-level, otherwise parent chain ID
    let title: String
    let slug: String
    let status: String       // "active", "resolved", "stale"
    let summary: String
    let channelIDs: String   // JSON array of channel IDs
    let firstSeen: Double    // Unix timestamp
    let lastSeen: Double     // Unix timestamp
    let itemCount: Int
    let readAt: String?      // nil if unread
    let createdAt: String
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id, title, slug, status, summary
        case parentID = "parent_id"
        case channelIDs = "channel_ids"
        case firstSeen = "first_seen"
        case lastSeen = "last_seen"
        case itemCount = "item_count"
        case readAt = "read_at"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var isActive: Bool { status == "active" }
    var isResolved: Bool { status == "resolved" }
    var isStale: Bool { status == "stale" }
    var isRead: Bool { readAt != nil }
    var isParent: Bool { parentID == nil || parentID == 0 }

    var decodedChannelIDs: [String] {
        guard let data = channelIDs.data(using: .utf8),
              let ids = try? JSONDecoder().decode([String].self, from: data) else {
            return []
        }
        return ids
    }

    var firstSeenDate: Date { Date(timeIntervalSince1970: firstSeen) }
    var lastSeenDate: Date { Date(timeIntervalSince1970: lastSeen) }
}

/// A reference linking a chain to a decision (in a digest), a track, or a digest itself.
struct ChainRef: FetchableRecord, Decodable, Identifiable, Equatable {
    let id: Int
    let chainID: Int
    let refType: String      // "decision", "track", "digest"
    let digestID: Int
    let decisionIdx: Int
    let trackID: Int
    let channelID: String
    let timestamp: Double    // Unix timestamp
    let createdAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case chainID = "chain_id"
        case refType = "ref_type"
        case digestID = "digest_id"
        case decisionIdx = "decision_idx"
        case trackID = "track_id"
        case channelID = "channel_id"
        case timestamp
        case createdAt = "created_at"
    }

    var isDecision: Bool { refType == "decision" }
    var isTrack: Bool { refType == "track" }
    var isDigest: Bool { refType == "digest" }
    var timestampDate: Date { Date(timeIntervalSince1970: timestamp) }
}
