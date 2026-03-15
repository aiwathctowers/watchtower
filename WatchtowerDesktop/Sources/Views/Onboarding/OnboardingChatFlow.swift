import SwiftUI
import GRDB

/// The onboarding chat flow: AI learns about the user, then a team form appears.
///
/// Flow:
/// 1. Chat with AI to discover role, pain points, track focus
/// 2. Wait for sync if still running
/// 3. Team form (reports, manager, peers)
/// 4. Generate custom_prompt_context
/// 5. Mark onboarding_done = 1
struct OnboardingChatFlow: View {
    @Environment(AppState.self) private var appState
    @State private var configService: ConfigService?

    enum Phase {
        case chat
        case teamForm
        case generating
    }

    @State private var phase: Phase = .chat
    @State private var viewModel: OnboardingChatViewModel?

    var body: some View {
        VStack(spacing: 0) {
            switch phase {
            case .chat:
                if let vm = viewModel {
                    OnboardingChatView(viewModel: vm) {
                        phase = .teamForm
                    }
                } else {
                    ProgressView("Preparing...")
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }

            case .teamForm:
                if let vm = viewModel {
                    OnboardingTeamFormView(viewModel: vm) {
                        phase = .generating
                        Task {
                            await vm.generatePromptContext()
                            await vm.markOnboardingDone()
                            if vm.errorMessage == nil {
                                appState.completeOnboarding()
                            } else {
                                phase = .teamForm
                            }
                        }
                    }
                }

            case .generating:
                VStack(spacing: 16) {
                    ProgressView()
                    Text("Setting up your personalized experience...")
                        .foregroundStyle(.secondary)
                    if let error = viewModel?.errorMessage {
                        Text(error)
                            .foregroundStyle(.red)
                            .font(.caption)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .task {
            guard let db = appState.databaseManager else { return }
            // Load config to get user's language preference
            let configSvc = ConfigService()
            self.configService = configSvc
            let language = configSvc.digestLanguage ?? "English"

            let vm = OnboardingChatViewModel(
                claudeService: ClaudeService(),
                language: language,
                dbManager: db
            )
            viewModel = vm
        }
    }
}
