import Foundation

struct MeetingExtractedTopic: Equatable {
    var text: String
    var priority: String
}

struct MeetingTopicsExtractResult: Equatable {
    var topics: [MeetingExtractedTopic]
    var notes: String
}

/// Bridges the Desktop app to `watchtower meeting-prep extract-topics --json`.
struct MeetingTopicsExtractService {
    let runner: CLIRunnerProtocol

    func extract(text: String, eventID: String = "") async throws -> MeetingTopicsExtractResult {
        var args = ["meeting-prep", "extract-topics", "--json", "--text", text]
        if !eventID.isEmpty {
            args.append(contentsOf: ["--event-id", eventID])
        }
        let data = try await runner.run(args: args)
        let decoded = try JSONDecoder().decode(CLIExtractTopicsResponse.self, from: data)

        let topics = decoded.topics.map {
            MeetingExtractedTopic(text: $0.text, priority: $0.priority)
        }
        return MeetingTopicsExtractResult(topics: topics, notes: decoded.notes)
    }
}

private struct CLIExtractTopicsResponse: Decodable {
    let topics: [CLIExtractTopicItem]
    let notes: String
}

private struct CLIExtractTopicItem: Decodable {
    let text: String
    let priority: String

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        text = try c.decode(String.self, forKey: .text)
        priority = (try c.decodeIfPresent(String.self, forKey: .priority)) ?? ""
    }

    enum CodingKeys: String, CodingKey {
        case text
        case priority
    }
}
