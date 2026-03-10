import Foundation

enum StreamEvent {
    case text(String)         // streaming delta (append)
    case turnComplete(String) // full turn text (replace — only last turn shown)
    case sessionID(String)
    case done
}

protocol ClaudeServiceProtocol: Sendable {
    func stream(prompt: String, systemPrompt: String?, sessionID: String?, dbPath: String?, model: String?, extraAllowedTools: [String]) -> AsyncThrowingStream<StreamEvent, Error>
}

extension ClaudeServiceProtocol {
    func stream(prompt: String, systemPrompt: String?, sessionID: String?, dbPath: String?) -> AsyncThrowingStream<StreamEvent, Error> {
        stream(prompt: prompt, systemPrompt: systemPrompt, sessionID: sessionID, dbPath: dbPath, model: nil, extraAllowedTools: [])
    }

    func stream(prompt: String, systemPrompt: String?, sessionID: String?, dbPath: String?, model: String?) -> AsyncThrowingStream<StreamEvent, Error> {
        stream(prompt: prompt, systemPrompt: systemPrompt, sessionID: sessionID, dbPath: dbPath, model: model, extraAllowedTools: [])
    }

    func stream(prompt: String, systemPrompt: String?, sessionID: String?, dbPath: String?, extraAllowedTools: [String]) -> AsyncThrowingStream<StreamEvent, Error> {
        stream(prompt: prompt, systemPrompt: systemPrompt, sessionID: sessionID, dbPath: dbPath, model: nil, extraAllowedTools: extraAllowedTools)
    }
}

/// Thread-safe handle for terminating a subprocess (H3 fix).
private final class ProcessHandle: @unchecked Sendable {
    private var process: Process?
    private let lock = NSLock()

    func set(_ process: Process) {
        lock.lock()
        self.process = process
        lock.unlock()
    }

    func terminate() {
        lock.lock()
        if let p = process, p.isRunning { p.terminate() }
        lock.unlock()
    }
}

final class ClaudeService: ClaudeServiceProtocol, Sendable {
    private let model: String

    init(model: String = "claude-sonnet-4-6") {
        self.model = model
    }

    func stream(prompt: String, systemPrompt: String?, sessionID: String?, dbPath: String?, model: String?, extraAllowedTools: [String]) -> AsyncThrowingStream<StreamEvent, Error> {
        let processHandle = ProcessHandle()
        let effectiveModel = model ?? self.model
        return AsyncThrowingStream { continuation in
            // H3: terminate process when stream is cancelled
            continuation.onTermination = { @Sendable _ in
                processHandle.terminate()
            }
            Task {
                do {
                    try await self.runClaude(
                        prompt: prompt,
                        systemPrompt: systemPrompt,
                        sessionID: sessionID,
                        dbPath: dbPath,
                        model: effectiveModel,
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

    private func runClaude(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String,
        extraAllowedTools: [String],
        processHandle: ProcessHandle,
        continuation: AsyncThrowingStream<StreamEvent, Error>.Continuation
    ) async throws {
        let claudePath = try findClaude()

        var args = ["-p", prompt, "--output-format", "stream-json", "--model", model]

        if let sessionID {
            args += ["--resume", sessionID]
        } else if let systemPrompt {
            args += ["--system-prompt", systemPrompt]
        }

        var allowedTools = "mcp__sqlite__*,Bash(sqlite3*)"
        for tool in extraAllowedTools {
            allowedTools += ",\(tool)"
        }
        args += ["--allowedTools", allowedTools]
        args += ["--disallowedTools", "Edit,Write,NotebookEdit"]

        if let dbPath {
            // C1 fix: build MCP JSON safely via JSONSerialization (prevents injection)
            let mcpConfig: [String: Any] = [
                "mcpServers": [
                    "sqlite": [
                        "command": "npx",
                        "args": ["-y", "@anthropic-ai/mcp-server-sqlite", dbPath]
                    ]
                ]
            ]
            let jsonData = try JSONSerialization.data(withJSONObject: mcpConfig)
            guard let jsonStr = String(data: jsonData, encoding: .utf8) else {
                throw ClaudeError.exitCode(-1, "Failed to encode MCP config")
            }
            args += ["--mcp-config", jsonStr]
        }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: claudePath)
        process.arguments = args

        // Set up clean environment for claude subprocess
        var env = ProcessInfo.processInfo.environment
        if let fullPath = Self.resolveUserPath() {
            env["PATH"] = fullPath
        }
        // Remove CLAUDECODE to avoid "nested session" error
        env.removeValue(forKey: "CLAUDECODE")
        process.environment = env

        let stdout = Pipe()
        let stderr = Pipe()
        process.standardOutput = stdout
        process.standardError = stderr

        processHandle.set(process)
        try process.run()

        // Read stderr in background (cap at 64KB)
        let stderrTask = Task.detached { () -> String in
            let data = stderr.fileHandleForReading.readDataToEndOfFile()
            return String(data: data.prefix(65536), encoding: .utf8) ?? ""
        }

        let handle = stdout.fileHandleForReading
        for try await line in handle.bytes.lines {
            if Task.isCancelled { break }
            if let event = parseStreamLine(line) {
                continuation.yield(event)
            }
        }

        process.waitUntilExit()

        if !Task.isCancelled && process.terminationStatus != 0 {
            let stderrText = await stderrTask.value
            let detail = stderrText.trimmingCharacters(in: .whitespacesAndNewlines)
            throw ClaudeError.exitCode(Int(process.terminationStatus), detail.isEmpty ? "Claude CLI failed" : detail)
        }

        continuation.yield(.done)
        continuation.finish()
    }

    private func parseStreamLine(_ line: String) -> StreamEvent? {
        guard let data = line.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = json["type"] as? String else {
            return nil
        }

        switch type {
        case "content_block_delta":
            // Streaming text deltas — real-time character-by-character output
            if let delta = json["delta"] as? [String: Any],
               delta["type"] as? String == "text_delta",
               let text = delta["text"] as? String {
                return .text(text)
            }
        case "assistant":
            // End of a conversation turn — extract final text, replaces any accumulated deltas
            if let message = json["message"] as? [String: Any],
               let content = message["content"] as? [[String: Any]] {
                let texts = content.compactMap { block -> String? in
                    guard block["type"] as? String == "text" else { return nil }
                    return block["text"] as? String
                }
                let combined = texts.joined(separator: "\n\n")
                if !combined.isEmpty {
                    return .turnComplete(combined)
                }
            }
        case "result":
            if let sid = json["session_id"] as? String {
                return .sessionID(sid)
            }
        default:
            break
        }

        return nil
    }

    /// Resolve the user's full PATH via login shell (cached)
    private static let _cachedPath: String? = {
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/bin/zsh")
        proc.arguments = ["-lc", "echo $PATH"]
        let pipe = Pipe()
        proc.standardOutput = pipe
        proc.standardError = FileHandle.nullDevice
        try? proc.run()
        proc.waitUntilExit()
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        return String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines)
    }()

    private static func resolveUserPath() -> String? { _cachedPath }

    private func findClaude() throws -> String {
        var paths = [
            "/usr/local/bin/claude",
            "/opt/homebrew/bin/claude",
            NSString("~/.claude/bin/claude").expandingTildeInPath,
        ]

        // Search nvm node versions
        let nvmDir = NSString("~/.nvm/versions/node").expandingTildeInPath
        if let versions = try? FileManager.default.contentsOfDirectory(atPath: nvmDir) {
            for v in versions.sorted().reversed() {
                paths.append("\(nvmDir)/\(v)/bin/claude")
            }
        }

        for path in paths {
            if FileManager.default.isExecutableFile(atPath: path) {
                return path
            }
        }

        // Try via login shell to pick up user's full PATH
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/zsh")
        process.arguments = ["-lc", "which claude"]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice
        try process.run()
        process.waitUntilExit()

        if process.terminationStatus == 0 {
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            if let path = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines),
               !path.isEmpty {
                return path
            }
        }

        throw ClaudeError.notFound
    }
}

enum ClaudeError: LocalizedError {
    case notFound
    case exitCode(Int, String)

    var errorDescription: String? {
        switch self {
        case .notFound:
            "Claude CLI not found. Install it from https://docs.anthropic.com/en/docs/claude-code"
        case .exitCode(let code, let stderr):
            "Claude exited with code \(code): \(stderr)"
        }
    }
}