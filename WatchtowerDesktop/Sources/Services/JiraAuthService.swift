import Foundation
import Yams

@MainActor
@Observable
final class JiraAuthService {
    var isConnected: Bool = false
    var isAuthenticating: Bool = false
    var error: String?
    var siteURL: String?
    var userDisplayName: String?

    private var authProcess: Process?

    init() {
        checkStatus()
    }

    // MARK: - Connect

    func connect() {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }

        isAuthenticating = true
        error = nil

        let process = Process()
        process.executableURL = URL(fileURLWithPath: cliPath)
        process.arguments = ["jira", "login"]
        process.environment = Constants.resolvedEnvironment()
        process.currentDirectoryURL = Constants.processWorkingDirectory()
        authProcess = process

        Task.detached {
            let result = await Self.runProcess(process)
            await MainActor.run {
                self.authProcess = nil
                self.isAuthenticating = false
                if result.exitCode == 0 {
                    self.isConnected = true
                    self.error = nil
                    self.readConfig()
                } else if result.exitCode == 15
                            || result.exitCode == 9 {
                    // SIGTERM/SIGKILL — user cancelled
                    self.error = nil
                } else {
                    self.error = result.stderr.isEmpty
                        ? "Login failed (exit \(result.exitCode))"
                        : String(result.stderr.prefix(200))
                }
            }
        }
    }

    func cancelConnect() {
        if let process = authProcess, process.isRunning {
            process.terminate()
        }
        authProcess = nil
        isAuthenticating = false
    }

    // MARK: - Disconnect

    func disconnect() {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }

        Task.detached {
            let result = await Self.runCLI(
                path: cliPath,
                arguments: ["jira", "logout"]
            )
            await MainActor.run {
                if result.exitCode == 0 {
                    self.isConnected = false
                    self.siteURL = nil
                    self.userDisplayName = nil
                    self.error = nil
                } else {
                    self.error = result.stderr.isEmpty
                        ? "Disconnect failed (exit \(result.exitCode))"
                        : String(result.stderr.prefix(200))
                }
            }
        }
    }

    // MARK: - Status

    func checkStatus() {
        let basePath = Constants.databasePath
        let fileManager = FileManager.default
        guard let contents = try? fileManager.contentsOfDirectory(
            atPath: basePath
        ) else {
            isConnected = false
            return
        }
        for dir in contents {
            let tokenPath = "\(basePath)/\(dir)/jira_token.json"
            if fileManager.fileExists(atPath: tokenPath) {
                isConnected = true
                readConfig()
                return
            }
        }
        isConnected = false
    }

    // MARK: - Config Reader

    private func readConfig() {
        let configPath = Constants.configPath
        guard let data = FileManager.default.contents(atPath: configPath),
              let str = String(data: data, encoding: .utf8),
              let yaml = try? Yams.load(yaml: str) as? [String: Any],
              let jira = yaml["jira"] as? [String: Any] else {
            return
        }
        siteURL = jira["site_url"] as? String
        userDisplayName = jira["user_display_name"] as? String
    }

    // MARK: - CLI Helpers

    nonisolated private static func runCLI(
        path: String,
        arguments: [String]
    ) async -> (exitCode: Int32, stdout: String, stderr: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = arguments
        process.environment = Constants.resolvedEnvironment()
        process.currentDirectoryURL = Constants.processWorkingDirectory()
        return await runProcess(process)
    }

    nonisolated private static func runProcess(
        _ process: Process
    ) async -> (exitCode: Int32, stdout: String, stderr: String) {
        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe

        do {
            try process.run()
        } catch {
            return (-1, "", error.localizedDescription)
        }

        let stdoutData = stdoutPipe.fileHandleForReading
            .readDataToEndOfFile()
        let stderrData = stderrPipe.fileHandleForReading
            .readDataToEndOfFile()
        process.waitUntilExit()

        let stdout = String(
            data: stdoutData, encoding: .utf8
        )?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let stderr = String(
            data: stderrData, encoding: .utf8
        )?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

        return (process.terminationStatus, stdout, stderr)
    }
}
