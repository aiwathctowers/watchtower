import Foundation

struct SyncProgressData: Decodable {
    let phase: String
    let elapsedSec: Double
    let usersTotal: Int
    let usersDone: Int
    let channelsTotal: Int
    let channelsDone: Int
    let discoveryPages: Int
    let discoveryTotalPages: Int
    let discoveryChannels: Int
    let discoveryUsers: Int
    let userProfilesTotal: Int
    let userProfilesDone: Int
    let msgChannelsTotal: Int
    let msgChannelsDone: Int
    let messagesFetched: Int
    let threadsTotal: Int?
    let threadsDone: Int?
    let threadsFetched: Int?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case phase
        case elapsedSec = "elapsed_sec"
        case usersTotal = "users_total"
        case usersDone = "users_done"
        case channelsTotal = "channels_total"
        case channelsDone = "channels_done"
        case discoveryPages = "discovery_pages"
        case discoveryTotalPages = "discovery_total_pages"
        case discoveryChannels = "discovery_channels"
        case discoveryUsers = "discovery_users"
        case userProfilesTotal = "user_profiles_total"
        case userProfilesDone = "user_profiles_done"
        case msgChannelsTotal = "msg_channels_total"
        case msgChannelsDone = "msg_channels_done"
        case messagesFetched = "messages_fetched"
        case threadsTotal = "threads_total"
        case threadsDone = "threads_done"
        case threadsFetched = "threads_fetched"
        case error
    }
}
