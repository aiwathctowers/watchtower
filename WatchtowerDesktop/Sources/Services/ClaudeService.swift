import Foundation

enum StreamEvent {
    case text(String)         // streaming delta (append)
    case turnComplete(String) // full turn text (replace — only last turn shown)
    case sessionID(String)
    case done
}

protocol AIServiceProtocol: Sendable {
    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String?,
        extraAllowedTools: [String]
    ) -> AsyncThrowingStream<StreamEvent, Error>
}

extension AIServiceProtocol {
    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?
    ) -> AsyncThrowingStream<StreamEvent, Error> {
        stream(
            prompt: prompt,
            systemPrompt: systemPrompt,
            sessionID: sessionID,
            dbPath: dbPath,
            model: nil,
            extraAllowedTools: []
        )
    }

    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        model: String?
    ) -> AsyncThrowingStream<StreamEvent, Error> {
        stream(
            prompt: prompt,
            systemPrompt: systemPrompt,
            sessionID: sessionID,
            dbPath: dbPath,
            model: model,
            extraAllowedTools: []
        )
    }

    func stream(
        prompt: String,
        systemPrompt: String?,
        sessionID: String?,
        dbPath: String?,
        extraAllowedTools: [String]
    ) -> AsyncThrowingStream<StreamEvent, Error> {
        stream(
            prompt: prompt,
            systemPrompt: systemPrompt,
            sessionID: sessionID,
            dbPath: dbPath,
            model: nil,
            extraAllowedTools: extraAllowedTools
        )
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
        if let proc = process, proc.isRunning { proc.terminate() }
        lock.unlock()
    }
}

final class ClaudeService: AIServiceProtocol, Sendable {
    private let model: String

    init(model: String = "claude-sonnet-4-6") {
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

        var args = ["-p", prompt, "--output-format", "stream-json", "--verbose", "--model", model]

        if let sessionID {
            args += ["--resume", sessionID]
        }

        if let systemPrompt {
            args += ["--system-prompt", systemPrompt]
        }

        var allowedTools = "mcp__sqlite__*,Bash(sqlite3*)"
        for tool in extraAllowedTools {
            allowedTools += ",\(tool)"
        }
        args += ["--allowedTools", allowedTools]
        args += ["--disallowedTools", "Edit,Write,NotebookEdit,WebFetch,WebSearch,mcp__claude_ai_Slack__*"]

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
        process.currentDirectoryURL = Constants.processWorkingDirectory()
        process.arguments = args

        // Use shared resolved environment (caches login shell PATH, removes CLAUDECODE)
        process.environment = Constants.resolvedEnvironment()

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

        // Wait on a detached thread to avoid blocking the cooperative thread pool
        let exitStatus = await Task.detached { () -> Int32 in
            process.waitUntilExit()
            return process.terminationStatus
        }.value

        if !Task.isCancelled && exitStatus != 0 {
            let stderrText = await stderrTask.value
            let detail = stderrText.trimmingCharacters(in: .whitespacesAndNewlines)
            throw ClaudeError.exitCode(Int(exitStatus), detail.isEmpty ? "Claude CLI failed" : detail)
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

    private func findClaude() throws -> String {
        // Delegate to shared search logic in Constants
        if let path = Constants.findClaudePath() {
            return path
        }
        throw ClaudeError.notFound
    }
}

// MARK: - Track Generation from Digest

struct GeneratedTrack: Decodable {
    let text: String
    let context: String
    let priority: String
    let dueDate: String?
    let requester: GeneratedRequester?
    let category: String
    let blocking: String?
    let tags: [String]?
    let decisionSummary: String?
    let decisionOptions: [GeneratedDecisionOption]?
    let participants: [GeneratedParticipant]?
    let sourceRefs: [GeneratedSourceRef]?
    let subItems: [GeneratedSubItem]?

    enum CodingKeys: String, CodingKey {
        case text, context, priority, requester, category, blocking, tags
        case dueDate = "due_date"
        case decisionSummary = "decision_summary"
        case decisionOptions = "decision_options"
        case participants
        case sourceRefs = "source_refs"
        case subItems = "sub_items"
    }
}

struct GeneratedRequester: Codable {
    let name: String
    let userID: String?
    enum CodingKeys: String, CodingKey {
        case name
        case userID = "user_id"
    }
}

struct GeneratedParticipant: Codable {
    let name: String
    let userID: String?
    let stance: String?
    enum CodingKeys: String, CodingKey {
        case name, stance
        case userID = "user_id"
    }
}

struct GeneratedSourceRef: Codable {
    let ts: String
    let author: String
    let text: String
}

struct GeneratedDecisionOption: Codable {
    let option: String
    let supporters: [String]?
    let pros: String?
    let cons: String?
}

struct GeneratedSubItem: Codable {
    let text: String
    let status: String
}

extension ClaudeService {
    /// Generate a structured track from a digest using Claude CLI.
    /// Returns the parsed track and the raw response for token tracking.
    func generateTrack(from digest: Digest, userNote: String?, channelName: String?) async throws -> GeneratedTrack {
        let digestInfo = buildDigestPrompt(digest, channelName: channelName, userNote: userNote)

        var fullText = ""
        for try await event in stream(prompt: digestInfo, systemPrompt: trackFromDigestSystemPrompt, sessionID: nil, dbPath: nil) {
            switch event {
            case .text(let delta):
                fullText += delta
            case .turnComplete(let text):
                fullText = text
            case .done:
                break
            case .sessionID:
                break
            }
        }

        // Extract JSON from response (strip markdown fences if present)
        let jsonString = extractJSON(from: fullText)
        guard let data = jsonString.data(using: .utf8) else {
            throw ClaudeError.exitCode(-1, "Empty response from Claude")
        }
        return try JSONDecoder().decode(GeneratedTrack.self, from: data)
    }

    /// H7 fix: sanitize AI-generated and user-controlled values before embedding in prompts.
    /// Prevents prompt delimiter injection via === and --- sequences.
    private func sanitizePromptValue(_ text: String) -> String {
        guard text.contains("===") || text.contains("---") || text.contains("\n") || text.contains("\r") else {
            return text
        }
        var result = text
        result = result.replacingOccurrences(of: "\r", with: "")
        result = result.replacingOccurrences(of: "\n", with: " ")
        result = result.replacingOccurrences(of: "===", with: "= = =")
        result = result.replacingOccurrences(of: "---", with: "- - -")
        return result
    }

    private func buildDigestPrompt(_ digest: Digest, channelName: String?, userNote: String?) -> String {
        var parts: [String] = []
        parts.append("Create a track from the following digest.")
        if let note = userNote, !note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            parts.append("\nUser's instructions: \(sanitizePromptValue(note))")
        }
        parts.append("\n--- DIGEST ---")
        if let name = channelName {
            parts.append("Channel: #\(sanitizePromptValue(name))")
        }
        parts.append("Type: \(digest.type)")
        let periodFrom = TimeFormatting.shortDateTime(fromUnix: digest.periodFrom)
        let periodTo = TimeFormatting.shortDateTime(fromUnix: digest.periodTo)
        parts.append("Period: \(periodFrom) — \(periodTo)")
        parts.append("Summary: \(sanitizePromptValue(digest.summary))")
        if !digest.topics.isEmpty && digest.topics != "[]" {
            parts.append("Topics: \(sanitizePromptValue(digest.topics))")
        }
        if !digest.decisions.isEmpty && digest.decisions != "[]" {
            parts.append("Decisions: \(sanitizePromptValue(digest.decisions))")
        }
        if !digest.tracksJSON.isEmpty && digest.tracksJSON != "[]" {
            parts.append("Tracks mentioned in digest: \(sanitizePromptValue(digest.tracksJSON))")
        }
        return parts.joined(separator: "\n")
    }

    private func extractJSON(from text: String) -> String {
        var s = text.trimmingCharacters(in: .whitespacesAndNewlines)
        // Strip ```json ... ``` fences
        if s.hasPrefix("```") {
            if let startRange = s.range(of: "\n") {
                s = String(s[startRange.upperBound...])
            }
            if s.hasSuffix("```") {
                s = String(s.dropLast(3))
            }
            s = s.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        return s
    }

    private var trackFromDigestSystemPrompt: String {
        """
        You are an assistant that creates structured tracks from Slack workspace digests.

        The user will provide a digest (summary, topics, decisions,
        existing tracks) and optionally their own instructions.

        Your task: analyze the digest and create ONE comprehensive,
        high-quality track that captures the most important actionable
        work from this digest.

        If the user provides instructions, follow them to focus the track on what they need.

        Return ONLY a JSON object (no markdown fences, no explanation):

        {
          "text": "clear, actionable title of what needs to be done",
          "context": "detailed context (3-5 sentences): what was discussed, what decisions were made, why this action is needed.",
          "priority": "high|medium|low",
          "due_date": "YYYY-MM-DD (only if clearly implied, otherwise omit)",
          "requester": {"name": "person or team who needs this", "user_id": ""},
          "category": "task|decision_needed|follow_up|code_review|info_request|approval|bug_fix|discussion",
          "blocking": "who or what is blocked if this isn't done (empty string if nothing)",
          "tags": ["topic-1", "topic-2"],
          "decision_summary": "how the group arrived at the current state (empty string if no decision context)",
          "decision_options": [
            {"option": "description", "supporters": ["@user1"], "pros": "advantages", "cons": "disadvantages"}
          ],
          "participants": [
            {"name": "@username", "user_id": "", "stance": "brief summary of position"}
          ],
          "source_refs": [],
          "sub_items": [
            {"text": "specific sub-task", "status": "open"}
          ]
        }

        Rules:
        - "text" should be a concise, actionable title (not a paragraph)
        - "context" should be comprehensive — include background, decisions made, and why this matters
        - "category" MUST be one of: code_review, decision_needed, info_request, task, approval, follow_up, bug_fix, discussion
        - "priority": high = urgent/blocking, medium = normal, low = nice-to-have
        - "sub_items": break down into 2-5 concrete sub-tasks when possible
        - "participants": list people mentioned in the digest with their roles/stances
        - "tags": 1-3 lowercase tags for the topic area
        - Keep the language consistent with the digest (if the digest is in Russian, write in Russian)
        - Return valid JSON only, no other text
        """
    }
}

enum ClaudeError: LocalizedError {
    case notFound
    case exitCode(Int, String)

    var errorDescription: String? {
        switch self {
        case .notFound:
            "Claude CLI not found. Install: npm install -g @anthropic-ai/claude-code\nOr set claude_path in ~/.config/watchtower/config.yaml"
        case let .exitCode(code, stderr):
            "Claude exited with code \(code): \(stderr)"
        }
    }
}
