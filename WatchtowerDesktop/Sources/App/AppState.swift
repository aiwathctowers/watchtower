import SwiftUI
import GRDB

@MainActor
@Observable
final class AppState {
    var selectedDestination: SidebarDestination = .chat
    var databaseManager: DatabaseManager?
    var errorMessage: String?
    var isDBAvailable: Bool { databaseManager != nil }

    /// Whether the user needs to complete the onboarding chat flow.
    var needsOnboarding: Bool = false

    /// Cache for custom workspace emoji images.
    let emojiImageCache = EmojiImageCache()
    /// Map of custom emoji name → image URL, loaded from DB.
    var customEmojiMap: [String: String] = [:]

    /// Persistent chat ViewModels — survive tab switches.
    private(set) var chatViewModel: ChatViewModel?
    private(set) var chatHistoryViewModel: ChatHistoryViewModel?

    /// Tracks status filter (nil = inbox+active).
    var trackStatusFilter: String?

    /// Tracks ownership filter (nil = all ownerships).
    var trackOwnershipFilter: String?

    /// Set to navigate to a specific digest from anywhere in the app.
    var pendingDigestID: Int?

    /// Watches for new digests and sends notifications.
    private(set) var digestWatcher: DigestWatcher?

    /// Manages app updates from GitHub Releases.
    let updateService = UpdateService()

    /// Manages background pipeline tasks (digests, tracks) started after onboarding sync.
    let backgroundTaskManager = BackgroundTaskManager()

    /// Ensures chat ViewModels exist (lazy init, called from ChatView).
    func ensureChatViewModels() {
        guard let db = databaseManager, chatViewModel == nil else { return }
        let cvm = ChatViewModel(claudeService: ClaudeService(), dbManager: db)
        let hvm = ChatHistoryViewModel(dbManager: db)
        hvm.load()

        cvm.onConversationUpdated = { [weak hvm] convID, title, sessionID in
            guard let hvm else { return }
            if let title { hvm.updateTitle(convID, title: title) }
            if let sessionID { hvm.updateSessionID(convID, sessionID: sessionID) }
            if title == nil && sessionID == nil { hvm.touch(convID) }
        }

        chatViewModel = cvm
        chatHistoryViewModel = hvm
    }

    func navigateToDigest(_ digestID: Int) {
        pendingDigestID = digestID
        selectedDestination = .digests
    }

    func initialize() {
        Task {
            do {
                let manager = try await Task.detached {
                    // Run Go CLI to apply any pending DB migrations before opening
                    DatabaseManager.runCLIMigrations()
                    let dbPath = try DatabaseManager.resolveDBPath()
                    return try DatabaseManager(path: dbPath)
                }.value
                databaseManager = manager
                errorMessage = nil
                needsOnboarding = await checkNeedsOnboarding(dbPool: manager.dbPool)
                loadCustomEmoji(from: manager)
                startDigestWatcher(dbPool: manager.dbPool)
            } catch {
                errorMessage = error.localizedDescription
                databaseManager = nil
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
        needsOnboarding = false
    }

    /// Re-triggers the onboarding flow (from Settings).
    func startOnboarding() {
        needsOnboarding = true
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
