import SwiftUI

enum JiraHelpers {
    // MARK: - Constants

    static let staleThresholdDays = 7
    static let velocityWindowDays = 28
    static let blockedRatioThreshold = 0.3
    static let progressAtRiskThreshold = 0.8

    // MARK: - Avatar Color

    static func avatarColor(for userID: String) -> Color {
        let colors: [Color] = [
            .red, .orange, .yellow, .green, .mint, .teal,
            .cyan, .blue, .indigo, .purple, .pink, .brown,
        ]
        let hash = userID.unicodeScalars.reduce(0) { $0 &+ Int($1.value) }
        return colors[abs(hash) % colors.count]
    }

    // MARK: - Date Helpers

    static let isoFormatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fmt
    }()

    static func daysSince(_ dateStr: String) -> Int {
        guard !dateStr.isEmpty else { return 0 }
        guard let date = isoFormatter.date(from: dateStr)
                ?? ISO8601DateFormatter().date(from: dateStr) else {
            return 0
        }
        return max(
            0,
            Calendar.current.dateComponents([.day], from: date, to: Date()).day ?? 0
        )
    }
}
