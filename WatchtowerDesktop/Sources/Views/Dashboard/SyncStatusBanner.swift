import SwiftUI

struct SyncStatusBanner: View {
    let syncedAt: String?
    let isRunning: Bool

    var body: some View {
        HStack {
            Circle()
                .fill(isRunning ? Color.green : Color.gray)
                .frame(width: 8, height: 8)

            Text(isRunning ? "Daemon running" : "Daemon stopped")
                .font(.subheadline)

            Spacer()

            if let syncedAt {
                Text("Last sync: \(TimeFormatting.relativeTime(from: syncedAt))")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                Text("Never synced")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 8))
    }
}
