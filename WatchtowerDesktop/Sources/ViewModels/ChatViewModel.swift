import Foundation
import GRDB

struct ChatMessage: Identifiable, Equatable {
    let id: UUID
    var role: Role
    var text: String
    var timestamp: Date
    var isStreaming: Bool

    enum Role: Equatable {
        case user
        case assistant
        case system
    }
}

enum ChatModel: String, CaseIterable, Identifiable {
    case sonnet = "claude-sonnet-4-6"
    case haiku = "claude-haiku-4-5-20251001"
    case opus = "claude-opus-4-6"

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .sonnet: "Sonnet 4.6"
        case .haiku: "Haiku 4.5"
        case .opus: "Opus 4.6"
        }
    }
}

@MainActor
@Observable
final class ChatViewModel {
    var messages: [ChatMessage] = []
    var isStreaming = false
    var inputText = ""
    var errorMessage: String?
    var selectedModel: ChatModel = .sonnet

    private(set) var conversationID: Int64?
    private var sessionID: String?
    private let claudeService: any ClaudeServiceProtocol
    private let dbManager: DatabaseManager
    private var streamTask: Task<Void, Never>?

    /// Callback to notify history that title/session changed
    var onConversationUpdated: ((Int64, String?, String?) -> Void)?

    init(claudeService: any ClaudeServiceProtocol, dbManager: DatabaseManager) {
        self.claudeService = claudeService
        self.dbManager = dbManager
    }

    func bind(to conversation: ChatConversation) {
        // If switching to a different conversation, load from DB
        if conversationID != conversation.id {
            cancelStream()
            messages.removeAll()
            errorMessage = nil
            conversationID = conversation.id
            sessionID = conversation.sessionID
            loadMessages(conversationID: conversation.id)
        }
    }

    func send() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isStreaming else { return }

        // H9: cancel any previous stream
        streamTask?.cancel()

        inputText = ""
        let userMsg = ChatMessage(id: UUID(), role: .user, text: text, timestamp: Date(), isStreaming: false)
        messages.append(userMsg)

        // Persist user message
        if let convID = conversationID {
            persistMessage(conversationID: convID, role: "user", text: text)
        }

        let assistantMsg = ChatMessage(id: UUID(), role: .assistant, text: "", timestamp: Date(), isStreaming: true)
        messages.append(assistantMsg)
        isStreaming = true

        // Auto-generate title from first user message
        let isFirstMessage = messages.filter({ $0.role == .user }).count == 1
        if isFirstMessage, let convID = conversationID {
            let title = String(text.prefix(80))
            onConversationUpdated?(convID, title, nil)
        }

        // H2/M5: build system prompt off main thread
        let currentSessionID = sessionID
        let dbPath = dbManager.dbPool.path
        let dbPool = dbManager.dbPool
        let model = selectedModel.rawValue

        streamTask = Task { [weak self] in
            guard let self else { return }

            let systemPrompt: String? = if currentSessionID == nil {
                Self.buildSystemPrompt(dbPool: dbPool)
            } else {
                nil
            }

            do {
                let stream = claudeService.stream(
                    prompt: text,
                    systemPrompt: systemPrompt,
                    sessionID: currentSessionID,
                    dbPath: dbPath,
                    model: model
                )
                // Track turn boundaries: after a turnComplete, next delta starts fresh
                var sawTurnComplete = false
                for try await event in stream {
                    switch event {
                    case .text(let chunk):
                        if let idx = self.messages.indices.last {
                            if sawTurnComplete {
                                // New turn — replace previous turn's text
                                self.messages[idx].text = chunk
                                sawTurnComplete = false
                            } else {
                                self.messages[idx].text += chunk
                            }
                        }
                    case .turnComplete(let fullText):
                        // End of a conversation turn — replace with clean text
                        if let idx = self.messages.indices.last {
                            self.messages[idx].text = fullText
                        }
                        sawTurnComplete = true
                    case .sessionID(let sid):
                        self.sessionID = sid
                        if let convID = self.conversationID {
                            self.onConversationUpdated?(convID, nil, sid)
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
                // Persist assistant message (only if non-empty)
                let assistantText = self.messages[idx].text
                if !assistantText.isEmpty, let convID = self.conversationID {
                    self.persistMessage(conversationID: convID, role: "assistant", text: assistantText)
                }
            }
            self.isStreaming = false

            // Touch conversation to update timestamp
            if let convID = self.conversationID {
                self.onConversationUpdated?(convID, nil, nil)
            }
        }
    }

    func cancelStream() {
        streamTask?.cancel()
        streamTask = nil
        isStreaming = false
        if let idx = messages.indices.last, messages[idx].isStreaming {
            // Save partial assistant response if non-empty
            let partialText = messages[idx].text
            if !partialText.isEmpty, let convID = conversationID {
                persistMessage(conversationID: convID, role: "assistant", text: partialText)
            }
            messages[idx].isStreaming = false
        }
    }

    func newChat() {
        // H9: cancel in-flight stream before clearing
        cancelStream()
        messages.removeAll()
        sessionID = nil
        conversationID = nil
        errorMessage = nil
    }

    // MARK: - Persistence

    private func loadMessages(conversationID: Int64) {
        do {
            let records = try dbManager.dbPool.read { db in
                try ChatMessageQueries.fetchByConversation(db, conversationID: conversationID)
            }
            messages = records.map { $0.toChatMessage() }
        } catch {
            // silently ignore
        }
    }

    private func persistMessage(conversationID: Int64, role: String, text: String) {
        try? dbManager.dbPool.write { db in
            try ChatMessageQueries.insert(db, conversationID: conversationID, role: role, text: text)
        }
    }

    // MARK: - System Prompt

    // H2: static method avoids capturing self in GRDB closure
    nonisolated static func buildSystemPrompt(dbPool: DatabasePool) -> String {
        do {
            return try dbPool.read { db in
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let name = ws?.name ?? "unknown"
                let domain = ws?.domain ?? "unknown"
                let dbPath = dbPool.path

                // Get schema from the database itself
                let schema = try Self.fetchSchema(db)

                let now = {
                    let f = DateFormatter()
                    f.dateFormat = "yyyy-MM-dd HH:mm 'UTC'"
                    f.timeZone = TimeZone(identifier: "UTC")
                    return f.string(from: Date())
                }()

                return """
                You are Watchtower, an AI assistant that answers questions about a Slack workspace by querying its SQLite database.

                Workspace: "\(name)" (domain: \(domain).slack.com)
                Current time: \(now)
                Database: \(dbPath)

                IMPORTANT: You MUST query the database to answer every question. You have NO pre-loaded data — the database is your only source of truth.

                === HOW TO QUERY ===
                You have MCP tools for SQLite. Use them:
                - read_query: run SELECT queries (use this for all data retrieval)
                - list_tables: see all tables
                - describe_table: see table schema

                Fallback (if MCP tools fail): sqlite3 -header -separator '|' "\(dbPath)" "SQL"

                === DATABASE SCHEMA ===
                \(schema)

                === QUERY PATTERNS ===

                First, orient yourself — find what channels and users exist:
                  SELECT name, id, type FROM channels WHERE is_archived = 0 ORDER BY name;
                  SELECT name, display_name, id FROM users WHERE is_deleted = 0 ORDER BY name;

                Messages in a channel (recent first):
                  SELECT m.ts, u.display_name, m.text FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = (SELECT id FROM channels WHERE name = 'general') AND m.ts_unix > unixepoch('now', '-1 day') ORDER BY m.ts_unix DESC LIMIT 50;

                Messages from a user:
                  SELECT m.ts, m.text, c.name FROM messages m JOIN channels c ON m.channel_id = c.id WHERE m.user_id = (SELECT id FROM users WHERE name = 'alice') ORDER BY m.ts_unix DESC LIMIT 30;

                Activity overview:
                  SELECT c.name, COUNT(*) as cnt FROM messages m JOIN channels c ON m.channel_id = c.id WHERE m.ts_unix > unixepoch('now', '-1 day') GROUP BY c.name ORDER BY cnt DESC;

                Full-text search:
                  SELECT m.text, u.display_name, c.name, m.ts FROM messages_fts fts JOIN messages m ON fts.channel_id = m.channel_id AND fts.ts = m.ts JOIN users u ON m.user_id = u.id JOIN channels c ON m.channel_id = c.id WHERE messages_fts MATCH 'keyword' ORDER BY m.ts_unix DESC LIMIT 20;

                Thread replies:
                  SELECT m.ts, u.display_name, m.text FROM messages m JOIN users u ON m.user_id = u.id WHERE m.channel_id = 'C123' AND m.thread_ts = '1234567890.123456' ORDER BY m.ts_unix ASC;

                Permalink format: https://\(domain).slack.com/archives/{channel_id}/p{ts_without_dots}
                  Example: ts "1740577800.000100" → p1740577800000100

                === IMPORTANT RESTRICTIONS ===
                - You have NO internet access. Do NOT call any Slack API, WebFetch, or WebSearch tools.
                - Your ONLY data source is the local SQLite database. Query it, do not try to fetch from Slack.

                === WORKFLOW ===
                1. Run a SQL query using the read_query MCP tool
                2. If results are empty or insufficient, broaden the query (wider time range, different search terms)
                3. Analyze the actual message content from query results
                4. Respond with insights, organized by channel or topic
                5. Include Slack permalinks for key messages

                === LINKING RULES ===
                ALWAYS include Slack links as descriptive markdown — never bare URLs.

                Channel link: [#channel-name](https://\(domain).slack.com/archives/{channel_id})
                Message link: [описательный текст](https://\(domain).slack.com/archives/{channel_id}/p{ts_no_dots})
                  To convert ts to permalink: remove the dot. "1740577800.000100" → "p1740577800000100"

                Rules:
                - Every channel mention (#name) MUST be a link to that channel
                - Every referenced message or thread MUST have a link with descriptive text in the user's language
                - Always SELECT channel_id and ts in your queries so you can build links

                === RESPONSE STYLE ===
                - Be concise and direct — give the answer, not the process
                - Do NOT describe your search steps, reasoning, or tool usage. Present findings directly.
                - Match the user's language and tone
                - Use markdown for readability (headers, bullet lists, bold for emphasis)
                - Use line breaks between sections for clarity
                - Highlight: decisions, action items, unanswered questions, unusual activity
                """
            }
        } catch {
            return "You are Watchtower, an AI assistant for Slack workspace analysis. Query the SQLite database to answer questions."
        }
    }

    /// Fetch the database schema (CREATE TABLE statements)
    nonisolated static func fetchSchema(_ db: Database) throws -> String {
        let rows = try Row.fetchAll(db, sql: """
            SELECT sql FROM sqlite_master
            WHERE type IN ('table', 'view') AND sql IS NOT NULL
            ORDER BY CASE type WHEN 'table' THEN 0 ELSE 1 END, name
        """)
        return rows.compactMap { $0["sql"] as? String }.joined(separator: ";\n\n")
    }
}
