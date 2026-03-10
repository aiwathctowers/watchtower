import SwiftUI

struct DecisionCard: View {
    let decision: Decision
    var slackURL: URL? = nil

    private var accentColor: Color {
        switch decision.resolvedImportance {
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
                    ImportanceBadge(importance: decision.resolvedImportance)
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
                }
            }
            .padding(12)
        }
        .background(accentColor.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
    }
}
