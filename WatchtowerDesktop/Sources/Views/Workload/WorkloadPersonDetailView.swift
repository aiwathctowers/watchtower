import SwiftUI
import GRDB

struct WorkloadPersonDetailView: View {
    let entry: WorkloadViewModel.WorkloadEntry
    let dbManager: DatabaseManager
    var onClose: (() -> Void)?

    @State private var issues: [JiraIssue] = []
    @State private var isLoading = true
    @State private var jiraSiteURL: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header
                statsGrid
                Divider()
                issuesList
            }
            .padding()
        }
        .onAppear { loadIssues() }
        .onChange(of: entry.slackUserID) { loadIssues() }
    }

    // MARK: - Header

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: "person.circle.fill")
                    .font(.title)
                    .foregroundStyle(.secondary)

                VStack(alignment: .leading, spacing: 2) {
                    Text(entry.displayName)
                        .font(.title2)
                        .fontWeight(.bold)
                    Text(entry.slackUserID)
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }

                Spacer()

                signalBadge

                if let onClose {
                    Button { onClose() } label: {
                        Image(systemName: "xmark")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    private var signalBadge: some View {
        HStack(spacing: 4) {
            Text(entry.signal.emoji)
            Text(entry.signal.label)
                .fontWeight(.semibold)
        }
        .font(.subheadline)
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(signalColor.opacity(0.15), in: Capsule())
        .foregroundStyle(signalColor)
    }

    private var signalColor: Color {
        switch entry.signal {
        case .overload: .red
        case .watch: .orange
        case .low: .secondary
        case .normal: .green
        }
    }

    // MARK: - Stats Grid

    private var statsGrid: some View {
        LazyVGrid(columns: [
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible()),
            GridItem(.flexible())
        ], spacing: 12) {
            statCard("Open Issues", value: "\(entry.openIssues)", icon: "doc.text", color: .blue)
            statCard("In Progress", value: "\(entry.inProgressCount)", icon: "arrow.forward.circle", color: .blue)
            statCard("Testing", value: "\(entry.testingCount)", icon: "checkmark.shield", color: .purple)
            statCard("Overdue", value: "\(entry.overdueCount)", icon: "exclamationmark.triangle", color: entry.overdueCount > 0 ? .red : .secondary)
            statCard("Blocked", value: "\(entry.blockedCount)", icon: "xmark.octagon", color: entry.blockedCount > 0 ? .orange : .secondary)
            statCard("Cycle Time", value: String(format: "%.1fd", entry.avgCycleTimeDays), icon: "clock", color: .secondary)
        }
    }

    private func statCard(_ title: String, value: String, icon: String, color: Color) -> some View {
        VStack(spacing: 4) {
            Image(systemName: icon)
                .font(.caption)
                .foregroundStyle(color)
            Text(value)
                .font(.headline)
                .fontWeight(.bold)
            Text(title)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .background(Color.secondary.opacity(0.06), in: RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - Issues List

    private var issuesList: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Issues (\(issues.count))")
                .font(.headline)

            if isLoading {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .padding()
            } else if issues.isEmpty {
                Text("No issues found")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding()
            } else {
                ForEach(issues, id: \.key) { issue in
                    issueRow(issue)
                }
            }
        }
    }

    private func issueRow(_ issue: JiraIssue) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Group {
                    if let url = JiraHelpers.browseURL(siteURL: jiraSiteURL, issueKey: issue.key) {
                        Link(destination: url) {
                            Text(issue.key)
                                .font(.caption)
                                .fontWeight(.bold)
                                .foregroundStyle(.blue)
                        }
                        .help("Open in Jira")
                    } else {
                        Text(issue.key)
                            .font(.caption)
                            .fontWeight(.bold)
                            .foregroundStyle(.blue)
                    }
                }

                statusBadge(issue)

                Spacer()

                if !issue.priority.isEmpty {
                    Text(issue.priority)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }

                if let sp = issue.storyPoints, sp > 0 {
                    Text("\(Int(sp)) SP")
                        .font(.caption2)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 1)
                        .background(Color.purple.opacity(0.12), in: Capsule())
                        .foregroundStyle(.purple)
                }
            }

            Text(issue.summary)
                .font(.subheadline)
                .lineLimit(2)

            HStack(spacing: 8) {
                if !issue.issueType.isEmpty {
                    Label(issue.issueType, systemImage: "tag")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }

                if !issue.dueDate.isEmpty {
                    Label(issue.dueDate, systemImage: "calendar")
                        .font(.caption2)
                        .foregroundStyle(isOverdue(issue) ? Color.red : Color.secondary)
                }
            }
        }
        .padding(8)
        .background(Color.secondary.opacity(0.04), in: RoundedRectangle(cornerRadius: 6))
    }

    private func statusBadge(_ issue: JiraIssue) -> some View {
        let color: Color = switch issue.statusCategory {
        case "done": .green
        case "in_progress": .blue
        default: .secondary
        }

        return Text(issue.status)
            .font(.caption2)
            .fontWeight(.medium)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(color.opacity(0.12), in: Capsule())
            .foregroundStyle(color)
    }

    private func isOverdue(_ issue: JiraIssue) -> Bool {
        guard !issue.dueDate.isEmpty, issue.statusCategory != "done" else { return false }
        return issue.dueDate < Self.dayFormatter.string(from: Date())
    }

    private static let dayFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        fmt.locale = Locale(identifier: "en_US_POSIX")
        return fmt
    }()

    // MARK: - Data Loading

    private func loadIssues() {
        isLoading = true
        do {
            issues = try dbManager.dbPool.read { db in
                try JiraQueries.fetchIssuesByAssignee(db, slackID: entry.slackUserID)
            }
            jiraSiteURL = JiraConfigHelper.readSiteURL()
        } catch {
            issues = []
        }
        isLoading = false
    }
}
