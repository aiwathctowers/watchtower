import SwiftUI
import GRDB

// MARK: - ViewModel

@MainActor
@Observable
final class ActionItemChatViewModel {
    var messages: [ChatMessage] = []
    var isStreaming = false
    var inputText = ""
    var errorMessage: String?

    private var conversationID: Int64?
    private var sessionID: String?
    private let claudeService: ClaudeService
    private let dbManager: DatabaseManager
    private let item: ActionItem
    private let viewModel: ActionItemsViewModel
    private var streamTask: Task<Void, Never>?

    init(item: ActionItem, viewModel: ActionItemsViewModel, dbManager: DatabaseManager) {
        self.item = item
        self.viewModel = viewModel
        self.dbManager = dbManager
        self.claudeService = ClaudeService()

        loadOrCreateConversation()
    }

    private func loadOrCreateConversation() {
        do {
            let (conv, records) = try dbManager.dbPool.write { db -> (ChatConversation, [ChatMessageRecord]) in
                if let existing = try ChatConversationQueries.fetchByContext(db, type: "action_item", id: String(item.id)) {
                    let msgs = try ChatMessageQueries.fetchByConversation(db, conversationID: existing.id)
                    return (existing, msgs)
                }
                let conv = try ChatConversationQueries.create(
                    db,
                    title: "Action: \(String(item.text.prefix(60)))",
                    contextType: "action_item",
                    contextID: String(item.id)
                )
                return (conv, [])
            }
            conversationID = conv.id
            sessionID = conv.sessionID
            messages = records.map { $0.toChatMessage() }
        } catch {
            // silently ignore
        }
    }

    func send() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isStreaming else { return }

        streamTask?.cancel()
        inputText = ""

        messages.append(ChatMessage(id: UUID(), role: .user, text: text, timestamp: Date(), isStreaming: false))

        // Persist user message
        if let convID = conversationID {
            persistMessage(conversationID: convID, role: "user", text: text)
        }

        messages.append(ChatMessage(id: UUID(), role: .assistant, text: "", timestamp: Date(), isStreaming: true))
        isStreaming = true

        let currentSessionID = sessionID
        let dbPath = dbManager.dbPool.path
        let dbPool = dbManager.dbPool

        streamTask = Task { [weak self] in
            guard let self else { return }

            let systemPrompt: String? = if currentSessionID == nil {
                Self.buildSystemPrompt(item: self.item, dbPool: dbPool)
            } else {
                nil
            }

            do {
                let stream = claudeService.stream(
                    prompt: text,
                    systemPrompt: systemPrompt,
                    sessionID: currentSessionID,
                    dbPath: dbPath,
                    extraAllowedTools: ["Bash(watchtower*)"]
                )
                var sawTurnComplete = false
                for try await event in stream {
                    switch event {
                    case .text(let chunk):
                        if let idx = self.messages.indices.last {
                            if sawTurnComplete {
                                self.messages[idx].text = chunk
                                sawTurnComplete = false
                            } else {
                                self.messages[idx].text += chunk
                            }
                        }
                    case .turnComplete(let fullText):
                        if let idx = self.messages.indices.last {
                            self.messages[idx].text = fullText
                        }
                        sawTurnComplete = true
                    case .sessionID(let sid):
                        self.sessionID = sid
                        // Persist session ID for resume
                        if let convID = self.conversationID {
                            self.persistSessionID(conversationID: convID, sessionID: sid)
                        }
                    case .done:
                        break
                    }
                }
            } catch {
                if !Task.isCancelled {
                    self.errorMessage = error.localizedDescription
                }
            }
            if let idx = self.messages.indices.last {
                self.messages[idx].isStreaming = false
                // Persist assistant message
                let assistantText = self.messages[idx].text
                if !assistantText.isEmpty, let convID = self.conversationID {
                    self.persistMessage(conversationID: convID, role: "assistant", text: assistantText)
                    self.touchConversation(convID)
                }
            }
            self.isStreaming = false

            // Refresh the action item after Claude may have updated it
            self.viewModel.load()
        }
    }

    func cancelStream() {
        streamTask?.cancel()
        streamTask = nil
        isStreaming = false
        if let idx = messages.indices.last, messages[idx].isStreaming {
            let partialText = messages[idx].text
            if !partialText.isEmpty, let convID = conversationID {
                persistMessage(conversationID: convID, role: "assistant", text: partialText)
            }
            messages[idx].isStreaming = false
        }
    }

    private func persistMessage(conversationID: Int64, role: String, text: String) {
        _ = try? dbManager.dbPool.write { db in
            try ChatMessageQueries.insert(db, conversationID: conversationID, role: role, text: text)
        }
    }

    private func touchConversation(_ id: Int64) {
        _ = try? dbManager.dbPool.write { db in
            try ChatConversationQueries.touch(db, id: id)
        }
    }

    private func persistSessionID(conversationID: Int64, sessionID: String) {
        _ = try? dbManager.dbPool.write { db in
            try ChatConversationQueries.updateSessionID(db, id: conversationID, sessionID: sessionID)
        }
    }

    nonisolated static func buildSystemPrompt(item: ActionItem, dbPool: DatabasePool) -> String {
        let schema = (try? dbPool.read { db in try ChatViewModel.fetchSchema(db) }) ?? ""
        let dbPath = dbPool.path

        let domain: String = (try? dbPool.read { db in
            try WorkspaceQueries.fetchWorkspace(db)?.domain
        }) ?? "unknown"

        return """
        You are Watchtower, an AI assistant helping the user manage a specific action item from their Slack workspace.

        === CURRENT ACTION ITEM ===
        ID: \(item.id)
        Text: \(item.text)
        Status: \(item.status)
        Priority: \(item.priority)
        Channel: #\(item.sourceChannelName) (\(item.channelID))
        Context: \(item.context)
        Due date: \(item.dueDateFormatted ?? "none")
        Created: \(item.createdAt)

        === CAPABILITIES ===
        You can query the database to find related messages, threads, and people involved.
        You can also UPDATE this action item by running CLI commands via Bash:

        - Accept (inbox→active): watchtower actions accept \(item.id)
        - Mark done:              watchtower actions done \(item.id)
        - Dismiss:                watchtower actions dismiss \(item.id)
        - Snooze:                 watchtower actions snooze \(item.id) --until tomorrow

        When the user asks to change the status, run the appropriate command.

        === DATABASE ===
        Database: \(dbPath)
        \(schema)

        === QUERY TIPS ===
        - Find the original message: SELECT m.text, u.display_name FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = '\(item.channelID)' AND m.ts = '\(item.sourceMessageTS)'
        - Find thread context: SELECT m.text, u.display_name FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = '\(item.channelID)' AND m.thread_ts = '\(item.sourceMessageTS)' ORDER BY m.ts_unix ASC
        - Find who else discussed this: Look for messages in the same channel around the same time
        - Permalink format: https://\(domain).slack.com/archives/{channel_id}/p{ts_without_dots}

        === RESPONSE STYLE ===
        - Be concise and direct
        - Match the user's language
        - Use markdown for readability
        - Include Slack links when referencing messages
        """
    }
}

// MARK: - View

struct ActionItemChatSection: View {
    @Bindable var chatVM: ActionItemChatViewModel

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Chat")
                    .font(.caption)
                    .fontWeight(.semibold)
                    .foregroundStyle(.secondary)
                Spacer()
                if chatVM.isStreaming {
                    Button("Stop") { chatVM.cancelStream() }
                        .buttonStyle(.borderless)
                        .font(.caption)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)

            Divider()

            // Messages
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 8) {
                    if chatVM.messages.isEmpty {
                        Text("Ask about this action item, who's involved, related discussions, or ask to update it.")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                            .padding()
                    }
                    ForEach(chatVM.messages) { msg in
                        chatBubble(msg)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            }

            Divider()

            // Input
            HStack(spacing: 8) {
                TextField("Ask about this action item...", text: $chatVM.inputText)
                    .textFieldStyle(.plain)
                    .font(.subheadline)
                    .onSubmit {
                        chatVM.send()
                    }

                Button {
                    chatVM.send()
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.title3)
                }
                .buttonStyle(.borderless)
                .disabled(chatVM.inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || chatVM.isStreaming)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)

            if let err = chatVM.errorMessage {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 4)
            }
        }
    }

    @ViewBuilder
    private func chatBubble(_ msg: ChatMessage) -> some View {
        switch msg.role {
        case .user:
            HStack {
                Spacer()
                Text(msg.text)
                    .font(.subheadline)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .background(Color.accentColor.opacity(0.15), in: RoundedRectangle(cornerRadius: 10))
            }
        case .assistant:
            HStack {
                VStack(alignment: .leading, spacing: 4) {
                    if msg.text.isEmpty && msg.isStreaming {
                        Text("Thinking...")
                            .font(.subheadline)
                            .foregroundStyle(.tertiary)
                    } else {
                        MarkdownText(text: msg.text)
                            .font(.subheadline)
                    }
                    if msg.isStreaming {
                        ProgressView()
                            .controlSize(.mini)
                    }
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
                Spacer()
            }
        case .system:
            Text(msg.text)
                .font(.caption)
                .foregroundStyle(.tertiary)
                .frame(maxWidth: .infinity)
        }
    }
}
