import SwiftUI
import GRDB

struct EpicCardView: View {
    let epic: ProjectMapViewModel.EpicItem
    let dbPool: DatabasePool
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            compactView
                .contentShape(Rectangle())
                .onTapGesture { withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() } }

            if isExpanded {
                Divider()
                    .padding(.horizontal, 12)
                expandedView
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(nsColor: .controlBackgroundColor))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color.secondary.opacity(0.15), lineWidth: 1)
        )
    }

    // MARK: - Compact View

    private var compactView: some View {
        HStack(spacing: 10) {
            // Left: status color bar
            RoundedRectangle(cornerRadius: 2)
                .fill(badgeColor)
                .frame(width: 4, height: 50)

            VStack(alignment: .leading, spacing: 6) {
                // Row 1: Key badge + name + status badge
                HStack(spacing: 8) {
                    Text(epic.key)
                        .font(.caption)
                        .fontWeight(.bold)
                        .foregroundStyle(.blue)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.blue.opacity(0.1), in: RoundedRectangle(cornerRadius: 4))

                    Text(epic.name)
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .lineLimit(1)

                    Spacer()

                    statusChip
                }

                // Row 2: Progress bar + counts + warnings
                HStack(spacing: 10) {
                    // Owner
                    if let owner = epic.ownerName {
                        Label(owner, systemImage: "person.fill")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }

                    Spacer()

                    // Counts
                    Text("\(epic.doneIssues)/\(epic.totalIssues)")
                        .font(.caption)
                        .foregroundStyle(.secondary)

                    // Progress bar
                    progressBar
                        .frame(width: 100)

                    // Stale warning
                    if epic.staleCount > 0 {
                        HStack(spacing: 2) {
                            Image(systemName: "clock.badge.exclamationmark")
                                .font(.caption2)
                            Text("\(epic.staleCount)")
                                .font(.caption2)
                                .fontWeight(.medium)
                        }
                        .foregroundStyle(.orange)
                    }

                    // Blocked warning
                    if epic.blockedCount > 0 {
                        HStack(spacing: 2) {
                            Image(systemName: "xmark.octagon.fill")
                                .font(.caption2)
                            Text("\(epic.blockedCount)")
                                .font(.caption2)
                                .fontWeight(.medium)
                        }
                        .foregroundStyle(.red)
                    }

                    // Expand indicator
                    Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.vertical, 10)
            .padding(.trailing, 12)
        }
    }

    // MARK: - Progress Bar

    private var progressBar: some View {
        GeometryReader { geo in
            let width = geo.size.width
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 3)
                    .fill(Color.secondary.opacity(0.15))
                    .frame(height: 6)

                RoundedRectangle(cornerRadius: 3)
                    .fill(progressColor)
                    .frame(width: max(0, width * epic.progressPct), height: 6)
            }
        }
        .frame(height: 6)
    }

    private var progressColor: Color {
        switch epic.statusBadge {
        case .onTrack: .green
        case .atRisk: .yellow
        case .behind: .red
        }
    }

    // MARK: - Status Chip

    private var statusChip: some View {
        Text(epic.statusBadge.label)
            .font(.caption2)
            .fontWeight(.semibold)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(badgeColor.opacity(0.15), in: Capsule())
            .foregroundStyle(badgeColor)
    }

    private var badgeColor: Color {
        switch epic.statusBadge {
        case .onTrack: .green
        case .atRisk: .orange
        case .behind: .red
        }
    }

    // MARK: - Expanded View

    private var expandedView: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Forecast
            if let fw = epic.forecastWeeks {
                HStack(spacing: 4) {
                    Image(systemName: "chart.line.uptrend.xyaxis")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text("Forecast: ~\(String(format: "%.1f", fw)) weeks remaining")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            // Issues table
            if !epic.issues.isEmpty {
                issuesSection
            }

            // Participants
            if !epic.participants.isEmpty {
                participantsSection
            }

            // Who to ping
            if !epic.pingTargets.isEmpty {
                WhoToPingView(targets: epic.pingTargets, dbPool: dbPool)
            }
        }
    }

    // MARK: - Issues Section

    private var issuesSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Issues (\(epic.issues.count))")
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)

            ForEach(epic.issues, id: \.key) { issue in
                issueRow(issue)
            }
        }
    }

    private func issueRow(_ issue: JiraIssue) -> some View {
        let isStale = !issue.statusCategoryChangedAt.isEmpty
            && issue.statusCategory != "done"
            && JiraHelpers.daysSince(issue.statusCategoryChangedAt) > JiraHelpers.staleThresholdDays

        return HStack(spacing: 8) {
            // Status icon
            Image(systemName: statusIcon(for: issue))
                .font(.caption)
                .foregroundStyle(statusIconColor(for: issue))
                .frame(width: 16)

            Text(issue.key)
                .font(.caption)
                .fontWeight(.medium)
                .foregroundStyle(.blue)

            Text(issue.summary)
                .font(.caption)
                .lineLimit(1)
                .foregroundStyle(isStale ? .orange : .primary)

            Spacer()

            if !issue.assigneeDisplayName.isEmpty {
                Text(issue.assigneeDisplayName)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }

            issueStatusBadge(issue.status)

            if isStale {
                Image(systemName: "clock.badge.exclamationmark")
                    .font(.caption2)
                    .foregroundStyle(.orange)
                    .help("Stale: no status change in 7+ days")
            }
        }
        .padding(.vertical, 3)
        .padding(.horizontal, 6)
        .background(
            isStale
                ? Color.orange.opacity(0.06)
                : Color.clear,
            in: RoundedRectangle(cornerRadius: 4)
        )
    }

    private func statusIcon(for issue: JiraIssue) -> String {
        if issue.status.lowercased().contains("block") { return "xmark.octagon.fill" }
        switch issue.statusCategory {
        case "done": return "checkmark.circle.fill"
        case "indeterminate", "in_progress": return "circle.dotted.circle"
        default: return "circle"
        }
    }

    private func statusIconColor(for issue: JiraIssue) -> Color {
        if issue.status.lowercased().contains("block") { return .red }
        switch issue.statusCategory {
        case "done": return .green
        case "indeterminate", "in_progress": return .blue
        default: return .secondary
        }
    }

    private func issueStatusBadge(_ status: String) -> some View {
        Text(status)
            .font(.system(size: 9))
            .fontWeight(.medium)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(Color.secondary.opacity(0.1), in: Capsule())
            .foregroundStyle(.secondary)
    }

    // MARK: - Participants Section

    private var participantsSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Participants (\(epic.participants.count))")
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)

            FlowLayout(spacing: 6) {
                ForEach(epic.participants, id: \.slackID) { participant in
                    participantChip(participant)
                }
            }
        }
    }

    private func participantChip(
        _ participant: (slackID: String, name: String)
    ) -> some View {
        HStack(spacing: 4) {
            let initial = String(participant.name.prefix(1)).uppercased()
            ZStack {
                Circle()
                    .fill(avatarColor(for: participant.slackID).gradient)
                    .frame(width: 20, height: 20)
                Text(initial)
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.white)
            }
            Text(participant.name)
                .font(.caption)
                .lineLimit(1)
        }
        .padding(.trailing, 4)
        .padding(.vertical, 2)
        .padding(.leading, 2)
        .background(.quaternary.opacity(0.5), in: Capsule())
    }

    private func avatarColor(for userID: String) -> Color {
        JiraHelpers.avatarColor(for: userID)
    }
}

