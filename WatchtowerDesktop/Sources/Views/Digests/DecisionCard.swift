import SwiftUI

struct DecisionCard: View {
    let decision: Decision
    var slackURL: URL?
    var feedbackEntityID: String?   // "digestID:decisionIdx"
    var dbManager: DatabaseManager?
    var correctedImportance: String?
    var onImportanceChange: ((String) -> Void)?
    var jiraIssues: [String: JiraIssue] = [:]  // key -> issue
    var jiraSiteURL: String?

    private var effectiveImportance: String {
        correctedImportance ?? decision.resolvedImportance
    }

    private var accentColor: Color {
        switch effectiveImportance {
        case "high": .red
        case "low": .gray
        default: .orange
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            Rectangle()
                .fill(accentColor)
                .frame(width: 3)

            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(decision.text)
                        .textSelection(.enabled)
                    Spacer()
                    jiraBadgesForDecision
                    if let onChange = onImportanceChange {
                        EditableImportanceBadge(
                            importance: effectiveImportance,
                            isCorrected: correctedImportance != nil,
                            onChange: onChange
                        )
                    } else {
                        ImportanceBadge(importance: effectiveImportance)
                    }
                }

                HStack {
                    if let by = decision.by, !by.isEmpty {
                        Text("by \(by)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    if let url = slackURL {
                        Spacer()
                        Link(destination: url) {
                            Label("View in Slack", systemImage: "arrow.up.right.square")
                                .font(.caption)
                        }
                        .buttonStyle(.borderless)
                    }

                    if let entityID = feedbackEntityID, let dbManager {
                        FeedbackButtons(
                            entityType: "decision",
                            entityID: entityID,
                            dbManager: dbManager
                        )
                    }
                }
            }
            .padding(12)
        }
        .background(accentColor.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - Jira Badges

    private var jiraBadgesForDecision: some View {
        JiraKeyBadgesView(
            text: decision.text,
            issues: jiraIssues,
            siteURL: jiraSiteURL,
            isConnected: !jiraIssues.isEmpty || jiraSiteURL != nil
        )
    }
}
