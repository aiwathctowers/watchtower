import Foundation
import GRDB

@MainActor
@Observable
final class ChatHistoryViewModel {
    var conversations: [ChatConversation] = []
    var selectedConversationID: Int64?
    var searchText = ""

    private let dbManager: DatabaseManager

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    var filteredConversations: [ChatConversation] {
        let query = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        if query.isEmpty { return conversations }
        return conversations.filter { $0.title.localizedCaseInsensitiveContains(query) }
    }

    var selectedConversation: ChatConversation? {
        conversations.first { $0.id == selectedConversationID }
    }

    func load(completion: (() -> Void)? = nil) {
        Task.detached { [dbManager] in
            let items = try? await dbManager.dbPool.read { db in
                try ChatConversationQueries.fetchStandalone(db)
            }
            await MainActor.run {
                self.conversations = items ?? []
                completion?()
            }
        }
    }

    func createConversation() -> ChatConversation? {
        do {
            let conv = try dbManager.dbPool.write { db in
                try ChatConversationQueries.create(db)
            }
            conversations.insert(conv, at: 0)
            selectedConversationID = conv.id
            return conv
        } catch {
            return nil
        }
    }

    func deleteConversation(_ id: Int64) {
        do {
            try dbManager.dbPool.write { db in
                try ChatConversationQueries.delete(db, id: id)
            }
            conversations.removeAll { $0.id == id }
            if selectedConversationID == id {
                selectedConversationID = conversations.first?.id
            }
        } catch {
            // silently ignore
        }
    }

    func updateTitle(_ id: Int64, title: String) {
        let trimmed = String(title.prefix(80))
        do {
            try dbManager.dbPool.write { db in
                try ChatConversationQueries.updateTitle(db, id: id, title: trimmed)
            }
            reloadSynchronously()
        } catch {
            // silently ignore
        }
    }

    func updateSessionID(_ id: Int64, sessionID: String) {
        do {
            try dbManager.dbPool.write { db in
                try ChatConversationQueries.updateSessionID(db, id: id, sessionID: sessionID)
            }
            reloadSynchronously()
        } catch {
            // silently ignore
        }
    }

    func touch(_ id: Int64) {
        do {
            try dbManager.dbPool.write { db in
                try ChatConversationQueries.touch(db, id: id)
            }
            reloadSynchronously()
        } catch {
            // silently ignore
        }
    }

    private func reloadSynchronously() {
        let items = try? dbManager.dbPool.read { db in
            try ChatConversationQueries.fetchStandalone(db)
        }
        conversations = items ?? []
    }
}
