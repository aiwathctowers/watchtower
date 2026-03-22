import Foundation
import GRDB

@MainActor
@Observable
final class SearchViewModel {
    var query = ""
    var results: [SearchResult] = []
    var isSearching = false
    var errorMessage: String?
    private(set) var workspaceTeamID: String?

    private let dbManager: DatabaseManager
    private var searchTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
        self.workspaceTeamID = try? dbManager.dbPool.read { db in
            try String.fetchOne(db, sql: "SELECT id FROM workspace LIMIT 1")
        }
    }

    func slackChannelURL(channelID: String) -> URL? {
        guard let teamID = workspaceTeamID, !teamID.isEmpty else { return nil }
        return URL(string: "slack://channel?team=\(teamID)&id=\(channelID)")
    }

    func search() {
        searchTask?.cancel()
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            results = []
            return
        }

        searchTask = Task { [weak self] in
            guard let self else { return }
            // Debounce
            do { try await Task.sleep(for: .milliseconds(300)) } catch { return }

            self.isSearching = true
            do {
                self.results = try await dbManager.dbPool.read { db in
                    try SearchQueries.search(db, query: trimmed)
                }
                self.errorMessage = nil
            } catch {
                if !Task.isCancelled {
                    self.errorMessage = error.localizedDescription
                    self.results = []
                }
            }
            self.isSearching = false
        }
    }
}
