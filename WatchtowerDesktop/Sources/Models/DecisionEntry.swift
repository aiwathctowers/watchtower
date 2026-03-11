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
    let correctedImportance: String?  // user override, nil = no correction

    var id: String { "\(digestID)-\(decision.id)" }

    /// The effective importance: user correction if present, otherwise AI-generated.
    var effectiveImportance: String {
        correctedImportance ?? decision.resolvedImportance
    }

    /// Returns a copy with the specified fields overridden.
    func with(isRead: Bool? = nil, correctedImportance: String?? = nil) -> DecisionEntry {
        DecisionEntry(
            decision: decision,
            digestID: digestID,
            decisionIdx: decisionIdx,
            channelID: channelID,
            channelName: channelName,
            digestSummary: digestSummary,
            digestType: digestType,
            date: date,
            messageTS: messageTS,
            isRead: isRead ?? self.isRead,
            correctedImportance: correctedImportance ?? self.correctedImportance
        )
    }
}
