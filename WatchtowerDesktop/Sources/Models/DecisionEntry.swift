import Foundation

/// A decision paired with metadata from its parent digest, for the flat decisions list.
struct DecisionEntry: Identifiable, Equatable {
    let decision: Decision
    let digestID: Int
    let decisionIdx: Int   // index in the decisions JSON array
    let channelID: String
    let channelName: String?
    let digestSummary: String
    let digestType: String
    let date: Date       // from digest's periodTo
    let messageTS: String?
    let isRead: Bool

    var id: String { "\(digestID)-\(decision.id)" }
}
