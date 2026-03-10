import SwiftUI

enum SidebarDestination: String, CaseIterable, Identifiable {
    case chat
    case actions
    case digests
    case people
    case channels
    case search

    var id: String { rawValue }

    var title: String {
        switch self {
        case .chat: "AI Chat"
        case .actions: "Actions"
        case .digests: "Digests"
        case .people: "People"
        case .channels: "Channels"
        case .search: "Search"
        }
    }

    var icon: String {
        switch self {
        case .chat: "bubble.left.and.bubble.right"
        case .actions: "checklist"
        case .digests: "doc.text.magnifyingglass"
        case .people: "person.2"
        case .channels: "number"
        case .search: "magnifyingglass"
        }
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

    var body: some View {
        @Bindable var state = appState
        VStack(spacing: 0) {
            // Content
            HStack(spacing: 0) {
                // Toggle menu button (always visible)
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        showMenu.toggle()
                    }
                } label: {
                    Image(systemName: "sidebar.leading")
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .frame(width: 28, height: 28)
                }
                .buttonStyle(.borderless)
                .help("Toggle Menu")
                .keyboardShortcut("b", modifiers: [.command])
                .padding(.leading, 8)

                if showMenu {
                    // Left sidebar (menu)
                    SidebarView(selection: $state.selectedDestination)
                        .frame(width: 180)
                        .transition(.move(edge: .leading).combined(with: .opacity))

                    Divider()
                }

                // Main content
                detailView
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(nsColor: .controlBackgroundColor))
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
        case .channels:
            ChannelListView()
        case .search:
            SearchView()
        }
    }
}

// MARK: - Onboarding

enum OnboardingStep: Int, CaseIterable {
    case connect = 0
    case settings = 1
    case sync = 2
    case generate = 3
    case ready = 4

    var title: String {
        switch self {
        case .connect: "Connect"
        case .settings: "Settings"
        case .sync: "Sync"
        case .generate: "Insights"
        case .ready: "Ready"
        }
    }
}

struct OnboardingView: View {
    let errorMessage: String?
    let onRetry: () -> Void

    @State private var step: OnboardingStep = .connect
    @State private var isRunning = false
    @State private var output = ""
    @State private var cliError: String?
    @State private var syncProgress: SyncProgressData?
    @State private var digestProgress: InsightProgressData?
    @State private var actionsProgress: InsightProgressData?
    @State private var peopleProgress: InsightProgressData?
    @State private var totalTokensIn = 0
    @State private var totalTokensOut = 0
    @State private var totalCost = 0.0

    // Settings
    @State private var settingsLanguage = "English"
    @State private var settingsHistoryDays = 3
    @State private var settingsCustomDays = ""
    @State private var settingsModelPreset = ModelPreset.balanced
    @State private var settingsPollPreset = PollPreset.normal
    @State private var settingsNotifications = true

    private var cliPath: String? { Constants.findCLIPath() }
    private var claudePath: String? { Constants.findClaudePath() }
    private var hasClaudeCLI: Bool { claudePath != nil }
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
            case .sync:
                syncStep
            case .generate:
                generateStep
            case .ready:
                readyStep
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
            // If config already exists, skip to settings
            if FileManager.default.fileExists(atPath: Constants.configPath) {
                step = .settings
            }
        }
    }

    // MARK: - Steps Indicator

    private var stepsIndicator: some View {
        HStack(spacing: 4) {
            ForEach(OnboardingStep.allCases, id: \.rawValue) { s in
                HStack(spacing: 4) {
                    Circle()
                        .fill(s.rawValue <= step.rawValue ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 8, height: 8)
                    Text(s.title)
                        .font(.caption)
                        .foregroundStyle(s.rawValue <= step.rawValue ? .primary : .secondary)
                }
                if s != OnboardingStep.allCases.last {
                    Rectangle()
                        .fill(s.rawValue < step.rawValue ? Color.accentColor : Color.secondary.opacity(0.3))
                        .frame(width: 30, height: 1)
                }
            }
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
            } else if !hasClaudeCLI {
                VStack(spacing: 8) {
                    Label("Claude Code CLI not found", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                        .font(.subheadline)
                    Text("AI features (chat, digests, action items) require Claude Code.\nInstall it: **npm install -g @anthropic-ai/claude-code**")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)

                    Button {
                        runAuthLogin()
                    } label: {
                        Text("Continue without AI")
                            .frame(minWidth: 200)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.large)
                    .disabled(isRunning)

                    Button {
                        runAuthLogin()
                    } label: {
                        Text("I've installed it, Connect to Slack")
                            .frame(minWidth: 200)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .disabled(isRunning)
                }
            } else {
                Button {
                    runAuthLogin()
                } label: {
                    HStack {
                        if isRunning {
                            ProgressView()
                                .controlSize(.small)
                        }
                        Text(isRunning ? "Waiting for browser..." : "Connect to Slack")
                    }
                    .frame(minWidth: 200)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(isRunning)

                if isRunning {
                    Text("A browser window should have opened.\nComplete the Slack authorization and return here.")
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

    // MARK: - Generate Step

    private var generateStep: some View {
        VStack(spacing: 16) {
            Text("Generating insights from your Slack data.")
                .foregroundStyle(.secondary)

            if isRunning {
                VStack(alignment: .leading, spacing: 12) {
                    insightRow(
                        label: "Digests",
                        icon: "doc.text.magnifyingglass",
                        progress: digestProgress
                    )
                    insightRow(
                        label: "People",
                        icon: "person.2",
                        progress: peopleProgress
                    )
                    insightRow(
                        label: "Action Items",
                        icon: "checklist",
                        progress: actionsProgress
                    )

                    if totalTokensIn > 0 || totalTokensOut > 0 {
                        Divider()
                        HStack {
                            Image(systemName: "cpu")
                                .foregroundStyle(.secondary)
                            Text("Tokens: \(fmtNum(totalTokensIn)) in + \(fmtNum(totalTokensOut)) out")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            if totalCost > 0 {
                                Text("| $\(String(format: "%.4f", totalCost))")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                .padding()
                .frame(maxWidth: 450)
                .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 8))
            }
        }
    }

    private func insightRow(label: String, icon: String, progress: InsightProgressData?) -> some View {
        let isFinished = progress?.finished == true
        let hasError = progress?.error != nil && !(progress?.error?.isEmpty ?? true)
        let isActive = progress != nil && !isFinished
        let done = progress?.done ?? 0
        let total = progress?.total ?? 0
        let itemsFound = progress?.itemsFound ?? 0

        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                if isFinished && !hasError {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                        .frame(width: 16)
                } else if hasError {
                    Image(systemName: "exclamationmark.circle.fill")
                        .foregroundStyle(.red)
                        .frame(width: 16)
                } else {
                    Image(systemName: icon)
                        .foregroundStyle(isActive ? Color.accentColor : Color.secondary.opacity(0.4))
                        .frame(width: 16)
                }

                Text(label)
                    .font(.caption)
                    .fontWeight(isActive ? .semibold : .regular)
                    .foregroundStyle(progress == nil ? Color.secondary.opacity(0.4) : Color.primary)

                Spacer()

                if isFinished && !hasError {
                    Text("\(itemsFound) generated")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                } else if isFinished && hasError {
                    Text("failed")
                        .font(.caption2)
                        .foregroundStyle(.red)
                } else if isActive && total > 0 {
                    Text("\(done)/\(total)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }

            if isActive && total > 0 {
                ProgressView(value: Double(done), total: Double(max(total, 1)))
                    .tint(.accentColor)
            } else if isActive {
                ProgressView()
                    .controlSize(.small)
                    .scaleEffect(0.7, anchor: .leading)
            }

            if isActive, let status = progress?.status, !status.isEmpty {
                Text(status)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
        }
    }

    // MARK: - Ready Step

    private var readyStep: some View {
        VStack(spacing: 16) {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 32))
                .foregroundStyle(.green)

            Text("You're all set!")
                .font(.headline)

            Button("Get Started") {
                onRetry()
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
        }
    }

    // MARK: - CLI Execution

    private func runAuthLogin() {
        guard let path = cliPath else { return }
        isRunning = true
        cliError = nil
        output = ""

        Task.detached {
            let result = await Self.runCLI(path: path, arguments: ["auth", "login"])
            await MainActor.run {
                isRunning = false
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
                step = .sync
                runSync()
            }
        }
    }

    private func runSync() {
        guard let path = cliPath else { return }
        isRunning = true
        cliError = nil
        syncProgress = nil

        Task {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)
            process.arguments = ["sync", "--progress-json"]
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
                step = .generate
                syncProgress = nil
                runGenerate()
            } else {
                cliError = stderrText.isEmpty
                    ? "Sync failed (exit code \(exitCode))"
                    : stderrText
            }
        }
    }

    private func runGenerate() {
        guard let path = cliPath else { return }
        isRunning = true
        cliError = nil
        digestProgress = nil
        actionsProgress = nil
        peopleProgress = nil
        totalTokensIn = 0
        totalTokensOut = 0
        totalCost = 0.0

        Task {
            // Phase 1: Digests + People in parallel (independent)
            await withTaskGroup(of: (String, InsightProgressData?).self) { group in
                let pipelines: [(String, [String])] = [
                    ("digest", ["digest", "generate", "--progress-json"]),
                    ("people", ["people", "generate", "--progress-json"]),
                ]

                for (name, args) in pipelines {
                    group.addTask { [path] in
                        let final = await self.runPipelineWithProgress(path: path, arguments: args, pipeline: name)
                        return (name, final)
                    }
                }

                for await (_, final) in group {
                    if let f = final {
                        totalTokensIn += f.inputTokens
                        totalTokensOut += f.outputTokens
                        totalCost += f.costUsd
                    }
                }
            }

            // Phase 2: Action items (depend on digests for related_digest_ids)
            let actionsFinal = await runPipelineWithProgress(path: path, arguments: ["actions", "generate", "--progress-json"], pipeline: "actions")
            if let f = actionsFinal {
                totalTokensIn += f.inputTokens
                totalTokensOut += f.outputTokens
                totalCost += f.costUsd
            }

            // Start daemon
            _ = await Self.runCLI(path: path, arguments: ["sync", "--daemon", "--detach"])

            isRunning = false
            step = .ready
            onRetry()
        }
    }

    private func runPipelineWithProgress(path: String, arguments: [String], pipeline: String) async -> InsightProgressData? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = arguments
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            return nil
        }

        let decoder = JSONDecoder()

        // Read JSON lines
        let readTask = Task<InsightProgressData?, Never> {
            var final: InsightProgressData?
            do {
                for try await line in stdoutPipe.fileHandleForReading.bytes.lines {
                    if let data = line.data(using: .utf8),
                       let json = try? decoder.decode(InsightProgressData.self, from: data) {
                        await MainActor.run {
                            switch pipeline {
                            case "digest": self.digestProgress = json
                            case "actions": self.actionsProgress = json
                            case "people": self.peopleProgress = json
                            default: break
                            }
                        }
                        if json.finished == true {
                            final = json
                        }
                    }
                }
            } catch {
                // EOF
            }
            return final
        }

        // Wait for process exit
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            process.terminationHandler = { _ in
                cont.resume()
            }
        }

        let lastFinished = await readTask.value

        // Read stderr (for debugging, not shown to user)
        _ = stderrPipe.fileHandleForReading.readDataToEndOfFile()

        return lastFinished
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
