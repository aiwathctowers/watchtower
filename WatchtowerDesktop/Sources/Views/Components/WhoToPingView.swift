import SwiftUI
import GRDB

/// Reusable component showing up to 3 "who to ping" targets with avatar,
/// name, reason badge, and an "Open in Slack" button.
///
/// Returns `EmptyView` when the list is empty.
struct WhoToPingView: View {
    let targets: [PingTargetItem]
    let dbPool: DatabasePool

    @State private var teamID: String?

    var body: some View {
        if targets.isEmpty {
            EmptyView()
        } else {
            content
        }
    }

    // MARK: - Content

    private var content: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Who to Ping", systemImage: "person.wave.2")
                .font(.subheadline)
                .fontWeight(.semibold)
                .foregroundStyle(.secondary)

            VStack(spacing: 6) {
                ForEach(targets.prefix(3)) { target in
                    targetRow(target)
                }
            }
        }
        .task {
            await loadTeamID()
        }
    }

    // MARK: - Target Row

    private func targetRow(_ target: PingTargetItem) -> some View {
        HStack(spacing: 8) {
            avatar(for: target)

            Text(target.displayName)
                .font(.callout)
                .lineLimit(1)

            reasonChip(target.reasonLabel)

            Spacer()

            slackButton(for: target)
        }
        .padding(.vertical, 4)
        .padding(.horizontal, 8)
        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 6))
    }

    // MARK: - Avatar

    private func avatar(for target: PingTargetItem) -> some View {
        let initial = String(target.displayName.prefix(1)).uppercased()
        let color = avatarColor(for: target.slackUserID)

        return ZStack {
            Circle()
                .fill(color.gradient)
                .frame(width: 28, height: 28)

            Text(initial)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(.white)
        }
    }

    private func avatarColor(for userID: String) -> Color {
        JiraHelpers.avatarColor(for: userID)
    }

    // MARK: - Reason Chip

    private func reasonChip(_ label: String) -> some View {
        Text(label)
            .font(.caption2)
            .fontWeight(.medium)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(.quaternary, in: Capsule())
    }

    // MARK: - Slack Button

    private func slackButton(for target: PingTargetItem) -> some View {
        Button {
            openInSlack(target)
        } label: {
            Image(systemName: "bubble.left.and.text.bubble.right")
                .font(.caption)
                .foregroundStyle(.blue)
        }
        .buttonStyle(.plain)
        .help("Open in Slack")
    }

    // MARK: - Helpers

    private func openInSlack(_ target: PingTargetItem) {
        var urlString = "slack://user?id=\(target.slackUserID)"
        if let team = teamID, !team.isEmpty {
            urlString = "slack://user?team=\(team)&id=\(target.slackUserID)"
        }
        guard let url = URL(string: urlString) else { return }
        NSWorkspace.shared.open(url)
    }

    private func loadTeamID() async {
        do {
            let ws = try await Task.detached { [dbPool] in
                try dbPool.read { db in
                    try WorkspaceQueries.fetchWorkspace(db)
                }
            }.value
            teamID = ws?.id
        } catch {
            teamID = nil
        }
    }
}
