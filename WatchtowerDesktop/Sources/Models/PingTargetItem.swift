import Foundation

/// A person to contact about a Jira issue — used by WhoToPingView.
struct PingTargetItem: Identifiable, Codable {
    var id: String { slackUserID }
    let slackUserID: String
    let displayName: String
    /// Raw reason key: "assignee", "assignee_blocker", "expert", "reporter",
    /// "slack_participant", "decision_maker".
    let reason: String

    enum CodingKeys: String, CodingKey {
        case slackUserID = "slack_user_id"
        case displayName = "display_name"
        case reason
    }

    /// Human-readable label for the reason.
    var reasonLabel: String {
        switch reason {
        case "assignee":           return "Assignee"
        case "assignee_blocker":   return "Blocker assignee"
        case "expert":             return "Expert"
        case "reporter":           return "Reporter"
        case "slack_participant":  return "Active in Slack"
        case "decision_maker":    return "Decision maker"
        default:                   return reason.capitalized
        }
    }
}
