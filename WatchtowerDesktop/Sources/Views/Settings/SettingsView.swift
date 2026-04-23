import SwiftUI

struct SettingsView: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        TabView {
            GeneralSettings()
                .environment(appState)
                .tabItem { Label("General", systemImage: "gear") }

            ProfileSettings()
                .environment(appState)
                .tabItem { Label("Profile", systemImage: "person.crop.circle") }

            NotificationSettings()
                .tabItem { Label("Notifications", systemImage: "bell") }

            DaemonSettings()
                .tabItem { Label("Daemon", systemImage: "arrow.triangle.2.circlepath") }

            LogsSettings()
                .tabItem { Label("Logs", systemImage: "doc.text") }

            DataSettings()
                .environment(appState)
                .tabItem { Label("Data", systemImage: "externaldrive") }
        }
        .frame(width: 700, height: 550)
    }
}

struct GeneralSettings: View {
    @Environment(AppState.self) private var appState
    @State private var config = ConfigService()
    @State private var saveError: String?
    @State private var showSaved = false
    @State private var connectionTestRunning = false
    @State private var connectionTestResult: String?
    @State private var connectionTestSuccess = false
    @State private var daemonManager = DaemonManager()
    @State private var slackReconnecting = false
    @State private var slackReconnectResult: String?
    @State private var slackReconnectSuccess = false
    @State private var slackAuthProcess: Process?
    @State private var googleAuth = GoogleAuthService()
    @State private var jiraAuth = JiraAuthService()

    var body: some View {
        Form {
            workspaceSection
            syncSection
            digestSection
            briefingSection
            dayPlanSection
            aiSection
            calendarSettingsSection
            jiraSettingsSection

            if let error = config.parseError {
                Section("Parse Error") {
                    Text(error).foregroundStyle(.red)
                }
            }

            if let error = saveError {
                Section("Save Error") {
                    Text(error).foregroundStyle(.red)
                }
            }

            usageLinkSection
            updateSection
        }
        .formStyle(.grouped)
        .padding(.horizontal)
        .padding(.top, 4)
        .safeAreaInset(edge: .bottom) {
            bottomBar
        }
    }

    private var workspaceSection: some View {
        Section("Workspace") {
            LabeledContent("Active Workspace") {
                Text(config.activeWorkspace ?? "None")
                    .foregroundStyle(.secondary)
            }

            HStack {
                Button {
                    reconnectSlack()
                } label: {
                    HStack(spacing: 4) {
                        if slackReconnecting {
                            ProgressView()
                                .controlSize(.small)
                        }
                        Text(slackReconnecting ? "Connecting..." : "Reconnect Slack")
                    }
                }
                .disabled(slackReconnecting)

                if slackReconnecting {
                    Button("Cancel") {
                        cancelSlackReconnect()
                    }
                }

                if let result = slackReconnectResult {
                    Image(systemName: slackReconnectSuccess ? "checkmark.circle.fill" : "xmark.circle.fill")
                        .foregroundStyle(slackReconnectSuccess ? .green : .red)
                    Text(result)
                        .font(.caption)
                        .foregroundStyle(slackReconnectSuccess ? .green : .red)
                        .lineLimit(3)
                        .textSelection(.enabled)
                }
            }
        }
    }

    private var syncSection: some View {
        Section("Sync") {
            TextField(
                "Poll Interval",
                text: Binding(
                    get: { config.syncInterval ?? "" },
                    set: { config.syncInterval = $0 }
                ),
                prompt: Text("15m")
            )
            .help("e.g. 15m, 1h, 30s")

            TextField(
                "Workers",
                value: Binding(
                    get: { config.syncWorkers },
                    set: { config.syncWorkers = $0 }
                ),
                format: .number,
                prompt: Text("1")
            )

            TextField(
                "Initial History Days",
                value: Binding(
                    get: { config.initialHistoryDays },
                    set: { config.initialHistoryDays = $0 }
                ),
                format: .number,
                prompt: Text("30")
            )

            Toggle("Sync Threads", isOn: $config.syncThreads)
        }
    }

    private var digestSection: some View {
        Section("Digest") {
            Toggle("Enabled", isOn: $config.digestEnabled)

            TextField(
                "Model",
                text: Binding(
                    get: { config.digestModel ?? "" },
                    set: { config.digestModel = $0.isEmpty ? nil : $0 }
                ),
                prompt: Text("claude-haiku-4-5-20251001")
            )

            TextField(
                "Min Messages",
                value: Binding(
                    get: { config.digestMinMessages },
                    set: { config.digestMinMessages = $0 }
                ),
                format: .number,
                prompt: Text("5")
            )

            TextField(
                "Language",
                text: Binding(
                    get: { config.digestLanguage ?? "" },
                    set: { config.digestLanguage = $0.isEmpty ? nil : $0 }
                ),
                prompt: Text("English")
            )
        }
    }

    private var briefingSection: some View {
        Section("Briefing") {
            Picker(
                "Briefing Hour",
                selection: $config.briefingHour
            ) {
                ForEach(0..<24, id: \.self) { hour in
                    Text(String(format: "%02d:00", hour)).tag(hour)
                }
            }
            .help("Hour of day when daily briefing should be generated (0-23)")
        }
    }

    private var dayPlanSection: some View {
        Section("Day Plan") {
            Toggle("Enable day plan", isOn: $config.dayPlanEnabled)

            Picker("Generate at hour", selection: $config.dayPlanHour) {
                ForEach(5..<13, id: \.self) { h in
                    Text(String(format: "%02d:00", h)).tag(h)
                }
            }
            .help("Hour of day when the day plan should be generated (5-12)")

            HStack {
                Text("Working hours:")
                TextField(
                    "Start",
                    text: $config.workingHoursStart,
                    prompt: Text("09:00")
                )
                .frame(width: 70)
                Text("–")
                TextField(
                    "End",
                    text: $config.workingHoursEnd,
                    prompt: Text("19:00")
                )
                .frame(width: 70)
            }
            .help("Working window used when scheduling time blocks (HH:MM)")

            Stepper(
                "Max timeblocks: \(config.maxTimeblocks)",
                value: $config.maxTimeblocks,
                in: 1...5
            )
            .help("Maximum number of focused time blocks per day")

            HStack {
                Stepper(
                    "Backlog min: \(config.minBacklog)",
                    value: $config.minBacklog,
                    in: 1...10
                )
                Stepper(
                    "Backlog max: \(config.maxBacklog)",
                    value: $config.maxBacklog,
                    in: 1...15
                )
            }
            .help("Minimum and maximum backlog items shown in the day plan")
        }
    }

    private var aiSection: some View {
        Section(header: Text("AI")) {
            Picker(
                "AI Provider",
                selection: Binding(
                    get: { config.aiProvider ?? "claude" },
                    set: { newProvider in
                        let oldProvider = config.aiProvider ?? "claude"
                        config.aiProvider = newProvider
                        // Reset model when switching providers so it doesn't carry over
                        if newProvider != oldProvider {
                            config.aiModel = nil
                            connectionTestResult = nil
                        }
                    }
                )
            ) {
                Text("Claude").tag("claude")
                Text("Codex").tag("codex")
            }

            TextField(
                "Model",
                text: Binding(
                    get: { config.aiModel ?? "" },
                    set: { config.aiModel = $0.isEmpty ? nil : $0 }
                ),
                prompt: Text(config.aiProvider == "codex" ? "gpt-5.4" : "claude-sonnet-4-6")
            )

            TextField(
                "Workers",
                value: Binding(
                    get: { config.aiWorkers },
                    set: { config.aiWorkers = $0 }
                ),
                format: .number,
                prompt: Text("5")
            )
            .help("Max parallel LLM calls across all pipelines")

            HStack {
                TextField(
                    "Claude CLI Path",
                    text: Binding(
                        get: { config.claudePath ?? "" },
                        set: { config.claudePath = $0.isEmpty ? nil : $0 }
                    ),
                    prompt: Text("auto-detect")
                )

                if let path = Constants.findInPath("claude") {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                        .help("Found: \(path)")
                } else {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.red)
                        .help("Claude CLI not found")
                }
            }
            .help("Override auto-detection. Run 'which claude' in terminal to find the path.")

            if config.aiProvider == "codex" {
                HStack {
                    TextField(
                        "Codex CLI Path",
                        text: Binding(
                            get: { config.codexPath ?? "" },
                            set: { config.codexPath = $0.isEmpty ? nil : $0 }
                        ),
                        prompt: Text("auto-detect")
                    )

                    if let path = Constants.findInPath("codex") {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(.green)
                            .help("Found: \(path)")
                    } else {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundStyle(.red)
                            .help("Codex CLI not found")
                    }
                }
                .help("Override auto-detection. Run 'which codex' in terminal to find the path.")
            }

            HStack {
                Button {
                    testConnection()
                } label: {
                    HStack(spacing: 4) {
                        if connectionTestRunning {
                            ProgressView()
                                .controlSize(.small)
                        }
                        Text(connectionTestRunning ? "Testing..." : "Test Connection")
                    }
                }
                .disabled(connectionTestRunning)

                if let result = connectionTestResult {
                    Image(systemName: connectionTestSuccess ? "checkmark.circle.fill" : "xmark.circle.fill")
                        .foregroundStyle(connectionTestSuccess ? .green : .red)
                    Text(result)
                        .font(.caption)
                        .foregroundStyle(connectionTestSuccess ? .green : .red)
                        .lineLimit(3)
                        .textSelection(.enabled)
                }
            }
        }
    }

    private var calendarSettingsSection: some View {
        Section("Google Calendar") {
            if googleAuth.isConnected {
                HStack {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    Text("Connected")
                    Spacer()
                    Button("Disconnect") {
                        googleAuth.disconnect()
                        config.calendarEnabled = false
                        saveConfig()
                    }
                }

                Toggle("Enable calendar sync", isOn: $config.calendarEnabled)
                    .onChange(of: config.calendarEnabled) { _, _ in saveConfig() }

                Picker("Sync days ahead", selection: $config.calendarSyncDaysAhead) {
                    Text("2 days").tag(2)
                    Text("3 days").tag(3)
                    Text("5 days").tag(5)
                    Text("7 days").tag(7)
                    Text("14 days").tag(14)
                }
            } else {
                HStack {
                    Image(systemName: "calendar.badge.plus")
                        .foregroundStyle(.secondary)
                    Text("Not connected")
                    Spacer()

                    if googleAuth.isAuthenticating {
                        ProgressView().controlSize(.small)
                        Button("Cancel") { googleAuth.cancelConnect() }
                    } else {
                        Button("Connect") {
                            googleAuth.connect()
                        }
                        .buttonStyle(.borderedProminent)
                    }
                }
            }

            if let err = googleAuth.error {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .onChange(of: googleAuth.isConnected) { _, connected in
            if connected && !config.calendarEnabled {
                config.calendarEnabled = true
                saveConfig()
            }
        }
    }

    @ViewBuilder
    private var jiraSettingsSection: some View {
        Section("Jira") {
            if jiraAuth.isConnected {
                HStack {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Connected")
                        if let site = jiraAuth.siteURL {
                            Text(site)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        if let user = jiraAuth.userDisplayName {
                            Text(user)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer()
                    Button("Disconnect") {
                        jiraAuth.disconnect()
                    }
                }
            } else {
                HStack {
                    Image(systemName: "bolt.horizontal.circle")
                        .foregroundStyle(.secondary)
                    Text("Not connected")
                    Spacer()

                    if jiraAuth.isAuthenticating {
                        ProgressView().controlSize(.small)
                        Button("Cancel") {
                            jiraAuth.cancelConnect()
                        }
                    } else {
                        Button("Connect") { jiraAuth.connect() }
                            .buttonStyle(.borderedProminent)
                    }
                }
            }

            if let err = jiraAuth.error {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }

        if jiraAuth.isConnected {
            Section {
                Button {
                    appState.selectedDestination = .boards
                } label: {
                    HStack {
                        Label(
                            "Manage Boards",
                            systemImage: "rectangle.on.rectangle.angled"
                        )
                        Spacer()
                        Image(systemName: "chevron.right")
                            .foregroundStyle(.secondary)
                    }
                }
                .buttonStyle(.plain)
            }
        }
    }

    private var bottomBar: some View {
        VStack(spacing: 0) {
            Divider()
            HStack {
                Button("Open in Editor") {
                    config.openInEditor()
                }

                Button("Reveal in Finder") {
                    config.revealInFinder()
                }

                Spacer()

                if showSaved {
                    Text("Saved")
                        .foregroundStyle(.green)
                        .transition(.opacity)
                }

                Button("Reload") {
                    config.reload()
                    saveError = nil
                }

                Button("Save") {
                    do {
                        try config.save()
                        saveError = nil
                        withAnimation { showSaved = true }
                        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                            withAnimation { showSaved = false }
                        }
                    } catch {
                        saveError = error.localizedDescription
                    }
                }
                .keyboardShortcut("s", modifiers: .command)
                .buttonStyle(.borderedProminent)
            }
            .padding(.horizontal)
            .padding(.vertical, 10)
        }
        .background(Color(nsColor: .windowBackgroundColor))
    }

    // MARK: - Helpers

    private func saveConfig() {
        do {
            try config.save()
            saveError = nil
        } catch {
            saveError = error.localizedDescription
        }
    }

    // MARK: - Usage Link

    private var usageLinkSection: some View {
        Section {
            Button {
                NSApp.keyWindow?.close()
                appState.selectedDestination = .usage
            } label: {
                Label("View Usage & Pipeline Progress", systemImage: "chart.bar")
            }
        }
    }

    // MARK: - Update Section

    @ViewBuilder
    private var updateSection: some View {
        Section("Update") {
            let service = appState.updateService

            LabeledContent("Current Version") {
                Text(Constants.appVersion)
                    .foregroundStyle(.secondary)
            }

            switch service.state {
            case .idle:
                HStack {
                    Text("No updates available")
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Check for Updates") {
                        Task { await service.checkForUpdates() }
                    }
                }

            case .checking:
                HStack {
                    ProgressView()
                        .controlSize(.small)
                    Text("Checking for updates...")
                        .foregroundStyle(.secondary)
                }

            case let .available(version, notes, _):
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Label("Version \(version) available", systemImage: "arrow.down.circle.fill")
                            .foregroundStyle(.blue)
                        Spacer()
                        Button("Download") {
                            Task { await service.downloadUpdate() }
                        }
                        .buttonStyle(.borderedProminent)
                    }
                    if !notes.isEmpty {
                        Text(notes)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(4)
                    }
                }

            case .downloading(let progress):
                HStack {
                    ProgressView(value: progress)
                        .frame(maxWidth: 200)
                    Text("Downloading...")
                        .foregroundStyle(.secondary)
                }

            case .readyToInstall:
                HStack {
                    Label("Ready to install", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                    Spacer()
                    Button("Install & Restart") {
                        Task { await service.install(daemonManager: daemonManager) }
                    }
                    .buttonStyle(.borderedProminent)
                }

            case .installing:
                HStack {
                    ProgressView()
                        .controlSize(.small)
                    Text("Installing update...")
                        .foregroundStyle(.secondary)
                }

            case .error(let message):
                VStack(alignment: .leading, spacing: 4) {
                    Label("Update error", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                    Text(message)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Button("Retry") {
                        Task { await service.checkForUpdates() }
                    }
                }
            }
        }
    }

    private func reconnectSlack() {
        guard let cliPath = Constants.findCLIPath() else {
            slackReconnectResult = "watchtower CLI not found"
            slackReconnectSuccess = false
            return
        }

        slackReconnecting = true
        slackReconnectResult = nil
        slackReconnectSuccess = false

        Task.detached {
            // Ensure TLS cert is trusted first
            let trustResult = await Self.runCLIProcess(path: cliPath, arguments: ["auth", "trust-cert"])
            if trustResult.exitCode != 0 {
                await MainActor.run {
                    slackReconnecting = false
                    slackReconnectResult = trustResult.stderr.isEmpty
                        ? "Failed to set up secure connection"
                        : String(trustResult.stderr.prefix(200))
                }
                return
            }

            await MainActor.run {
                slackReconnectResult = "Complete authorization in your browser..."
            }

            // Run auth login (opens browser) — keep reference to process for cancellation
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = ["auth", "login"]
            process.environment = Constants.resolvedEnvironment()

            let stdoutPipe = Pipe()
            let stderrPipe = Pipe()
            process.standardOutput = stdoutPipe
            process.standardError = stderrPipe

            do {
                try process.run()
            } catch {
                await MainActor.run {
                    slackReconnecting = false
                    slackReconnectResult = "Failed to launch: \(error.localizedDescription)"
                }
                return
            }

            await MainActor.run {
                slackAuthProcess = process
            }

            process.waitUntilExit()

            let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
            let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
            let stderr = String(data: stderrData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            _ = String(data: stdoutData, encoding: .utf8) // consume stdout

            await MainActor.run {
                slackAuthProcess = nil
                slackReconnecting = false

                let exitCode = process.terminationStatus
                if exitCode == 0 {
                    slackReconnectSuccess = true
                    slackReconnectResult = "Connected"
                    config.reload()
                } else if exitCode == 15 || exitCode == 9 {
                    // SIGTERM / SIGKILL — user cancelled
                    slackReconnectResult = nil
                } else {
                    slackReconnectResult = stderr.isEmpty
                        ? "Authentication failed (exit \(exitCode))"
                        : String(stderr.prefix(200))
                }
            }
        }
    }

    private func cancelSlackReconnect() {
        if let process = slackAuthProcess, process.isRunning {
            process.terminate()
        }
        slackAuthProcess = nil
        slackReconnecting = false
        slackReconnectResult = nil
    }

    private static func runCLIProcess(path: String, arguments: [String]) async -> (exitCode: Int32, stdout: String, stderr: String) {
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
            return (-1, "", error.localizedDescription)
        }

        process.waitUntilExit()

        let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
        let stdout = String(data: stdoutData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let stderr = String(data: stderrData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

        return (process.terminationStatus, stdout, stderr)
    }

    private func testConnection() {
        let isCodex = (config.aiProvider ?? "claude") == "codex"
        let cliPath: String? = isCodex ? Constants.findInPath("codex") : Constants.findInPath("claude")
        let providerName = isCodex ? "Codex" : "Claude"
        let defaultModel = isCodex ? "gpt-5.4" : "claude-sonnet-4-6"

        guard let path = cliPath else {
            connectionTestResult = "\(providerName) CLI not found"
            connectionTestSuccess = false
            return
        }

        connectionTestRunning = true
        connectionTestResult = nil

        let model = (config.aiModel ?? "").isEmpty ? defaultModel : (config.aiModel ?? defaultModel)

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: path)

            if isCodex {
                process.arguments = ["exec", "--model", model, "--json", "--skip-git-repo-check", "-c", "approval_policy=never", "respond with: OK"]
            } else {
                process.arguments = ["-p", "respond with: OK", "--output-format", "text", "--model", model]
            }

            process.environment = Constants.resolvedEnvironment()

            let stdoutPipe = Pipe()
            let stderrPipe = Pipe()
            process.standardOutput = stdoutPipe
            process.standardError = stderrPipe

            do {
                try process.run()
            } catch {
                await MainActor.run {
                    connectionTestRunning = false
                    connectionTestSuccess = false
                    connectionTestResult = "Failed to launch: \(error.localizedDescription)"
                }
                return
            }

            process.waitUntilExit()

            let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
            let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
            let stdout = String(data: stdoutData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            let stderr = String(data: stderrData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

            await MainActor.run {
                connectionTestRunning = false
                if process.terminationStatus == 0 && !stdout.isEmpty {
                    connectionTestSuccess = true
                    connectionTestResult = "Connected (\(model))"
                } else {
                    connectionTestSuccess = false
                    connectionTestResult = Self.diagnoseError(stderr: stderr, exitCode: process.terminationStatus)
                }
            }
        }
    }

    private static func diagnoseError(stderr: String, exitCode: Int32) -> String {
        let lower = stderr.lowercased()
        if lower.contains("not authenticated") || lower.contains("unauthorized")
            || lower.contains("api key") || lower.contains("log in") || lower.contains("login") {
            return "Not authenticated. Run 'claude' in Terminal."
        }
        if lower.contains("model") && (lower.contains("access") || lower.contains("available") || lower.contains("permission")) {
            return "Model not available for your account."
        }
        if lower.contains("rate limit") || lower.contains("overloaded") {
            return "API overloaded. Try again later."
        }
        if lower.contains("network") || lower.contains("connection") || lower.contains("timed out") {
            return "Network error."
        }
        if !stderr.isEmpty {
            let short = stderr.count > 200 ? String(stderr.prefix(200)) + "..." : stderr
            return "Error (exit \(exitCode)): \(short)"
        }
        return "Failed (exit code \(exitCode))"
    }
}
