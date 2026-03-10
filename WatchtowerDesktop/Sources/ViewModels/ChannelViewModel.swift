import Foundation
import GRDB

@MainActor
@Observable
final class ChannelViewModel {
    var channels: [Channel] = []
    var filterType: String?
    var searchText = ""
    var isLoading = false
    var watchedIDs: Set<String> = []
    var errorMessage: String?

    /// Display names for channels (resolves DM user IDs to real names)
    private var displayNames: [String: String] = [:]

    private let dbManager: DatabaseManager

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func load() {
        isLoading = true
        do {
            let (ch, watched, names) = try dbManager.dbPool.read { db in
                let ch = try ChannelQueries.fetchAll(db, type: filterType)
                let watched = try WatchQueries.fetchAll(db)

                // Resolve DM/group_dm channel names to user display names
                var names: [String: String] = [:]
                for c in ch where c.type == "dm" || c.type == "im" {
                    // Try dm_user_id first, fall back to name (which is often the user ID)
                    let uid = c.dmUserID ?? c.name
                    if let user = try UserQueries.fetchByID(db, id: uid) {
                        names[c.id] = user.bestName
                    }
                }
                for c in ch where c.type == "group_dm" {
                    // Group DMs have names like "mpdm-user1--user2--user3-1"
                    // Extract user slugs and resolve to display names
                    let slug = c.name
                        .replacingOccurrences(of: "mpdm-", with: "")
                        .replacingOccurrences(of: "-1", with: "")
                    let parts = slug.components(separatedBy: "--")
                    let resolved = parts.map { part in
                        if let user = try? UserQueries.fetchByName(db, name: part) {
                            return user.bestName
                        }
                        return part
                    }
                    names[c.id] = resolved.joined(separator: ", ")
                }
                return (ch, watched, names)
            }
            channels = ch
            displayNames = names
            watchedIDs = Set(watched.filter { $0.entityType == "channel" }.map(\.entityID))
            errorMessage = nil
        } catch {
            channels = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    /// Returns a human-readable name for the channel
    func displayName(for channel: Channel) -> String {
        if let resolved = displayNames[channel.id] {
            return resolved
        }
        return channel.name
    }

    var filteredChannels: [Channel] {
        if searchText.isEmpty { return channels }
        return channels.filter {
            let name = displayName(for: $0)
            return name.localizedCaseInsensitiveContains(searchText)
                || $0.name.localizedCaseInsensitiveContains(searchText)
        }
    }

    func toggleWatch(channel: Channel) {
        // M6: mutate state only after successful DB write
        let shouldRemove = watchedIDs.contains(channel.id)
        do {
            try dbManager.dbPool.write { db in
                if shouldRemove {
                    try WatchQueries.remove(db, entityType: "channel", entityID: channel.id)
                } else {
                    try WatchQueries.add(db, entityType: "channel", entityID: channel.id, entityName: channel.name)
                }
            }
            if shouldRemove {
                watchedIDs.remove(channel.id)
            } else {
                watchedIDs.insert(channel.id)
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
