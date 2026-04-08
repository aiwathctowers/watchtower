import Foundation
import GRDB

@MainActor
@Observable
final class TasksViewModel {
    var todayTasks: [TaskItem] = []
    var allTasks: [TaskItem] = []
    var activeCount: Int = 0
    var overdueCount: Int = 0
    var isLoading = false
    var errorMessage: String?

    // Filters
    var statusFilter: String?
    var priorityFilter: String?
    var ownershipFilter: String?
    var showDone: Bool = false
    var sourceFilter: SourceFilter = .all

    enum SourceFilter: String, CaseIterable {
        case all = "All"
        case jira = "Jira"
        case slack = "Slack"
        case manual = "Manual"

        func matches(_ sourceType: String) -> Bool {
            switch self {
            case .all: return true
            case .jira: return sourceType == "jira"
            case .slack:
                return ["track", "digest", "briefing", "chat", "inbox"]
                    .contains(sourceType)
            case .manual: return sourceType == "manual"
            }
        }
    }

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
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM tasks") ?? 0
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
                let counts = try TaskQueries.fetchCounts(db)
                let all = try TaskQueries.fetchAll(
                    db,
                    status: self.statusFilter,
                    priority: self.priorityFilter,
                    ownership: self.ownershipFilter,
                    includeDone: self.showDone
                )
                return (all, counts)
            }

            let tasks = result.0
            activeCount = result.1.active
            overdueCount = result.1.overdue

            // Apply source filter
            let filtered = sourceFilter == .all
                ? tasks
                : tasks.filter { sourceFilter.matches($0.sourceType) }

            // Today: overdue + due today + high priority active
            todayTasks = filtered.filter { task in
                task.isActive && (task.isOverdue || task.isDueToday || task.priority == "high")
            }

            // All: everything else (excluding what's in today)
            let todayIDs = Set(todayTasks.map(\.id))
            allTasks = filtered.filter { !todayIDs.contains($0.id) }

            errorMessage = nil
        } catch {
            todayTasks = []
            allTasks = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func markDone(_ task: TaskItem) {
        updateStatus(task, to: "done")
    }

    func dismiss(_ task: TaskItem) {
        updateStatus(task, to: "dismissed")
    }

    func snooze(_ task: TaskItem, until: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.snooze(db, id: task.id, until: until)
            }
            load()
        } catch {
            errorMessage = "Failed to snooze: \(error.localizedDescription)"
        }
    }

    func toggleSubItem(_ task: TaskItem, index: Int) {
        var items = task.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items[index].done.toggle()
        guard let data = try? JSONEncoder().encode(items),
              let json = String(data: data, encoding: .utf8) else { return }
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateSubItems(db, id: task.id, subItems: json)
            }
            load()
        } catch {
            errorMessage = "Failed to update sub-items: \(error.localizedDescription)"
        }
    }

    func updateText(_ task: TaskItem, to text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateText(db, id: task.id, text: trimmed)
            }
            load()
        } catch {
            errorMessage = "Failed to update text: \(error.localizedDescription)"
        }
    }

    func updateIntent(_ task: TaskItem, to intent: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateIntent(db, id: task.id, intent: intent.trimmingCharacters(in: .whitespacesAndNewlines))
            }
            load()
        } catch {
            errorMessage = "Failed to update intent: \(error.localizedDescription)"
        }
    }

    func updateDueDate(_ task: TaskItem, to dueDate: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateDueDate(db, id: task.id, dueDate: dueDate)
            }
            load()
        } catch {
            errorMessage = "Failed to update due date: \(error.localizedDescription)"
        }
    }

    func updateOwnership(_ task: TaskItem, to ownership: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateOwnership(db, id: task.id, ownership: ownership)
            }
            load()
        } catch {
            errorMessage = "Failed to update ownership: \(error.localizedDescription)"
        }
    }

    func updateBlocking(_ task: TaskItem, to blocking: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateBlocking(db, id: task.id, blocking: blocking.trimmingCharacters(in: .whitespacesAndNewlines))
            }
            load()
        } catch {
            errorMessage = "Failed to update blocking: \(error.localizedDescription)"
        }
    }

    func updateBallOn(_ task: TaskItem, to ballOn: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateBallOn(db, id: task.id, ballOn: ballOn.trimmingCharacters(in: .whitespacesAndNewlines))
            }
            load()
        } catch {
            errorMessage = "Failed to update ball on: \(error.localizedDescription)"
        }
    }

    func addSubItem(_ task: TaskItem, text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        var items = task.decodedSubItems
        items.append(TaskSubItem(text: trimmed, done: false))
        saveSubItems(task, items: items)
    }

    func removeSubItem(_ task: TaskItem, index: Int) {
        var items = task.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items.remove(at: index)
        saveSubItems(task, items: items)
    }

    func editSubItem(_ task: TaskItem, index: Int, newText: String) {
        let trimmed = newText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        var items = task.decodedSubItems
        guard index >= 0, index < items.count else { return }
        items[index].text = trimmed
        saveSubItems(task, items: items)
    }

    private func saveSubItems(_ task: TaskItem, items: [TaskSubItem]) {
        guard let data = try? JSONEncoder().encode(items),
              let json = String(data: data, encoding: .utf8) else { return }
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateSubItems(db, id: task.id, subItems: json)
            }
            load()
        } catch {
            errorMessage = "Failed to update sub-items: \(error.localizedDescription)"
        }
    }

    func updatePriority(_ task: TaskItem, to priority: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updatePriority(db, id: task.id, priority: priority)
            }
            load()
        } catch {
            errorMessage = "Failed to update priority: \(error.localizedDescription)"
        }
    }

    func updateStatus(_ task: TaskItem, to status: String) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.updateStatus(db, id: task.id, status: status)
            }
            load()
        } catch {
            errorMessage = "Failed to update status: \(error.localizedDescription)"
        }
    }

    func deleteTask(_ task: TaskItem) {
        do {
            try dbManager.dbPool.write { db in
                try TaskQueries.delete(db, id: task.id)
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

    func itemByID(_ id: Int) -> TaskItem? {
        do {
            return try dbManager.dbPool.read { db in
                try TaskQueries.fetchByID(db, id: id)
            }
        } catch {
            return nil
        }
    }

    func submitFeedback(taskID: Int, rating: Int) {
        do {
            try dbManager.dbPool.write { db in
                try FeedbackQueries.addFeedback(
                    db,
                    entityType: "task",
                    entityID: "\(taskID)",
                    rating: rating,
                    comment: ""
                )
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
