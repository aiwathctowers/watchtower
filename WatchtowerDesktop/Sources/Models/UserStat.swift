import Foundation
import GRDB

struct UserStat: Identifiable, FetchableRecord, Decodable {
    let id: String
    let name: String
    let displayName: String
    let realName: String
    let email: String
    let isBot: Bool
    let isDeleted: Bool
    let isBotOverride: Bool?
    let isMutedForLLM: Bool
    let totalMessages: Int
    let channelCount: Int
    let threadReplies: Int
    let lastActivity: Double
    let updatedAt: String

    enum CodingKeys: String, CodingKey {
        case id
        case name
        case displayName = "display_name"
        case realName = "real_name"
        case email
        case isBot = "is_bot"
        case isDeleted = "is_deleted"
        case isBotOverride = "is_bot_override"
        case isMutedForLLM = "is_muted_for_llm"
        case totalMessages = "total_messages"
        case channelCount = "channel_count"
        case threadReplies = "thread_replies"
        case lastActivity = "last_activity"
        case updatedAt = "updated_at"
    }

    var bestName: String {
        if !displayName.isEmpty { return displayName }
        if !realName.isEmpty { return realName }
        if !name.isEmpty { return name }
        return id
    }

    /// Effective bot status considering manual override.
    var effectiveIsBot: Bool {
        isBotOverride ?? isBot
    }

    var lastActivityDaysAgo: Int? {
        guard lastActivity > 0 else { return nil }
        let age = Date().timeIntervalSince1970 - lastActivity
        return max(0, Int(age / 86400))
    }
}
