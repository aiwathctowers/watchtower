import SwiftUI

struct DecisionCard: View {
    let decision: Decision
    var slackURL: URL? = nil
    var feedbackEntityID: String? = nil  // "digestID:decisionIdx"
    var dbManager: DatabaseManager? = nil
    var correctedImportance: String? = nil
    var onImportanceChange: ((String) -> Void)? = nil

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
}
