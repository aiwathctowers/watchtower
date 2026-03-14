import SwiftUI
import GRDB

// MARK: - ViewModel

@MainActor
@Observable
final class TrackChatViewModel {
    var messages: [ChatMessage] = []
    var isStreaming = false
    var inputText = ""
    var errorMessage: String?

    private var conversationID: Int64?
    private var sessionID: String?
    private let claudeService: any ClaudeServiceProtocol
    private let dbManager: DatabaseManager
    private var item: Track
    private weak var viewModel: TracksViewModel?
    private var streamTask: Task<Void, Never>?

    init(item: Track, viewModel: TracksViewModel, dbManager: DatabaseManager, claudeService: (any ClaudeServiceProtocol)? = nil) {
        self.item = item
        self.viewModel = viewModel
        self.dbManager = dbManager
        self.claudeService = claudeService ?? ClaudeService()

        loadOrCreateConversation()
    }

    private func loadOrCreateConversation() {
        do {
            // Try read-only first to avoid unnecessary write locks.
            if let existing = try dbManager.dbPool.read({ db in
                try ChatConversationQueries.fetchByContext(db, type: "track", id: String(item.id))
            }) {
                let records = try dbManager.dbPool.read { db in
                    try ChatMessageQueries.fetchByConversation(db, conversationID: existing.id)
                }
                conversationID = existing.id
                sessionID = existing.sessionID
                messages = records.map { $0.toChatMessage() }
                return
            }
            // No existing conversation — create one.
            let conv = try dbManager.dbPool.write { db in
                try ChatConversationQueries.create(
                    db,
                    title: "Track: \(String(item.text.prefix(60)))",
                    contextType: "track",
                    contextID: String(item.id)
                )
            }
            conversationID = conv.id
            sessionID = conv.sessionID
            messages = []
        } catch {
            errorMessage = "Failed to load conversation: \(error.localizedDescription)"
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

        let capturedItem = item
        let capturedClaudeService = claudeService

        streamTask = Task { [weak self] in
            let systemPrompt: String? = if currentSessionID == nil {
                Self.buildSystemPrompt(item: capturedItem, dbPool: dbPool)
            } else {
                nil
            }

            do {
                let stream = capturedClaudeService.stream(
                    prompt: text,
                    systemPrompt: systemPrompt,
                    sessionID: currentSessionID,
                    dbPath: dbPath,
                    extraAllowedTools: ["Bash(watchtower*)"]
                )
                var sawTurnComplete = false
                for try await event in stream {
                    guard let self else { return }
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
                    self?.errorMessage = error.localizedDescription
                }
            }
            guard let self else { return }
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

            // Refresh the track from DB after Claude may have updated it
            self.reloadItem()
            self.viewModel?.load()
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

    private func reloadItem() {
        do {
            if let updated = try dbManager.dbPool.read({ db in
                try TrackQueries.fetchByID(db, id: item.id)
            }) {
                item = updated
            }
        } catch {
            // Non-fatal: keep stale item
        }
    }

    private func persistSessionID(conversationID: Int64, sessionID: String) {
        _ = try? dbManager.dbPool.write { db in
            try ChatConversationQueries.updateSessionID(db, id: conversationID, sessionID: sessionID)
        }
    }

    nonisolated static func buildSystemPrompt(item: Track, dbPool: DatabasePool) -> String {
        let schema = (try? dbPool.read { db in try ChatViewModel.fetchSchema(db) }) ?? ""
        let dbPath = dbPool.path

        let domain: String = (try? dbPool.read { db in
            try WorkspaceQueries.fetchWorkspace(db)?.domain
        }) ?? "unknown"

        return """
        You are Watchtower, an AI assistant helping the user manage a specific track from their Slack workspace.

        === CURRENT TRACK ===
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
        You can also UPDATE this track by running CLI commands via Bash:

        - Accept (inbox→active): watchtower tracks accept \(item.id)
        - Mark done:              watchtower tracks done \(item.id)
        - Dismiss:                watchtower tracks dismiss \(item.id)
        - Snooze:                 watchtower tracks snooze \(item.id) --until tomorrow

        When the user asks to change the status, run the appropriate command.

        === DATABASE ===
        Database: \(dbPath)
        \(schema)

        === QUERY TIPS ===
        - Find the original message: SELECT m.text, u.display_name FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = ? AND m.ts = ?  (bind: '\(item.channelID.replacingOccurrences(of: "'", with: "''"))', '\(item.sourceMessageTS.replacingOccurrences(of: "'", with: "''"))')
        - Find thread context: SELECT m.text, u.display_name FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = ? AND m.thread_ts = ? ORDER BY m.ts_unix ASC  (bind same values)
        - Find who else discussed this: Look for messages in the same channel around the same time
        - Permalink format: https://\(domain.replacingOccurrences(of: "'", with: "")).slack.com/archives/{channel_id}/p{ts_without_dots}

        === RESPONSE STYLE ===
        - Be concise and direct
        - Match the user's language
        - Use markdown for readability
        - Include Slack links when referencing messages
        """
    }
}

// MARK: - View

struct TrackChatSection: View {
    @Bindable var chatVM: TrackChatViewModel

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
                        Text("Ask about this track, who's involved, related discussions, or ask to update it.")
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
                TextField("Ask about this track...", text: $chatVM.inputText)
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
