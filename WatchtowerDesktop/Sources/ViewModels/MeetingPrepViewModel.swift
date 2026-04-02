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

struct MeetingPrepResult: Codable, Equatable {
    let eventID: String
    let title: String
    let startTime: String
    let talkingPoints: [TalkingPoint]
    let openItems: [OpenItem]
    let peopleNotes: [PersonNote]
    let suggestedPrep: [String]

    enum CodingKeys: String, CodingKey {
        case title
        case eventID = "event_id"
        case startTime = "start_time"
        case talkingPoints = "talking_points"
        case openItems = "open_items"
        case peopleNotes = "people_notes"
        case suggestedPrep = "suggested_prep"
    }
}

// MARK: - ViewModel

@MainActor
@Observable
final class MeetingPrepViewModel {
    var result: MeetingPrepResult?
    var isLoading: Bool = false
    var error: String?

    func generate(eventID: String) {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }

        isLoading = true
        error = nil
        result = nil

        Task.detached {
            let cliResult = await Self.runCLI(
                path: cliPath,
                arguments: ["meeting-prep", eventID, "--json"]
            )
            await MainActor.run {
                self.isLoading = false
                if cliResult.exitCode == 0, !cliResult.stdout.isEmpty {
                    self.parseCLIOutput(cliResult.stdout)
                } else {
                    self.error = cliResult.stderr.isEmpty
                        ? "Meeting prep failed (exit \(cliResult.exitCode))"
                        : String(cliResult.stderr.prefix(300))
                }
            }
        }
    }

    func generateNext() {
        guard let cliPath = Constants.findCLIPath() else {
            error = "Watchtower CLI not found"
            return
        }

        isLoading = true
        error = nil
        result = nil

        Task.detached {
            let cliResult = await Self.runCLI(
                path: cliPath,
                arguments: ["meeting-prep", "next", "--json"]
            )
            await MainActor.run {
                self.isLoading = false
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
