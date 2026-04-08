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

    @ViewBuilder
    private var jiraBadgesForDecision: some View {
        let keys = decision.text.extractJiraKeys()
        if !keys.isEmpty {
            ForEach(keys, id: \.self) { key in
                if let issue = jiraIssues[key] {
                    JiraBadgeView(
                        issue: issue,
                        siteURL: jiraSiteURL
                    )
                } else if let siteURL = jiraSiteURL,
                          let url = URL(
                              string: "\(siteURL)/browse/\(key)"
                          ) {
                    Link(destination: url) {
                        Text(key)
                            .font(.caption2)
                            .fontWeight(.medium)
                            .foregroundStyle(.blue)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(
                                Color.blue.opacity(0.10),
                                in: Capsule()
                            )
                    }
                    .buttonStyle(.plain)
                    .help("Open \(key) in Jira")
                }
            }
        }
    }
}
