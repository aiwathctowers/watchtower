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

    var isExtractingProfile = false
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

    /// Set to true when AI signals it has gathered enough info (via [READY] marker),
    /// or when fallback triggers after enough user messages.
    var chatReady = false

    /// Number of free-form user messages sent (after role questionnaire).
    var userMessageCount = 0

    /// Fallback: show Continue button after this many user messages even without [READY].
    /// Set high so the LLM can finish its full interview (4-6 questions) via [READY] marker first.
    private static let fallbackMessageCount = 10

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

        let roleName = determinedRole?.displayName ?? "unknown"
        let roleDesc = determinedRole?.shortDescription ?? ""
        let langInstruction = language != "English" ? " Respond in \(language)." : ""
        let hiddenPrompt = "The user completed the role questionnaire. Role level: \(roleName) (\(roleDesc)). " +
            "Greet them briefly (1 sentence), acknowledge their role, and ask your first question about their team and domain." + langInstruction

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
        userMessageCount += 1

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
            // Fallback: if AI didn't send [READY] but user answered enough questions, allow proceeding
            if !self.chatReady && self.userMessageCount >= Self.fallbackMessageCount {
                self.chatReady = true
            }
            self.isStreaming = false
        }
    }

    /// Finish the chat phase and extract profile data from the conversation via LLM.
    func finishChat() async {
        streamTask?.cancel()
        streamTask = nil
        isStreaming = false
        chatCompleted = true
        isExtractingProfile = true
        await parseProfileFromChat()
        isExtractingProfile = false
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

        // Role string will be extracted from AI conversation in parseProfileFromChat()
    }

    // MARK: - Profile Generation

    /// Generate custom_prompt_context via LLM based on the full onboarding conversation.
    func generatePromptContext() async {
        let conversationTranscript = messages
            .filter { $0.role == .user || $0.role == .assistant }
            .map { "\($0.role == .user ? "USER" : "ASSISTANT"): \($0.text)" }
            .joined(separator: "\n")

        let roleName = determinedRole?.displayName ?? role
        let roleDesc = determinedRole?.shortDescription ?? ""

        let prompt = """
        Based on the onboarding conversation below, generate a detailed profile context that will be \
        injected into AI prompts to personalize Slack workspace analysis for this user.

        The context will be used by 4 AI features:
        1. Digests — daily/weekly Slack summaries (what to prioritize, what to skip)
        2. Tracks — personal task extraction from Slack (what counts as a task for this user)
        3. People Analytics — team communication analysis (what patterns to watch for)
        4. Action Items — requests/tasks needing attention (what's actionable for this user)

        Write a comprehensive profile context (5-10 sentences) in English that covers:
        - Who this person is (role, team, domain)
        - What they're responsible for and what decisions they make
        - What information is most important to them
        - What they want prioritized vs filtered out
        - What team dynamics they care about
        - What kind of tasks/tracks are relevant for them

        ADDITIONAL INFO:
        Role level: \(roleName) (\(roleDesc))
        Reports: \(reportIDs.count) direct reports
        Has manager: \(!managerID.isEmpty)
        Peers: \(peerIDs.count) key peers

        Return ONLY the context text, no explanation or formatting.

        === ONBOARDING CONVERSATION ===
        \(conversationTranscript)
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
            // Fallback: use the conversation transcript directly
            contextText = buildProfileSummary()
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

    /// Minimum user answers before secondary "no question" heuristic kicks in.
    private static let minAnswersForNoQuestionHeuristic = 6

    /// Check for [READY] marker in the last assistant message, strip it, and set chatReady.
    /// Also applies secondary heuristic: if the LLM stopped asking questions after enough answers,
    /// consider the interview complete.
    private func stripReadyMarker(at idx: Int) {
        let text = messages[idx].text

        // Primary: detect [READY] marker (case-insensitive)
        if let range = text.range(of: Self.readyMarker, options: .caseInsensitive) {
            messages[idx].text = text.replacingCharacters(in: range, with: "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            chatReady = true
            return
        }

        // Secondary: if LLM response contains no question mark and user has given enough answers,
        // the LLM has finished the interview (wrote a summary without asking another question).
        if !chatReady &&
            userMessageCount >= Self.minAnswersForNoQuestionHeuristic &&
            !text.contains("?") {
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

    /// Extract role, team, and pain points from the onboarding conversation via LLM.
    private func parseProfileFromChat() async {
        let transcript = messages
            .filter { $0.role == .user || $0.role == .assistant }
            .map { "\($0.role == .user ? "USER" : "ASSISTANT"): \($0.text)" }
            .joined(separator: "\n")

        let roleLevelHint = determinedRole?.displayName ?? "unknown"

        let extractionPrompt = """
        Extract the user's job title and team name from this onboarding conversation.

        The user's organizational level is: \(roleLevelHint) (from a questionnaire).
        This is NOT their job title — extract the ACTUAL job title they mentioned (e.g. "Engineering Manager", \
        "Staff Backend Engineer", "Head of Platform", "Product Designer").

        If the user never explicitly stated their job title, infer the most likely one from context \
        (their responsibilities, what they manage, their domain).

        Return ONLY valid JSON, no markdown, no explanation:
        {"role": "their job title", "team": "their team name", "pain_points": ["point1", "point2"]}

        Rules:
        - "role": The user's actual job title / position. NOT the organizational level.
        - "team": The team or department name. Empty string if not mentioned.
        - "pain_points": List of specific problems they want Watchtower to solve. Empty array if none mentioned.
        - All values in English regardless of conversation language.

        === CONVERSATION ===
        \(transcript)
        """

        var responseText = ""
        do {
            for try await event in claudeService.stream(prompt: extractionPrompt, systemPrompt: nil, sessionID: nil, dbPath: nil) {
                switch event {
                case .text(let chunk): responseText += chunk
                case .turnComplete(let text): responseText = text
                case .sessionID, .done: break
                }
            }
        } catch {
            // Fallback: keep whatever we have from questionnaire
            return
        }

        // Parse JSON response
        let cleaned = responseText
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "```json", with: "")
            .replacingOccurrences(of: "```", with: "")
            .trimmingCharacters(in: .whitespacesAndNewlines)

        guard let data = cleaned.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return
        }

        if let extractedRole = json["role"] as? String, !extractedRole.isEmpty {
            role = extractedRole
        }
        if let extractedTeam = json["team"] as? String, !extractedTeam.isEmpty {
            team = extractedTeam
        }
        if let extractedPainPoints = json["pain_points"] as? [String] {
            for point in extractedPainPoints where !painPoints.contains(point) {
                painPoints.append(point)
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
            langRule = "- LANGUAGE: Respond in Russian (Русский). All your messages MUST be in Russian."
        case "Ukrainian":
            langRule = "- LANGUAGE: Respond in Ukrainian (Українська). All your messages MUST be in Ukrainian."
        default:
            langRule = "- LANGUAGE: Respond in English."
        }

        return """
        You are the onboarding assistant for Watchtower — a tool that monitors a Slack workspace and provides:

        1. **Digests** — daily/weekly summaries of what happened in channels: key decisions, topics, action items
        2. **Tracks** — personal task tracker extracted from Slack: requests directed at the user, assignments, commitments, follow-ups
        3. **People Analytics** — communication pattern analysis for team members: engagement, decision-making style, red flags, accomplishments
        4. **Action Items** — tasks and requests that need the user's attention, extracted from messages

        The user's ROLE has already been determined via a questionnaire. Do NOT ask about their role level.

        YOUR TASK: Conduct a brief interview (4-6 questions) to understand the user well enough to personalize ALL of the above features. Ask ONE question at a time. Be concise (1-3 sentences per message).

        You need to learn:
        1. **Team & domain**: What team are they on? What does the team do? What projects/products do they own?
        2. **Responsibilities**: What are they personally responsible for? What decisions do they make? What do they delegate?
        3. **Information needs**: What do they currently miss in Slack? What's the most important information they need but struggle to find? (e.g., decisions made while AFK, tasks assigned in threads, status of delegated work)
        4. **Priorities**: What should Watchtower prioritize for them? (e.g., blockers and deadlines vs strategic decisions vs team health signals)
        5. **Team dynamics**: What do they watch for in their team? (e.g., someone going silent, unresolved conflicts, missed commitments, workload imbalance)

        ADAPT your questions to the user's role:
        - For managers: focus on delegation, team oversight, decision tracking, people signals
        - For ICs: focus on personal task tracking, code reviews, blocking requests, staying informed

        DO NOT ask generic questions. DO NOT ask about tools (JIRA, etc.) — Watchtower only works with Slack.
        DO NOT ask more than 6 questions total. Keep it conversational and natural.

        WHEN DONE: After you have enough information (4+ answers), write a brief summary of what you learned, then on a NEW LINE write exactly:
        [READY]

        You MUST write [READY] after the summary. Do not skip it. Do not continue asking after it.

        \(langRule)
        """
    }
}
