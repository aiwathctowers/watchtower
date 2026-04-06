import SwiftUI
import GRDB

struct JiraBoardsSettingsView: View {
    @Environment(AppState.self) private var appState
    @State private var boards: [JiraBoard] = []
    @State private var observationTask: Task<Void, Never>?
    @State private var toggleError: String?

    var body: some View {
        Section("Boards") {
            if boards.isEmpty {
                Text("No boards synced yet")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(boards, id: \.id) { board in
                    boardRow(board)
                }
            }

            if let err = toggleError {
                Text(err)
                    .font(.caption)
                    .foregroundStyle(.red)
            }
        }
        .onAppear { startObserving() }
        .onDisappear { observationTask?.cancel() }
    }

    private func boardRow(_ board: JiraBoard) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(board.name)
                    .fontWeight(.medium)
                HStack(spacing: 6) {
                    Text(board.projectKey)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(board.boardType)
                        .font(.caption2)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            boardTypeBadgeColor(board.boardType),
                            in: Capsule()
                        )
                }
            }

            Spacer()

            Text("\(board.issueCount) issues")
                .font(.caption)
                .foregroundStyle(.secondary)

            Toggle(
                "",
                isOn: Binding(
                    get: { board.isSelected },
                    set: { newValue in
                        toggleBoard(board, selected: newValue)
                    }
                )
            )
            .labelsHidden()
            .toggleStyle(.switch)
        }
    }

    private func boardTypeBadgeColor(
        _ type: String
    ) -> Color {
        switch type {
        case "scrum": return .blue.opacity(0.2)
        case "kanban": return .green.opacity(0.2)
        default: return .gray.opacity(0.2)
        }
    }

    private func toggleBoard(_ board: JiraBoard, selected: Bool) {
        guard let cliPath = Constants.findCLIPath() else {
            toggleError = "Watchtower CLI not found"
            return
        }

        toggleError = nil
        let action = selected ? "select" : "deselect"

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "boards", action, String(board.id)
            ]
            process.environment = Constants.resolvedEnvironment()
            process.currentDirectoryURL =
                Constants.processWorkingDirectory()

            let stderrPipe = Pipe()
            process.standardOutput = Pipe()
            process.standardError = stderrPipe

            do {
                try process.run()
            } catch {
                await MainActor.run {
                    toggleError = "Failed to launch CLI"
                }
                return
            }

            let stderrData = stderrPipe.fileHandleForReading
                .readDataToEndOfFile()
            process.waitUntilExit()

            if process.terminationStatus != 0 {
                let stderr = String(
                    data: stderrData, encoding: .utf8
                )?.trimmingCharacters(
                    in: .whitespacesAndNewlines
                ) ?? ""
                await MainActor.run {
                    toggleError = stderr.isEmpty
                        ? "Failed to \(action) board"
                        : String(stderr.prefix(200))
                }
            }
        }
    }

    private func startObserving() {
        guard let db = appState.databaseManager else { return }
        loadBoards(db: db)
        let dbPool = db.dbPool
        observationTask = Task {
            let observation = ValueObservation.tracking { db in
                try JiraQueries.fetchAllBoards(db)
            }
            do {
                for try await newBoards in observation.values(
                    in: dbPool
                ).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self.boards = newBoards
                }
            } catch {}
        }
    }

    private func loadBoards(db: DatabaseManager) {
        Task {
            let result = try? await db.dbPool.read { db in
                try JiraQueries.fetchAllBoards(db)
            }
            if let result {
                boards = result
            }
        }
    }
}
