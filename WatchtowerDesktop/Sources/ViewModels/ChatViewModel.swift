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

        streamTask?.cancel()
        inputText = ""
        messages.append(ChatMessage(id: UUID(), role: .user, text: text, timestamp: Date(), isStreaming: false))

        if let convID = conversationID {
            persistMessage(conversationID: convID, role: "user", text: text)
        }

        messages.append(ChatMessage(id: UUID(), role: .assistant, text: "", timestamp: Date(), isStreaming: true))
        isStreaming = true

        autoGenerateTitle(text: text)

        let currentSessionID = sessionID
        let dbPath = dbManager.dbPool.path
        let dbPool = dbManager.dbPool
        let model = selectedModel.rawValue

        streamTask = Task { [weak self] in
            guard let self else { return }
            let systemPrompt: String? = currentSessionID == nil ? Self.buildSystemPrompt(dbPool: dbPool) : nil
            await self.runStream(
                text: text,
                systemPrompt: systemPrompt,
                sessionID: currentSessionID,
                dbPath: dbPath,
                model: model
            )
        }
    }

    private func autoGenerateTitle(text: String) {
        let isFirstMessage = messages.filter { $0.role == .user }.count == 1
        if isFirstMessage, let convID = conversationID {
            onConversationUpdated?(convID, String(text.prefix(80)), nil)
        }
    }

    private func runStream(
        text: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String,
        model: String
    ) async {
        do {
            let stream = claudeService.stream(
                prompt: text,
                systemPrompt: systemPrompt,
                sessionID: sessionID,
                dbPath: dbPath,
                model: model
            )
            var sawTurnComplete = false
            for try await event in stream {
                handleStreamEvent(event, sawTurnComplete: &sawTurnComplete)
            }
        } catch {
            if !Task.isCancelled {
                errorMessage = error.localizedDescription
            }
        }
        finalizeStream()
    }

    private func handleStreamEvent(_ event: StreamEvent, sawTurnComplete: inout Bool) {
        switch event {
        case .text(let chunk):
            if let idx = messages.indices.last {
                if sawTurnComplete {
                    messages[idx].text = chunk
                    sawTurnComplete = false
                } else {
                    messages[idx].text += chunk
                }
            }
        case .turnComplete(let fullText):
            if let idx = messages.indices.last {
                messages[idx].text = fullText
            }
            sawTurnComplete = true
        case .sessionID(let sid):
            self.sessionID = sid
            if let convID = conversationID {
                onConversationUpdated?(convID, nil, sid)
            }
        case .done:
            break
        }
    }

    private func finalizeStream() {
        if let idx = messages.indices.last {
            messages[idx].isStreaming = false
            let assistantText = messages[idx].text
            if !assistantText.isEmpty, let convID = conversationID {
                persistMessage(conversationID: convID, role: "assistant", text: assistantText)
            }
        }
        isStreaming = false
        if let convID = conversationID {
            onConversationUpdated?(convID, nil, nil)
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
        _ = try? dbManager.dbPool.write { db in
            try ChatMessageQueries.insert(db, conversationID: conversationID, role: role, text: text)
        }
    }

    // MARK: - System Prompt

    // H2: static method avoids capturing self in GRDB closure
    nonisolated static func buildSystemPrompt(dbPool: DatabasePool) -> String {
        do {
            return try dbPool.read { db in
                let ws = try WorkspaceQueries.fetchWorkspace(db)
                let schema = try Self.fetchSchema(db)
                return Self.formatSystemPrompt(
                    workspace: ws,
                    dbPath: dbPool.path,
                    schema: schema
                )
            }
        } catch {
            return "You are Watchtower, an AI assistant for Slack workspace analysis. Query the SQLite database to answer questions."
        }
    }

    nonisolated static func formatSystemPrompt(
        workspace ws: Workspace?,
        dbPath: String,
        schema: String
    ) -> String {
        let name = ws?.name ?? "unknown"
        let domain = ws?.domain ?? "unknown"
        let teamID = ws?.id ?? "unknown"

        let now = {
            let fmt = DateFormatter()
            fmt.dateFormat = "yyyy-MM-dd HH:mm 'UTC'"
            fmt.timeZone = TimeZone(identifier: "UTC")
            return fmt.string(from: Date())
        }()

        return promptHeader(name: name, domain: domain, now: now, dbPath: dbPath, schema: schema)
            + promptQueryPatterns(teamID: teamID)
            + promptRules(teamID: teamID)
    }

    nonisolated private static func promptHeader(
        name: String,
        domain: String,
        now: String,
        dbPath: String,
        schema: String
    ) -> String {
        """
        You are Watchtower, an AI assistant that answers questions about a Slack workspace by querying its SQLite database.

        Workspace: "\(name)" (domain: \(domain).slack.com)
        Current time: \(now)
        Database: \(dbPath)

        IMPORTANT: You MUST query the database to answer every question.
        You have NO pre-loaded data — the database is your only source of truth.

        === HOW TO QUERY ===
        You have MCP tools for SQLite. Use them:
        - read_query: run SELECT queries (use this for all data retrieval)
        - list_tables: see all tables
        - describe_table: see table schema

        Fallback (if MCP tools fail): sqlite3 -header -separator '|' "\(dbPath)" "SQL"

        === DATABASE SCHEMA ===
        \(schema)

        """
    }

    nonisolated private static func promptQueryPatterns(teamID: String) -> String {
        """
        === QUERY PATTERNS ===

        First, orient yourself — find what channels and users exist:
          SELECT name, id, type FROM channels WHERE is_archived = 0 ORDER BY name;
          SELECT name, display_name, id FROM users WHERE is_deleted = 0 ORDER BY name;

        Messages in a channel (recent first):
          SELECT m.ts, u.display_name, m.text
          FROM messages m JOIN users u ON m.user_id = u.id
          WHERE m.channel_id = (SELECT id FROM channels
          WHERE name = 'general')
          AND m.ts_unix > unixepoch('now', '-1 day')
          ORDER BY m.ts_unix DESC LIMIT 50;

        Messages from a user:
          SELECT m.ts, m.text, c.name
          FROM messages m JOIN channels c ON m.channel_id = c.id
          WHERE m.user_id = (SELECT id FROM users
          WHERE name = 'alice')
          ORDER BY m.ts_unix DESC LIMIT 30;

        Activity overview:
          SELECT c.name, COUNT(*) as cnt
          FROM messages m JOIN channels c ON m.channel_id = c.id
          WHERE m.ts_unix > unixepoch('now', '-1 day')
          GROUP BY c.name ORDER BY cnt DESC;

        Full-text search:
          SELECT m.text, u.display_name, c.name, m.ts
          FROM messages_fts fts
          JOIN messages m ON fts.channel_id = m.channel_id
            AND fts.ts = m.ts
          JOIN users u ON m.user_id = u.id
          JOIN channels c ON m.channel_id = c.id
          WHERE messages_fts MATCH 'keyword'
          ORDER BY m.ts_unix DESC LIMIT 20;

        Thread replies:
          SELECT m.ts, u.display_name, m.text
          FROM messages m JOIN users u ON m.user_id = u.id
          WHERE m.channel_id = 'C123'
          AND m.thread_ts = '1234567890.123456'
          ORDER BY m.ts_unix ASC;

        Deep link format:
          slack://channel?team=\(teamID)&id={channel_id}&message={ts}
          Example: ts "1740577800.000100" →
          slack://channel?team=\(teamID)&id=C123&message=1740577800.000100

        === IMPORTANT RESTRICTIONS ===
        - You have NO internet access. Do NOT call any Slack API, WebFetch, or WebSearch tools.
        - Your ONLY data source is the local SQLite database. Query it, do not try to fetch from Slack.

        """
    }

    nonisolated private static func promptRules(teamID: String) -> String {
        """
        === WORKFLOW ===
        1. Run a SQL query using the read_query MCP tool
        2. If results are empty or insufficient, broaden the query (wider time range, different search terms)
        3. Analyze the actual message content from query results
        4. Respond with insights, organized by channel or topic
        5. Include Slack permalinks for key messages

        === LINKING RULES ===
        ALWAYS include Slack links as descriptive markdown — never bare URLs.

        Channel link: [#channel-name](slack://channel?team=\(teamID)&id={channel_id})
        Message link: [описательный текст](slack://channel?team=\(teamID)&id={channel_id}&message={ts})
          Use the raw ts value (with dot). Example: "1740577800.000100" → message=1740577800.000100

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
        - Highlight: decisions, tracks, unanswered questions, unusual activity
        """
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
