import Foundation

/// AI service that delegates to the bundled `watchtower ai query` CLI.
/// Replaces direct Claude/Codex subprocess invocations — the desktop app
/// no longer needs to know about AI provider binaries or their PATH.
final class WatchtowerAIService: AIServiceProtocol, Sendable {

    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String?,
        extraAllowedTools: [String]
    ) -> AsyncThrowingStream<StreamEvent, Error> {
        let processHandle = WatchtowerProcessHandle()
        return AsyncThrowingStream { continuation in
            continuation.onTermination = { @Sendable _ in
                processHandle.terminate()
            }
            Task {
                do {
                    try await self.run(
                        prompt: prompt,
                        systemPrompt: systemPrompt,
                        sessionID: sessionID,
                        dbPath: dbPath,
                        model: model,
                        extraAllowedTools: extraAllowedTools,
                        processHandle: processHandle,
                        continuation: continuation
                    )
                } catch {
                    continuation.finish(throwing: error)
                }
            }
        }
    }

    /// Run a quick connectivity check via `watchtower ai test`.
    /// Returns (ok, provider, model, error).
    static func testConnection() async throws -> (ok: Bool, provider: String, model: String) {
        let cliPath = try findCLI()

        let process = Process()
        process.executableURL = URL(fileURLWithPath: cliPath)
        process.currentDirectoryURL = Constants.processWorkingDirectory()
        process.arguments = ["ai", "test"]

        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr

        try process.run()

        let exitStatus = await Task.detached { () -> Int32 in
            process.waitUntilExit()
            return process.terminationStatus
        }.value

        let data = stdout.fileHandleForReading.readDataToEndOfFile()
        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw WatchtowerAIError.badResponse("Invalid JSON from watchtower ai test")
        }

        let ok = json["ok"] as? Bool ?? false
        let provider = json["provider"] as? String ?? "unknown"
        let model = json["model"] as? String ?? "unknown"

        if !ok {
            let errMsg = json["error"] as? String ?? "Unknown error"
            throw WatchtowerAIError.testFailed(errMsg, provider: provider, model: model)
        }

        if exitStatus != 0 {
            throw WatchtowerAIError.exitCode(Int(exitStatus), "watchtower ai test failed")
        }

        return (ok: true, provider: provider, model: model)
    }

    // MARK: - Private

    private func run(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String?,
        extraAllowedTools: [String],
        processHandle: WatchtowerProcessHandle,
        continuation: AsyncThrowingStream<StreamEvent, Error>.Continuation
    ) async throws {
        let cliPath = try Self.findCLI()

        var args = ["ai", "query", prompt]

        if let systemPrompt, !systemPrompt.isEmpty {
            args += ["--system-prompt", systemPrompt]
        }
        if let sessionID, !sessionID.isEmpty {
            args += ["--session-id", sessionID]
        }
        if let dbPath, !dbPath.isEmpty {
            args += ["--db-path", dbPath]
        }
        if let model, !model.isEmpty {
            args += ["--model", model]
        }
        if !extraAllowedTools.isEmpty {
            args += ["--allowed-tools", extraAllowedTools.joined(separator: ",")]
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: cliPath)
        process.currentDirectoryURL = Constants.processWorkingDirectory()
        process.arguments = args

        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr

        processHandle.set(process)
        try process.run()

        let stderrTask = Task.detached { () -> String in
            let data = stderr.fileHandleForReading.readDataToEndOfFile()
            return String(data: data.prefix(65536), encoding: .utf8) ?? ""
        }

        var accumulatedText = ""
        let handle = stdout.fileHandleForReading

        for try await line in handle.bytes.lines {
            if Task.isCancelled { break }
            if let event = parseLine(line, accumulatedText: &accumulatedText) {
                continuation.yield(event)
            }
        }

        let exitStatus = await Task.detached { () -> Int32 in
            process.waitUntilExit()
            return process.terminationStatus
        }.value

        if !Task.isCancelled && exitStatus != 0 {
            let stderrText = await stderrTask.value
            let detail = stderrText.trimmingCharacters(in: .whitespacesAndNewlines)
            throw WatchtowerAIError.exitCode(
                Int(exitStatus),
                detail.isEmpty ? "watchtower ai query failed" : detail
            )
        }

        // Emit turnComplete with full accumulated text (matches old behavior)
        if !accumulatedText.isEmpty {
            continuation.yield(.turnComplete(accumulatedText))
        }

        continuation.yield(.done)
        continuation.finish()
    }

    /// Parse a JSON line from `watchtower ai query` output.
    private func parseLine(_ line: String, accumulatedText: inout String) -> StreamEvent? {
        guard let data = line.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = json["type"] as? String else {
            return nil
        }

        switch type {
        case "text":
            if let text = json["text"] as? String {
                accumulatedText += text
                return .text(text)
            }
        case "session_id":
            if let sid = json["session_id"] as? String {
                return .sessionID(sid)
            }
        case "error":
            if let errMsg = json["error"] as? String {
                // Emit as text so the UI can display it
                return .text("[Error] \(errMsg)")
            }
        case "done":
            // Handled after the stream loop (turnComplete + done)
            break
        default:
            break
        }
        return nil
    }

    /// Find the watchtower CLI binary — bundled in .app or in PATH.
    private static func findCLI() throws -> String {
        if let path = Constants.findCLIPath() {
            return path
        }
        throw WatchtowerAIError.cliNotFound
    }
}

// MARK: - Process Handle

private final class WatchtowerProcessHandle: @unchecked Sendable {
    private var process: Process?
    private let lock = NSLock()

    func set(_ process: Process) {
        lock.lock()
        self.process = process
        lock.unlock()
    }

    func terminate() {
        lock.lock()
        if let proc = process, proc.isRunning { proc.terminate() }
        lock.unlock()
    }
}

// MARK: - Errors

enum WatchtowerAIError: LocalizedError {
    case cliNotFound
    case exitCode(Int, String)
    case badResponse(String)
    case testFailed(String, provider: String, model: String)

    var errorDescription: String? {
        switch self {
        case .cliNotFound:
            "Watchtower CLI not found in app bundle"
        case let .exitCode(code, detail):
            "AI query failed (exit \(code)): \(detail)"
        case let .badResponse(detail):
            "Bad AI response: \(detail)"
        case let .testFailed(error, provider, model):
            "AI test failed (\(provider)/\(model)): \(error)"
        }
    }
}
