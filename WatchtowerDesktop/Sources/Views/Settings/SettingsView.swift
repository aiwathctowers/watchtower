import SwiftUI

struct SettingsView: View {
    var body: some View {
        TabView {
            GeneralSettings()
                .tabItem { Label("General", systemImage: "gear") }

            NotificationSettings()
                .tabItem { Label("Notifications", systemImage: "bell") }

            DaemonSettings()
                .tabItem { Label("Daemon", systemImage: "arrow.triangle.2.circlepath") }
        }
        .frame(width: 500, height: 480)
    }
}

struct GeneralSettings: View {
    @State private var config = ConfigService()
    @State private var saveError: String?
    @State private var showSaved = false

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
        }
        .formStyle(.grouped)
        .padding(.horizontal)
        .padding(.top, 4)
        .safeAreaInset(edge: .bottom) {
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
            .padding()
        }
    }
}
