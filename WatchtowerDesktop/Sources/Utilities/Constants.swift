import Foundation

enum Constants {
    static let configPath = NSString("~/.config/watchtower/config.yaml").expandingTildeInPath
    static let databasePath = NSString("~/.local/share/watchtower").expandingTildeInPath
    static let bundleID = "com.watchtower.desktop"
    static let configDir = NSString("~/.config/watchtower").expandingTildeInPath

    /// Safe working directory for subprocesses — avoids TCC prompts for ~/Music, ~/Downloads etc.
    /// Uses ~/.config/watchtower (already ours, not TCC-protected).
    nonisolated static func processWorkingDirectory() -> URL {
        let dir = configDir
        let fm = FileManager.default
        if !fm.fileExists(atPath: dir) {
            try? fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
        }
        return URL(fileURLWithPath: dir)
    }

    /// App version — reads from Info.plist (set at build time), falls back to hardcoded default.
    static let appVersion: String = {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.2.0"
    }()

    /// UserDefaults key for tracking whether initial pipelines have completed.
    static let pipelinesCompletedKey = "pipelines_completed"

    enum NotificationCategory {
        static let decision = "DECISION"
        static let dailySummary = "DAILY_SUMMARY"
    }

    /// Search for a binary in well-known directories first, then resolved PATH.
    /// Well-known dirs are checked first to avoid TCC prompts from iterating
    /// the full PATH (which may include ~/Documents, ~/Music, etc.).
    nonisolated static func findInPath(_ binary: String) -> String? {
        let home = NSHomeDirectory()

        // 1. Well-known directories — fast, no TCC risk
        let knownDirs = [
            "\(home)/.local/bin",
            "\(home)/.claude/bin",
            "/usr/local/bin",
            "/opt/homebrew/bin",
            "\(home)/.volta/bin"
        ]
        for dir in knownDirs {
            let fullPath = "\(dir)/\(binary)"
            if FileManager.default.isExecutableFile(atPath: fullPath) {
                return fullPath
            }
        }

        // 2. Scan nvm/fnm versioned directories
        let versionedDirs = [
            "\(home)/.nvm/versions/node",
            "\(home)/.local/share/fnm/node-versions",
            "\(home)/.fnm/node-versions"
        ]
        for dir in versionedDirs {
            if let found = searchNodeVersions(dir: dir, binary: binary) {
                return found
            }
        }

        // 3. Resolved PATH — skip TCC-protected directories
        let tccProtected: Set<String> = [
            "\(home)/Documents", "\(home)/Downloads", "\(home)/Desktop",
            "\(home)/Music", "\(home)/Movies", "\(home)/Pictures"
        ]
        let env = resolvedEnvironment()
        guard let pathValue = env["PATH"] else { return nil }
        for dir in pathValue.split(separator: ":") {
            let dirStr = String(dir)
            if tccProtected.contains(where: { dirStr.hasPrefix($0) }) { continue }
            let fullPath = "\(dirStr)/\(binary)"
            if FileManager.default.isExecutableFile(atPath: fullPath) {
                return fullPath
            }
        }

        return nil
    }

    /// Search versioned node manager directories for a binary.
    nonisolated private static func searchNodeVersions(dir: String, binary: String) -> String? {
        guard let versions = try? FileManager.default.contentsOfDirectory(atPath: dir) else { return nil }
        for ver in versions.sorted().reversed() {
            for sub in ["bin", "installation/bin"] {
                let path = "\(dir)/\(ver)/\(sub)/\(binary)"
                if FileManager.default.isExecutableFile(atPath: path) {
                    return path
                }
            }
        }
        return nil
    }

    /// Returns a process environment with the user's full PATH resolved from their login shell.
    /// Cached after first call. Useful for launching subprocesses from a macOS app (where PATH is minimal).
    nonisolated static func resolvedEnvironment() -> [String: String] {
        struct Cache {
            static let env: [String: String] = {
                var env = ProcessInfo.processInfo.environment
                let shell = env["SHELL"] ?? "/bin/zsh"
                let pathProc = Process()
                pathProc.executableURL = URL(fileURLWithPath: shell)
                let configDir = NSString("~/.config/watchtower").expandingTildeInPath
                try? FileManager.default.createDirectory(atPath: configDir, withIntermediateDirectories: true)
                pathProc.currentDirectoryURL = URL(fileURLWithPath: configDir)
                pathProc.arguments = ["-lc", "echo $PATH"]
                let pathPipe = Pipe()
                pathProc.standardOutput = pathPipe
                pathProc.standardError = FileHandle.nullDevice
                try? pathProc.run()
                // Timeout: kill after 5s to avoid hanging on broken shell configs
                let timer = DispatchSource.makeTimerSource()
                timer.schedule(deadline: .now() + 5)
                timer.setEventHandler { pathProc.terminate() }
                timer.resume()
                pathProc.waitUntilExit()
                timer.cancel()
                if let fullPath = String(data: pathPipe.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8)?
                    .trimmingCharacters(in: .whitespacesAndNewlines), !fullPath.isEmpty {
                    env["PATH"] = fullPath
                }
                env.removeValue(forKey: "CLAUDECODE")
                // Ensure claude's nvm/fnm dir is in PATH (login-only shell may miss .zshrc).
                // Inline search: can't call Constants methods from nested Cache struct.
                let nvmDirs = [
                    NSHomeDirectory() + "/.nvm/versions/node",
                    NSHomeDirectory() + "/.local/share/fnm/node-versions",
                    NSHomeDirectory() + "/.fnm/node-versions"
                ]
                outer: for nvmDir in nvmDirs {
                    guard let versions = try? FileManager.default.contentsOfDirectory(atPath: nvmDir) else { continue }
                    for ver in versions.sorted().reversed() {
                        for sub in ["bin", "installation/bin"] {
                            let candidate = "\(nvmDir)/\(ver)/\(sub)/claude"
                            if FileManager.default.isExecutableFile(atPath: candidate) {
                                let claudeDir = (candidate as NSString).deletingLastPathComponent
                                if let path = env["PATH"], !path.contains(claudeDir) {
                                    env["PATH"] = claudeDir + ":" + path
                                }
                                break outer
                            }
                        }
                    }
                }
                return env
            }()
        }
        return Cache.env
    }

    /// Resolve the watchtower CLI binary path.
    /// Priority: app bundle → search resolved PATH.
    nonisolated static func findCLIPath() -> String? {
        // 1. Inside the app bundle
        if let bundlePath = Bundle.main.executableURL?
            .deletingLastPathComponent()
            .appendingPathComponent("watchtower").path,
           FileManager.default.isExecutableFile(atPath: bundlePath) {
            return bundlePath
        }

        // 2. Search user's PATH
        return findInPath("watchtower")
    }
}
