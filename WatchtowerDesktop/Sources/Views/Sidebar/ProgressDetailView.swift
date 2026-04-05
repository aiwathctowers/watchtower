import SwiftUI

/// Reusable content for pipeline progress display.
/// Used both in the standalone window (ProgressDetailView) and embedded in UsageView.
struct ProgressDetailContent: View {
    @Environment(AppState.self) private var appState
    @State private var expandedSteps: Set<UUID> = []
    @State private var collapsedTasks: Set<BackgroundTaskManager.TaskKind> = []
    @State private var historyVM = PipelineHistoryViewModel()
    @State private var expandedRuns: Set<Int64> = []

    var body: some View {
        let manager = appState.backgroundTaskManager

        VStack(alignment: .leading, spacing: 16) {
            // Live progress (only when tasks are active)
            if !manager.tasks.isEmpty {
                header(manager)

                ForEach(BackgroundTaskManager.TaskKind.allCases) { kind in
                    if let state = manager.tasks[kind] {
                        taskSection(kind: kind, state: state)
                    }
                }
            }

            // Run history from DB
            if !historyVM.runs.isEmpty {
                if !manager.tasks.isEmpty {
                    Divider()
                        .padding(.vertical, 4)
                }

                Text("Run History")
                    .font(.title2)
                    .fontWeight(.bold)

                ForEach(historyVM.runs) { run in
                    runSection(run)
                }
            } else if manager.tasks.isEmpty {
                Text("Pipeline Progress")
                    .font(.title2)
                    .fontWeight(.bold)
                Text("No pipeline runs yet.")
                    .foregroundStyle(.secondary)
            }
        }
        .onAppear {
            if let dbPool = appState.databaseManager?.dbPool {
                historyVM.start(dbPool: dbPool)
            }
        }
        .onDisappear {
            historyVM.stop()
        }
    }

    // MARK: - Run History Section

    private func runSection(_ run: PipelineRun) -> some View {
        let isExpanded = expandedRuns.contains(run.id)

        return GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label(run.pipelineTitle, systemImage: run.pipelineIcon)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                        .animation(.easeInOut(duration: 0.15), value: isExpanded)

                    Spacer()

                    Text(run.source)
                        .font(.system(size: 9))
                        .padding(.horizontal, 4)
                        .padding(.vertical, 1)
                        .background(.quaternary)
                        .cornerRadius(3)

                    runStatusBadge(run.status)

                    if run.itemsFound > 0 {
                        Text("\(run.itemsFound) items")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .contentShape(Rectangle())
                .onTapGesture {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        if isExpanded {
                            expandedRuns.remove(run.id)
                        } else {
                            expandedRuns.insert(run.id)
                            historyVM.loadSteps(for: run.id)
                        }
                    }
                }

                if isExpanded {
                    runDetailContent(run)
                }
            }
            .padding(4)
        }
    }

    private func runDetailContent(_ run: PipelineRun) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 16) {
                if let date = run.startedDate {
                    detailRow(label: "Started", value: date.formatted(date: .abbreviated, time: .shortened))
                }
                if run.durationSeconds > 0 {
                    detailRow(label: "Duration", value: formatDuration(run.durationSeconds))
                }
            }

            if run.inputTokens > 0 || run.outputTokens > 0 {
                HStack(spacing: 16) {
                    detailRow(label: "Input", value: formatTokens(run.inputTokens))
                    if run.totalApiTokens > 0 {
                        detailRow(label: "Input (+ cache)", value: formatTokens(run.totalApiTokens))
                    }
                    detailRow(label: "Output", value: formatTokens(run.outputTokens))

                }
            }

            if !run.errorMsg.isEmpty {
                Text(run.errorMsg)
                    .font(.caption)
                    .foregroundStyle(.red)
                    .lineLimit(3)
            }

            // Steps
            if let steps = historyVM.steps[run.id], !steps.isEmpty {
                Divider()
                Text("Steps")
                    .font(.caption)
                    .fontWeight(.medium)
                    .foregroundStyle(.secondary)

                ForEach(steps) { step in
                    dbStepRow(step)
                }
            }
        }
        .padding(.leading, 8)
    }

    private func dbStepRow(_ step: PipelineStepRecord) -> some View {
        HStack(spacing: 8) {
            Text("\(step.step)/\(step.total)")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .frame(width: 40)

            if !step.status.isEmpty {
                Text(step.status)
                    .font(.caption2)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }

            Spacer()

            if step.durationSeconds > 0 {
                Text(formatDuration(step.durationSeconds))
                    .font(.system(size: 9, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }

            let stepTokens = step.inputTokens + step.outputTokens
            if stepTokens > 0 {
                Text("\(formatTokens(step.inputTokens))/\(formatTokens(step.outputTokens))")
                    .font(.system(size: 9, design: .monospaced))
                    .foregroundStyle(.secondary)
            }

        }
    }

    @ViewBuilder
    private func runStatusBadge(_ status: String) -> some View {
        switch status {
        case "done":
            HStack(spacing: 4) {
                Image(systemName: "checkmark.circle.fill").font(.caption)
                Text("Done").font(.caption)
            }
            .foregroundStyle(.green)
        case "error":
            HStack(spacing: 4) {
                Image(systemName: "exclamationmark.triangle.fill").font(.caption)
                Text("Error").font(.caption)
            }
            .foregroundStyle(.red)
        case "running":
            HStack(spacing: 4) {
                Image(systemName: "arrow.triangle.2.circlepath").font(.caption)
                Text("Running").font(.caption)
            }
            .foregroundStyle(Color.accentColor)
        default:
            Text(status).font(.caption).foregroundStyle(.secondary)
        }
    }

    // MARK: - Live Progress Header

    private func header(_ manager: BackgroundTaskManager) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Pipeline Progress")
                .font(.title2)
                .fontWeight(.bold)

            if manager.allFinished {
                Text("All pipelines completed")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else if manager.hasActiveTasks {
                Text("Processing...")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Task Section

    private func isTaskExpanded(_ kind: BackgroundTaskManager.TaskKind) -> Bool {
        !collapsedTasks.contains(kind)
    }

    private func toggleTask(_ kind: BackgroundTaskManager.TaskKind) {
        withAnimation(.easeInOut(duration: 0.2)) {
            if collapsedTasks.contains(kind) {
                collapsedTasks.remove(kind)
            } else {
                collapsedTasks.insert(kind)
            }
        }
    }

    private func taskSection(
        kind: BackgroundTaskManager.TaskKind,
        state: BackgroundTaskManager.TaskState
    ) -> some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Label(kind.title, systemImage: kind.icon)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 9))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isTaskExpanded(kind) ? 90 : 0))
                        .animation(.easeInOut(duration: 0.15), value: isTaskExpanded(kind))

                    Spacer()

                    statusBadge(state.status)

                    if let progress = state.progress, progress.total > 0 {
                        Text("\(progress.done)/\(progress.total)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    if let eta = state.etaSeconds, state.status == .running {
                        Text(formatETA(eta))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
                .contentShape(Rectangle())
                .onTapGesture { toggleTask(kind) }

                if isTaskExpanded(kind) {
                    if state.status == .running, let prog = state.progress {
                        if prog.total > 0 {
                            ProgressView(value: Double(prog.done), total: Double(max(prog.total, 1)))
                                .tint(.accentColor)
                        } else {
                            ProgressView()
                                .controlSize(.small)
                        }

                        if let status = prog.status, !status.isEmpty {
                            Text(status)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .lineLimit(2)
                        }
                    }

                    if case .error(let msg) = state.status {
                        Text(msg)
                            .font(.caption)
                            .foregroundStyle(.red)
                            .lineLimit(3)

                        Button("Retry") {
                            appState.backgroundTaskManager.retry(kind)
                        }
                        .font(.caption2)
                        .buttonStyle(.bordered)
                        .controlSize(.mini)
                    }

                    if !state.stepHistory.isEmpty {
                        Divider()
                        stepHistorySection(state.stepHistory)
                    }
                }
            }
            .padding(4)
        }
    }

    private func stepHistorySection(_ history: [BackgroundTaskManager.StepRecord]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Steps")
                .font(.caption)
                .fontWeight(.medium)
                .foregroundStyle(.secondary)

            ForEach(history.reversed()) { record in
                stepRow(record)
            }
        }
    }

    // MARK: - Step Row

    private func stepRow(_ record: BackgroundTaskManager.StepRecord) -> some View {
        let isExpanded = expandedSteps.contains(record.id)

        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Text(record.timestamp, style: .time)
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.tertiary)

                Text("\(record.step)/\(record.total)")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .frame(width: 40)

                if !record.status.isEmpty {
                    Text(record.status)
                        .font(.caption2)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }

                Spacer()

                if record.durationSeconds > 0 {
                    Text(formatDuration(record.durationSeconds))
                        .font(.system(size: 9, design: .monospaced))
                        .foregroundStyle(.tertiary)
                }

                let stepTokens = record.inputTokens + record.outputTokens
                if stepTokens > 0 {
                    let apiLabel = record.totalApiTokens > 0 ? "(\(formatTokens(record.totalApiTokens)))" : ""
                    Text("\(formatTokens(record.inputTokens))\(apiLabel)/\(formatTokens(record.outputTokens))")
                        .font(.system(size: 9, design: .monospaced))
                        .foregroundStyle(.secondary)
                }

                Image(systemName: "chevron.right")
                    .font(.system(size: 8))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(isExpanded ? 90 : 0))
                    .animation(.easeInOut(duration: 0.15), value: isExpanded)
            }
            .contentShape(Rectangle())
            .onTapGesture {
                withAnimation(.easeInOut(duration: 0.2)) {
                    if isExpanded {
                        expandedSteps.remove(record.id)
                    } else {
                        expandedSteps.insert(record.id)
                    }
                }
            }

            if isExpanded {
                stepDetail(record)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
    }

    // MARK: - Step Detail

    private func stepDetail(_ record: BackgroundTaskManager.StepRecord) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Divider()
                .padding(.vertical, 4)

            detailRow(label: "Duration", value: formatDuration(record.durationSeconds))

            let hasTokens = record.inputTokens + record.outputTokens > 0
            if hasTokens {
                detailRow(label: "Input", value: formatTokens(record.inputTokens))
                if record.totalApiTokens > 0 {
                    detailRow(label: "Input (+ cache)", value: formatTokens(record.totalApiTokens))
                }
                detailRow(label: "Output", value: formatTokens(record.outputTokens))
            }

            if record.durationSeconds > 0 && record.outputTokens > 0 {
                detailRow(
                    label: "Speed",
                    value: String(format: "%.0f tok/s", Double(record.outputTokens) / record.durationSeconds)
                )
            }

            if let count = record.messageCount, count > 0 {
                detailRow(label: "Messages", value: "\(count)")
            }

            if let periodStr = formatStepPeriod(record) {
                detailRow(label: "Period", value: periodStr)
            }
        }
        .padding(.leading, 24)
        .padding(.bottom, 4)
    }

    private func detailRow(label: String, value: String) -> some View {
        HStack {
            Text(label)
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)
                .frame(width: 90, alignment: .leading)
            Text(value)
                .font(.system(size: 10, design: .monospaced))
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Helpers

    @ViewBuilder
    private func statusBadge(_ status: BackgroundTaskManager.TaskStatus) -> some View {
        switch status {
        case .pending:
            HStack(spacing: 4) {
                Image(systemName: "clock").font(.caption)
                Text("Pending").font(.caption)
            }
            .foregroundStyle(.secondary)
        case .running:
            HStack(spacing: 4) {
                Image(systemName: "arrow.triangle.2.circlepath").font(.caption)
                Text("Running").font(.caption)
            }
            .foregroundStyle(Color.accentColor)
        case .done:
            HStack(spacing: 4) {
                Image(systemName: "checkmark.circle.fill").font(.caption)
                Text("Done").font(.caption)
            }
            .foregroundStyle(.green)
        case .error:
            HStack(spacing: 4) {
                Image(systemName: "exclamationmark.triangle.fill").font(.caption)
                Text("Error").font(.caption)
            }
            .foregroundStyle(.red)
        }
    }

    private func formatETA(_ seconds: Double) -> String {
        let s = Int(seconds)
        if s < 60 { return "~\(max(s, 1))s left" }
        let min = s / 60
        let rem = s % 60
        if rem == 0 { return "~\(min)m left" }
        return "~\(min)m \(rem)s left"
    }

    private func formatDuration(_ seconds: Double) -> String {
        let s = Int(seconds)
        if s < 60 { return "\(max(s, 1))s" }
        let min = s / 60
        let rem = s % 60
        return "\(min)m \(rem)s"
    }

    private func formatTokens(_ count: Int) -> String {
        if count >= 1_000_000 {
            return String(format: "%.1fM", Double(count) / 1_000_000)
        } else if count >= 1_000 {
            return String(format: "%.1fK", Double(count) / 1_000)
        }
        return "\(count)"
    }

    private func formatStepPeriod(_ record: BackgroundTaskManager.StepRecord) -> String? {
        guard let from = record.periodFrom, let to = record.periodTo else { return nil }
        let df = DateFormatter()
        df.dateStyle = .short
        df.timeStyle = .short
        let fromDate = Date(timeIntervalSince1970: from)
        let toDate = Date(timeIntervalSince1970: to)
        return "\(df.string(from: fromDate)) - \(df.string(from: toDate))"
    }
}

/// Standalone window wrapper for ProgressDetailContent.
struct ProgressDetailView: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        ScrollView {
            ProgressDetailContent()
                .environment(appState)
                .padding(20)
        }
        .frame(minWidth: 500, minHeight: 400)
    }
}
