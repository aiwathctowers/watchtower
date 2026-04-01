import Foundation

// MARK: - Codex Error

enum CodexError: LocalizedError {
    case notFound
    case exitCode(Int, String)

    var errorDescription: String? {
        switch self {
        case .notFound:
            "Codex CLI not found. Install: npm install -g @openai/codex\nOr set codex_path in ~/.config/watchtower/config.yaml"
        case let .exitCode(code, stderr):
            "Codex exited with code \(code): \(stderr)"
        }
    }
}

// MARK: - ProcessHandle (thread-safe termination)

private final class CodexProcessHandle: @unchecked Sendable {
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

// MARK: - CodexService

final class CodexService: AIServiceProtocol, Sendable {
    private let model: String

    init(model: String = "gpt-5.4") {
        self.model = model
    }

    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String?,
        extraAllowedTools: [String]
    ) -> AsyncThrowingStream<StreamEvent, Error> {
        let processHandle = CodexProcessHandle()
        let effectiveModel = model ?? self.model
        return AsyncThrowingStream { continuation in
            continuation.onTermination = { @Sendable _ in
                processHandle.terminate()
            }
            Task {
                do {
                    try await self.runCodex(
                        prompt: prompt,
                        systemPrompt: systemPrompt,
                        sessionID: sessionID,
                        dbPath: dbPath,
                        model: effectiveModel,
                        processHandle: processHandle,
                        continuation: continuation
                    )
                } catch {
                    continuation.finish(throwing: error)
                }
            }
        }
    }

    // MARK: - Run Codex subprocess

    private func runCodex(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String,
        processHandle: CodexProcessHandle,
        continuation: AsyncThrowingStream<StreamEvent, Error>.Continuation
    ) async throws {
        let codexPath = try findCodex()

        var args: [String]

        if let sessionID, !sessionID.isEmpty {
            // Session resume
            args = ["exec", "resume", "--last",
                    "--model", model, "--json", "--ephemeral",
                    "-c", "approval_policy=never",
                    "-c", "sandbox_mode=read-only"]
        } else {
            args = ["exec",
                    "--model", model, "--json", "--ephemeral",
                    "-c", "approval_policy=never",
                    "-c", "sandbox_mode=read-only"]
        }

        if let systemPrompt, !systemPrompt.isEmpty {
            args += ["-c", "developer_instructions=\(systemPrompt)"]
        }

        // MCP config for SQLite via temp .codex directory
        var tempConfigDir: URL?
        if let dbPath {
            let mcpDir = try setupMCPConfig(dbPath: dbPath)
            tempConfigDir = mcpDir
            args += ["--cd", mcpDir.path]
        } else {
            args += ["--cd", Constants.processWorkingDirectory().path]
        }

        args.append(prompt)

        let process = Process()
        process.executableURL = URL(fileURLWithPath: codexPath)
        process.arguments = args
        process.environment = Constants.resolvedEnvironment()

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

        let handle = stdout.fileHandleForReading
        for try await line in handle.bytes.lines {
            if Task.isCancelled { break }
            if let event = parseCodexEvent(line) {
                continuation.yield(event)
            }
        }

        let exitStatus = await Task.detached { () -> Int32 in
            process.waitUntilExit()
            return process.terminationStatus
        }.value

        // Clean up temp config
        if let tempDir = tempConfigDir {
            try? FileManager.default.removeItem(at: tempDir)
        }

        if !Task.isCancelled && exitStatus != 0 {
            let stderrText = await stderrTask.value
            let detail = stderrText.trimmingCharacters(in: .whitespacesAndNewlines)
            throw CodexError.exitCode(
                Int(exitStatus),
                detail.isEmpty ? "Codex CLI failed" : detail
            )
        }

        continuation.yield(.done)
        continuation.finish()
    }

    // MARK: - JSONL Event Parsing

    private func parseCodexEvent(_ line: String) -> StreamEvent? {
        guard let data = line.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data)
                as? [String: Any],
              let type = json["type"] as? String else {
            return nil
        }

        switch type {
        case "item.completed":
            if let item = json["item"] as? [String: Any],
               item["type"] as? String == "agent_message",
               let content = item["content"] as? String,
               !content.isEmpty {
                return .turnComplete(content)
            }
        case "item.started":
            if let item = json["item"] as? [String: Any],
               item["type"] as? String == "agent_message",
               let content = item["content"] as? String,
               !content.isEmpty {
                return .text(content)
            }
        case "error":
            break
        default:
            break
        }

        return nil
    }

    // MARK: - MCP Configuration

    private func setupMCPConfig(dbPath: String) throws -> URL {
        let tempDir = FileManager.default.temporaryDirectory
            .appendingPathComponent(
                "codex-watchtower-\(UUID().uuidString)"
            )
        let codexConfigDir = tempDir
            .appendingPathComponent(".codex")
        try FileManager.default.createDirectory(
            at: codexConfigDir,
            withIntermediateDirectories: true
        )

        let toml = """
        [mcp_servers.sqlite]
        command = "npx"
        args = ["-y", "@anthropic-ai/mcp-server-sqlite", "\(dbPath)"]
        """
        try toml.write(
            to: codexConfigDir.appendingPathComponent("config.toml"),
            atomically: true,
            encoding: .utf8
        )
        return tempDir
    }

    // MARK: - Binary Discovery

    private func findCodex() throws -> String {
        if let path = Constants.findCodexPath() {
            return path
        }
        throw CodexError.notFound
    }
}
