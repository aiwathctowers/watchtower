import SwiftUI
import GRDB

struct JiraUserMappingSettingsView: View {
    @Environment(AppState.self) private var appState
    @State private var mappings: [JiraUserMap] = []
    @State private var mappedCount: Int = 0
    @State private var unmappedCount: Int = 0
    @State private var slackUsers: [User] = []
    @State private var observationTask: Task<Void, Never>?

    var body: some View {
        Section("User Mapping") {
            HStack {
                Label(
                    "\(mappedCount) matched",
                    systemImage: "person.fill.checkmark"
                )
                .font(.caption)
                .foregroundStyle(.green)

                Label(
                    "\(unmappedCount) unmatched",
                    systemImage: "person.fill.questionmark"
                )
                .font(.caption)
                .foregroundStyle(.orange)
            }

            if mappings.isEmpty {
                Text("No Jira users synced yet")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(
                    mappings,
                    id: \.jiraAccountId
                ) { mapping in
                    userRow(mapping)
                }
            }
        }
        .onAppear { startObserving() }
        .onDisappear { observationTask?.cancel() }
    }

    private func userRow(
        _ mapping: JiraUserMap
    ) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(mapping.displayName)
                    .fontWeight(.medium)
                Spacer()
                Text(mapping.matchMethod)
                    .font(.caption2)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(
                        matchMethodColor(mapping.matchMethod),
                        in: Capsule()
                    )
                if mapping.matchConfidence > 0 {
                    Text(
                        String(
                            format: "%.0f%%",
                            mapping.matchConfidence * 100
                        )
                    )
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                }
            }
            HStack {
                Text(mapping.email)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if mapping.slackUserId.isEmpty {
                    slackUserPicker(mapping)
                } else {
                    Label(
                        slackDisplayName(
                            for: mapping.slackUserId
                        ),
                        systemImage: "bubble.left.fill"
                    )
                    .font(.caption)
                    .foregroundStyle(.blue)
                }
            }
        }
    }

    private func slackUserPicker(
        _ mapping: JiraUserMap
    ) -> some View {
        Menu {
            ForEach(slackUsers, id: \.id) { user in
                Button(user.displayName) {
                    assignSlackUser(
                        mapping: mapping,
                        slackUserId: user.id
                    )
                }
            }
        } label: {
            Label("Assign Slack user", systemImage: "link")
                .font(.caption)
                .foregroundStyle(.orange)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
    }

    private func matchMethodColor(
        _ method: String
    ) -> Color {
        switch method {
        case "email": return .green.opacity(0.2)
        case "display_name": return .blue.opacity(0.2)
        case "manual": return .purple.opacity(0.2)
        default: return .orange.opacity(0.2)
        }
    }

    private func slackDisplayName(
        for slackId: String
    ) -> String {
        slackUsers.first { $0.id == slackId }?.displayName
            ?? slackId
    }

    private func assignSlackUser(
        mapping: JiraUserMap,
        slackUserId: String
    ) {
        guard let cliPath = Constants.findCLIPath() else { return }

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "users", "map",
                mapping.jiraAccountId, slackUserId
            ]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()
            process.standardOutput = Pipe()
            process.standardError = Pipe()

            do {
                try process.run()
                process.waitUntilExit()
            } catch {}
        }
    }

    private func startObserving() {
        guard let db = appState.databaseManager else { return }
        loadData(db: db)
        let dbPool = db.dbPool
        observationTask = Task {
            let observation = ValueObservation.tracking { db in
                try JiraQueries.fetchUserMappings(db)
            }
            do {
                for try await newMappings in observation.values(
                    in: dbPool
                ).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self.mappings = newMappings
                    loadCounts(db: dbPool)
                }
            } catch {}
        }
    }

    private func loadData(db: DatabaseManager) {
        Task {
            let result = try? await db.dbPool.read { db in
                (
                    mappings: try JiraQueries.fetchUserMappings(db),
                    mapped: try JiraQueries.fetchMappedCount(db),
                    unmapped: try JiraQueries.fetchUnmappedCount(db),
                    users: try UserQueries.fetchAll(db)
                )
            }
            if let result {
                mappings = result.mappings
                mappedCount = result.mapped
                unmappedCount = result.unmapped
                slackUsers = result.users
            }
        }
    }

    private func loadCounts(db: DatabasePool) {
        Task {
            let result = try? await db.read { db in
                (
                    mapped: try JiraQueries.fetchMappedCount(db),
                    unmapped: try JiraQueries.fetchUnmappedCount(db)
                )
            }
            if let result {
                mappedCount = result.mapped
                unmappedCount = result.unmapped
            }
        }
    }
}
