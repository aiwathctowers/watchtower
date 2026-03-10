import Foundation

enum Constants {
    static let configPath = NSString("~/.config/watchtower/config.yaml").expandingTildeInPath
    static let databasePath = NSString("~/.local/share/watchtower").expandingTildeInPath
    static let bundleID = "com.watchtower.desktop"

    enum NotificationCategory {
        static let decision = "DECISION"
        static let dailySummary = "DAILY_SUMMARY"
    }

    /// Check if Claude Code CLI is available.
    nonisolated static func findClaudePath() -> String? {
        let paths = [
            "/usr/local/bin/claude",
            "/opt/homebrew/bin/claude",
            NSString("~/.claude/bin/claude").expandingTildeInPath,
        ]

        // Check known paths
        for path in paths {
            if FileManager.default.isExecutableFile(atPath: path) {
                return path
            }
        }

        // Search nvm node versions
        let nvmDir = NSString("~/.nvm/versions/node").expandingTildeInPath
        if let versions = try? FileManager.default.contentsOfDirectory(atPath: nvmDir) {
            for v in versions.sorted().reversed() {
                let path = "\(nvmDir)/\(v)/bin/claude"
                if FileManager.default.isExecutableFile(atPath: path) {
                    return path
                }
            }
        }

        // Fallback: which via login shell
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/zsh")
        process.arguments = ["-lc", "which claude"]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice
        try? process.run()
        process.waitUntilExit()

        if process.terminationStatus == 0 {
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            if let path = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines),
               !path.isEmpty {
                return path
            }
        }

        return nil
    }

    /// Resolve the watchtower CLI binary path.
    /// Priority: app bundle → known system paths → `which` lookup.
    nonisolated static func findCLIPath() -> String? {
        // 1. Inside the app bundle
        if let bundlePath = Bundle.main.executableURL?
            .deletingLastPathComponent()
            .appendingPathComponent("watchtower").path,
           FileManager.default.isExecutableFile(atPath: bundlePath) {
            return bundlePath
        }

        // 2. Known system paths
        let paths = [
            "/usr/local/bin/watchtower",
            "/opt/homebrew/bin/watchtower",
            NSString("~/go/bin/watchtower").expandingTildeInPath,
        ]
        for path in paths {
            if FileManager.default.isExecutableFile(atPath: path) {
                return path
            }
        }

        // 3. which
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/which")
        process.arguments = ["watchtower"]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice
        try? process.run()
        process.waitUntilExit()

        if process.terminationStatus == 0 {
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            if let path = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines),
               !path.isEmpty {
                return path
            }
        }

        return nil
    }
}
