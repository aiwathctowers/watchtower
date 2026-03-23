import Foundation
import GRDB

@MainActor
@Observable
final class SearchViewModel {
    var query = ""
    var results: [SearchResult] = []
    var isSearching = false
    var errorMessage: String?

    private let dbManager: DatabaseManager
    private var searchTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func search() {
        searchTask?.cancel()
        let q = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !q.isEmpty else {
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
                    try SearchQueries.search(db, query: q)
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
