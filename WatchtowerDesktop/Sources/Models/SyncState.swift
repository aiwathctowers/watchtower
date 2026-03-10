import GRDB

struct SyncState: FetchableRecord, Decodable, Identifiable, Equatable {
    let channelID: String
    let lastSyncedTS: String
    let oldestSyncedTS: String
    let isInitialSyncComplete: Bool
    let cursor: String
    let messagesSynced: Int
    let lastSyncAt: String?
    let error: String

    var id: String { channelID }

    enum CodingKeys: String, CodingKey {
        case cursor, error
        case channelID = "channel_id"
        case lastSyncedTS = "last_synced_ts"
        case oldestSyncedTS = "oldest_synced_ts"
        case isInitialSyncComplete = "is_initial_sync_complete"
        case messagesSynced = "messages_synced"
        case lastSyncAt = "last_sync_at"
    }
}
