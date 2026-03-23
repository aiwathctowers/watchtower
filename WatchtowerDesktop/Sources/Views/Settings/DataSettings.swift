import SwiftUI

struct DataSettings: View {
    @State private var showResetConfirmation = false
    @State private var showFinalConfirmation = false
    @State private var isResetting = false
    @State private var configSize: String?
    @State private var databaseSize: String?
    @State private var cacheSize: String?

    private let configDir = NSString("~/.config/watchtower").expandingTildeInPath
    private let dataDir = Constants.databasePath
    private let cacheDir = NSString("~/Library/Caches/WatchtowerDesktop").expandingTildeInPath

    var body: some View {
        Form {
            Section("Storage") {
                LabeledContent("Config") {
                    HStack {
                        Text(configDir)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        if let size = configSize {
                            Text(size)
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                        }
                    }
                }

                LabeledContent("Database") {
                    HStack {
                        Text(dataDir)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        if let size = databaseSize {
                            Text(size)
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                        }
                    }
                }

                LabeledContent("Caches") {
                    HStack {
                        Text(cacheDir)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        if let size = cacheSize {
                            Text(size)
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                        }
                    }
                }
            }

            Section("Danger Zone") {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Reset All Data")
                        .font(.headline)
                    Text("Stops the daemon, removes config, database, caches, and macOS preferences. The app will quit after reset.")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    Button(role: .destructive) {
                        showResetConfirmation = true
                    } label: {
                        if isResetting {
                            ProgressView()
                                .controlSize(.small)
                        } else {
                            Text("Reset All Data…")
                        }
                    }
                    .disabled(isResetting)
                }
                .padding(.vertical, 4)
            }
        }
        .formStyle(.grouped)
        .padding()
        .onAppear { refreshSizes() }
        .alert("Reset All Data?", isPresented: $showResetConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Continue", role: .destructive) {
                showFinalConfirmation = true
            }
        } message: {
            Text("This will permanently delete all Watchtower data including config, database, and caches. The app will quit afterwards. This cannot be undone.")
        }
        .alert("Are you sure?", isPresented: $showFinalConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Delete Everything", role: .destructive) {
                Task { await performReset() }
            }
        } message: {
            Text("Last chance. All data will be permanently deleted and the app will quit.")
        }
    }

    private func refreshSizes() {
        configSize = Self.directorySize(configDir)
        databaseSize = Self.directorySize(dataDir)
        cacheSize = Self.directorySize(cacheDir)
    }

    private func performReset() async {
        isResetting = true

        // 1. Stop daemon
        let daemon = DaemonManager()
        daemon.resolvePathIfNeeded()
        if DaemonManager.checkDaemonRunning() {
            await daemon.stopDaemon()
            // Give it a moment to stop
            try? await Task.sleep(for: .milliseconds(500))
        }

        // 2. Remove config & database
        let fm = FileManager.default
        try? fm.removeItem(atPath: configDir)
        try? fm.removeItem(atPath: dataDir)

        // 3. Remove macOS preferences & caches
        let home = NSHomeDirectory()
        let pathsToRemove = [
            "\(home)/Library/Preferences/com.watchtower.desktop.plist",
            "\(home)/Library/Preferences/WatchtowerDesktop.plist",
            "\(home)/Library/Caches/WatchtowerDesktop",
            "\(home)/Library/HTTPStorages/WatchtowerDesktop",
        ]
        for path in pathsToRemove {
            try? fm.removeItem(atPath: path)
        }

        // Remove crash reports / diagnostic logs matching pattern
        let crashDirs: [(String, String)] = [
            ("\(home)/Library/Application Support/CrashReporter", "WatchtowerDesktop_"),
            ("\(home)/Library/Logs/DiagnosticReports", "WatchtowerDesktop-"),
        ]
        for (dir, prefix) in crashDirs {
            if let files = try? fm.contentsOfDirectory(atPath: dir) {
                for file in files where file.hasPrefix(prefix) {
                    try? fm.removeItem(atPath: "\(dir)/\(file)")
                }
            }
        }

        // Remove UserDefaults
        if let bundleID = Bundle.main.bundleIdentifier {
            UserDefaults.standard.removePersistentDomain(forName: bundleID)
        }
        UserDefaults.standard.removePersistentDomain(forName: "com.watchtower.desktop")
        UserDefaults.standard.removePersistentDomain(forName: "WatchtowerDesktop")

        // L5 fix: dispatch terminate to main thread explicitly (not from async context)
        await MainActor.run {
            NSApplication.shared.terminate(nil)
        }
    }

    private static func directorySize(_ path: String) -> String? {
        let fm = FileManager.default
        guard fm.fileExists(atPath: path) else { return nil }

        var totalSize: UInt64 = 0
        guard let enumerator = fm.enumerator(atPath: path) else { return nil }

        while let file = enumerator.nextObject() as? String {
            let fullPath = "\(path)/\(file)"
            if let attrs = try? fm.attributesOfItem(atPath: fullPath),
               let size = attrs[.size] as? UInt64 {
                totalSize += size
            }
        }

        return ByteCountFormatter.string(fromByteCount: Int64(totalSize), countStyle: .file)
    }
}
