import SwiftUI
import GRDB

struct JiraBoardsSettingsView: View {
    @Environment(AppState.self) private var appState
    @State private var boards: [JiraBoard] = []
    @State private var observationTask: Task<Void, Never>?
    @State private var toggleError: String?

    @State private var isFetching = false
    @State private var reAnalyzingBoardID: Int?
    // TODO: notifiedBoardIDs resets when the view is recreated. Consider @AppStorage or a static Set if persistent dedup is needed.
    @State private var notifiedBoardIDs: Set<Int> = []

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

            Button(action: fetchBoards) {
                HStack(spacing: 6) {
                    if isFetching {
                        ProgressView()
                            .controlSize(.small)
                    }
                    Text(boards.isEmpty ? "Fetch Boards" : "Refresh Boards")
                }
            }
            .disabled(isFetching)

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
        NavigationLink(destination: JiraBoardProfileView(board: board)) {
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
                        analyzedBadge(board)
                        if board.isConfigChanged {
                            configChangedBadge
                        }
                    }
                }

                Spacer()

                if board.isConfigChanged {
                    reAnalyzeButton(board)
                }

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
        .onAppear {
            checkAndNotifyConfigChange(board)
        }
    }

    @ViewBuilder
    private func analyzedBadge(_ board: JiraBoard) -> some View {
        if !board.llmProfileJSON.isEmpty {
            Text("Analyzed")
                .font(.caption2)
                .foregroundStyle(.green)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.green.opacity(0.15), in: Capsule())
        } else {
            Text("Not analyzed")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.gray.opacity(0.15), in: Capsule())
        }
    }

    private var configChangedBadge: some View {
        Text("Config changed")
            .font(.caption2)
            .foregroundStyle(.orange)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(Color.orange.opacity(0.15), in: Capsule())
    }

    private func reAnalyzeButton(_ board: JiraBoard) -> some View {
        Button {
            reAnalyzeBoard(board)
        } label: {
            HStack(spacing: 4) {
                if reAnalyzingBoardID == board.id {
                    ProgressView().controlSize(.mini)
                }
                Text("Re-analyze")
                    .font(.caption)
            }
        }
        .buttonStyle(.bordered)
        .controlSize(.small)
        .disabled(reAnalyzingBoardID == board.id)
    }

    private func reAnalyzeBoard(_ board: JiraBoard) {
        guard let cliPath = Constants.findCLIPath() else {
            toggleError = "Watchtower CLI not found"
            return
        }

        reAnalyzingBoardID = board.id
        toggleError = nil

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = [
                "jira", "boards", "analyze", "--force",
                String(board.id),
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
                    reAnalyzingBoardID = nil
                    toggleError = "Failed to launch CLI"
                }
                return
            }

            let stderrData = stderrPipe.fileHandleForReading
                .readDataToEndOfFile()
            process.waitUntilExit()

            await MainActor.run {
                reAnalyzingBoardID = nil
                if process.terminationStatus != 0 {
                    let stderr = String(
                        data: stderrData, encoding: .utf8
                    )?.trimmingCharacters(
                        in: .whitespacesAndNewlines
                    ) ?? ""
                    toggleError = stderr.isEmpty
                        ? "Re-analysis failed"
                        : String(stderr.prefix(200))
                }
            }
        }
    }

    private func checkAndNotifyConfigChange(_ board: JiraBoard) {
        guard board.isConfigChanged,
              !notifiedBoardIDs.contains(board.id) else { return }
        notifiedBoardIDs.insert(board.id)
        NotificationService.shared
            .sendBoardConfigChangedNotification(boardName: board.name)
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

    private func fetchBoards() {
        guard let cliPath = Constants.findCLIPath() else {
            toggleError = "Watchtower CLI not found"
            return
        }

        isFetching = true
        toggleError = nil

        Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: cliPath)
            process.arguments = ["jira", "boards"]
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
                    isFetching = false
                    toggleError = "Failed to launch CLI"
                }
                return
            }

            let stderrData = stderrPipe.fileHandleForReading
                .readDataToEndOfFile()
            process.waitUntilExit()

            await MainActor.run {
                isFetching = false
                if process.terminationStatus != 0 {
                    let stderr = String(
                        data: stderrData, encoding: .utf8
                    )?.trimmingCharacters(
                        in: .whitespacesAndNewlines
                    ) ?? ""
                    toggleError = stderr.isEmpty
                        ? "Failed to fetch boards"
                        : String(stderr.prefix(200))
                }
            }
        }
    }
}
