import SwiftUI

struct BlockerCardView: View {
    let entry: BlockerMapViewModel.BlockerEntry
    @State private var siteURL: String?

    var body: some View {
        HStack(spacing: 0) {
            // Left urgency border
            RoundedRectangle(cornerRadius: 2)
                .fill(entry.urgency.color)
                .frame(width: 4)
                .padding(.vertical, 4)

            VStack(alignment: .leading, spacing: 8) {
                // Row 1: Issue key + status + days
                HStack(alignment: .center, spacing: 8) {
                    Group {
                        if let url = JiraHelpers.browseURL(siteURL: siteURL, issueKey: entry.issueKey) {
                            Link(destination: url) {
                                Text(entry.issueKey)
                                    .font(.subheadline)
                                    .fontWeight(.bold)
                                    .foregroundStyle(.blue)
                            }
                            .help("Open in Jira")
                        } else {
                            Text(entry.issueKey)
                                .font(.subheadline)
                                .fontWeight(.bold)
                                .foregroundStyle(.blue)
                        }
                    }

                    statusBadge

                    Spacer()

                    Text(daysLabel)
                        .font(.caption)
                        .foregroundStyle(entry.urgency.color)
                        .fontWeight(.medium)
                }

                // Row 2: Summary
                Text(entry.summary)
                    .font(.subheadline)
                    .lineLimit(2)
                    .foregroundStyle(.primary)

                // Row 3: Assignee
                if !entry.assigneeName.isEmpty {
                    Label(entry.assigneeName, systemImage: "person.fill")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                // Row 4: Blocking chain
                if !entry.blockingChain.isEmpty {
                    chainView
                }

                // Row 5: Downstream count
                if entry.downstreamCount > 0 {
                    Label(
                        "Blocks \(entry.downstreamCount) issue\(entry.downstreamCount == 1 ? "" : "s")",
                        systemImage: "arrow.triangle.branch"
                    )
                    .font(.caption)
                    .foregroundStyle(.orange)
                }

                // Row 6: Who to ping
                if !entry.whoToPing.isEmpty {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Who to ping:")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .fontWeight(.semibold)

                        ForEach(entry.whoToPing) { target in
                            HStack(spacing: 4) {
                                Image(systemName: "person.circle.fill")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                Text(target.displayName)
                                    .font(.caption)
                                    .fontWeight(.medium)
                                Text("(\(target.reason))")
                                    .font(.caption2)
                                    .foregroundStyle(.tertiary)
                            }
                        }
                    }
                }

                // Row 7: Slack context
                if !entry.slackContext.isEmpty {
                    Text(entry.slackContext)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .padding(6)
                        .background(
                            Color.secondary.opacity(0.06),
                            in: RoundedRectangle(cornerRadius: 4)
                        )
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
        }
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(nsColor: .controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
        )
        .onAppear {
            siteURL = JiraConfigHelper.readSiteURL()
        }
    }

    // MARK: - Components

    private var statusBadge: some View {
        Text(entry.status)
            .font(.caption2)
            .fontWeight(.medium)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(
                statusColor.opacity(0.12),
                in: Capsule()
            )
            .foregroundStyle(statusColor)
    }

    private var statusColor: Color {
        let lower = entry.status.lowercased()
        if lower.contains("block") { return .red }
        if lower.contains("progress") { return .blue }
        return .secondary
    }

    private var daysLabel: String {
        let kind = entry.blockerType == "blocked" ? "blocked" : "stale"
        return "\(entry.blockedDays)d \(kind)"
    }

    private var chainView: some View {
        HStack(spacing: 4) {
            Image(systemName: "link")
                .font(.caption2)
                .foregroundStyle(.tertiary)

            ForEach(
                Array(entry.blockingChain.enumerated()),
                id: \.offset
            ) { index, key in
                if index > 0 {
                    Image(systemName: "arrow.left")
                        .font(.system(size: 8))
                        .foregroundStyle(.tertiary)
                }
                Group {
                    if let url = JiraHelpers.browseURL(siteURL: siteURL, issueKey: key) {
                        Link(destination: url) {
                            Text(key)
                                .font(.caption)
                                .fontWeight(.medium)
                                .foregroundStyle(.blue)
                        }
                        .help("Open in Jira")
                    } else {
                        Text(key)
                            .font(.caption)
                            .fontWeight(.medium)
                            .foregroundStyle(.blue)
                    }
                }
            }

            if entry.blockingChain.count > 1 {
                Text("(root cause)")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
    }
}
