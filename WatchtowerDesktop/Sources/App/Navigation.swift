import SwiftUI

enum SidebarDestination: String, CaseIterable, Identifiable {
    case chat
    case briefings
    case inbox
    case calendar
    case tasks
    case tracks
    case digests
    case people
    case statistics
    case search
    case usage
    case training

    var id: String { rawValue }

    var title: String {
        switch self {
        case .chat: "AI Chat"
        case .briefings: "Briefings"
        case .inbox: "Inbox"
        case .calendar: "Calendar"
        case .tasks: "Tasks"
        case .tracks: "Tracks"
        case .digests: "Digests"
        case .people: "People"
        case .statistics: "Statistics"
        case .search: "Search"
        case .usage: "Usage"
        case .training: "Training"
        }
    }

    var icon: String {
        switch self {
        case .chat: "bubble.left.and.bubble.right"
        case .briefings: "sun.max"
        case .inbox: "tray"
        case .calendar: "calendar"
        case .tasks: "checkmark.circle"
        case .tracks: "binoculars"
        case .digests: "doc.text.magnifyingglass"
        case .people: "person.2"
        case .statistics: "chart.bar.xaxis"
        case .search: "magnifyingglass"
        case .usage: "chart.bar"
        case .training: "brain.head.profile"
        }
    }

    /// Main navigation items (shown above the separator).
    static var mainItems: [Self] {
        [.chat, .briefings, .inbox, .calendar, .tasks, .tracks, .digests, .people, .statistics, .search]
    }

    /// Tool items (shown below the separator).
    static var toolItems: [Self] {
        [.usage, .training]
    }
}

struct NavigationRoot: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        if appState.isLoading {
            SplashView()
        } else if appState.needsOnboarding {
            OnboardingView {
                appState.initialize()
            }
        } else {
            MainNavigationView()
        }
    }
}

struct SplashView: View {
    @State private var opacity: Double = 0

    var body: some View {
        VStack(spacing: 24) {
            Spacer()

            BannerImage(maxWidth: 360)

            ProgressView()
                .scaleEffect(0.8)
                .padding(.top, 8)

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
        .opacity(opacity)
        .onAppear {
            withAnimation(.easeIn(duration: 0.4)) {
                opacity = 1
            }
        }
    }
}

struct MainNavigationView: View {
    @Environment(AppState.self) private var appState
    @State private var showMenu = true

    private var sidebarToggleRow: some View {
        HStack(spacing: 8) {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    showMenu.toggle()
                }
            } label: {
                Image(systemName: "sidebar.leading")
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.borderless)
            .help("Toggle Menu")
            .keyboardShortcut("b", modifiers: [.command])

            Spacer()
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
    }

    var body: some View {
        @Bindable var state = appState
        VStack(spacing: 0) {
            // Content
            HStack(spacing: 0) {
                if showMenu {
                    // Left sidebar with toggle button in toolbar row
                    VStack(spacing: 0) {
                        sidebarToggleRow

                        SidebarView(selection: $state.selectedDestination)
                    }
                    .frame(width: 180)
                    .background(Color(nsColor: .windowBackgroundColor))
                    .transition(.move(edge: .leading).combined(with: .opacity))

                    Divider()
                }

                // Main content
                VStack(spacing: 0) {
                    if !showMenu {
                        sidebarToggleRow
                            .background(Color(nsColor: .windowBackgroundColor))
                    }

                    detailView
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(Color(nsColor: .controlBackgroundColor))
                }
            }

            StatusBarView()
        }
        .background(Color(nsColor: .windowBackgroundColor))
    }

    @ViewBuilder
    private var detailView: some View {
        switch appState.selectedDestination {
        case .chat:
            ChatView()
        case .briefings:
            BriefingsListView()
        case .inbox:
            InboxListView()
        case .calendar:
            CalendarEventsView()
        case .tasks:
            TasksListView()
        case .tracks:
            TracksListView()
        case .digests:
            DigestListView()
        case .people:
            PeopleListView()
        case .statistics:
            StatisticsView()
        case .search:
            SearchView()
        case .usage:
            UsageView()
        case .training:
            TrainingView()
        }
    }
}

// MARK: - Onboarding

struct OnboardingView: View {
    let onRetry: () -> Void

    @Environment(AppState.self) private var appState
    @State private var isRunning = false
    @State private var output = ""
    @State private var cliError: String?
    @State private var syncProgress: SyncProgressData?
    @State private var syncPhaseStartedAt: Date?
    @State private var syncLastPhase: String?
    @State private var syncEtaSeconds: Double?

    // Onboarding chat (runs in parallel with sync)
    @State private var onboardingVM: OnboardingChatViewModel?

    // Settings
    @State private var settingsLanguage = "English"
    @State private var settingsHistoryDays = 3
    @State private var settingsCustomDays = ""
    @State private var settingsModelPreset = ModelPreset.balanced
    @State private var settingsPollPreset = PollPreset.normal
    @State private var settingsNotifications = true

    // OAuth
    @State private var oauthStatus = ""

    // Claude setup
    @State private var manualClaudePath = ""
    @State private var claudeCheckResult: String?
    // Claude health check
    @State private var claudeHealthPassed = false
    @State private var claudeHealthError: String?
    @State private var hasClaudeCLI = Constants.findClaudePath() != nil

    private var cliPath: String? { Constants.findCLIPath() }
    private var claudePath: String? { Constants.findClaudePath() }
    private var hasCLI: Bool { cliPath != nil }

    var body: some View {
        VStack(spacing: 24) {
            // Header
            BannerImage(maxWidth: 320)

            // Steps indicator
            stepsIndicator

            // Current step content
            switch appState.onboarding.currentStep {
            case .connect:
                connectStep
            case .settings:
                settingsStep
            case .claude:
                claudeStepView
            case .chat:
                chatStep
            case .teamForm:
                teamFormStep
            case .generating:
                generatingStep
            case .complete:
                EmptyView()
            }

            // Error display (only show cliError, not appState.errorMessage which is stale)
            if let err = cliError {
                Text(err)
                    .foregroundStyle(.red)
                    .font(.caption)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 400)
            }
        }
        .padding(40)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .safeAreaInset(edge: .bottom) {
            if appState.onboarding.currentStep <= .claude {
                onboardingStatusBar
            }
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear {
            appState.onboarding.skipCompleted()
        }
    }

    // MARK: - Steps Indicator

    private var stepsIndicator: some View {
        let visible = OnboardingStep.indicatorSteps
        let current = appState.onboarding.currentStep
        return HStack(spacing: 4) {
            ForEach(visible, id: \.rawValue) { s in
                HStack(spacing: 4) {
                    Circle()
                        .fill(s.rawValue <= current.rawValue ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 8, height: 8)
                    if let title = s.indicatorTitle {
                        Text(title)
                            .font(.caption)
                            .foregroundStyle(s.rawValue <= current.rawValue ? .primary : .secondary)
                    }
                }
                if s != visible.last {
                    Rectangle()
                        .fill(s.rawValue < current.rawValue ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 30, height: 1)
                }
            }
        }
    }

    // MARK: - Claude Step (combined install + verify)

    private var claudeStepView: some View {
        VStack(spacing: 20) {
            if !hasClaudeCLI {
                claudeNotInstalledView
            } else if claudeHealthPassed {
                claudeVerifiedView
            } else if isRunning {
                claudeCheckingView
            } else if let error = claudeHealthError {
                claudeHealthFailedView(error)
            } else {
                ProgressView()
                    .controlSize(.small)
                Text("Preparing...")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
        }
        .onAppear {
            // Re-check hasClaudeCLI in case user installed it while on another step
            hasClaudeCLI = Constants.findClaudePath() != nil
            if hasClaudeCLI && claudeHealthPassed && !isRunning {
                // Already verified — auto-advance to chat
                appState.onboarding.goTo(.chat)
                runSync()
            } else if hasClaudeCLI && !claudeHealthPassed && !isRunning && claudeHealthError == nil {
                runClaudeHealthCheck()
            }
        }
    }

    private var claudeNotInstalledView: some View {
        Group {
            Image(systemName: "terminal")
                .font(.system(size: 36))
                .foregroundStyle(.orange)

            Text("Claude Code CLI Required")
                .font(.title2)
                .fontWeight(.semibold)

            Text("Watchtower uses Claude Code for AI-powered digests,\npeople analytics, and tracks.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            claudeInstallInstructions
            claudeManualPathBox
            claudeInstallButtons
        }
    }

    private var claudeInstallInstructions: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 12) {
                Text("Install Claude Code")
                    .font(.headline)

                installStep(number: 1, text: "Open **Terminal** app")

                installStep(number: 2, text: "Run this command:")
                codeBlock("npm install -g @anthropic-ai/claude-code")

                installStep(number: 3, text: "Verify installation:")
                codeBlock("which claude")

                Divider()

                Text("Don't have npm?")
                    .font(.subheadline)
                    .fontWeight(.medium)
                Text("Install Node.js first from **nodejs.org**, then run the command above.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(8)
        }
        .frame(maxWidth: 480)
    }

    private var claudeManualPathBox: some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                Text("Or specify the path manually")
                    .font(.subheadline)
                    .fontWeight(.medium)

                HStack {
                    TextField("e.g. /usr/local/bin/claude", text: $manualClaudePath)
                        .textFieldStyle(.roundedBorder)

                    Button("Browse...") {
                        let panel = NSOpenPanel()
                        panel.canChooseFiles = true
                        panel.canChooseDirectories = false
                        panel.allowsMultipleSelection = false
                        panel.message = "Select the 'claude' executable"
                        if panel.runModal() == .OK, let url = panel.url {
                            manualClaudePath = url.path
                        }
                    }
                }

                if let result = claudeCheckResult {
                    Text(result)
                        .font(.caption)
                        .foregroundStyle(result.contains("Found") ? .green : .red)
                }
            }
            .padding(8)
        }
        .frame(maxWidth: 480)
    }

    private var claudeInstallButtons: some View {
        HStack(spacing: 16) {
            Button("Back to Settings") {
                appState.onboarding.goTo(.settings)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)

            Button("Skip for now") {
                appState.onboarding.goTo(.chat)
                runSync()
            }
            .buttonStyle(.bordered)
            .controlSize(.large)

            Button {
                checkAndContinue()
            } label: {
                HStack {
                    if isRunning {
                        ProgressView().controlSize(.small)
                    }
                    Text("Check & Verify")
                }
                .frame(minWidth: 160)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(isRunning)
        }
    }

    private var claudeVerifiedView: some View {
        Group {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 36))
                .foregroundStyle(.green)

            Text("AI Connection Verified")
                .font(.title2)
                .fontWeight(.semibold)

            Text("Claude is ready with **\(settingsModelPreset.title)** model.")
                .font(.subheadline)
                .foregroundStyle(.secondary)

            ProgressView()
                .controlSize(.small)
            Text("Starting sync...")
                .font(.caption)
                .foregroundStyle(.tertiary)
        }
    }

    private var claudeCheckingView: some View {
        Group {
            ProgressView()
                .controlSize(.regular)

            Text("Testing AI Connection")
                .font(.title2)
                .fontWeight(.semibold)

            Text("Sending a test request to Claude (**\(settingsModelPreset.aiModel)**)...")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            Text("This may take 10–30 seconds")
                .font(.caption)
                .foregroundStyle(.tertiary)
        }
    }

    private func claudeHealthFailedView(_ error: String) -> some View {
        Group {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 36))
                .foregroundStyle(.red)

            Text("AI Connection Failed")
                .font(.title2)
                .fontWeight(.semibold)

            GroupBox {
                Text(error)
                    .font(.caption)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(4)
            }
            .frame(maxWidth: 480)

            GroupBox {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Quick fix — open Terminal and run:")
                        .font(.subheadline)
                        .fontWeight(.medium)

                    codeBlock("claude")

                    Text("Complete the authentication, then press Retry.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                .padding(4)
            }
            .frame(maxWidth: 480)

            HStack(spacing: 16) {
                Button("Back to Settings") {
                    claudeHealthError = nil
                    appState.onboarding.goTo(.settings)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)

                Button {
                    runClaudeHealthCheck()
                } label: {
                    Text("Retry")
                        .frame(minWidth: 100)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
            }
        }
    }

    private func installStep(number: Int, text: LocalizedStringKey) -> some View {
        HStack(alignment: .top, spacing: 8) {
            Text("\(number).")
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)
                .frame(width: 20, alignment: .trailing)
            Text(text)
                .font(.subheadline)
        }
    }

    private func codeBlock(_ code: String) -> some View {
        HStack {
            Text(code)
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
            Spacer()
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(code, forType: .string)
            } label: {
                Image(systemName: "doc.on.doc")
                    .font(.caption)
            }
            .buttonStyle(.borderless)
            .help("Copy to clipboard")
        }
        .padding(8)
        .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 6))
        .padding(.leading, 28)
    }

    private func checkAndContinue() {
        isRunning = true
        claudeCheckResult = nil

        // If user provided a manual path, save it to config
        if !manualClaudePath.isEmpty {
            let path = manualClaudePath.trimmingCharacters(in: .whitespacesAndNewlines)
            if FileManager.default.isExecutableFile(atPath: path) {
                saveClaudePathToConfig(path)
                claudeCheckResult = "Found: \(path)"
                hasClaudeCLI = true
                // Don't reset isRunning — health check continues the running state
                runClaudeHealthCheck()
                return
            } else {
                claudeCheckResult = "File not found or not executable: \(path)"
                isRunning = false
                return
            }
        }

        // Re-check auto-detection
        if let found = Constants.findClaudePath() {
            claudeCheckResult = "Found: \(found)"
            hasClaudeCLI = true
            // Don't reset isRunning — health check continues the running state
            runClaudeHealthCheck()
        } else {
            claudeCheckResult = "Claude CLI not found. Install it and try again."
            isRunning = false
        }
    }

    /// Quote a YAML value safely: wrap in single quotes, escaping internal single quotes.
    private func yamlQuote(_ value: String) -> String {
        "'" + value.replacingOccurrences(of: "'", with: "''") + "'"
    }

    private func saveClaudePathToConfig(_ path: String) {
        let configDir = (Constants.configPath as NSString).deletingLastPathComponent
        try? FileManager.default.createDirectory(atPath: configDir, withIntermediateDirectories: true)

        // C3 fix: YAML-safe quoting to prevent injection via path value
        let safeLine = "claude_path: \(yamlQuote(path))\n"

        if var content = try? String(contentsOfFile: Constants.configPath, encoding: .utf8) {
            // Remove existing claude_path line if present
            let lines = content.components(separatedBy: "\n").filter { !$0.hasPrefix("claude_path:") }
            content = lines.joined(separator: "\n")
            if !content.hasSuffix("\n") { content += "\n" }
            content += safeLine
            try? content.write(toFile: Constants.configPath, atomically: true, encoding: .utf8)
            try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: Constants.configPath)
        } else {
            try? safeLine.write(toFile: Constants.configPath, atomically: true, encoding: .utf8)
            try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: Constants.configPath)
        }
    }

    // MARK: - Connect Step

    private var connectStep: some View {
        VStack(spacing: 16) {
            Text("Connect your Slack workspace to get started.")
                .foregroundStyle(.secondary)

            if !hasCLI {
                Text("watchtower CLI not found. Reinstall the app or place the binary in your PATH.")
                    .foregroundStyle(.red)
                    .font(.caption)
            } else {
                // Privacy notice
                GroupBox {
                    HStack(alignment: .top, spacing: 10) {
                        Image(systemName: "lock.shield")
                            .font(.title2)
                            .foregroundStyle(.green)
                            .frame(width: 28)

                        VStack(alignment: .leading, spacing: 6) {
                            Text("Your data never leaves your Mac")
                                .font(.subheadline)
                                .fontWeight(.semibold)

                            Text("Watchtower stores everything locally — messages, "
                                + "digests, and analytics never leave your Mac. "
                                + "A local TLS certificate will be generated on "
                                + "your machine to securely handle the Slack OAuth "
                                + "callback. No data is sent to external servers.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .padding(4)
                }
                .frame(maxWidth: 420)

                Button {
                    startBrowserOAuthFlow()
                } label: {
                    HStack {
                        if isRunning {
                            ProgressView().controlSize(.small)
                        }
                        Text(isRunning ? "Authenticating..." : "Connect to Slack")
                    }
                    .frame(minWidth: 200)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(isRunning)

                if isRunning, !oauthStatus.isEmpty {
                    Text(oauthStatus)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
            }
        }
    }

    // MARK: - Status Bar

    private var onboardingStatusBar: some View {
        HStack(spacing: 16) {
            HStack(spacing: 4) {
                Circle()
                    .fill(hasCLI ? .green : .red)
                    .frame(width: 6, height: 6)
                Text("Watchtower CLI")
            }

            Divider().frame(height: 12)

            HStack(spacing: 4) {
                Circle()
                    .fill(hasClaudeCLI ? .green : .orange)
                    .frame(width: 6, height: 6)
                Text("Claude Code")
            }

            Spacer()
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(Color(nsColor: .windowBackgroundColor))
    }

    // MARK: - Settings Step

    enum ModelPreset: String, CaseIterable {
        case fast
        case balanced
        case quality

        var title: String {
            switch self {
            case .fast: "Fast"
            case .balanced: "Balanced"
            case .quality: "Quality"
            }
        }

        var subtitle: String {
            switch self {
            case .fast: "Haiku — quick, low cost"
            case .balanced: "Sonnet — good balance"
            case .quality: "Opus — best insights, slower"
            }
        }

        var settingDescription: String {
            switch self {
            case .fast: "Fastest responses at the lowest cost. Great for large workspaces where speed matters most."
            case .balanced: "A good balance of quality and speed. Recommended for most teams."
            case .quality: "Deepest analysis and best insights. Ideal when quality matters more than speed."
            }
        }

        var digestModel: String {
            switch self {
            case .fast: "claude-haiku-4-5-20251001"
            case .balanced: "claude-sonnet-4-6"
            case .quality: "claude-opus-4-6"
            }
        }

        var aiModel: String {
            switch self {
            case .fast: "claude-haiku-4-5-20251001"
            case .balanced: "claude-sonnet-4-6"
            case .quality: "claude-opus-4-6"
            }
        }
    }

    enum PollPreset: String, CaseIterable {
        case frequent
        case normal
        case relaxed
        case hourly

        var title: String {
            switch self {
            case .frequent: "5 min"
            case .normal: "15 min"
            case .relaxed: "30 min"
            case .hourly: "1 hour"
            }
        }

        var interval: String {
            switch self {
            case .frequent: "5m"
            case .normal: "15m"
            case .relaxed: "30m"
            case .hourly: "1h"
            }
        }
    }

    private var resolvedHistoryDays: Int {
        if settingsHistoryDays == -1, let custom = Int(settingsCustomDays), custom >= 1, custom <= 7 {
            return custom
        }
        return settingsHistoryDays
    }

    private var settingsStep: some View {
        VStack(spacing: 24) {
            Text("Configure your preferences before the first sync.")
                .foregroundStyle(.secondary)

            settingsCardStack
                .frame(maxWidth: 520)

            Button {
                applySettingsAndSync()
            } label: {
                Text(isRunning ? "Saving..." : "Continue")
                    .frame(minWidth: 200)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(isRunning || (settingsHistoryDays == -1 && resolvedHistoryDays == -1))
        }
    }

    private var settingsCardStack: some View {
        VStack(spacing: 1) {
            settingCard(
                icon: "globe",
                iconColor: .secondary,
                title: "Language",
                description: "Choose the language for digests, insights, "
                    + "and AI-generated content.",
                isFirst: true
            ) {
                Picker("", selection: $settingsLanguage) {
                    Text("English").tag("English")
                    Text("Українська").tag("Ukrainian")
                    Text("Русский").tag("Russian")
                }
                .pickerStyle(.menu)
            }

            settingCard(
                icon: "cpu",
                iconColor: .secondary,
                title: "AI Model",
                description: settingsModelPreset.settingDescription
            ) {
                Picker("", selection: $settingsModelPreset) {
                    ForEach(ModelPreset.allCases, id: \.self) { preset in
                        Text(preset.title).tag(preset)
                    }
                }
                .pickerStyle(.segmented)
            }

            settingCard(
                icon: "calendar.badge.clock",
                iconColor: .secondary,
                title: "History Depth",
                description: "How far back to look when syncing. "
                    + "More days = richer digests but longer first sync."
            ) {
                HStack(spacing: 8) {
                    Picker("", selection: $settingsHistoryDays) {
                        Text("1 day").tag(1)
                        Text("3 days").tag(3)
                        Text("5 days").tag(5)
                        Text("7 days").tag(7)
                        Text("Custom").tag(-1)
                    }
                    .pickerStyle(.menu)

                    if settingsHistoryDays == -1 {
                        TextField("days", text: $settingsCustomDays)
                            .frame(width: 40)
                            .textFieldStyle(.roundedBorder)
                    }
                }
            }

            settingCard(
                icon: "arrow.triangle.2.circlepath",
                iconColor: .secondary,
                title: "Sync Frequency",
                description: "How often Watchtower checks Slack for new messages."
            ) {
                Picker("", selection: $settingsPollPreset) {
                    ForEach(PollPreset.allCases, id: \.self) { preset in
                        Text(preset.title).tag(preset)
                    }
                }
                .pickerStyle(.menu)
            }

            settingCard(
                icon: "bell.badge",
                iconColor: .secondary,
                title: "Notifications",
                description: "Get notified about tracks, action items, and digests.",
                isLast: true
            ) {
                Toggle("", isOn: $settingsNotifications)
                    .toggleStyle(.switch)
            }
        }
        .background(Color(nsColor: .controlBackgroundColor).opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .overlay(
            RoundedRectangle(cornerRadius: 12)
                .strokeBorder(Color.primary.opacity(0.08), lineWidth: 1)
        )
    }

    private func settingCard<Content: View>(
        icon: String,
        iconColor: Color,
        title: String,
        description: String,
        isFirst: Bool = false,
        isLast: Bool = false,
        @ViewBuilder control: () -> Content
    ) -> some View {
        VStack(spacing: 0) {
            HStack(alignment: .top, spacing: 14) {
                Image(systemName: icon)
                    .font(.system(size: 16))
                    .foregroundStyle(iconColor)
                    .frame(width: 32, height: 32)
                    .background(Color.primary.opacity(0.06))
                    .clipShape(RoundedRectangle(cornerRadius: 8))

                VStack(alignment: .leading, spacing: 6) {
                    HStack(alignment: .center) {
                        Text(title)
                            .font(.headline)
                        Spacer()
                        control()
                            .frame(width: 200, alignment: .trailing)
                    }
                    .frame(minHeight: 28)

                    Text(description)
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .lineSpacing(2)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)

            if !isLast {
                Divider()
                    .padding(.leading, 62)
            }
        }
    }

    private func runClaudeHealthCheck() {
        guard let claudePath = Constants.findClaudePath() else {
            claudeHealthError = "Claude CLI not found.\nInstall Claude Code and press Retry."
            return
        }

        isRunning = true
        claudeHealthError = nil
        claudeHealthPassed = false

        let model = settingsModelPreset.aiModel

        Task.detached {
            let result = await Self.runClaudeCLICheck(claudePath: claudePath, model: model)
            await MainActor.run {
                self.isRunning = false
                let stdout = result.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
                let stderr = result.stderr.trimmingCharacters(in: .whitespacesAndNewlines)

                if result.exitCode == 0 && !stdout.isEmpty {
                    self.claudeHealthPassed = true
                    Task { @MainActor in
                        try? await Task.sleep(for: .seconds(1.5))
                        if self.appState.onboarding.currentStep == .claude {
                            self.appState.onboarding.goTo(.chat)
                            self.runSync()
                        }
                    }
                } else {
                    self.claudeHealthError = Self.diagnoseClaudeError(
                        stderr: stderr, exitCode: result.exitCode
                    )
                }
            }
        }
    }

    private static func runClaudeCLICheck(claudePath: String, model: String) async -> CLIResult {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: claudePath)
        process.currentDirectoryURL = Constants.processWorkingDirectory()
        process.arguments = [
            "-p", "respond with: OK",
            "--output-format", "text",
            "--model", model
        ]

        // Use shared resolved environment (caches login shell PATH)
        process.environment = Constants.resolvedEnvironment()

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            return CLIResult(exitCode: -1, stdout: "", stderr: error.localizedDescription)
        }

        process.waitUntilExit()

        let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()

        return CLIResult(
            exitCode: process.terminationStatus,
            stdout: String(data: stdoutData, encoding: .utf8) ?? "",
            stderr: String(data: stderrData, encoding: .utf8) ?? ""
        )
    }

    private static func diagnoseClaudeError(stderr: String, exitCode: Int32) -> String {
        let lower = stderr.lowercased()

        let authKeywords = ["not authenticated", "unauthorized", "api key", "log in", "login"]
        if authKeywords.contains(where: { lower.contains($0) }) {
            return "Claude is not authenticated.\n\nOpen Terminal and run: claude\nComplete the login, then press Retry."
        }

        let modelAccessKeywords = ["access", "available", "permission", "not found"]
        if lower.contains("model") && modelAccessKeywords.contains(where: { lower.contains($0) }) {
            return "The selected model is not available for your account.\n\n"
                + "Go back to Settings and try a different AI model (e.g. \"Fast\" for Haiku)."
        }

        if lower.contains("rate limit") || lower.contains("overloaded") || lower.contains("529") {
            return "Claude API is temporarily overloaded.\nWait a moment and press Retry."
        }

        let networkKeywords = ["network", "connection", "timed out", "resolve"]
        if networkKeywords.contains(where: { lower.contains($0) }) {
            return "Network error.\nCheck your internet connection and press Retry."
        }

        if !stderr.isEmpty {
            let truncated = stderr.count > 500 ? String(stderr.prefix(500)) + "..." : stderr
            return "Claude error (exit code \(exitCode)):\n\(truncated)"
        }

        return "Claude failed (exit code \(exitCode)).\nMake sure Claude Code is installed and authenticated."
    }

    // MARK: - Chat Step (questionnaire + AI conversation, sync runs in background)

    private var chatStep: some View {
        VStack(spacing: 16) {
            if !appState.onboarding.chatFinished {
                chatActiveView
            } else {
                chatWaitingForSyncView
            }
        }
        .task {
            guard onboardingVM == nil else { return }
            let configSvc = ConfigService()
            let language = configSvc.digestLanguage ?? settingsLanguage
            let db = appState.databaseManager
            onboardingVM = OnboardingChatViewModel(claudeService: ClaudeService(), language: language, dbManager: db)
            if !isRunning && !appState.onboarding.syncCompleted {
                runSync()
            }
        }
        .onChange(of: appState.onboarding.syncCompleted) {
            // CASE B reactive fallback: chat finished before sync — auto-advance when sync completes.
            // Primary path is in runSync() completion, but this ensures transition even if
            // the imperative path fails (e.g. DB open throws, @State capture issue in Task).
            guard appState.onboarding.chatFinished && appState.onboarding.syncCompleted
                && appState.onboarding.currentStep == .chat else { return }
            ensureOnboardingDatabase()
            appState.onboarding.goTo(.teamForm)
        }
    }

    @ViewBuilder
    private var chatActiveView: some View {
        if let vm = onboardingVM {
            OnboardingChatView(viewModel: vm) {
                if appState.onboarding.syncCompleted {
                    appState.onboarding.goTo(.teamForm)
                } else {
                    appState.onboarding.chatFinished = true
                }
            }
        } else {
            ProgressView("Preparing...")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }

        if isRunning {
            Divider()
            syncProgressCompactBanner
        } else if appState.onboarding.syncCompleted {
            Divider()
            HStack(spacing: 6) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                Text("Sync complete!")
                    .font(.caption)
                    .fontWeight(.medium)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
        }
    }

    @ViewBuilder
    private var chatWaitingForSyncView: some View {
        VStack(spacing: 20) {
            if let err = cliError {
                chatSyncErrorView(err)
            } else if !isRunning {
                chatSyncCompleteView
            } else {
                chatSyncInProgressView
            }
        }
    }

    private func chatSyncErrorView(_ err: String) -> some View {
        Group {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 36))
                .foregroundStyle(.red)
            Text("Sync failed")
                .font(.title3)
                .fontWeight(.medium)
            Text(err)
                .font(.caption)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 450)
            Button("Retry Sync") {
                cliError = nil
                runSync()
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
        }
    }

    private var chatSyncCompleteView: some View {
        Group {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 36))
                .foregroundStyle(.green)
            Text("Sync complete!")
                .font(.title3)
                .fontWeight(.medium)
            Button {
                guard ensureOnboardingDatabase() else { return }
                appState.onboarding.syncCompleted = true
                appState.onboarding.goTo(.teamForm)
            } label: {
                Label("Continue", systemImage: "arrow.right.circle.fill")
                    .font(.headline)
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .frame(maxWidth: 300)
        }
    }

    @ViewBuilder
    private var chatSyncInProgressView: some View {
        Image(systemName: "arrow.triangle.2.circlepath")
            .font(.system(size: 36))
            .foregroundStyle(Color.accentColor)
        Text("Syncing your workspace...")
            .font(.title3)
            .fontWeight(.medium)
        if let progress = syncProgress {
            syncProgressView(progress).frame(maxWidth: 450)
        } else {
            ProgressView()
        }
    }

    // MARK: - Team Form Step

    private var teamFormStep: some View {
        Group {
            if let vm = onboardingVM {
                OnboardingTeamFormView(viewModel: vm) {
                    appState.onboarding.goTo(.generating)
                    Task {
                        await vm.generatePromptContext()
                        await vm.markOnboardingDone()
                        if vm.errorMessage == nil {
                            appState.backgroundTaskManager.startPipelines(legacyPeople: appState.analysisLegacyMode)
                            appState.completeOnboarding()
                            onRetry()
                        } else {
                            appState.onboarding.goTo(.teamForm)
                        }
                    }
                }
            } else {
                ProgressView("Loading...")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task {
            // Ensure VM exists when resuming from a restart at teamForm step
            guard onboardingVM == nil else { return }
            let configSvc = ConfigService()
            let language = configSvc.digestLanguage ?? settingsLanguage
            if let db = appState.databaseManager {
                onboardingVM = OnboardingChatViewModel(claudeService: ClaudeService(), language: language, dbManager: db)
            } else {
                // DB not available — need sync first, go back to chat
                appState.onboarding.goTo(.chat)
            }
        }
    }

    // MARK: - Generating Step

    private var generatingStep: some View {
        VStack(spacing: 16) {
            ProgressView()
            Text("Setting up your personalized experience...")
                .foregroundStyle(.secondary)
            if let error = onboardingVM?.errorMessage {
                Text(error)
                    .foregroundStyle(.red)
                    .font(.caption)
            }
        }
    }

    private var syncProgressCompactBanner: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Image(systemName: "arrow.triangle.2.circlepath")
                    .font(.system(size: 11))
                    .foregroundStyle(Color.accentColor)

                if let progress = syncProgress {
                    Text("Syncing: \(progress.phase)")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.primary)
                    if progress.elapsedSec > 0 {
                        Text("·").font(.caption).foregroundStyle(.secondary.opacity(0.5))
                        Text(formatElapsed(progress.elapsedSec))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if let eta = syncEtaSeconds, eta > 0 {
                        Text("·").font(.caption).foregroundStyle(.secondary.opacity(0.5))
                        Text("\(formatETA(eta)) left")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                } else {
                    Text("Starting sync...")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.primary)
                }
                Spacer()
            }

            // Progress bar from current phase
            if let progress = syncProgress {
                let (done, total) = currentPhaseProgress(progress)
                if total > 0 {
                    ProgressView(value: Double(done), total: Double(total))
                        .tint(.accentColor)
                        .scaleEffect(y: 0.65, anchor: .leading)
                } else {
                    ProgressView()
                        .controlSize(.small)
                        .scaleEffect(0.8, anchor: .leading)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(Color(nsColor: .controlBackgroundColor))
    }

    private func currentPhaseProgress(_ progress: SyncProgressData) -> (done: Int, total: Int) {
        switch progress.phase {
        case "Discovery":
            return (progress.discoveryPages, progress.discoveryTotalPages)
        case "Messages":
            return (progress.msgChannelsDone, progress.msgChannelsTotal)
        case "Users":
            return (progress.userProfilesDone, progress.userProfilesTotal)
        case "Threads":
            return (progress.threadsDone ?? 0, progress.threadsTotal ?? 0)
        default:
            return (0, 0)
        }
    }

private func syncProgressView(_ progress: SyncProgressData) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Syncing workspace...").font(.subheadline).fontWeight(.medium)
                Spacer()
                if let eta = syncEtaSeconds, eta > 0 {
                    Text("\(formatETA(eta)) left").font(.caption).foregroundStyle(.secondary)
                    Text("·").font(.caption).foregroundStyle(.secondary.opacity(0.5))
                }
                Text(formatElapsed(progress.elapsedSec)).font(.caption).foregroundStyle(.secondary)
            }
            syncPhaseRow(
                label: "Discovery",
                icon: "magnifyingglass",
                phase: "Discovery",
                cur: progress.phase,
                done: progress.discoveryPages,
                total: progress.discoveryTotalPages,
                detail: progress.discoveryChannels > 0
                    ? "\(progress.discoveryChannels) ch, \(progress.discoveryUsers) users, \(fmtNum(progress.messagesFetched)) msgs"
                    : nil
            )
            syncPhaseRow(
                label: "Messages",
                icon: "message",
                phase: "Messages",
                cur: progress.phase,
                done: progress.msgChannelsDone,
                total: progress.msgChannelsTotal,
                detail: progress.messagesFetched > 0 ? "\(fmtNum(progress.messagesFetched)) messages" : nil
            )
            syncPhaseRow(
                label: "Users",
                icon: "person.2",
                phase: "Users",
                cur: progress.phase,
                done: progress.userProfilesDone,
                total: progress.userProfilesTotal,
                detail: nil
            )
            syncPhaseRow(
                label: "Threads",
                icon: "bubble.left.and.bubble.right",
                phase: "Threads",
                cur: progress.phase,
                done: progress.threadsDone ?? 0,
                total: progress.threadsTotal ?? 0,
                detail: (progress.threadsFetched ?? 0) > 0 ? "\(fmtNum(progress.threadsFetched ?? 0)) replies" : nil
            )
            if progress.phase == "Done" {
                HStack {
                    Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
                    Text("Sync complete!").font(.caption).fontWeight(.medium)
                }
            }
        }
        .padding()
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 8))
    }

    private func syncPhaseRow(label: String, icon: String, phase: String, cur: String, done: Int, total: Int, detail: String?) -> some View {
        let order = ["Metadata": 0, "Discovery": 1, "Messages": 2, "Users": 3, "Threads": 4, "Done": 5]
        let curIdx = order[cur] ?? 0, phaseIdx = order[phase] ?? 0
        let isActive = curIdx == phaseIdx, isDone = curIdx > phaseIdx, isWaiting = curIdx < phaseIdx
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: isDone ? "checkmark.circle.fill" : icon)
                    .foregroundStyle(isDone ? .green : isActive ? .accentColor : .secondary.opacity(0.4))
                    .frame(width: 16)
                Text(label)
                    .font(.caption)
                    .fontWeight(isActive ? .semibold : .regular)
                    .foregroundStyle(isWaiting ? Color.secondary.opacity(0.4) : Color.primary)
                Spacer()
                if isActive && total > 0 {
                    Text("\(done)/\(total)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                } else if isDone, let detail {
                    Text(detail).font(.caption2).foregroundStyle(.secondary)
                }
            }
            if isActive && total > 0 {
                ProgressView(value: Double(done), total: Double(max(total, 1))).tint(.accentColor)
            } else if isActive && !isWaiting {
                ProgressView().controlSize(.small).scaleEffect(0.7, anchor: .leading)
            }
        }
    }

    private func formatElapsed(_ s: Double) -> String {
        let i = Int(s); return i < 60 ? "\(i)s" : "\(i / 60)m \(i % 60)s"
    }

    private func fmtNum(_ n: Int) -> String {
        if n >= 1_000_000 { return String(format: "%.1fM", Double(n) / 1_000_000) }
        if n >= 1_000 { return String(format: "%.1fK", Double(n) / 1_000) }
        return "\(n)"
    }

    private func updateSyncETA(_ progress: SyncProgressData) {
        // Reset timer when phase changes
        if progress.phase != syncLastPhase {
            syncLastPhase = progress.phase
            syncPhaseStartedAt = Date()
            syncEtaSeconds = nil
            return
        }

        guard let phaseStart = syncPhaseStartedAt else {
            syncEtaSeconds = nil
            return
        }

        // Get done/total for current phase
        let (done, total) = syncPhaseCounts(progress)
        guard done > 0, total > 0 else {
            syncEtaSeconds = nil
            return
        }

        let elapsed = Date().timeIntervalSince(phaseStart)
        guard elapsed > 2 else {
            syncEtaSeconds = nil
            return
        }

        let rate = Double(done) / elapsed
        let remaining = Double(total - done) / rate
        syncEtaSeconds = remaining
    }

    private func syncPhaseCounts(_ progress: SyncProgressData) -> (done: Int, total: Int) {
        switch progress.phase {
        case "Discovery": return (progress.discoveryPages, progress.discoveryTotalPages)
        case "Messages": return (progress.msgChannelsDone, progress.msgChannelsTotal)
        case "Users": return (progress.userProfilesDone, progress.userProfilesTotal)
        case "Threads": return (progress.threadsDone ?? 0, progress.threadsTotal ?? 0)
        default: return (0, 0)
        }
    }

    private func formatETA(_ seconds: Double) -> String {
        let s = Int(seconds)
        if s < 5 { return "< 5s" }
        if s < 60 { return "~\(s)s" }
        let min = s / 60, rem = s % 60
        if rem == 0 { return "~\(min)m" }
        return "~\(min)m \(rem)s"
    }

    // MARK: - CLI Execution

    /// Open OAuth in the default browser.
    /// Ensures the localhost TLS cert is trusted (silent, no admin prompt needed),
    /// then runs `watchtower auth login` which opens the browser.
    private func startBrowserOAuthFlow() {
        guard let path = cliPath else { return }
        isRunning = true
        cliError = nil
        oauthStatus = "Preparing secure connection..."

        Task.detached {
            // Ensure cert is trusted (silent — adds to user trust store, no password needed)
            let trustResult = await Self.runCLI(path: path, arguments: ["auth", "trust-cert"])
            if trustResult.exitCode != 0 {
                await MainActor.run {
                    isRunning = false
                    oauthStatus = ""
                    cliError = trustResult.stderr.isEmpty
                        ? "Failed to set up secure connection"
                        : trustResult.stderr
                }
                return
            }

            await MainActor.run {
                oauthStatus = "Complete the Slack authorization in your browser."
            }

            // Run auth login (opens default browser, trusted HTTPS callback)
            let result = await Self.runCLI(path: path, arguments: ["auth", "login"])
            await MainActor.run {
                isRunning = false
                oauthStatus = ""
                if result.exitCode == 0 {
                    appState.onboarding.goTo(.settings)
                } else {
                    cliError = result.stderr.isEmpty
                        ? "Authentication failed (exit code \(result.exitCode))"
                        : result.stderr
                }
            }
        }
    }

    private func applySettingsAndSync() {
        guard let path = cliPath else { return }
        isRunning = true
        cliError = nil

        // Capture values before entering detached task
        let lang = settingsLanguage
        let days = resolvedHistoryDays
        let model = settingsModelPreset
        let poll = settingsPollPreset

        Task.detached {
            // Apply settings via `watchtower config set`
            let settings: [(String, String)] = [
                ("digest.language", lang),
                ("sync.initial_history_days", "\(days)"),
                ("digest.model", model.digestModel),
                ("ai.model", model.aiModel),
                ("sync.poll_interval", poll.interval)
            ]
            for (key, value) in settings {
                let result = await Self.runCLI(path: path, arguments: ["config", "set", key, value])
                if result.exitCode != 0 {
                    await MainActor.run {
                        cliError = "Failed to set \(key): \(result.stderr)"
                        isRunning = false
                    }
                    return
                }
            }

            await MainActor.run {
                isRunning = false
                appState.onboarding.goTo(.claude)
            }
        }
    }

    /// Opens the database and passes it to the onboarding ViewModel.
    /// No-op if the VM already has users loaded.
    @discardableResult
    private func ensureOnboardingDatabase() -> Bool {
        guard onboardingVM?.allUsers.isEmpty ?? true else { return true }
        do {
            DatabaseManager.runCLIMigrations()
            let dbPath = try DatabaseManager.resolveDBPath()
            let manager = try DatabaseManager(path: dbPath)
            onboardingVM?.setDatabase(manager)
            return true
        } catch {
            cliError = "Failed to open database: \(error.localizedDescription)"
            return false
        }
    }

    private func runSync() {
        guard let path = cliPath else { return }
        isRunning = true
        cliError = nil
        syncProgress = nil
        syncPhaseStartedAt = nil
        syncLastPhase = nil
        syncEtaSeconds = nil

        Task {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)
            process.arguments = ["sync", "--progress-json"]
            process.environment = Constants.resolvedEnvironment()
            let stdoutPipe = Pipe()
            let stderrPipe = Pipe()
            process.standardOutput = stdoutPipe
            process.standardError = stderrPipe

            do {
                try process.run()
            } catch {
                cliError = error.localizedDescription
                isRunning = false
                return
            }

            // Read stdout lines via async sequence — runs on MainActor,
            // so syncProgress updates trigger SwiftUI re-renders directly.
            let decoder = JSONDecoder()
            let readTask = Task<Void, Never> {
                do {
                    for try await line in stdoutPipe.fileHandleForReading.bytes.lines {
                        if let data = line.data(using: .utf8),
                           let json = try? decoder.decode(SyncProgressData.self, from: data) {
                            self.syncProgress = json
                            self.updateSyncETA(json)
                        }
                    }
                } catch {
                    // EOF or pipe closed
                }
            }

            // Wait for process exit without blocking main thread
            let exitCode: Int32 = await withCheckedContinuation { cont in
                process.terminationHandler = { proc in
                    cont.resume(returning: proc.terminationStatus)
                }
            }

            // Close the stdout file handle to force bytes.lines to see EOF, then cancel the task.
            // Don't await readTask.value — it can hang indefinitely if the pipe's write end
            // was inherited by a subprocess (e.g. Claude CLI). The exit code is already known,
            // progress parsing is no longer needed.
            stdoutPipe.fileHandleForReading.closeFile()
            readTask.cancel()

            let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
            let stderrText = String(data: stderrData, encoding: .utf8) ?? ""

            isRunning = false
            if exitCode == 0 {
                syncProgress = nil

                // Open DB and pass to onboarding ViewModel for team form
                ensureOnboardingDatabase()

                // Mark sync done AFTER DB setup attempt — triggers .onChange for CASE B transition
                appState.onboarding.syncCompleted = true

                // If chat already finished, move to team form
                if appState.onboarding.chatFinished {
                    appState.onboarding.goTo(.teamForm)
                }
            } else {
                cliError = stderrText.isEmpty
                    ? "Sync failed (exit code \(exitCode))"
                    : stderrText
            }
        }
    }

    private struct CLIResult {
        let exitCode: Int32
        let stdout: String
        let stderr: String
    }

    /// Thread-safe line buffer that splits streamed data on newlines.
    private final class LineBuffer: @unchecked Sendable {
        private let onLine: (String) -> Void
        private var buffer = Data()
        private let lock = NSLock()
        private var _allText = ""

        var allText: String {
            lock.lock()
            defer { lock.unlock() }
            return _allText
        }

        init(onLine: @escaping (String) -> Void) {
            self.onLine = onLine
        }

        func append(_ data: Data) {
            lock.lock()
            buffer.append(data)
            // Extract complete lines
            var lines: [String] = []
            let newline = UInt8(0x0A)
            while let idx = buffer.firstIndex(of: newline) {
                let lineData = buffer[buffer.startIndex..<idx]
                buffer = buffer[(idx + 1)...]
                if let line = String(data: lineData, encoding: .utf8) {
                    _allText += line + "\n"
                    lines.append(line)
                }
            }
            lock.unlock()
            for line in lines {
                onLine(line)
            }
        }

        func flush() {
            lock.lock()
            let remaining = buffer
            buffer = Data()
            lock.unlock()
            if !remaining.isEmpty, let line = String(data: remaining, encoding: .utf8), !line.isEmpty {
                lock.lock()
                _allText += line
                lock.unlock()
                onLine(line)
            }
        }
    }

    private static func runCLI(
        path: String,
        arguments: [String],
        onOutputLine: (@Sendable (String) -> Void)? = nil
    ) async -> CLIResult {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = arguments
        process.environment = Constants.resolvedEnvironment()

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            return CLIResult(exitCode: -1, stdout: "", stderr: error.localizedDescription)
        }

        var stdoutText = ""

        if let onLine = onOutputLine {
            // Use readabilityHandler for real-time line streaming
            let lineBuffer = LineBuffer(onLine: onLine)
            stdoutPipe.fileHandleForReading.readabilityHandler = { handle in
                let data = handle.availableData
                if data.isEmpty {
                    // EOF
                    handle.readabilityHandler = nil
                    return
                }
                lineBuffer.append(data)
            }

            process.waitUntilExit()
            // Ensure we read any remaining data after process exits
            stdoutPipe.fileHandleForReading.readabilityHandler = nil
            let remaining = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
            if !remaining.isEmpty {
                lineBuffer.append(remaining)
            }
            lineBuffer.flush()
            stdoutText = lineBuffer.allText
        } else {
            process.waitUntilExit()
            let data = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
            stdoutText = String(data: data, encoding: .utf8) ?? ""
        }

        let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
        let stderrText = String(data: stderrData, encoding: .utf8) ?? ""

        return CLIResult(exitCode: process.terminationStatus, stdout: stdoutText, stderr: stderrText)
    }
}

// MARK: - Banner Image

struct BannerImage: View {
    var maxWidth: CGFloat = 360

    private var nsImage: NSImage? {
        // Try resource bundle first (SPM / .app)
        if let url = AppBundle.resources.url(forResource: "banner", withExtension: "png"),
           let img = NSImage(contentsOf: url) {
            return img
        }
        // Try Bundle.main directly
        if let url = Bundle.main.url(forResource: "banner", withExtension: "png"),
           let img = NSImage(contentsOf: url) {
            return img
        }
        // Try next to executable
        if let execURL = Bundle.main.executableURL?.deletingLastPathComponent() {
            let bundlePath = execURL.appendingPathComponent("WatchtowerDesktop_WatchtowerDesktop.bundle/banner.png")
            if let img = NSImage(contentsOf: bundlePath) {
                return img
            }
        }
        return nil
    }

    var body: some View {
        if let nsImage {
            Image(nsImage: nsImage)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: maxWidth)
        } else {
            // Fallback: show app name as text
            Text("WATCHTOWER")
                .font(.system(size: 32, weight: .bold))
                .foregroundStyle(.orange)
                .frame(maxWidth: maxWidth)
        }
    }
}
