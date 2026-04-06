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

enum AIProvider: String, CaseIterable, Identifiable {
    case claude
    case codex

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .claude: "Claude"
        case .codex: "Codex"
        }
    }
}

enum ChatModel: String, CaseIterable, Identifiable {
    // Claude
    case sonnet = "claude-sonnet-4-6"
    case haiku = "claude-haiku-4-5-20251001"
    case opus = "claude-opus-4-6"
    // Codex
    case gpt54 = "gpt-5.4"
    case gpt54mini = "gpt-5.4-mini"
    case gpt53codex = "gpt-5.3-codex"

    var id: String { rawValue }

    var provider: AIProvider {
        switch self {
        case .sonnet, .haiku, .opus: .claude
        case .gpt54, .gpt54mini, .gpt53codex: .codex
        }
    }

    var displayName: String {
        switch self {
        case .sonnet: "Sonnet 4.6"
        case .haiku: "Haiku 4.5"
        case .opus: "Opus 4.6"
        case .gpt54: "GPT-5.4"
        case .gpt54mini: "GPT-5.4 Mini"
        case .gpt53codex: "GPT-5.3 Codex"
        }
    }

    static func models(for provider: AIProvider) -> [Self] {
        allCases.filter { $0.provider == provider }
    }

    static func defaultModel(for provider: AIProvider) -> Self {
        switch provider {
        case .claude: .sonnet
        case .codex: .gpt54
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
    var selectedProvider: AIProvider = .claude
    var selectedModel: ChatModel = .sonnet

    private(set) var conversationID: Int64?
    private var sessionID: String?
    private var aiService: any AIServiceProtocol
    private let dbManager: DatabaseManager
    private var streamTask: Task<Void, Never>?
    private var observationTask: Task<Void, Never>?

    /// Callback to notify history that title/session changed
    var onConversationUpdated: ((Int64, String?, String?) -> Void)?

    init(aiService: any AIServiceProtocol, dbManager: DatabaseManager, provider: AIProvider = .claude) {
        self.aiService = aiService
        self.dbManager = dbManager
        self.selectedProvider = provider
        self.selectedModel = ChatModel.defaultModel(for: provider)
    }

    func switchProvider(_ provider: AIProvider) {
        guard provider != selectedProvider else { return }
        cancelStream()
        selectedProvider = provider
        selectedModel = ChatModel.defaultModel(for: provider)
        aiService = Self.createService(for: provider)
    }

    static func createService(for provider: AIProvider) -> any AIServiceProtocol {
        switch provider {
        case .claude: ClaudeService()
        case .codex: CodexService()
        }
    }

    func bind(to conversation: ChatConversation) {
        // If switching to a different conversation, load from DB
        if conversationID != conversation.id {
            cancelStream()
            observationTask?.cancel()
            observationTask = nil
            messages.removeAll()
            errorMessage = nil
            conversationID = conversation.id
            sessionID = conversation.sessionID
            loadMessages(conversationID: conversation.id)
            startMessageObservation()
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
        let capturedConvID = conversationID
        let capturedDBManager = dbManager
        let capturedAIService = aiService

        streamTask = Task { [weak self] in
            let systemPrompt: String? = currentSessionID == nil ? Self.buildSystemPrompt(dbPool: dbPool) : nil

            var fullText = ""
            var newSessionID: String?
            do {
                let stream = capturedAIService.stream(
                    prompt: text,
                    systemPrompt: systemPrompt,
                    sessionID: currentSessionID,
                    dbPath: dbPath,
                    model: model
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
                        self?.updateLastMessage(fullText)
                    case .turnComplete(let text):
                        fullText = text
                        sawTurnComplete = true
                        self?.updateLastMessage(fullText)
                    case .sessionID(let sid):
                        newSessionID = sid
                        self?.sessionID = sid
                        if let convID = capturedConvID {
                            self?.onConversationUpdated?(convID, nil, sid)
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

            // Always persist the response, even if self is gone
            if !fullText.isEmpty, let convID = capturedConvID {
                Self.persistResponseStatic(dbManager: capturedDBManager, conversationID: convID, text: fullText)
            }
            if let sid = newSessionID, let convID = capturedConvID {
                Self.persistSessionStatic(dbManager: capturedDBManager, conversationID: convID, sessionID: sid)
            }

            self?.finishStream()
        }
    }

    private func autoGenerateTitle(text: String) {
        let isFirstMessage = messages.filter { $0.role == .user }.count == 1
        if isFirstMessage, let convID = conversationID {
            onConversationUpdated?(convID, String(text.prefix(80)), nil)
        }
    }

    private func updateLastMessage(_ text: String) {
        if let idx = messages.indices.last {
            messages[idx].text = text
        }
    }

    private func finishStream() {
        if let idx = messages.indices.last {
            messages[idx].isStreaming = false
        }
        isStreaming = false
        if let convID = conversationID {
            onConversationUpdated?(convID, nil, nil)
        }
    }

    // MARK: - Static persistence (works even if self is deallocated)

    nonisolated private static func persistResponseStatic(dbManager: DatabaseManager, conversationID: Int64, text: String) {
        _ = try? dbManager.dbPool.write { db in
            try ChatMessageQueries.insert(db, conversationID: conversationID, role: "assistant", text: text)
            try ChatConversationQueries.touch(db, id: conversationID)
        }
    }

    nonisolated private static func persistSessionStatic(dbManager: DatabaseManager, conversationID: Int64, sessionID: String) {
        _ = try? dbManager.dbPool.write { db in
            try ChatConversationQueries.updateSessionID(db, id: conversationID, sessionID: sessionID)
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
        observationTask?.cancel()
        observationTask = nil
        messages.removeAll()
        sessionID = nil
        conversationID = nil
        errorMessage = nil
    }

    // MARK: - Observation

    /// Observe chat_messages for this conversation so background-persisted
    /// responses (from a stream that outlived a previous ViewModel) appear automatically.
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
            + promptAppGuide()
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

    nonisolated private static func promptAppGuide() -> String {
        """

        === WATCHTOWER APP GUIDE ===
        You are also an expert on the Watchtower app itself. When users ask about features,
        how to use the app, or what something means — answer based on this guide.

        Watchtower is a macOS desktop app that syncs a Slack workspace to a local SQLite database
        and uses AI to generate insights: daily briefings, inbox, calendar with meeting prep, digests, tracks, and people analytics.

        TABS:
        - AI Chat: chat with AI about workspace data. Provider selector (Claude/Codex), model selector.
          Claude: Sonnet/Haiku/Opus; Codex: GPT-5.4/GPT-5.4 Mini/GPT-5.3 Codex.
          Multi-turn with session memory (Claude only; Codex is ephemeral).
          Calendar events (48h) injected into context
        - Briefings: personalized daily overview — today's schedule (calendar events), needs attention, your day, what happened, team pulse, coaching
        - Inbox: messages awaiting your response — @mentions and DMs auto-detected after each sync, AI-prioritized (high/medium/low), auto-resolved when you reply. Statuses: pending, resolved, dismissed, snoozed. Actions: resolve, dismiss, snooze, create task, open in Slack
        - Calendar: Google Calendar integration — today's and tomorrow's events, meeting prep (AI-generated talking points, open items, people notes, suggested prep). Connect in Settings. Events highlight: green=happening now, blue=upcoming within 1 hour
        - Tasks: personal action items with priority, ownership, due dates, sub-items. Sources: track, briefing, digest, inbox, manual, chat
        - Tracks: auto-generated narrative summaries of ongoing initiatives from digests (priority: high/medium/low; narrative, timeline, participants, key messages)
        - Digests: AI summaries of channel activity (channel/daily/weekly), with topics, decisions, running context
        - Decisions: flat list of all decisions across digests, with importance ratings
        - People: team member profiles from AI analysis — communication style, decision role, accomplishments, red flags, activity hours
        - Statistics: channel analytics, bot traffic %, recommendations (mute/leave/favorite), mute channels for AI
        - Search: full-text search across all synced Slack messages
        - Usage: token consumption and costs by date, model, feature; live pipeline progress
        - Training: prompt editor, feedback stats, quality score, tuning controls

        SETTINGS: sync interval, workers, history depth, AI provider (Claude/Codex), digest model/language, briefing hour,
        Claude CLI path, Codex CLI path (when Codex selected), Google Calendar (connect/disconnect, sync days ahead),
        Jira (OAuth, board selection, Board Profiles with workflow viz and stale sliders,
        User Mapping, sync status, Feature toggles by category and role),
        profile (role, team, manager, reports, peers), notifications, daemon control, logs, data management.

        BACKGROUND PROCESSES: daemon syncs Slack periodically, then runs pipelines:
        calendar sync → inbox (detect + AI prioritize) → channel digests → tracks → rollup digests → people → briefing (automatic after each sync).
        Also auto-unsnoozes tasks and inbox items past their snooze date.

        KEY CONCEPTS:
        - Running context: AI maintains per-channel memory (active topics, decisions, open questions)
        - Situations: extracted interaction patterns used to build people cards
        - Feedback loop: thumbs up/down + importance corrections improve AI via prompt tuning
        - Starred items: prioritize specific channels and people in analysis
        - Muted channels: excluded from AI processing to reduce noise and token costs
        - Google Calendar: optional integration syncing events to local DB, enabling meeting prep and schedule-aware briefings/chat
        - Jira Cloud: optional integration via OAuth. Board Profiles (LLM-analyzed workflow stages,
          stale thresholds, health signals). Issues sync every 15 min. Jira keys (PROJ-123)
          auto-detected in Slack. Feature toggles by role (Your Work, Team, Product, Automation).
          CLI: jira login/logout/status, boards/select/analyze, users/map, sync, features

        When answering about the app, be specific and accurate. Do not invent features that don't exist.
        """
    }

    // MARK: - Welcome Message

    /// Send a welcome message in a new chat, using the user's profile for personalization.
    func sendWelcomeMessage(profile: UserProfile, language: String = "English") {
        guard !isStreaming else { return }

        let welcomePrompt = Self.buildWelcomePrompt(profile: profile, language: language)

        messages.append(ChatMessage(id: UUID(), role: .assistant, text: "", timestamp: Date(), isStreaming: true))
        isStreaming = true

        let dbPath = dbManager.dbPool.path
        let dbPool = dbManager.dbPool
        let model = selectedModel.rawValue
        let capturedConvID = conversationID
        let capturedDBManager = dbManager
        let capturedAIService = aiService

        streamTask = Task { [weak self] in
            let systemPrompt = Self.buildSystemPrompt(dbPool: dbPool)

            var fullText = ""
            var newSessionID: String?
            do {
                let stream = capturedAIService.stream(
                    prompt: welcomePrompt,
                    systemPrompt: systemPrompt,
                    sessionID: nil,
                    dbPath: dbPath,
                    model: model
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
                        self?.updateLastMessage(fullText)
                    case .turnComplete(let text):
                        fullText = text
                        sawTurnComplete = true
                        self?.updateLastMessage(fullText)
                    case .sessionID(let sid):
                        newSessionID = sid
                        self?.sessionID = sid
                        if let convID = capturedConvID {
                            self?.onConversationUpdated?(convID, nil, sid)
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

            if !fullText.isEmpty, let convID = capturedConvID {
                Self.persistResponseStatic(dbManager: capturedDBManager, conversationID: convID, text: fullText)
            }
            if let sid = newSessionID, let convID = capturedConvID {
                Self.persistSessionStatic(dbManager: capturedDBManager, conversationID: convID, sessionID: sid)
            }

            self?.finishStream()
        }
    }

    nonisolated private static func buildWelcomePrompt(profile: UserProfile, language: String) -> String {
        var parts: [String] = []
        parts.append("IMPORTANT: You MUST respond entirely in \(language).")
        parts.append("""
            This is the user's FIRST time opening Watchtower after onboarding. \
            Write a welcome message that serves as a quick tour of the app. \
            Structure it as a friendly, concise guide to what Watchtower does and how to use it.

            Cover these features in order, briefly (1-2 sentences each):
            1. **Briefings** — personalized daily morning overview combining all insights (needs attention, your day, what happened, team pulse, coaching)
            2. **Inbox** — messages awaiting your response (@mentions and DMs), auto-detected and AI-prioritized
            3. **Tasks** — personal action items you create from tracks, briefings, digests, or inbox items
            4. **Chat** (this tab!) — ask questions about your workspace, activity, decisions, people
            5. **Tracks** — auto-generated narratives about ongoing initiatives across channels
            6. **Digests** — AI summaries of channel activity, decisions, and trends
            7. **People** — team member profiles with communication style, activity patterns
            8. **Statistics** — channel analytics, digest coverage, recommendations to mute noisy channels

            Then mention:
            - The background daemon syncs Slack data automatically and runs AI pipelines after each sync
            - They can rate AI quality with thumbs up/down to improve results over time
            - Settings (⌘,) let them configure sync frequency, language, notifications, etc.

            End with a friendly invitation to ask anything or explore the tabs on the left.
            """)

        if !profile.role.isEmpty {
            parts.append("User's role: \(profile.role). Tailor examples to this role.")
        }
        if !profile.painPoints.isEmpty, profile.painPoints != "[]" {
            parts.append("User's pain points: \(profile.painPoints). Mention which features address these.")
        }

        parts.append("""
            Format: use **bold** for feature names, keep total length under 400 words. \
            Be warm but not cheesy. No emojis unless the language culturally expects them.
            """)

        return parts.joined(separator: "\n")
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
