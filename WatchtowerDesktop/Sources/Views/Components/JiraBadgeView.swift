import SwiftUI

/// Reusable badge for displaying a Jira issue with status indicator.
/// Supports compact mode (for list rows) and expanded mode (for detail views).
struct JiraBadgeView: View {
    let issueKey: String
    let status: String
    let statusCategory: String
    let priority: String
    let siteURL: String?
    var isExpanded: Bool = false

    var body: some View {
        if isExpanded {
            expandedBody
        } else {
            compactBody
        }
    }

    // MARK: - Compact (list rows)

    private var compactBody: some View {
        Group {
            if let url = browseURL {
                Link(destination: url) {
                    compactLabel
                }
                .buttonStyle(.plain)
                .help("Open \(issueKey) in Jira")
            } else {
                compactLabel
            }
        }
    }

    private var compactLabel: some View {
        HStack(spacing: 4) {
            statusDot
            Text(issueKey)
                .font(.caption2)
                .fontWeight(.medium)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 2)
        .background(statusColor.opacity(0.10), in: Capsule())
    }

    // MARK: - Expanded (detail views)

    private var expandedBody: some View {
        Group {
            if let url = browseURL {
                Link(destination: url) {
                    expandedLabel
                }
                .buttonStyle(.plain)
                .help("Open \(issueKey) in Jira")
            } else {
                expandedLabel
            }
        }
    }

    private var expandedLabel: some View {
        HStack(spacing: 6) {
            statusDot

            Text(issueKey)
                .font(.caption)
                .fontWeight(.semibold)
                .foregroundStyle(.primary)

            Text(status)
                .font(.caption2)
                .foregroundStyle(statusColor)
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(statusColor.opacity(0.12), in: Capsule())

            if !priority.isEmpty {
                priorityIcon
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 6))
    }

    // MARK: - Shared Components

    private var statusDot: some View {
        Circle()
            .fill(statusColor)
            .frame(width: 7, height: 7)
    }

    @ViewBuilder
    private var priorityIcon: some View {
        switch priority.lowercased() {
        case "highest", "critical":
            Image(systemName: "chevron.up.2")
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(.red)
        case "high":
            Image(systemName: "chevron.up")
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(.red)
        case "medium":
            Image(systemName: "minus")
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(.orange)
        case "low":
            Image(systemName: "chevron.down")
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(.blue)
        case "lowest":
            Image(systemName: "chevron.down.2")
                .font(.system(size: 8, weight: .bold))
                .foregroundStyle(.blue)
        default:
            EmptyView()
        }
    }

    // MARK: - Helpers

    private var statusColor: Color {
        switch statusCategory.lowercased() {
        case "done":
            .green
        case "in_progress", "indeterminate":
            .blue
        default:
            .secondary
        }
    }

    private var browseURL: URL? {
        guard let site = siteURL, !site.isEmpty else { return nil }
        let base = site.hasSuffix("/") ? String(site.dropLast()) : site
        return URL(string: "\(base)/browse/\(issueKey)")
    }
}

// MARK: - Convenience Init from JiraIssue

extension JiraBadgeView {
    init(issue: JiraIssue, siteURL: String?, isExpanded: Bool = false) {
        self.issueKey = issue.key
        self.status = issue.status
        self.statusCategory = issue.statusCategory
        self.priority = issue.priority
        self.siteURL = siteURL
        self.isExpanded = isExpanded
    }
}
