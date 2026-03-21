import SwiftUI

struct ProgressDetailView: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        let manager = appState.backgroundTaskManager

        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                header(manager)
                costSummary(manager)

                ForEach(BackgroundTaskManager.TaskKind.allCases) { kind in
                    if let state = manager.tasks[kind] {
                        taskSection(kind: kind, state: state)
                    }
                }
            }
            .padding(20)
        }
        .frame(minWidth: 500, minHeight: 400)
    }

    // MARK: - Header

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

    // MARK: - Cost Summary

    private func costSummary(_ manager: BackgroundTaskManager) -> some View {
        GroupBox("Total Cost") {
            HStack(spacing: 24) {
                costItem(label: "Input Tokens", value: formatTokens(manager.totalInputTokens))
                costItem(label: "Output Tokens", value: formatTokens(manager.totalOutputTokens))
                costItem(label: "Cost", value: String(format: "$%.4f", manager.totalCostUsd))
            }
            .padding(4)
        }
    }

    private func costItem(label: String, value: String) -> some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.headline)
                .fontWeight(.bold)
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    // MARK: - Task Section

    private func taskSection(kind: BackgroundTaskManager.TaskKind, state: BackgroundTaskManager.TaskState) -> some View {
        GroupBox {
            VStack(alignment: .leading, spacing: 8) {
                // Current status
                HStack {
                    statusBadge(state.status)
                    Spacer()
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

                if state.status == .running, let p = state.progress, p.total > 0 {
                    ProgressView(value: Double(p.done), total: Double(max(p.total, 1)))
                        .tint(.accentColor)

                    if let status = p.status, !status.isEmpty {
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
                }

                // Step history
                if !state.stepHistory.isEmpty {
                    Divider()
                    Text("Step History")
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundStyle(.secondary)

                    let taskInputTokens = state.stepHistory.reduce(0) { $0 + $1.inputTokens }
                    let taskOutputTokens = state.stepHistory.reduce(0) { $0 + $1.outputTokens }
                    let taskCost = state.stepHistory.reduce(0.0) { $0 + $1.costUsd }

                    HStack(spacing: 16) {
                        Text("\(taskInputTokens + taskOutputTokens) tokens")
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                        Text(String(format: "$%.4f", taskCost))
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }

                    ForEach(state.stepHistory.reversed()) { record in
                        stepRow(record)
                    }
                }
            }
            .padding(4)
        } label: {
            Label(kind.title, systemImage: kind.icon)
        }
    }

    // MARK: - Step Row

    private func stepRow(_ record: BackgroundTaskManager.StepRecord) -> some View {
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

            if record.costUsd > 0 {
                Text(String(format: "$%.4f", record.costUsd))
                    .font(.system(size: 9, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: - Helpers

    @ViewBuilder
    private func statusBadge(_ status: BackgroundTaskManager.TaskStatus) -> some View {
        switch status {
        case .pending:
            HStack(spacing: 4) {
                Image(systemName: "clock")
                    .font(.caption)
                Text("Pending")
                    .font(.caption)
            }
            .foregroundStyle(.secondary)
        case .running:
            HStack(spacing: 4) {
                Image(systemName: "arrow.triangle.2.circlepath")
                    .font(.caption)
                Text("Running")
                    .font(.caption)
            }
            .foregroundStyle(Color.accentColor)
        case .done:
            HStack(spacing: 4) {
                Image(systemName: "checkmark.circle.fill")
                    .font(.caption)
                Text("Done")
                    .font(.caption)
            }
            .foregroundStyle(.green)
        case .error:
            HStack(spacing: 4) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.caption)
                Text("Error")
                    .font(.caption)
            }
            .foregroundStyle(.red)
        }
    }

    private func formatETA(_ seconds: Double) -> String {
        let s = Int(seconds)
        if s < 60 { return "~\(max(s, 1))s left" }
        let m = s / 60
        let rem = s % 60
        if rem == 0 { return "~\(m)m left" }
        return "~\(m)m \(rem)s left"
    }

    private func formatTokens(_ count: Int) -> String {
        if count >= 1_000_000 {
            return String(format: "%.1fM", Double(count) / 1_000_000)
        } else if count >= 1_000 {
            return String(format: "%.1fK", Double(count) / 1_000)
        }
        return "\(count)"
    }
}