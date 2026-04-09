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
    private let aiService: any AIServiceProtocol
    private let dbManager: DatabaseManager
    private var track: Track
    private weak var viewModel: TracksViewModel?
    private var streamTask: Task<Void, Never>?
    private var observationTask: Task<Void, Never>?

    init(
        track: Track,
        viewModel: TracksViewModel,
        dbManager: DatabaseManager,
        aiService: (any AIServiceProtocol)? = nil
    ) {
        self.track = track
        self.viewModel = viewModel
        self.dbManager = dbManager
        self.aiService = aiService ?? WatchtowerAIService()

        loadOrCreateConversation()
        startMessageObservation()
    }

    private func loadOrCreateConversation() {
        do {
            if let existing = try dbManager.dbPool.read({ db in
                try ChatConversationQueries.fetchByContext(
                    db, type: "track", id: String(track.id)
                )
            }) {
                let records = try dbManager.dbPool.read { db in
                    try ChatMessageQueries.fetchByConversation(
                        db, conversationID: existing.id
                    )
                }
                conversationID = existing.id
                sessionID = existing.sessionID
                messages = records.map { $0.toChatMessage() }
                return
            }
            let conv = try dbManager.dbPool.write { db in
                try ChatConversationQueries.create(
                    db,
                    title: "Track: \(String(track.text.prefix(60)))",
                    contextType: "track",
                    contextID: String(track.id)
                )
            }
            conversationID = conv.id
            sessionID = conv.sessionID
            messages = []
        } catch {
            errorMessage = "Failed to load conversation: \(error.localizedDescription)"
        }
    }

    private func startMessageObservation() {
        guard let convID = conversationID else { return }
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try ChatMessageQueries.fetchByConversation(db, conversationID: convID)
            }
            do {
                for try await records in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    guard let self, !self.isStreaming else { continue }
                    if records.count != self.messages.count {
                        self.messages = records.map { $0.toChatMessage() }
                    }
                }
            } catch {}
        }
    }

    func send() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isStreaming else { return }

        streamTask?.cancel()
        inputText = ""

        messages.append(ChatMessage(
            id: UUID(),
            role: .user,
            text: text,
            timestamp: Date(),
            isStreaming: false
        ))

        if let convID = conversationID {
            persistMessage(conversationID: convID, role: "user", text: text)
        }

        messages.append(ChatMessage(
            id: UUID(),
            role: .assistant,
            text: "",
            timestamp: Date(),
            isStreaming: true
        ))
        isStreaming = true

        let currentSessionID = sessionID
        let dbPath = dbManager.dbPool.path
        let dbPool = dbManager.dbPool
        let capturedTrack = track
        let capturedAIService = aiService
        let capturedConvID = conversationID
        let capturedDBManager = dbManager

        streamTask = Task { [weak self] in
            await self?.executeStream(
                text: text,
                currentSessionID: currentSessionID,
                track: capturedTrack,
                dbPool: dbPool,
                dbPath: dbPath,
                aiService: capturedAIService,
                dbManager: capturedDBManager,
                conversationID: capturedConvID
            )
        }
    }

    // MARK: - Stream execution

    private func executeStream(
        text: String,
        currentSessionID: String?,
        track: Track,
        dbPool: DatabasePool,
        dbPath: String,
        aiService: any AIServiceProtocol,
        dbManager: DatabaseManager,
        conversationID: Int64?
    ) async {
        let systemPrompt: String? = currentSessionID == nil
            ? Self.buildSystemPrompt(track: track, dbPool: dbPool)
            : nil

        var fullText = ""
        var newSessionID: String?
        do {
            let stream = aiService.stream(
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
                    if sawTurnComplete {
                        fullText = chunk
                        sawTurnComplete = false
                    } else {
                        fullText += chunk
                    }
                    updateLastMessage(fullText)
                case .turnComplete(let text):
                    fullText = text
                    sawTurnComplete = true
                    updateLastMessage(fullText)
                case .sessionID(let sid):
                    newSessionID = sid
                    handleSessionID(sid)
                case .done:
                    break
                }
            }
        } catch {
            if !Task.isCancelled {
                errorMessage = error.localizedDescription
            }
        }

        if !fullText.isEmpty, let convID = conversationID {
            Self.persistResponse(
                dbManager: dbManager,
                conversationID: convID,
                text: fullText
            )
        }
        if let sid = newSessionID, let convID = conversationID {
            Self.persistSession(
                dbManager: dbManager,
                conversationID: convID,
                sessionID: sid
            )
        }

        finishStream()
    }

    // MARK: - Persistence helpers

    nonisolated private static func persistResponse(
        dbManager: DatabaseManager, conversationID: Int64, text: String
    ) {
        _ = try? dbManager.dbPool.write { db in
            try ChatMessageQueries.insert(
                db, conversationID: conversationID, role: "assistant", text: text
            )
            try ChatConversationQueries.touch(db, id: conversationID)
        }
    }

    nonisolated private static func persistSession(
        dbManager: DatabaseManager, conversationID: Int64, sessionID: String
    ) {
        _ = try? dbManager.dbPool.write { db in
            try ChatConversationQueries.updateSessionID(
                db, id: conversationID, sessionID: sessionID
            )
        }
    }

    private func updateLastMessage(_ text: String) {
        if let idx = messages.indices.last {
            messages[idx].text = text
        }
    }

    private func handleSessionID(_ sid: String) {
        self.sessionID = sid
        if let convID = conversationID {
            persistSessionID(conversationID: convID, sessionID: sid)
        }
    }

    private func finishStream() {
        if let idx = messages.indices.last {
            messages[idx].isStreaming = false
        }
        isStreaming = false
        reloadTrack()
        viewModel?.load()
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
            try ChatMessageQueries.insert(
                db, conversationID: conversationID, role: role, text: text
            )
        }
    }

    private func reloadTrack() {
        do {
            if let updated = try dbManager.dbPool.read({ db in
                try TrackQueries.fetchByID(db, id: track.id)
            }) {
                track = updated
            }
        } catch {}
    }

    private func persistSessionID(conversationID: Int64, sessionID: String) {
        _ = try? dbManager.dbPool.write { db in
            try ChatConversationQueries.updateSessionID(
                db, id: conversationID, sessionID: sessionID
            )
        }
    }

    nonisolated static func buildSystemPrompt(
        track: Track, dbPool: DatabasePool
    ) -> String {
        let schema = (try? dbPool.read { db in
            try ChatViewModel.fetchSchema(db)
        }) ?? ""
        let dbPath = dbPool.path

        let ws: Workspace? = try? dbPool.read { db in
            try WorkspaceQueries.fetchWorkspace(db)
        }
        let teamID = ws?.id ?? "unknown"

        let channelIDs = track.decodedChannelIDs
        let channelList = channelIDs.isEmpty ? "none" : channelIDs.joined(separator: ", ")

        return """
        You are Watchtower, an AI assistant helping the user understand a specific track \
        from their Slack workspace.

        === CURRENT TRACK ===
        ID: \(track.id)
        Text: \(track.text)
        Context: \(track.context)
        Category: \(track.category)
        Ownership: \(track.ownership)
        Priority: \(track.priority)
        Requester: \(track.requesterName)
        Blocking: \(track.blocking)
        Channels: \(channelList)
        Created: \(track.createdAt)
        Updated: \(track.updatedAt)

        === CAPABILITIES ===
        You can query the database to find related messages, threads, and people involved.

        === DATABASE ===
        Database: \(dbPath)
        \(schema)

        === QUERY TIPS ===
        - Find messages in track channels:
          SELECT m.text, u.display_name, m.ts FROM messages m
          JOIN users u ON m.user_id = u.id
          WHERE m.channel_id IN ('\(channelIDs.joined(separator: "','"))')
          ORDER BY m.ts_unix DESC LIMIT 20
        - Deep link format: slack://channel?team=\(teamID)&id={channel_id}&message={ts}

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

            ScrollView {
                LazyVStack(alignment: .leading, spacing: 8) {
                    if chatVM.messages.isEmpty {
                        Text("Ask about this track, who's involved, or related discussions.")
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

            HStack(spacing: 8) {
                TextField("Ask about this track...", text: $chatVM.inputText)
                    .textFieldStyle(.plain)
                    .font(.subheadline)
                    .onSubmit { chatVM.send() }

                Button { chatVM.send() } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.title3)
                }
                .buttonStyle(.borderless)
                .disabled(
                    chatVM.inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                        || chatVM.isStreaming
                )
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
                    .background(
                        Color.accentColor.opacity(0.15),
                        in: RoundedRectangle(cornerRadius: 10)
                    )
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
                .background(
                    Color.secondary.opacity(0.08),
                    in: RoundedRectangle(cornerRadius: 10)
                )
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
