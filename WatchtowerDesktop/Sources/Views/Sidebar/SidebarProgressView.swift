import SwiftUI

/// Shows background task progress panels in the sidebar.
struct SidebarProgressView: View {
    @Environment(AppState.self) private var appState
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        let manager = appState.backgroundTaskManager
        let visibleTasks = BackgroundTaskManager.TaskKind.allCases.filter { kind in
            guard let state = manager.tasks[kind] else { return false }
            switch state.status {
            case .done: return false
            default: return true
            }
        }

        if !visibleTasks.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Text("BACKGROUND")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 12)

                ForEach(visibleTasks) { kind in
                    if let state = manager.tasks[kind] {
                        taskPanel(kind: kind, state: state)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func taskPanel(kind: BackgroundTaskManager.TaskKind, state: BackgroundTaskManager.TaskState) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                statusIcon(state.status)
                    .frame(width: 14)

                Text(kind.title)
                    .font(.caption)
                    .fontWeight(.medium)
                    .lineLimit(1)

                Spacer()

                if let eta = state.etaSeconds, state.status == .running {
                    Text(formatETA(eta))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }

            switch state.status {
            case .running:
                if let p = state.progress, p.total > 0 {
                    ProgressView(value: Double(p.done), total: Double(max(p.total, 1)))
                        .tint(.accentColor)
                        .scaleEffect(y: 0.7)

                    if let status = p.status, !status.isEmpty {
                        Text(status)
                            .font(.system(size: 9))
                            .foregroundStyle(.tertiary)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                } else {
                    ProgressView()
                        .controlSize(.small)
                        .scaleEffect(0.6, anchor: .leading)
                }

            case .error(let msg):
                Text(msg)
                    .font(.system(size: 9))
                    .foregroundStyle(.red)
                    .lineLimit(2)

                Button("Retry") {
                    appState.backgroundTaskManager.retry(kind)
                }
                .font(.caption2)
                .buttonStyle(.bordered)
                .controlSize(.mini)

            case .pending:
                Text("Waiting...")
                    .font(.system(size: 9))
                    .foregroundStyle(.tertiary)

            case .done:
                EmptyView()
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .contentShape(Rectangle())
        .onTapGesture {
            openWindow(id: "progress-detail")
        }
        .background(
            RoundedRectangle(cornerRadius: 6)
                .fill(Color(nsColor: .controlBackgroundColor))
        )
        .padding(.horizontal, 8)
    }

    @ViewBuilder
    private func statusIcon(_ status: BackgroundTaskManager.TaskStatus) -> some View {
        switch status {
        case .pending:
            Image(systemName: "clock")
                .font(.system(size: 10))
                .foregroundStyle(.secondary)
        case .running:
            Image(systemName: "arrow.triangle.2.circlepath")
                .font(.system(size: 10))
                .foregroundStyle(Color.accentColor)
        case .done:
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 10))
                .foregroundStyle(.green)
        case .error:
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 10))
                .foregroundStyle(.red)
        }
    }

    private func formatETA(_ seconds: Double) -> String {
        let s = Int(seconds)
        if s < 60 { return "~\(max(s, 1))s" }
        let m = s / 60
        let rem = s % 60
        if rem == 0 { return "~\(m)m" }
        return "~\(m)m \(rem)s"
    }
}
