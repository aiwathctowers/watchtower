import Foundation

@MainActor
@Observable
final class GoogleAuthService {
    var isConnected: Bool = false
    var isAuthenticating: Bool = false
    var error: String?

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

        Task.detached {
            let result = await Self.runCLI(
                path: cliPath,
                arguments: ["calendar", "login"]
            )
            await MainActor.run {
                self.isAuthenticating = false
                if result.exitCode == 0 {
                    self.isConnected = true
                    self.error = nil
                } else if result.exitCode == 15 || result.exitCode == 9 {
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
                arguments: ["calendar", "logout"]
            )
            await MainActor.run {
                if result.exitCode == 0 {
                    self.isConnected = false
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
        // Check if token file exists in any workspace dir
        let basePath = Constants.databasePath
        let fm = FileManager.default
        guard let contents = try? fm.contentsOfDirectory(atPath: basePath) else {
            isConnected = false
            return
        }
        for dir in contents {
            let tokenPath = "\(basePath)/\(dir)/google_token.json"
            if fm.fileExists(atPath: tokenPath) {
                isConnected = true
                return
            }
        }
        isConnected = false
    }

    // MARK: - CLI Helper

    nonisolated private static func runCLI(
        path: String,
        arguments: [String]
    ) async -> (exitCode: Int32, stdout: String, stderr: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = arguments
        process.environment = Constants.resolvedEnvironment()
        process.currentDirectoryURL = Constants.processWorkingDirectory()

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
        let stdout = String(data: stdoutData, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let stderr = String(data: stderrData, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

        return (process.terminationStatus, stdout, stderr)
    }
}
