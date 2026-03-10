import SwiftUI
import GRDB

@MainActor
@Observable
final class AppState {
    var selectedDestination: SidebarDestination = .chat
    var databaseManager: DatabaseManager?
    var errorMessage: String?
    var isDBAvailable: Bool { databaseManager != nil }

    /// Cache for custom workspace emoji images.
    let emojiImageCache = EmojiImageCache()
    /// Map of custom emoji name → image URL, loaded from DB.
    var customEmojiMap: [String: String] = [:]

    /// Action items status filter (nil = inbox+active).
    var actionStatusFilter: String?

    /// Set to navigate to a specific channel from anywhere in the app.
    var pendingChannelID: String?

    /// Set to navigate to a specific digest from anywhere in the app.
    var pendingDigestID: Int?

    func navigateToChannel(_ channelID: String) {
        pendingChannelID = channelID
        selectedDestination = .channels
    }

    func navigateToDigest(_ digestID: Int) {
        pendingDigestID = digestID
        selectedDestination = .digests
    }

    func initialize() {
        Task {
            do {
                let manager = try await Task.detached {
                    let dbPath = try DatabaseManager.resolveDBPath()
                    return try DatabaseManager(path: dbPath)
                }.value
                databaseManager = manager
                errorMessage = nil
                loadCustomEmoji(from: manager)
            } catch {
                errorMessage = error.localizedDescription
                databaseManager = nil
            }
        }
    }

    private func loadCustomEmoji(from manager: DatabaseManager) {
        Task.detached {
            let map = try? await manager.dbPool.read { db in
                try CustomEmojiQueries.fetchEmojiMap(db)
            }
            await MainActor.run {
                self.customEmojiMap = map ?? [:]
            }
        }
    }
}
