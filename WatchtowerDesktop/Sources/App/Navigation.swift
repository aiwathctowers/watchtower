import SwiftUI

enum SidebarDestination: String, CaseIterable, Identifiable {
    case chat
    case actions
    case digests
    case people
    case search
    case training

    var id: String { rawValue }

    var title: String {
        switch self {
        case .chat: "AI Chat"
        case .actions: "Actions"
        case .digests: "Digests"
        case .people: "People"
        case .search: "Search"
        case .training: "Training"
        }
    }

    var icon: String {
        switch self {
        case .chat: "bubble.left.and.bubble.right"
        case .actions: "checklist"
        case .digests: "doc.text.magnifyingglass"
        case .people: "person.2"
        case .search: "magnifyingglass"
        case .training: "brain.head.profile"
        }
    }

    /// Main navigation items (shown above the separator).
    static var mainItems: [SidebarDestination] {
        [.chat, .actions, .digests, .people, .search]
    }

    /// Tool items (shown below the separator).
    static var toolItems: [SidebarDestination] {
        [.training]
    }
}

struct NavigationRoot: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        if appState.isDBAvailable {
            MainNavigationView()
        } else {
            OnboardingView(errorMessage: appState.errorMessage) {
                appState.initialize()
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
        case .actions:
            ActionItemsListView()
        case .digests:
            DigestListView()
        case .people:
            PeopleListView()
        case .search:
            SearchView()
        case .training:
            TrainingView()
        }
    }
}

// MARK: - Onboarding

enum OnboardingStep: Int, CaseIterable {
    case connect = 0
    case settings = 1
    case claude = 2
    case sync = 3

    var title: String {
        switch self {
        case .connect: "Connect"
        case .settings: "Settings"
        case .claude: "AI Setup"
        case .sync: "Sync"
        }
    }
}

struct OnboardingView: View {
    let errorMessage: String?
    let onRetry: () -> Void

    @Environment(AppState.self) private var appState
    @State private var step: OnboardingStep = .connect
    @State private var isRunning = false
    @State private var output = ""
    @State private var cliError: String?
    @State private var syncProgress: SyncProgressData?
    @State private var syncPhaseStartedAt: Date?
    @State private var syncLastPhase: String?
    @State private var syncEtaSeconds: Double?

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
            Image(systemName: "binoculars.circle")
                .font(.system(size: 64))
                .foregroundStyle(.secondary)

            Text("Welcome to Watchtower")
                .font(.largeTitle)
                .fontWeight(.bold)

            // Steps indicator
            stepsIndicator

            // Current step content
            switch step {
            case .connect:
                connectStep
            case .settings:
                settingsStep
            case .claude:
                claudeStepView
            case .sync:
                syncStep
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
            onboardingStatusBar
        }
        .background(Color(nsColor: .windowBackgroundColor))
        .onAppear {
            // If Claude was found and config exists, skip connect → settings
            if step == .connect && FileManager.default.fileExists(atPath: Constants.configPath) {
                step = .settings
            }
        }
    }

    // MARK: - Steps Indicator

    private var stepsIndicator: some View {
        let visible = OnboardingStep.allCases
        return HStack(spacing: 4) {
            ForEach(visible, id: \.rawValue) { s in
                HStack(spacing: 4) {
                    Circle()
                        .fill(s.rawValue <= step.rawValue ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 8, height: 8)
                    Text(s.title)
                        .font(.caption)
                        .foregroundStyle(s.rawValue <= step.rawValue ? .primary : .secondary)
                }
                if s != visible.last {
                    Rectangle()
                        .fill(s.rawValue < step.rawValue ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 30, height: 1)
                }
            }
        }
    }

    // MARK: - Claude Step (combined install + verify)

    private var claudeStepView: some View {
        VStack(spacing: 20) {
            if !hasClaudeCLI {
                // Claude CLI not found — show installation instructions
                Image(systemName: "terminal")
                    .font(.system(size: 36))
                    .foregroundStyle(.orange)

                Text("Claude Code CLI Required")
                    .font(.title2)
                    .fontWeight(.semibold)

                Text("Watchtower uses Claude Code for AI-powered digests,\npeople analytics, and action items.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)

                // Installation instructions
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

                // Manual path override
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

                // Action buttons
                HStack(spacing: 16) {
                    Button("Back to Settings") {
                        step = .settings
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.large)

                    Button("Skip for now") {
                        step = .sync
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

            } else if claudeHealthPassed {
                // Health check passed
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

            } else if isRunning {
                // Health check in progress
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

            } else if let error = claudeHealthError {
                // Health check failed
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
                        step = .settings
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
            } else {
                // Initial state — auto-start health check
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
                // Already verified — auto-advance to sync
                step = .sync
                runSync()
            } else if hasClaudeCLI && !claudeHealthPassed && !isRunning && claudeHealthError == nil {
                runClaudeHealthCheck()
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

                            Text("Watchtower stores everything locally — messages, digests, and analytics never leave your Mac. A local TLS certificate will be generated on your machine to securely handle the Slack OAuth callback. No data is sent to external servers.")
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
        case fast = "fast"
        case balanced = "balanced"
        case quality = "quality"

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
        case frequent = "frequent"
        case normal = "normal"
        case relaxed = "relaxed"
        case hourly = "hourly"

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
        VStack(spacing: 20) {
            Text("Configure your preferences before the first sync.")
                .foregroundStyle(.secondary)

            GroupBox {
                VStack(alignment: .leading, spacing: 16) {
                    // Language
                    settingRow(
                        title: "Language",
                        subtitle: "Digests and insights language"
                    ) {
                        Picker("", selection: $settingsLanguage) {
                            Text("English").tag("English")
                            Text("Українська").tag("Ukrainian")
                            Text("Русский").tag("Russian")
                        }
                        .frame(width: 150)
                    }

                    Divider()

                    // AI Model
                    settingRow(
                        title: "AI Model",
                        subtitle: "For digests, analysis, action items"
                    ) {
                        Picker("", selection: $settingsModelPreset) {
                            ForEach(ModelPreset.allCases, id: \.self) { preset in
                                Text(preset.title).tag(preset)
                            }
                        }
                        .frame(width: 150)
                    }
                    Text(settingsModelPreset.subtitle)
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .padding(.leading, 4)
                        .padding(.top, -12)

                    Divider()

                    // History depth
                    settingRow(
                        title: "History Depth",
                        subtitle: "How many days back to sync (max 7)"
                    ) {
                        HStack(spacing: 8) {
                            Picker("", selection: $settingsHistoryDays) {
                                Text("1 day").tag(1)
                                Text("3 days").tag(3)
                                Text("5 days").tag(5)
                                Text("7 days").tag(7)
                                Text("Custom").tag(-1)
                            }
                            .frame(width: 110)

                            if settingsHistoryDays == -1 {
                                TextField("days", text: $settingsCustomDays)
                                    .frame(width: 40)
                                    .textFieldStyle(.roundedBorder)
                            }
                        }
                    }

                    Divider()

                    // Poll interval
                    settingRow(
                        title: "Sync Frequency",
                        subtitle: "How often to check for new messages"
                    ) {
                        Picker("", selection: $settingsPollPreset) {
                            ForEach(PollPreset.allCases, id: \.self) { preset in
                                Text(preset.title).tag(preset)
                            }
                        }
                        .frame(width: 150)
                    }

                    Divider()

                    // Notifications
                    settingRow(
                        title: "Notifications",
                        subtitle: "Action items and daily digest alerts"
                    ) {
                        Toggle("", isOn: $settingsNotifications)
                            .toggleStyle(.switch)
                    }
                }
                .padding(8)
            }
            .frame(maxWidth: 500)

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

    private func settingRow<Content: View>(title: String, subtitle: String, @ViewBuilder control: () -> Content) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            control()
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
                        if self.step == .claude {
                            self.step = .sync
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
        process.arguments = [
            "-p", "respond with: OK",
            "--output-format", "text",
            "--model", model,
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

        if lower.contains("not authenticated") || lower.contains("unauthorized")
            || lower.contains("api key") || lower.contains("log in") || lower.contains("login")
        {
            return "Claude is not authenticated.\n\nOpen Terminal and run: claude\nComplete the login, then press Retry."
        }

        if lower.contains("model")
            && (lower.contains("access") || lower.contains("available")
                || lower.contains("permission") || lower.contains("not found"))
        {
            return "The selected model is not available for your account.\n\nGo back to Settings and try a different AI model (e.g. \"Fast\" for Haiku)."
        }

        if lower.contains("rate limit") || lower.contains("overloaded") || lower.contains("529") {
            return "Claude API is temporarily overloaded.\nWait a moment and press Retry."
        }

        if lower.contains("network") || lower.contains("connection")
            || lower.contains("timed out") || lower.contains("resolve")
        {
            return "Network error.\nCheck your internet connection and press Retry."
        }

        if !stderr.isEmpty {
            let truncated = stderr.count > 500 ? String(stderr.prefix(500)) + "..." : stderr
            return "Claude error (exit code \(exitCode)):\n\(truncated)"
        }

        return "Claude failed (exit code \(exitCode)).\nMake sure Claude Code is installed and authenticated."
    }

    // MARK: - Sync Step

    private var syncStep: some View {
        VStack(spacing: 16) {
            Text("Syncing your Slack data for the first time.")
                .foregroundStyle(.secondary)

            if isRunning {
                if let p = syncProgress {
                    syncProgressView(p).frame(maxWidth: 450)
                } else {
                    ProgressView().controlSize(.regular)
                    Text("Starting sync...").font(.caption).foregroundStyle(.secondary)
                }
            } else {
                Button("Start Sync") { runSync() }
                    .buttonStyle(.borderedProminent).controlSize(.large)
            }
        }
    }

    private func syncProgressView(_ p: SyncProgressData) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Syncing workspace...").font(.subheadline).fontWeight(.medium)
                Spacer()
                if let eta = syncEtaSeconds, eta > 0 {
                    Text("\(formatETA(eta)) left").font(.caption).foregroundStyle(.secondary)
                    Text("·").font(.caption).foregroundStyle(.secondary.opacity(0.5))
                }
                Text(formatElapsed(p.elapsedSec)).font(.caption).foregroundStyle(.secondary)
            }
            syncPhaseRow(label: "Discovery", icon: "magnifyingglass", phase: "Discovery", cur: p.phase,
                         done: p.discoveryPages, total: p.discoveryTotalPages,
                         detail: p.discoveryChannels > 0 ? "\(p.discoveryChannels) ch, \(p.discoveryUsers) users, \(fmtNum(p.messagesFetched)) msgs" : nil)
            syncPhaseRow(label: "Messages", icon: "message", phase: "Messages", cur: p.phase,
                         done: p.msgChannelsDone, total: p.msgChannelsTotal,
                         detail: p.messagesFetched > 0 ? "\(fmtNum(p.messagesFetched)) messages" : nil)
            syncPhaseRow(label: "Users", icon: "person.2", phase: "Users", cur: p.phase,
                         done: p.userProfilesDone, total: p.userProfilesTotal, detail: nil)
            syncPhaseRow(label: "Threads", icon: "bubble.left.and.bubble.right", phase: "Threads", cur: p.phase,
                         done: p.threadsDone, total: p.threadsTotal,
                         detail: p.threadsFetched > 0 ? "\(fmtNum(p.threadsFetched)) replies" : nil)
            if p.phase == "Done" {
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
        let c = order[cur] ?? 0, t = order[phase] ?? 0
        let isActive = c == t, isDone = c > t, isWaiting = c < t
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: isDone ? "checkmark.circle.fill" : icon)
                    .foregroundStyle(isDone ? .green : isActive ? .accentColor : .secondary.opacity(0.4))
                    .frame(width: 16)
                Text(label).font(.caption).fontWeight(isActive ? .semibold : .regular)
                    .foregroundStyle(isWaiting ? Color.secondary.opacity(0.4) : Color.primary)
                Spacer()
                if isActive && total > 0 {
                    Text("\(done)/\(total)").font(.caption2).foregroundStyle(.secondary)
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
        let i = Int(s); return i < 60 ? "\(i)s" : "\(i/60)m \(i%60)s"
    }

    private func fmtNum(_ n: Int) -> String {
        if n >= 1_000_000 { return String(format: "%.1fM", Double(n)/1_000_000) }
        if n >= 1_000 { return String(format: "%.1fK", Double(n)/1_000) }
        return "\(n)"
    }

    private func updateSyncETA(_ p: SyncProgressData) {
        // Reset timer when phase changes
        if p.phase != syncLastPhase {
            syncLastPhase = p.phase
            syncPhaseStartedAt = Date()
            syncEtaSeconds = nil
            return
        }

        guard let phaseStart = syncPhaseStartedAt else {
            syncEtaSeconds = nil
            return
        }

        // Get done/total for current phase
        let (done, total) = syncPhaseCounts(p)
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

    private func syncPhaseCounts(_ p: SyncProgressData) -> (done: Int, total: Int) {
        switch p.phase {
        case "Discovery": return (p.discoveryPages, p.discoveryTotalPages)
        case "Messages": return (p.msgChannelsDone, p.msgChannelsTotal)
        case "Users": return (p.userProfilesDone, p.userProfilesTotal)
        case "Threads": return (p.threadsDone, p.threadsTotal)
        default: return (0, 0)
        }
    }

    private func formatETA(_ seconds: Double) -> String {
        let s = Int(seconds)
        if s < 5 { return "< 5s" }
        if s < 60 { return "~\(s)s" }
        let m = s / 60, rem = s % 60
        if rem == 0 { return "~\(m)m" }
        return "~\(m)m \(rem)s"
    }

    // MARK: - CLI Execution

    /// Open OAuth in the default browser (Chrome, Firefox, Safari).
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
                    step = .settings
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
                ("sync.poll_interval", poll.interval),
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
                step = .claude
            }
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
                process.terminationHandler = { p in
                    cont.resume(returning: p.terminationStatus)
                }
            }

            // Ensure all output is read
            _ = await readTask.value

            let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
            let stderrText = String(data: stderrData, encoding: .utf8) ?? ""

            isRunning = false
            if exitCode == 0 {
                syncProgress = nil
                // Transition to main app immediately — pipelines run in background
                appState.backgroundTaskManager.startPipelines()
                onRetry()
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
