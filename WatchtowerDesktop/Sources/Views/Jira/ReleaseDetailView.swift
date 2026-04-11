import SwiftUI

struct ReleaseDetailView: View {
    let release: ReleaseDashboardViewModel.ReleaseItem
    @Environment(AppState.self) private var appState

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Epic Progress section
            if !release.epicProgress.isEmpty {
                epicProgressSection
            }

            // Blocked Issues section
            if release.blockedCount > 0 {
                blockedSection
            }

            // Scope Changes section
            if release.scopeChanges.added > 0 || release.scopeChanges.removed > 0 {
                scopeChangesSection
            }
        }
    }

    // MARK: - Epic Progress

    private var epicProgressSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Epic Progress", systemImage: "list.bullet.rectangle")
                .font(.subheadline)
                .fontWeight(.semibold)

            ForEach(release.epicProgress) { epic in
                epicRow(epic)
            }
        }
    }

    private func epicRow(_ epic: ReleaseDashboardViewModel.EpicProgressItem) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(epic.key)
                    .font(.caption)
                    .fontWeight(.medium)
                    .foregroundStyle(.blue)

                Text(epic.name)
                    .font(.caption)
                    .lineLimit(1)

                Spacer()

                Text(epic.statusBadge)
                    .font(.caption2)
                    .foregroundStyle(epicBadgeColor(epic.statusBadge))
                    .padding(.horizontal, 4)
                    .padding(.vertical, 1)
                    .background(
                        epicBadgeColor(epic.statusBadge).opacity(0.12),
                        in: Capsule()
                    )

                Text("\(epic.done)/\(epic.total)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            ProgressView(value: epic.progressPct)
                .tint(epicProgressColor(epic.progressPct))
        }
        .padding(8)
        .background(Color.secondary.opacity(0.04), in: RoundedRectangle(cornerRadius: 6))
    }

    private func epicBadgeColor(_ badge: String) -> Color {
        switch badge {
        case "Done": .green
        case "In Progress": .blue
        default: .secondary
        }
    }

    private func epicProgressColor(_ pct: Double) -> Color {
        if pct >= 1.0 { return .green }
        if pct >= 0.5 { return .blue }
        return .orange
    }

    // MARK: - Blocked Issues

    private var blockedSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label(
                "\(release.blockedCount) Blocked Issue\(release.blockedCount == 1 ? "" : "s")",
                systemImage: "exclamationmark.triangle.fill"
            )
            .font(.subheadline)
            .fontWeight(.semibold)
            .foregroundStyle(.orange)

            // List blocked issues
            let blockedIssues = release.issues.filter {
                $0.status.lowercased().contains("block")
            }
            ForEach(blockedIssues, id: \.key) { issue in
                HStack(spacing: 6) {
                    Text(issue.key)
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.blue)

                    Text(issue.summary)
                        .font(.caption)
                        .lineLimit(1)

                    Spacer()

                    if !issue.assigneeDisplayName.isEmpty {
                        Text(issue.assigneeDisplayName)
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 2)
            }

            // Who to ping
            if !release.pingTargets.isEmpty, let db = appState.databaseManager {
                WhoToPingView(targets: release.pingTargets, dbPool: db.dbPool)
            }
        }
    }

    // MARK: - Scope Changes

    private var scopeChangesSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Label("Scope Changes (7d)", systemImage: "arrow.triangle.2.circlepath")
                .font(.subheadline)
                .fontWeight(.semibold)

            HStack(spacing: 12) {
                if release.scopeChanges.added > 0 {
                    HStack(spacing: 4) {
                        Image(systemName: "plus.circle.fill")
                            .foregroundStyle(.green)
                        Text("\(release.scopeChanges.added) added")
                            .font(.caption)
                    }
                }
                if release.scopeChanges.removed > 0 {
                    HStack(spacing: 4) {
                        Image(systemName: "minus.circle.fill")
                            .foregroundStyle(.red)
                        Text("\(release.scopeChanges.removed) removed")
                            .font(.caption)
                    }
                }
            }
        }
    }
}
