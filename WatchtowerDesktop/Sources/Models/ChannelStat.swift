import Foundation
import GRDB
import SwiftUI

struct ChannelStat: Identifiable, FetchableRecord, Decodable {
    let id: String
    let name: String
    let type: String
    let isArchived: Bool
    let isMember: Bool
    let numMembers: Int
    let totalMessages: Int
    let userMessages: Int
    let botMessages: Int
    let botRatio: Double
    let mentionCount: Int
    let lastActivity: Double
    let lastUserActivity: Double
    let isMutedForLLM: Bool
    let isFavorite: Bool
    let isWatched: Bool
    // Digest processing info
    let digestCount: Int
    let lastDigestAt: String?
    let messagesSinceDigest: Int

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case type
        case isArchived = "is_archived"
        case isMember = "is_member"
        case numMembers = "num_members"
        case totalMessages = "total_messages"
        case userMessages = "user_messages"
        case botMessages = "bot_messages"
        case botRatio = "bot_ratio"
        case mentionCount = "mention_count"
        case lastActivity = "last_activity"
        case lastUserActivity = "last_user_activity"
        case isMutedForLLM = "is_muted_for_llm"
        case isFavorite = "is_favorite"
        case isWatched = "is_watched"
        case digestCount = "digest_count"
        case lastDigestAt = "last_digest_at"
        case messagesSinceDigest = "messages_since_digest"
    }

    /// Days since last activity in this channel (any message), or nil if no messages.
    var lastActivityDaysAgo: Int? {
        guard lastActivity > 0 else { return nil }
        let age = Date().timeIntervalSince1970 - lastActivity
        return max(0, Int(age / 86400))
    }

    /// Whether this channel has ever been processed by the digest pipeline.
    var hasDigests: Bool { digestCount > 0 }

    /// Digest processing status for display.
    var digestStatus: DigestProcessingStatus {
        if isMutedForLLM { return .muted }
        if digestCount == 0 && totalMessages < 5 { return .tooFewMessages }
        if digestCount == 0 { return .neverProcessed }
        if messagesSinceDigest > 0 { return .pending(messagesSinceDigest) }
        return .upToDate
    }
}

enum DigestProcessingStatus {
    case upToDate
    case pending(Int)
    case neverProcessed
    case tooFewMessages
    case muted

    var label: String {
        switch self {
        case .upToDate: "Up to date"
        case .pending(let n): "\(n) pending"
        case .neverProcessed: "Not processed"
        case .tooFewMessages: "Too few msgs"
        case .muted: "Muted"
        }
    }

    var icon: String {
        switch self {
        case .upToDate: "checkmark.circle.fill"
        case .pending: "clock.fill"
        case .neverProcessed: "minus.circle"
        case .tooFewMessages: "xmark.circle"
        case .muted: "speaker.slash.fill"
        }
    }

    var color: Color {
        switch self {
        case .upToDate: .green
        case .pending: .orange
        case .neverProcessed: .secondary
        case .tooFewMessages: .secondary
        case .muted: .orange
        }
    }
}

struct ChannelValueSignals: FetchableRecord, Decodable {
    let channelID: String
    let decisionCount: Int
    let activeTrackCount: Int
    let taskCount: Int
    let pendingInboxCount: Int

    enum CodingKeys: String, CodingKey {
        case channelID = "channel_id"
        case decisionCount = "decision_count"
        case activeTrackCount = "active_track_count"
        case taskCount = "task_count"
        case pendingInboxCount = "pending_inbox_count"
    }
}
