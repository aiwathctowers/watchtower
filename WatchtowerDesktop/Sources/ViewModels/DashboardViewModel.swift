import Foundation
import GRDB

@MainActor
@Observable
final class DashboardViewModel {
    var stats = WorkspaceStats(channelCount: 0, userCount: 0, messageCount: 0, digestCount: 0)
    var workspace: Workspace?
    var recentActivity: [MessageWithContext] = []
    var dbFileSize: Int64 = 0
    var isLoading = false
    var errorMessage: String?

    private let dbManager: DatabaseManager

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func load() async {
        isLoading = true
        do {
            // H2: read into locals off main thread, then assign on MainActor
            let (ws, st, activity) = try await Task.detached { [dbManager] in
                try await dbManager.dbPool.read { db in
                    let ws = try WorkspaceQueries.fetchWorkspace(db)
                    let st = try WorkspaceQueries.fetchStats(db)
                    let oneDayAgo = Date().timeIntervalSince1970 - 86400
                    let activity = try MessageQueries.fetchRecentWatched(db, sinceUnix: oneDayAgo)
                    return (ws, st, activity)
                }
            }.value
            workspace = ws
            stats = st
            recentActivity = activity
            dbFileSize = dbManager.fileSize
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}
