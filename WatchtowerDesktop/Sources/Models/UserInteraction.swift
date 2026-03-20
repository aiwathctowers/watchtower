import GRDB
import Foundation

/// Interaction metrics between two users for a time window (social graph edge).
struct UserInteraction: FetchableRecord, Decodable, Identifiable, Equatable {
    let userA: String
    let userB: String
    let periodFrom: Double
    let periodTo: Double
    let messagesTo: Int
    let messagesFrom: Int
    let sharedChannels: Int
    let threadRepliesTo: Int
    let threadRepliesFrom: Int
    let sharedChannelIDs: String
    let dmMessagesTo: Int
    let dmMessagesFrom: Int
    let mentionsTo: Int
    let mentionsFrom: Int
    let reactionsTo: Int
    let reactionsFrom: Int
    let interactionScore: Double
    let connectionType: String

    var id: String { "\(userA)-\(userB)-\(periodFrom)" }

    enum CodingKeys: String, CodingKey {
        case userA = "user_a"
        case userB = "user_b"
        case periodFrom = "period_from"
        case periodTo = "period_to"
        case messagesTo = "messages_to"
        case messagesFrom = "messages_from"
        case sharedChannels = "shared_channels"
        case threadRepliesTo = "thread_replies_to"
        case threadRepliesFrom = "thread_replies_from"
        case sharedChannelIDs = "shared_channel_ids"
        case dmMessagesTo = "dm_messages_to"
        case dmMessagesFrom = "dm_messages_from"
        case mentionsTo = "mentions_to"
        case mentionsFrom = "mentions_from"
        case reactionsTo = "reactions_to"
        case reactionsFrom = "reactions_from"
        case interactionScore = "interaction_score"
        case connectionType = "connection_type"
    }

    /// Total channel messages (both directions, excluding DMs).
    var totalMessages: Int {
        messagesTo + messagesFrom
    }

    /// Total DM messages (both directions).
    var totalDMs: Int {
        dmMessagesTo + dmMessagesFrom
    }

    /// Total thread replies (both directions).
    var totalThreadReplies: Int {
        threadRepliesTo + threadRepliesFrom
    }

    /// Total @-mentions (both directions).
    var totalMentions: Int {
        mentionsTo + mentionsFrom
    }

    /// Total reactions (both directions).
    var totalReactions: Int {
        reactionsTo + reactionsFrom
    }

    /// Connection type display label.
    var connectionTypeLabel: String {
        switch connectionType {
        case "peer": return "Peer"
        case "i_depend": return "I depend on"
        case "depends_on_me": return "Depends on me"
        case "weak": return "Weak signal"
        default: return connectionType
        }
    }

    /// Parsed shared channel IDs from JSON.
    var parsedSharedChannelIDs: [String] {
        guard let data = sharedChannelIDs.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }
}
