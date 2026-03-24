import Foundation
import GRDB

@MainActor
@Observable
final class BriefingViewModel {
    var briefings: [Briefing] = []
    var isLoading = false
    var errorMessage: String?
    var unreadCount: Int = 0

    private(set) var hasMore = true
    private var offset = 0
    var isLoadingMore = false
    private let pageSize = 30

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM briefings") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db -> ([Briefing], Int) in
                let items = try BriefingQueries.fetchRecent(db, limit: pageSize)
                let unread = try BriefingQueries.unreadCount(db)
                return (items, unread)
            }
            briefings = result.0
            unreadCount = result.1
            offset = result.0.count
            hasMore = result.0.count >= pageSize
            errorMessage = nil

        } catch {
            briefings = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func loadMore() {
        guard hasMore, !isLoadingMore else { return }
        isLoadingMore = true
        do {
            let batch = try dbManager.dbPool.read { db in
                try BriefingQueries.fetchRecent(db, limit: pageSize, offset: offset)
            }
            briefings.append(contentsOf: batch)
            offset += batch.count
            hasMore = batch.count >= pageSize
        } catch {
            print("Failed to load more briefings: \(error)")
        }
        isLoadingMore = false
    }

    func markAsRead(_ briefingID: Int) {
        do {
            try dbManager.dbPool.write { db in
                try BriefingQueries.markRead(db, id: briefingID)
            }
            if let idx = briefings.firstIndex(where: { $0.id == briefingID && !$0.isRead }) {
                unreadCount = max(0, unreadCount - 1)
                if let updated = briefingByID(briefingID) {
                    briefings[idx] = updated
                }
            }
        } catch {
            print("Failed to mark briefing read: \(error)")
        }
    }

    private func briefingByID(_ id: Int) -> Briefing? {
        do {
            return try dbManager.dbPool.read { db in
                try BriefingQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }
}
