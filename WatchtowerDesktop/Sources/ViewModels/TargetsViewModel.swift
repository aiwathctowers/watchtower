import Foundation
import GRDB

@MainActor
@Observable
final class TargetsViewModel {
    var todayTargets: [Target] = []
    var allTargets: [Target] = []
    var activeCount: Int = 0
    var overdueCount: Int = 0
    var isLoading = false
    var errorMessage: String?

    // Filters
    var levelFilter: String?
    var statusFilter: String?
    var priorityFilter: String?
    var ownershipFilter: String?
    var showDone: Bool = false
    var searchText: String = ""

    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?
    private let cliRunnerProvider: (() -> CLIRunnerProtocol?)?

    init(
        dbManager: DatabaseManager,
        cliRunnerProvider: (() -> CLIRunnerProtocol?)? = nil
    ) {
        self.dbManager = dbManager
        self.cliRunnerProvider = cliRunnerProvider
    }

    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM targets") ?? 0
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
            let result = try dbManager.dbPool.read { db in
                let counts = try TargetQueries.fetchCounts(db)
                var filter = TargetFilter()
                filter.level = self.levelFilter
                filter.status = self.statusFilter
                filter.priority = self.priorityFilter
                filter.ownership = self.ownershipFilter
                filter.includeDone = self.showDone
                if !self.searchText.isEmpty {
                    filter.search = self.searchText
                }
                let all = try TargetQueries.fetchAll(db, filter: filter)
                return (all, counts)
            }

            let targets = result.0
            activeCount = result.1.active
            overdueCount = result.1.overdue

            // Today: overdue + due today + high priority active
            todayTargets = targets.filter { target in
                target.isActive && (target.isOverdue || target.isDueToday || target.priority == "high")
            }

            // All: everything else
            let todayIDs = Set(todayTargets.map(\.id))
            allTargets = targets.filter { !todayIDs.contains($0.id) }

            errorMessage = nil
        } catch {
            todayTargets = []
            allTargets = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func markDone(_ target: Target) {
        updateStatus(target, to: "done")
    }

    func dismiss(_ target: Target) {
        updateStatus(target, to: "dismissed")
    }

    func snooze(_ target: Target, until: Date) {
        do {
            try dbManager.dbPool.write { db in
                try TargetQueries.snooze(db, id: target.id, until: until)
            }
            load()
        } catch {
            errorMessage = "Failed to snooze: \(error.localizedDescription)"
        }
    }

    func toggleSubItem(_ target: Target, index: Int) {
        var items = target.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items[index].done.toggle()
        do {
            try dbManager.dbPool.write { db in
                try TargetQueries.updateSubItems(db, id: target.id, subItems: items)
            }
            load()
        } catch {
            errorMessage = "Failed to update sub-items: \(error.localizedDescription)"
        }
    }

    func updateText(_ target: Target, to text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET text = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [trimmed, target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update text: \(error.localizedDescription)"
        }
    }

    func updateIntent(_ target: Target, to intent: String) {
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET intent = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [intent.trimmingCharacters(in: .whitespacesAndNewlines), target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update intent: \(error.localizedDescription)"
        }
    }

    func updateDueDate(_ target: Target, to dueDate: String) {
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET due_date = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [dueDate, target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update due date: \(error.localizedDescription)"
        }
    }

    func updateOwnership(_ target: Target, to ownership: String) {
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET ownership = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [ownership, target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update ownership: \(error.localizedDescription)"
        }
    }

    func updateBlocking(_ target: Target, to blocking: String) {
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET blocking = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [blocking.trimmingCharacters(in: .whitespacesAndNewlines), target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update blocking: \(error.localizedDescription)"
        }
    }

    func updateBallOn(_ target: Target, to ballOn: String) {
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET ball_on = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [ballOn.trimmingCharacters(in: .whitespacesAndNewlines), target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update ball on: \(error.localizedDescription)"
        }
    }

    func addSubItem(_ target: Target, text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        var items = target.decodedSubItems
        items.append(TargetSubItem(text: trimmed, done: false))
        saveSubItems(target, items: items)
    }

    func removeSubItem(_ target: Target, index: Int) {
        var items = target.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items.remove(at: index)
        saveSubItems(target, items: items)
    }

    func editSubItem(_ target: Target, index: Int, newText: String) {
        let trimmed = newText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        var items = target.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items[index].text = trimmed
        saveSubItems(target, items: items)
    }

    func moveSubItem(_ target: Target, from source: IndexSet, to destination: Int) {
        var items = target.decodedSubItems
        items.move(fromOffsets: source, toOffset: destination)
        saveSubItems(target, items: items)
    }

    private func saveSubItems(_ target: Target, items: [TargetSubItem]) {
        do {
            try dbManager.dbPool.write { db in
                try TargetQueries.updateSubItems(db, id: target.id, subItems: items)
            }
            load()
        } catch {
            errorMessage = "Failed to update sub-items: \(error.localizedDescription)"
        }
    }

    func replaceSubItems(_ target: Target, items: [TargetSubItem]) {
        saveSubItems(target, items: items)
    }

    func updateSubItemDueDate(_ target: Target, index: Int, dueDate: String?) {
        var items = target.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items[index].dueDate = dueDate
        saveSubItems(target, items: items)
    }

    // MARK: - Notes

    private static let iso8601Formatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    func addNote(_ target: Target, text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        var notes = target.decodedNotes
        let now = Self.iso8601Formatter.string(from: Date())
        notes.append(TargetNote(text: trimmed, createdAt: now))
        saveNotes(target, notes: notes)
    }

    func removeNote(_ target: Target, index: Int) {
        var notes = target.decodedNotes
        guard index >= 0, index < notes.count else { return }
        notes.remove(at: index)
        saveNotes(target, notes: notes)
    }

    private func saveNotes(_ target: Target, notes: [TargetNote]) {
        guard let data = try? JSONEncoder().encode(notes),
              let json = String(data: data, encoding: .utf8) else { return }
        do {
            try dbManager.dbPool.write { db in
                try db.execute(
                    sql: "UPDATE targets SET notes = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?",
                    arguments: [json, target.id]
                )
            }
            load()
        } catch {
            errorMessage = "Failed to update notes: \(error.localizedDescription)"
        }
    }

    func updatePriority(_ target: Target, to priority: String) {
        do {
            try dbManager.dbPool.write { db in
                try TargetQueries.updatePriority(db, id: target.id, priority: priority)
            }
            load()
        } catch {
            errorMessage = "Failed to update priority: \(error.localizedDescription)"
        }
    }

    func updateStatus(_ target: Target, to status: String) {
        do {
            try dbManager.dbPool.write { db in
                try TargetQueries.updateStatus(db, id: target.id, status: status)
            }
            load()
        } catch {
            errorMessage = "Failed to update status: \(error.localizedDescription)"
        }
    }

    func deleteTarget(_ target: Target) {
        do {
            try dbManager.dbPool.write { db in
                try TargetQueries.delete(db, id: target.id)
            }
            load()
        } catch {
            errorMessage = "Failed to delete: \(error.localizedDescription)"
        }
    }

    func fetchJiraIssue(key: String) -> JiraIssue? {
        guard !key.isEmpty else { return nil }
        return try? dbManager.dbPool.read { db in
            try JiraQueries.fetchIssueByKey(db, key: key)
        }
    }

    func itemByID(_ id: Int) -> Target? {
        do {
            return try dbManager.dbPool.read { db in
                try TargetQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    func fetchLinks(for targetID: Int) -> [TargetLink] {
        do {
            return try dbManager.dbPool.read { db in
                try TargetQueries.fetchLinks(db, targetID: targetID, direction: .both)
            }
        } catch {
            return []
        }
    }

    func submitFeedback(targetID: Int, rating: Int) {
        do {
            try dbManager.dbPool.write { db in
                try FeedbackQueries.addFeedback(
                    db,
                    entityType: "target",
                    entityID: "\(targetID)",
                    rating: rating,
                    comment: ""
                )
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    // MARK: - Promote sub-item to child target

    /// Resolves a CLIRunner via the injected provider or, in production, by
    /// locating the bundled `watchtower` binary.
    private func resolveCLIRunner() throws -> CLIRunnerProtocol {
        if let runner = cliRunnerProvider?() {
            return runner
        }
        if let runner = ProcessCLIRunner.makeDefault() {
            return runner
        }
        throw PromoteSubItemViewModelError.cliNotFound
    }

    /// Converts the sub-item at `index` of `target` into a standalone child
    /// target with `parent_id = target.id`. Returns the new child's ID.
    @discardableResult
    func promoteSubItem(
        _ target: Target,
        index: Int,
        overrides: PromoteSubItemOverrides = PromoteSubItemOverrides()
    ) async throws -> Int {
        let runner: CLIRunnerProtocol
        do {
            runner = try resolveCLIRunner()
        } catch {
            errorMessage = error.localizedDescription
            throw error
        }
        let svc = TargetPromoteSubItemService(runner: runner)
        do {
            let result = try await svc.promote(
                parentID: target.id,
                index: index,
                overrides: overrides
            )
            load()
            return result.id
        } catch {
            errorMessage = "Failed to promote sub-item: \(error.localizedDescription)"
            throw error
        }
    }

    /// Batch-promote sub-items of a freshly created parent. Iterates in
    /// descending `index` order so removals from `sub_items` on the Go side
    /// do not invalidate the indices that still need to be promoted.
    func promoteSubItemsAfterCreate(
        parentID: Int,
        items: [(index: Int, overrides: PromoteSubItemOverrides)]
    ) async throws {
        guard !items.isEmpty else { return }
        let runner: CLIRunnerProtocol
        do {
            runner = try resolveCLIRunner()
        } catch {
            errorMessage = error.localizedDescription
            throw error
        }
        let svc = TargetPromoteSubItemService(runner: runner)
        let sorted = items.sorted { $0.index > $1.index }
        do {
            for item in sorted {
                _ = try await svc.promote(
                    parentID: parentID,
                    index: item.index,
                    overrides: item.overrides
                )
            }
            load()
        } catch {
            errorMessage = "Failed to promote sub-items: \(error.localizedDescription)"
            throw error
        }
    }
}

/// Errors emitted by promote-related ViewModel methods.
enum PromoteSubItemViewModelError: LocalizedError {
    case cliNotFound

    var errorDescription: String? {
        switch self {
        case .cliNotFound:
            return "watchtower CLI not found in PATH"
        }
    }
}
