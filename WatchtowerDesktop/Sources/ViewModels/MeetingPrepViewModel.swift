import Foundation

// MARK: - Meeting Prep Result (matches Go MeetingPrepResult)

struct TalkingPoint: Codable, Identifiable, Equatable {
    var id: String { text }
    let text: String
    let sourceType: String
    let sourceID: String
    let priority: String

    enum CodingKeys: String, CodingKey {
        case text, priority
        case sourceType = "source_type"
        case sourceID = "source_id"
    }
}

struct OpenItem: Codable, Identifiable, Equatable {
    var id: String { "\(type)-\(itemID)" }
    let text: String
    let type: String
    let itemID: String
    let personName: String
    let personID: String

    enum CodingKeys: String, CodingKey {
        case text, type
        case itemID = "id"
        case personName = "person_name"
        case personID = "person_id"
    }
}

struct PersonNote: Codable, Identifiable, Equatable {
    var id: String { userID }
    let userID: String
    let name: String
    let communicationTip: String
    let recentContext: String

    enum CodingKeys: String, CodingKey {
        case name
        case userID = "user_id"
        case communicationTip = "communication_tip"
        case recentContext = "recent_context"
    }
}

struct MeetingRecommendation: Codable, Identifiable, Equatable {
    var id: String { text }
    let text: String
    let category: String // agenda, format, participants, followup, preparation
    let priority: String // high, medium, low
}

struct MeetingPrepResult: Codable, Equatable {
    let eventID: String
    let title: String
    let startTime: String
    let talkingPoints: [TalkingPoint]
    let openItems: [OpenItem]
    let peopleNotes: [PersonNote]
    let suggestedPrep: [String]
    let recommendations: [MeetingRecommendation]?
    let contextGaps: [String]?

    enum CodingKeys: String, CodingKey {
        case title, recommendations
        case eventID = "event_id"
        case startTime = "start_time"
        case talkingPoints = "talking_points"
        case openItems = "open_items"
        case peopleNotes = "people_notes"
        case suggestedPrep = "suggested_prep"
        case contextGaps = "context_gaps"
    }
}

// MARK: - ViewModel

@MainActor
@Observable
final class MeetingPrepViewModel {
    var result: MeetingPrepResult?
    var isLoading: Bool = false
    var error: String?
    var statusMessage: String = ""
    var isCached: Bool = false

    /// Generate meeting prep for a specific event.
    /// - Parameters:
    ///   - eventID: The calendar event ID.
    ///   - userNotes: Optional agenda or context from the user.
    ///   - forceRefresh: If true, bypasses cache and regenerates.
    func generate(eventID: String, userNotes: String = "", forceRefresh: Bool = false) {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }

        isLoading = true
        error = nil
        isCached = false
        statusMessage = "Gathering attendee context..."

        var args = ["meeting-prep", eventID, "--json"]
        if forceRefresh {
            args.append("--force-refresh")
        }
        if !userNotes.isEmpty {
            args.append(contentsOf: ["--user-notes", userNotes])
        }

        Task.detached {
            await MainActor.run { self.statusMessage = "Analyzing attendee activity..." }

            let cliResult = await Self.runCLI(path: cliPath, arguments: args)

            await MainActor.run {
                self.isLoading = false
                self.statusMessage = ""
                if cliResult.exitCode == 0, !cliResult.stdout.isEmpty {
                    self.parseCLIOutput(cliResult.stdout)
                    if !forceRefresh {
                        self.isCached = false // fresh generation looks same as cached from CLI
                    }
                } else {
                    self.error = cliResult.stderr.isEmpty
                        ? "Meeting prep failed (exit \(cliResult.exitCode))"
                        : String(cliResult.stderr.prefix(300))
                }
            }
        }
    }

    /// Generate meeting prep for the next upcoming meeting.
    func generateNext(userNotes: String = "") {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }

        isLoading = true
        error = nil
        isCached = false
        statusMessage = "Finding next meeting..."

        var args = ["meeting-prep", "next", "--json"]
        if !userNotes.isEmpty {
            args.append(contentsOf: ["--user-notes", userNotes])
        }

        Task.detached {
            await MainActor.run { self.statusMessage = "Analyzing attendees..." }

            let cliResult = await Self.runCLI(path: cliPath, arguments: args)

            await MainActor.run {
                self.isLoading = false
                self.statusMessage = ""
                if cliResult.exitCode == 0, !cliResult.stdout.isEmpty {
                    self.parseCLIOutput(cliResult.stdout)
                } else {
                    self.error = cliResult.stderr.isEmpty
                        ? "No upcoming meetings found"
                        : String(cliResult.stderr.prefix(300))
                }
            }
        }
    }

    /// Regenerate meeting prep, bypassing cache.
    func regenerate(eventID: String, userNotes: String = "") {
        generate(eventID: eventID, userNotes: userNotes, forceRefresh: true)
    }

    private func parseCLIOutput(_ output: String) {
        guard let data = output.data(using: .utf8) else {
            error = "Invalid CLI output encoding"
            return
        }
        do {
            result = try JSONDecoder().decode(MeetingPrepResult.self, from: data)
        } catch {
            self.error = "Failed to parse meeting prep: \(error.localizedDescription)"
        }
    }

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

        // Read pipe data BEFORE waitUntilExit to prevent deadlock when output exceeds 64KB
        let stdoutData = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
        let stderrData = stderrPipe.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        let stdout = String(data: stdoutData, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let stderr = String(data: stderrData, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

        return (process.terminationStatus, stdout, stderr)
    }
}
