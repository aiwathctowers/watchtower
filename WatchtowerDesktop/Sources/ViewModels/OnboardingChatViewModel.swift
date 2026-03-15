import Foundation
import GRDB

/// Quick reply option with associated action.
struct QuickReply: Identifiable {
    let id: UUID
    let label: String
    let action: () -> Void

    init(label: String, action: @escaping () -> Void) {
        self.id = UUID()
        self.label = label
        self.action = action
    }
}

/// ViewModel for the onboarding chat flow.
/// Manages: AI conversation, chat result parsing, team form state, profile generation.
@MainActor
@Observable
final class OnboardingChatViewModel {
    // MARK: - Chat State

    var messages: [ChatMessage] = []
    var isStreaming = false
    var inputText = ""
    var errorMessage: String?

    // MARK: - Parsed Profile Data (from chat)

    var role = ""
    var team = ""
    var painPoints: [String] = []
    var trackFocus: [String] = []

    // MARK: - Role Determination

    var hasAnsweredRoleQ1 = false
    var hasAnsweredRoleQ2 = false
    var hasAnsweredRoleQ3 = false
    var roleDetermination: RoleDetermination?

    var determinedRole: RoleLevel? {
        roleDetermination?.roleLevel
    }

    /// Check if Q3 (manage managers) should be shown
    var shouldShowRoleQ3: Bool {
        hasAnsweredRoleQ1 && hasAnsweredRoleQ2 &&
        (roleDetermination?.reportsToThem ?? false) &&
        (roleDetermination?.setStrategy ?? false)
    }

    /// Whether the role questionnaire is fully complete.
    var isRoleDetermined: Bool {
        guard hasAnsweredRoleQ1, hasAnsweredRoleQ2 else { return false }
        if shouldShowRoleQ3 { return hasAnsweredRoleQ3 }
        return true
    }

    // MARK: - Team Form State

    var reportIDs: [String] = []
    var managerID: String = ""
    var peerIDs: [String] = []
    var allUsers: [User] = []

    /// Set to true when AI signals it has gathered enough info (via [READY] marker).
    var chatReady = false

    // MARK: - Private

    private static let readyMarker = "[READY]"
    private var sessionID: String?
    private let claudeService: any ClaudeServiceProtocol
    private var dbManager: DatabaseManager?
    private var streamTask: Task<Void, Never>?
    private var chatCompleted = false

    /// The UI language selected during onboarding settings step.
    let language: String

    init(claudeService: any ClaudeServiceProtocol, language: String = "English", dbManager: DatabaseManager? = nil) {
        self.claudeService = claudeService
        self.language = language
        self.dbManager = dbManager
        if dbManager != nil { loadUsers() }
    }

    /// Set database after initialization (e.g. when sync completes and DB becomes available).
    func setDatabase(_ db: DatabaseManager) {
        self.dbManager = db
        loadUsers()
    }

    // MARK: - Questionnaire in Chat

    /// The current quick-reply options shown below the chat. Empty = show text input.
    var quickReplies: [QuickReply] = []

    /// Insert the first role question as a chat bubble.
    func startQuestionnaire() {
        guard messages.isEmpty else { return }
        addAssistantBubble(loc("q1"))
        quickReplies = [
            QuickReply(label: loc("yes"), action: { [weak self] in self?.answerRoleQ1(reportsToThem: true) }),
            QuickReply(label: loc("no"), action: { [weak self] in self?.answerRoleQ1(reportsToThem: false) }),
        ]
    }

    private func answerRoleQ1(reportsToThem: Bool) {
        addUserBubble(reportsToThem ? loc("yes") : loc("no"))
        recordRoleAnswer(reportsToThem: reportsToThem)

        if reportsToThem {
            addAssistantBubble(loc("q2a"))
            quickReplies = [
                QuickReply(label: loc("yes"), action: { [weak self] in self?.answerRoleQ2a(setStrategy: true) }),
                QuickReply(label: loc("no"), action: { [weak self] in self?.answerRoleQ2a(setStrategy: false) }),
            ]
        } else {
            addAssistantBubble(loc("q2b"))
            quickReplies = [
                QuickReply(label: loc("expertise"), action: { [weak self] in self?.answerRoleQ2b(influenceType: "expertise") }),
                QuickReply(label: loc("tasks"), action: { [weak self] in self?.answerRoleQ2b(influenceType: "tasks") }),
            ]
        }
    }

    private func answerRoleQ2a(setStrategy: Bool) {
        addUserBubble(setStrategy ? loc("yes") : loc("no"))
        recordRoleAnswer(setStrategy: setStrategy)

        if setStrategy {
            // Need Q3
            addAssistantBubble(loc("q3"))
            quickReplies = [
                QuickReply(label: loc("yes"), action: { [weak self] in self?.answerRoleQ3(manageManagers: true) }),
                QuickReply(label: loc("no"), action: { [weak self] in self?.answerRoleQ3(manageManagers: false) }),
            ]
        } else {
            finishQuestionnaire()
        }
    }

    private func answerRoleQ2b(influenceType: String) {
        addUserBubble(influenceType == "expertise" ? loc("expertise") : loc("tasks"))
        recordRoleAnswer(influenceType: influenceType)
        finishQuestionnaire()
    }

    private func answerRoleQ3(manageManagers: Bool) {
        addUserBubble(manageManagers ? loc("yes") : loc("no"))
        recordRoleAnswer(manageManagers: manageManagers)
        finishQuestionnaire()
    }

    private func finishQuestionnaire() {
        quickReplies = []
        initiateChat()
    }

    private func addAssistantBubble(_ text: String) {
        messages.append(ChatMessage(id: UUID(), role: .assistant, text: text, timestamp: Date(), isStreaming: false))
    }

    private func addUserBubble(_ text: String) {
        messages.append(ChatMessage(id: UUID(), role: .user, text: text, timestamp: Date(), isStreaming: false))
    }

    // MARK: - Chat

    /// AI sends the first message after role is determined — greets the user and asks the first question.
    func initiateChat() {
        guard !isStreaming else { return }

        let roleName = determinedRole?.displayName ?? "your role"
        let langInstruction = language != "English" ? " Respond in \(language)." : ""
        let hiddenPrompt = "The user just completed the role questionnaire and was identified as: \(roleName). " +
            "Greet them briefly, acknowledge the role, and ask your first question." + langInstruction

        let assistantMsg = ChatMessage(id: UUID(), role: .assistant, text: "", timestamp: Date(), isStreaming: true)
        messages.append(assistantMsg)
        isStreaming = true

        streamTask = Task { [weak self] in
            guard let self else { return }
            do {
                let stream = claudeService.stream(
                    prompt: hiddenPrompt,
                    systemPrompt: Self.onboardingSystemPrompt(language: self.language),
                    sessionID: nil,
                    dbPath: nil
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
            }
            self.isStreaming = false
        }
    }

    func send() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isStreaming else { return }

        streamTask?.cancel()
        inputText = ""

        let userMsg = ChatMessage(id: UUID(), role: .user, text: text, timestamp: Date(), isStreaming: false)
        messages.append(userMsg)

        let assistantMsg = ChatMessage(id: UUID(), role: .assistant, text: "", timestamp: Date(), isStreaming: true)
        messages.append(assistantMsg)
        isStreaming = true

        let currentSessionID = sessionID

        streamTask = Task { [weak self] in
            guard let self else { return }

            // Always use onboarding system prompt throughout the conversation
            let systemPrompt = Self.onboardingSystemPrompt(language: self.language)

            do {
                let stream = claudeService.stream(
                    prompt: text,
                    systemPrompt: systemPrompt,
                    sessionID: currentSessionID,
                    dbPath: nil
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
                self.stripReadyMarker(at: idx)
            }
            self.isStreaming = false
        }
    }

    /// Finish the chat phase and parse results from the conversation.
    func finishChat() {
        streamTask?.cancel()
        streamTask = nil
        isStreaming = false
        chatCompleted = true
        parseProfileFromChat()
    }

    /// Record role determination answer from UI questions.
    func recordRoleAnswer(reportsToThem: Bool? = nil, setStrategy: Bool? = nil, manageManagers: Bool? = nil, influenceType: String? = nil) {
        if let reportsToThem {
            roleDetermination = RoleDetermination(
                reportsToThem: reportsToThem,
                setStrategy: roleDetermination?.setStrategy ?? false,
                manageManagers: roleDetermination?.manageManagers,
                influenceType: roleDetermination?.influenceType
            )
            hasAnsweredRoleQ1 = true
        }

        if let setStrategy {
            roleDetermination = RoleDetermination(
                reportsToThem: roleDetermination?.reportsToThem ?? false,
                setStrategy: setStrategy,
                manageManagers: roleDetermination?.manageManagers,
                influenceType: roleDetermination?.influenceType
            )
            hasAnsweredRoleQ2 = true
        }

        if let manageManagers {
            roleDetermination = RoleDetermination(
                reportsToThem: roleDetermination?.reportsToThem ?? false,
                setStrategy: roleDetermination?.setStrategy ?? false,
                manageManagers: manageManagers,
                influenceType: roleDetermination?.influenceType
            )
            hasAnsweredRoleQ3 = true
        }

        if let influenceType {
            roleDetermination = RoleDetermination(
                reportsToThem: roleDetermination?.reportsToThem ?? false,
                setStrategy: roleDetermination?.setStrategy ?? false,
                manageManagers: roleDetermination?.manageManagers,
                influenceType: influenceType
            )
            hasAnsweredRoleQ2 = true
        }

        // Update role string from determined role
        if let determined = determinedRole {
            role = determined.rawValue
        }
    }

    // MARK: - Profile Generation

    /// Generate custom_prompt_context via LLM based on collected profile data.
    func generatePromptContext() async {
        let profileSummary = buildProfileSummary()
        let prompt = """
        Based on the following user profile, generate a concise context paragraph that will be \
        injected into AI prompts to personalize Slack workspace analysis.

        The context should describe who the user is, what they care about, and how to prioritize \
        information for them. Write in English, 3-5 sentences.

        PROFILE:
        Role: \(role)
        Team: \(team)
        Pain points: \(painPoints.joined(separator: ", "))
        Track focus: \(trackFocus.joined(separator: ", "))
        Reports: \(reportIDs.count) direct reports
        Has manager: \(managerID.isEmpty ? "no" : "yes")
        Peers: \(peerIDs.count) key peers

        Return ONLY the context paragraph, no explanation.
        """

        var contextText = ""
        do {
            for try await event in claudeService.stream(prompt: prompt, systemPrompt: nil, sessionID: nil, dbPath: nil) {
                switch event {
                case .text(let chunk): contextText += chunk
                case .turnComplete(let text): contextText = text
                case .sessionID, .done: break
                }
            }
        } catch {
            // Fallback: use the profile summary directly
            contextText = profileSummary
        }

        // Save profile
        let currentUserID = getCurrentUserID()
        guard !currentUserID.isEmpty else { return }
        guard let dbManager else {
            errorMessage = "Database not available"
            return
        }

        // Read existing profile to preserve onboardingDone state.
        let existingProfile: UserProfile? = try? await dbManager.dbPool.read { db in
            try ProfileQueries.fetchProfile(db, slackUserID: currentUserID)
        }

        let profile = UserProfile(
            slackUserID: currentUserID,
            role: role,
            team: team,
            reports: encodeJSON(reportIDs),
            peers: encodeJSON(peerIDs),
            manager: managerID,
            painPoints: encodeJSON(painPoints),
            trackFocus: encodeJSON(trackFocus),
            onboardingDone: existingProfile?.onboardingDone ?? false,
            customPromptContext: contextText.trimmingCharacters(in: .whitespacesAndNewlines)
        )

        do {
            try await dbManager.dbPool.write { db in
                try ProfileQueries.upsertProfile(db, profile: profile)
            }
        } catch {
            errorMessage = "Failed to save profile: \(error.localizedDescription)"
        }
    }

    /// Mark onboarding as complete in the profile.
    func markOnboardingDone() async {
        let currentUserID = getCurrentUserID()
        guard !currentUserID.isEmpty else { return }
        guard let dbManager else { return }

        do {
            try await dbManager.dbPool.write { db in
                try db.execute(sql: """
                    UPDATE user_profile SET onboarding_done = 1,
                        updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
                    WHERE slack_user_id = ?
                    """, arguments: [currentUserID])
            }
        } catch {
            errorMessage = "Failed to complete onboarding: \(error.localizedDescription)"
        }
    }

    // MARK: - Private Helpers

    /// Check for [READY] marker in the last assistant message, strip it, and set chatReady.
    private func stripReadyMarker(at idx: Int) {
        let text = messages[idx].text
        if text.contains(Self.readyMarker) {
            messages[idx].text = text
                .replacingOccurrences(of: Self.readyMarker, with: "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            chatReady = true
        }
    }

    private func loadUsers() {
        guard let dbManager else { allUsers = []; return }
        do {
            allUsers = try dbManager.dbPool.read { db in
                try UserQueries.fetchAll(db, activeOnly: true)
            }
        } catch {
            allUsers = []
        }
    }

    private func getCurrentUserID() -> String {
        guard let dbManager else { return "" }
        return (try? dbManager.dbPool.read { db in
            try String.fetchOne(db, sql: "SELECT current_user_id FROM workspace LIMIT 1")
        }) ?? ""
    }

    /// Parse the AI conversation to extract role, team, pain points, track focus.
    private func parseProfileFromChat() {
        let assistantMessages = messages
            .filter { $0.role == .assistant }
            .map { $0.text }
            .joined(separator: "\n")
        let userMessages = messages
            .filter { $0.role == .user }
            .map { $0.text }
            .joined(separator: "\n")

        // Simple heuristic extraction from user messages
        // The AI will have asked about role, pain points, etc. — the user's answers contain the data.
        // We keep it simple: store raw text, the LLM will generate proper context in generatePromptContext.

        // Try to detect role keywords only if the questionnaire didn't already determine a role.
        // Otherwise keyword matching can overwrite the structured questionnaire result
        // (e.g. user mentions "devops" when describing their team, not their role).
        if roleDetermination == nil {
            let roleKeywords = ["engineering manager", "tech lead", "product manager",
                               "software engineer", "data scientist", "staff engineer",
                               "designer", "devops", "director", "principal",
                               "cto", "vp", "em", "tl", "pm", "swe", "ic"]
            let lowerUser = userMessages.lowercased()
            for keyword in roleKeywords {
                // For short keywords (≤3 chars), require word boundaries to avoid false positives.
                if keyword.count <= 3 {
                    let pattern = "\\b\(NSRegularExpression.escapedPattern(for: keyword))\\b"
                    if lowerUser.range(of: pattern, options: .regularExpression) != nil {
                        role = keyword.uppercased()
                        break
                    }
                } else if lowerUser.contains(keyword) {
                    role = keyword.capitalized
                    break
                }
            }
        }

        // Try to extract team name from user messages.
        // Look for patterns like "my team is X", "I'm on the X team", "team: X", etc.
        if team.isEmpty {
            let teamPatterns = [
                "(?:my team is|i'm on the|i am on the|team:|our team is|work on the|work in the)\\s+([\\w\\s&/-]+?)(?:\\s*[.,!?]|\\s+team|$)",
                "([\\w\\s&/-]+?)\\s+team\\b"
            ]
            for pattern in teamPatterns {
                if let regex = try? NSRegularExpression(pattern: pattern, options: .caseInsensitive),
                   let match = regex.firstMatch(in: userMessages, range: NSRange(userMessages.startIndex..., in: userMessages)),
                   match.numberOfRanges > 1,
                   let range = Range(match.range(at: 1), in: userMessages) {
                    let extracted = String(userMessages[range]).trimmingCharacters(in: .whitespaces)
                    if !extracted.isEmpty && extracted.count <= 40 {
                        team = extracted.capitalized
                        break
                    }
                }
            }
        }

        // Extract pain points from user messages using word-boundary matching
        let painPointKeywords = [
            "missing": "Missing important messages while AFK",
            "decisions": "Decisions getting lost in threads",
            "tracking": "Losing track of who owes what",
            "lose track": "Losing track of who owes what",
            "what team": "Can't tell what team is working on",
            "deadlines": "Deadlines discussed in chat get forgotten",
            "urgent": "Hard to tell what's urgent vs can wait",
            "prioritize": "Hard to tell what's urgent vs can wait",
        ]
        let lowerUserMessages = userMessages.lowercased()
        for (key, value) in painPointKeywords {
            if lowerUserMessages.contains(key) && !painPoints.contains(value) {
                painPoints.append(value)
            }
        }
    }

    private func buildProfileSummary() -> String {
        var parts: [String] = []
        if !role.isEmpty { parts.append("Role: \(role)") }
        if !team.isEmpty { parts.append("Team: \(team)") }
        if !painPoints.isEmpty { parts.append("Focus areas: \(painPoints.joined(separator: ", "))") }
        if !trackFocus.isEmpty { parts.append("Tracking: \(trackFocus.joined(separator: ", "))") }
        return parts.joined(separator: ". ")
    }

    private func encodeJSON(_ array: [String]) -> String {
        guard let data = try? JSONEncoder().encode(array),
              let str = String(data: data, encoding: .utf8) else { return "[]" }
        return str
    }

    // MARK: - Localized Strings

    /// Localized question and button texts keyed by language.
    private static let strings: [String: [String: String]] = [
        "English": [
            "q1": "Let's understand your role. Do people report to you?",
            "q2a": "Do you determine strategy or vision for your area?",
            "q2b": "Your influence in the organization comes mainly through...",
            "q3": "Do you manage other managers?",
            "yes": "Yes",
            "no": "No",
            "expertise": "Expertise & authority",
            "tasks": "Solving tasks",
            "header": "Tell us about yourself",
            "subtitle": "Watchtower will personalize your experience based on your role and needs.",
            "continue": "Continue",
        ],
        "Russian": [
            "q1": "Давайте определим вашу роль. Вам кто-то подчиняется?",
            "q2a": "Вы определяете стратегию или видение для вашего направления?",
            "q2b": "Ваше влияние в организации основано главным образом на...",
            "q3": "Вы управляете другими руководителями?",
            "yes": "Да",
            "no": "Нет",
            "expertise": "Экспертизе и авторитете",
            "tasks": "Решении задач",
            "header": "Расскажите о себе",
            "subtitle": "Watchtower персонализирует ваш опыт на основе вашей роли и потребностей.",
            "continue": "Продолжить",
        ],
        "Ukrainian": [
            "q1": "Давайте визначимо вашу роль. Вам хтось підпорядковується?",
            "q2a": "Ви визначаєте стратегію або бачення для вашого напрямку?",
            "q2b": "Ваш вплив в організації базується переважно на...",
            "q3": "Ви керуєте іншими керівниками?",
            "yes": "Так",
            "no": "Ні",
            "expertise": "Експертизі та авторитеті",
            "tasks": "Вирішенні завдань",
            "header": "Розкажіть про себе",
            "subtitle": "Watchtower персоналізує ваш досвід на основі вашої ролі та потреб.",
            "continue": "Продовжити",
        ],
    ]

    /// Look up a localized string, falling back to English.
    func loc(_ key: String) -> String {
        Self.strings[language]?[key] ?? Self.strings["English"]![key]!
    }

    // MARK: - System Prompt

    static func onboardingSystemPrompt(language: String) -> String {
        let langRule: String
        switch language {
        case "Russian":
            langRule = "- IMPORTANT: Respond in Russian (Русский). All your messages must be in Russian."
        case "Ukrainian":
            langRule = "- IMPORTANT: Respond in Ukrainian (Українська). All your messages must be in Ukrainian."
        default:
            langRule = "- Respond in English."
        }

        return """
        You are Watchtower's onboarding assistant. Your goal is to learn exactly 3 things from the user:

        1. **Role & Team**: What's their position? (Engineering Manager, IC, Tech Lead, PM, etc?) And what team are they on?
        2. **Pain Points**: What's their main pain point with Slack? (missing messages, decisions lost in threads, losing track of tasks, etc.)
        3. **Track Focus**: What should Watchtower focus on for them? (team blockers, code reviews, decisions, deadlines, etc.)

        INSTRUCTIONS:
        - Ask ONE question at a time
        - Be concise (1-2 sentences per message)
        - After you get clear answers to ALL THREE questions, write a brief summary and then on a new line write exactly:
        [READY]

        CRITICAL: You must end with [READY] on its own line when you have answers to all 3 questions. Do not continue asking questions after that.

        \(langRule)
        """
    }
}
