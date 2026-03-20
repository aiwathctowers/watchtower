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

            UsageSettings()
                .environment(appState)
                .tabItem { Label("Usage", systemImage: "chart.bar") }

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

    var body: some View {
        Form {
            Section("Workspace") {
                LabeledContent("Active Workspace") {
                    Text(config.activeWorkspace ?? "None")
                        .foregroundStyle(.secondary)
                }
            }

            Section("Sync") {
                TextField("Poll Interval", text: Binding(
                    get: { config.syncInterval ?? "" },
                    set: { config.syncInterval = $0 }
                ), prompt: Text("15m"))
                .help("e.g. 15m, 1h, 30s")

                TextField("Workers", value: Binding(
                    get: { config.syncWorkers },
                    set: { config.syncWorkers = $0 }
                ), format: .number, prompt: Text("1"))

                TextField("Initial History Days", value: Binding(
                    get: { config.initialHistoryDays },
                    set: { config.initialHistoryDays = $0 }
                ), format: .number, prompt: Text("30"))

                Toggle("Sync Threads", isOn: $config.syncThreads)
            }

            Section("Digest") {
                Toggle("Enabled", isOn: $config.digestEnabled)

                TextField("Model", text: Binding(
                    get: { config.digestModel ?? "" },
                    set: { config.digestModel = $0.isEmpty ? nil : $0 }
                ), prompt: Text("claude-haiku-4-5-20251001"))

                TextField("Min Messages", value: Binding(
                    get: { config.digestMinMessages },
                    set: { config.digestMinMessages = $0 }
                ), format: .number, prompt: Text("5"))

                TextField("Language", text: Binding(
                    get: { config.digestLanguage ?? "" },
                    set: { config.digestLanguage = $0.isEmpty ? nil : $0 }
                ), prompt: Text("English"))
            }

            Section("AI") {
                TextField("Model", text: Binding(
                    get: { config.aiModel ?? "" },
                    set: { config.aiModel = $0.isEmpty ? nil : $0 }
                ), prompt: Text("claude-sonnet-4-6"))

                HStack {
                    TextField("Claude CLI Path", text: Binding(
                        get: { config.claudePath ?? "" },
                        set: { config.claudePath = $0.isEmpty ? nil : $0 }
                    ), prompt: Text("auto-detect"))

                    if let path = Constants.findClaudePath() {
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

                HStack {
                    Button {
                        testClaudeConnection()
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

            updateSection
        }
        .formStyle(.grouped)
        .padding(.horizontal)
        .padding(.top, 4)
        .safeAreaInset(edge: .bottom) {
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

            case .available(let version, let notes, _):
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

    private func testClaudeConnection() {
        guard let claudePath = Constants.findClaudePath() else {
            connectionTestResult = "Claude CLI not found"
            connectionTestSuccess = false
            return
        }

        connectionTestRunning = true
        connectionTestResult = nil

        let model = (config.aiModel ?? "").isEmpty ? "claude-sonnet-4-6" : config.aiModel!

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: claudePath)
            process.arguments = ["-p", "respond with: OK", "--output-format", "text", "--model", model]

            // Use shared resolved environment (caches login shell PATH)
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
