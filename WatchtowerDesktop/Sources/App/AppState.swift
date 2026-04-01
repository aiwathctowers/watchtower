import SwiftUI
import GRDB

@MainActor
@Observable
final class AppState {
    var selectedDestination: SidebarDestination = .chat
    var databaseManager: DatabaseManager?
    var errorMessage: String?
    var isDBAvailable: Bool { databaseManager != nil }

    /// True while initialize() is running (before DB and onboarding check complete).
    var isLoading: Bool = true

    /// Whether the user needs to complete the onboarding chat flow.
    var needsOnboarding: Bool = false

    /// Persistent onboarding state machine — tracks which step the user is on across app restarts.
    let onboarding = OnboardingStateMachine()

    /// Cache for custom workspace emoji images.
    let emojiImageCache = EmojiImageCache()
    /// Map of custom emoji name → image URL, loaded from DB.
    var customEmojiMap: [String: String] = [:]

    /// Persistent chat ViewModels — survive tab switches.
    private(set) var chatViewModel: ChatViewModel?
    private(set) var chatHistoryViewModel: ChatHistoryViewModel?

    /// Whether legacy people analytics is enabled (analysis.legacy_mode in config).
    var analysisLegacyMode: Bool = false

    /// Whether the user has completed onboarding (profile exists and onboarding_done == true).
    var profileComplete: Bool = true

    /// Set to navigate to a specific digest from anywhere in the app.
    var pendingDigestID: Int?

    /// Set to navigate to a specific task from anywhere in the app.
    var pendingTaskID: Int?

    /// Watches for new digests and sends notifications.
    private(set) var digestWatcher: DigestWatcher?

    /// Manages app updates from GitHub Releases.
    let updateService = UpdateService()

    /// Manages background pipeline tasks (digests, people) started after onboarding sync.
    let backgroundTaskManager = BackgroundTaskManager()

    /// Ensures chat ViewModels exist (lazy init, called from ChatView).
    func ensureChatViewModels() {
        guard let db = databaseManager, chatViewModel == nil else { return }
        let configProvider = ConfigService().aiProvider
        let provider: AIProvider = configProvider == "codex" ? .codex : .claude
        let service = ChatViewModel.createService(for: provider)
        let cvm = ChatViewModel(aiService: service, dbManager: db, provider: provider)
        let hvm = ChatHistoryViewModel(dbManager: db)
        hvm.load { [weak self, weak cvm, weak hvm] in
            self?.maybeCreateWelcomeChat(chatVM: cvm, historyVM: hvm)
        }

        cvm.onConversationUpdated = { [weak hvm] convID, title, sessionID in
            guard let hvm else { return }
            if let title { hvm.updateTitle(convID, title: title) }
            if let sessionID { hvm.updateSessionID(convID, sessionID: sessionID) }
            if title == nil && sessionID == nil { hvm.touch(convID) }
        }

        chatViewModel = cvm
        chatHistoryViewModel = hvm
    }

    /// Creates a welcome chat with AI greeting when no conversations exist and user profile is available.
    private func maybeCreateWelcomeChat(chatVM: ChatViewModel?, historyVM: ChatHistoryViewModel?) {
        guard let chatVM, let historyVM, let db = databaseManager else { return }
        guard historyVM.conversations.isEmpty else { return }

        // Load user profile
        let profile: UserProfile? = try? db.dbPool.read { db in
            try ProfileQueries.fetchCurrentProfile(db)
        }
        guard let profile, profile.onboardingDone else { return }

        let language = ConfigService().digestLanguage ?? "English"

        // Create conversation and send welcome message
        guard let conv = historyVM.createConversation() else { return }
        chatVM.newChat()
        chatVM.bind(to: conv)
        historyVM.updateTitle(conv.id, title: "Welcome")
        chatVM.sendWelcomeMessage(profile: profile, language: language)
    }

    func navigateToDigest(_ digestID: Int) {
        pendingDigestID = digestID
        selectedDestination = .digests
    }

    func navigateToTask(_ taskID: Int) {
        pendingTaskID = taskID
        selectedDestination = .tasks
    }

    private var isInitializing = false

    func initialize() {
        guard !isInitializing else { return }
        isInitializing = true
        isLoading = true
        NotificationCenter.default.addObserver(
            forName: NSApplication.willTerminateNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.backgroundTaskManager.terminateProcessesSync()
        }
        Task {
            let splashStart = ContinuousClock.now
            do {
                let manager = try await Task.detached {
                    // Run Go CLI to apply any pending DB migrations before opening
                    DatabaseManager.runCLIMigrations()
                    let dbPath = try DatabaseManager.resolveDBPath()
                    return try DatabaseManager(path: dbPath)
                }.value
                databaseManager = manager
                errorMessage = nil
                // Sync state machine with DB: if profile says done, mark complete
                if onboarding.currentStep != .complete {
                    let dbDone = await checkNeedsOnboarding(dbPool: manager.dbPool)
                    if !dbDone {
                        onboarding.markComplete()
                    } else {
                        onboarding.skipCompleted()
                    }
                }
                needsOnboarding = onboarding.currentStep != .complete
                profileComplete = !needsOnboarding
                analysisLegacyMode = ConfigService().analysisLegacyMode
                // Hold splash for at least 2 seconds
                let elapsed = ContinuousClock.now - splashStart
                if elapsed < .seconds(2) {
                    try? await Task.sleep(for: .seconds(2) - elapsed)
                }
                isLoading = false
                loadCustomEmoji(from: manager)
                startDigestWatcher(dbPool: manager.dbPool)
                // Resume pipelines if app was closed mid-generation
                if !needsOnboarding && !UserDefaults.standard.bool(forKey: Constants.pipelinesCompletedKey) {
                    backgroundTaskManager.startPipelines(legacyPeople: analysisLegacyMode)
                } else if !needsOnboarding {
                    // Restart daemon so it picks up the latest CLI binary
                    restartDaemonIfRunning()
                }
            } catch {
                errorMessage = error.localizedDescription
                databaseManager = nil
                // No DB available — if state machine not complete, onboarding needed
                needsOnboarding = onboarding.currentStep != .complete
                if needsOnboarding {
                    onboarding.skipCompleted()
                }
                isLoading = false
            }
        }
        // Check for updates in background (once per 24h)
        Task {
            await updateService.checkIfNeeded()
        }
    }

    /// Check if onboarding chat is needed (profile missing or onboarding_done == false).
    private func checkNeedsOnboarding(dbPool: DatabasePool) async -> Bool {
        do {
            return try await dbPool.read { db in
                guard let profile = try ProfileQueries.fetchCurrentProfile(db) else {
                    return true
                }
                return !profile.onboardingDone
            }
        } catch {
            return false // On error, don't block — skip onboarding
        }
    }

    /// Called when onboarding flow completes successfully.
    func completeOnboarding() {
        onboarding.markComplete()
        needsOnboarding = false
        profileComplete = true
    }

    /// Re-triggers the onboarding flow (from Settings).
    /// Resets to the chat step since connect/settings/claude are already done.
    func startOnboarding() {
        onboarding.reset(to: .chat)
        needsOnboarding = true
        profileComplete = false
        UserDefaults.standard.removeObject(forKey: Constants.pipelinesCompletedKey)
    }

    /// Wipe all LLM-generated data, stop daemon, and re-run post-onboarding pipelines.
    func resetLLMData() async throws {
        guard let db = databaseManager else { return }

        // 1. Stop running pipelines (if any) — await ensures process exits and releases file locks
        await backgroundTaskManager.stopAll()

        // 2. Stop daemon
        let daemon = DaemonManager()
        daemon.resolvePathIfNeeded()
        if DaemonManager.checkDaemonRunning() {
            await daemon.stopDaemon()
            try? await Task.sleep(for: .milliseconds(500))
        }

        // 3. Wipe LLM-generated tables
        try db.wipeLLMData()

        // 4. Reset pipelines flag and re-run
        UserDefaults.standard.removeObject(forKey: Constants.pipelinesCompletedKey)
        backgroundTaskManager.tasks.removeAll()
        backgroundTaskManager.startPipelines(legacyPeople: analysisLegacyMode)
    }

    /// Restart the daemon so it picks up the latest CLI binary (e.g. after app update or dev rebuild).
    private func restartDaemonIfRunning() {
        Task {
            let daemon = DaemonManager()
            daemon.resolvePathIfNeeded()
            guard DaemonManager.checkDaemonRunning() else { return }
            await daemon.stopDaemon()
            try? await Task.sleep(for: .milliseconds(500))
            await daemon.startDaemon()
        }
    }

    private func startDigestWatcher(dbPool: DatabasePool) {
        Task {
            let granted = await NotificationService.shared.requestPermission()
            guard granted else { return }
            let watcher = DigestWatcher(dbPool: dbPool)
            self.digestWatcher = watcher
            watcher.start()
        }
    }

    private func loadCustomEmoji(from manager: DatabaseManager) {
        Task.detached {
            let map = try? await manager.dbPool.read { db in
                try CustomEmojiQueries.fetchEmojiMap(db)
            }
            await MainActor.run {
                self.customEmojiMap = map ?? [:]
            }
        }
    }
}
