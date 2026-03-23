import Foundation

enum Constants {
    static let configPath = NSString("~/.config/watchtower/config.yaml").expandingTildeInPath
    static let databasePath = NSString("~/.local/share/watchtower").expandingTildeInPath
    static let bundleID = "com.watchtower.desktop"

    /// App version — reads from Info.plist (set at build time), falls back to hardcoded default.
    static let appVersion: String = {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.2.0"
    }()

    enum NotificationCategory {
        static let decision = "DECISION"
        static let dailySummary = "DAILY_SUMMARY"
    }

    /// Read `claude_path` override from config.yaml (lightweight, no Yams dependency).
    nonisolated static func claudePathFromConfig() -> String? {
        guard let data = FileManager.default.contents(atPath: configPath),
              let str = String(data: data, encoding: .utf8) else { return nil }
        // Simple line-based parse: "claude_path: /some/path"
        for line in str.components(separatedBy: .newlines) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("claude_path:") {
                let value = trimmed.dropFirst("claude_path:".count)
                    .trimmingCharacters(in: .whitespaces)
                    .trimmingCharacters(in: CharacterSet(charactersIn: "\"'"))
                if !value.isEmpty && FileManager.default.isExecutableFile(atPath: value) {
                    return value
                }
            }
        }
        return nil
    }

    /// Check if Claude Code CLI is available.
    /// Priority: config override → search resolved PATH.
    nonisolated static func findClaudePath() -> String? {
        if let override = claudePathFromConfig() {
            return override
        }
        return findInPath("claude")
    }

    /// Search for a binary in the resolved user PATH, with well-known fallback directories.
    private nonisolated static func findInPath(_ binary: String) -> String? {
        let env = resolvedEnvironment()
        guard let pathValue = env["PATH"] else { return nil }
        for dir in pathValue.split(separator: ":") {
            let fullPath = "\(dir)/\(binary)"
            if FileManager.default.isExecutableFile(atPath: fullPath) {
                return fullPath
            }
        }
        // Fallback: well-known directories not always in PATH (nvm, fnm, volta, Homebrew)
        let home = NSHomeDirectory()
        let fallbackDirs = [
            "/usr/local/bin",
            "/opt/homebrew/bin",
            "\(home)/.volta/bin",
        ]
        for dir in fallbackDirs {
            let fullPath = "\(dir)/\(binary)"
            if FileManager.default.isExecutableFile(atPath: fullPath) {
                return fullPath
            }
        }
        // Fallback: scan nvm/fnm versioned directories
        let versionedDirs = [
            "\(home)/.nvm/versions/node",
            "\(home)/.local/share/fnm/node-versions",
            "\(home)/.fnm/node-versions",
        ]
        for dir in versionedDirs {
            if let found = searchNodeVersions(dir: dir, binary: binary) {
                return found
            }
        }
        return nil
    }

    /// Search versioned node manager directories for a binary.
    private nonisolated static func searchNodeVersions(dir: String, binary: String) -> String? {
        guard let versions = try? FileManager.default.contentsOfDirectory(atPath: dir) else { return nil }
        for v in versions.sorted().reversed() {
            for sub in ["bin", "installation/bin"] {
                let path = "\(dir)/\(v)/\(sub)/\(binary)"
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
                pathProc.currentDirectoryURL = URL(fileURLWithPath: NSHomeDirectory())
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
                    NSHomeDirectory() + "/.fnm/node-versions",
                ]
                outer: for nvmDir in nvmDirs {
                    guard let versions = try? FileManager.default.contentsOfDirectory(atPath: nvmDir) else { continue }
                    for v in versions.sorted().reversed() {
                        for sub in ["bin", "installation/bin"] {
                            let candidate = "\(nvmDir)/\(v)/\(sub)/claude"
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
