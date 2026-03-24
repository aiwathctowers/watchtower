import Foundation

/// Unified onboarding step — replaces separate OnboardingStep + OnboardingChatPhase enums.
/// Persisted in UserDefaults so the user resumes from the last incomplete step on restart.
enum OnboardingStep: Int, CaseIterable, Comparable, Codable {
    case connect = 0      // Slack OAuth
    case settings = 1     // Language, model, history, sync frequency
    case claude = 2       // Claude CLI health check
    case chat = 3         // Role questionnaire + AI conversation (sync runs in background)
    case teamForm = 4     // Team form (reports, manager, peers)
    case generating = 5   // Profile generation via AI
    case complete = 6     // Done

    static func < (lhs: Self, rhs: Self) -> Bool {
        lhs.rawValue < rhs.rawValue
    }

    /// Title for the steps indicator (only first 4 shown as dots).
    var indicatorTitle: String? {
        switch self {
        case .connect: "Connect"
        case .settings: "Settings"
        case .claude: "AI Setup"
        case .chat, .teamForm, .generating: "Setup"
        case .complete: nil
        }
    }

    /// Steps shown in the visual indicator bar.
    static let indicatorSteps: [Self] = [.connect, .settings, .claude, .chat]
}

/// Manages onboarding progress with UserDefaults persistence.
@MainActor
@Observable
final class OnboardingStateMachine {
    private static let stepKey = "onboarding_current_step"
    private static let syncCompletedKey = "onboarding_sync_completed"
    private static let chatFinishedKey = "onboarding_chat_finished"

    /// The current step in the onboarding flow.
    private(set) var currentStep: OnboardingStep

    /// Whether the background sync has finished (persisted separately since sync runs in parallel).
    var syncCompleted: Bool {
        didSet { UserDefaults.standard.set(syncCompleted, forKey: Self.syncCompletedKey) }
    }

    /// Whether the user finished the chat phase (persisted so it survives restarts).
    var chatFinished: Bool {
        didSet { UserDefaults.standard.set(chatFinished, forKey: Self.chatFinishedKey) }
    }

    init() {
        if UserDefaults.standard.object(forKey: Self.stepKey) != nil {
            let raw = UserDefaults.standard.integer(forKey: Self.stepKey)
            self.currentStep = OnboardingStep(rawValue: raw) ?? .connect
        } else {
            self.currentStep = .connect
        }
        self.syncCompleted = UserDefaults.standard.bool(forKey: Self.syncCompletedKey)
        self.chatFinished = UserDefaults.standard.bool(forKey: Self.chatFinishedKey)
    }

    /// Advance to the next step.
    func advance() {
        guard let next = OnboardingStep(rawValue: currentStep.rawValue + 1) else { return }
        currentStep = next
        persist()
    }

    /// Jump to a specific step (e.g. for auto-skip or error recovery).
    func goTo(_ step: OnboardingStep) {
        currentStep = step
        persist()
    }

    /// Reset to a specific step (used by re-run from Settings).
    /// Also clears syncCompleted if going back before chat.
    func reset(to step: OnboardingStep = .connect) {
        currentStep = step
        if step <= .chat {
            syncCompleted = false
            chatFinished = false
        }
        persist()
    }

    /// Mark onboarding as fully complete and clean up UserDefaults.
    func markComplete() {
        currentStep = .complete
        syncCompleted = false
        chatFinished = false
        UserDefaults.standard.removeObject(forKey: Self.stepKey)
        UserDefaults.standard.removeObject(forKey: Self.syncCompletedKey)
        UserDefaults.standard.removeObject(forKey: Self.chatFinishedKey)
    }

    /// Check if a step should be auto-skipped based on current system state.
    func shouldSkip(_ step: OnboardingStep) -> Bool {
        switch step {
        case .connect:
            return FileManager.default.fileExists(atPath: Constants.configPath)
        case .settings:
            return false // Always let user review settings
        case .claude:
            return false // Always verify Claude works
        default:
            return false
        }
    }

    /// Advance past any steps that should be skipped. Returns the step we land on.
    @discardableResult
    func skipCompleted() -> OnboardingStep {
        while currentStep < .complete && shouldSkip(currentStep) {
            guard let next = OnboardingStep(rawValue: currentStep.rawValue + 1) else { break }
            currentStep = next
        }
        persist()
        return currentStep
    }

    private func persist() {
        UserDefaults.standard.set(currentStep.rawValue, forKey: Self.stepKey)
    }
}
