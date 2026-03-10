import Foundation
@testable import WatchtowerDesktop

final class MockClaudeService: ClaudeServiceProtocol, Sendable {
    private let events: [StreamEvent]
    private let error: (any Error)?

    init(events: [StreamEvent] = [.text("Hello from Claude"), .done]) {
        self.events = events
        self.error = nil
    }

    init(error: any Error) {
        self.events = []
        self.error = error
    }

    func stream(prompt: String, systemPrompt: String?, sessionID: String?, dbPath: String?, model: String?, extraAllowedTools: [String]) -> AsyncThrowingStream<StreamEvent, Error> {
        let events = self.events
        let error = self.error
        return AsyncThrowingStream { continuation in
            Task {
                if let error {
                    continuation.finish(throwing: error)
                    return
                }
                for event in events {
                    continuation.yield(event)
                }
                continuation.finish()
            }
        }
    }
}
