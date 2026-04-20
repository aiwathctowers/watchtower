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

    private static let fallbackISOFormatter = ISO8601DateFormatter()

    static func browseURL(siteURL: String?, issueKey: String) -> URL? {
        guard let site = siteURL, !site.isEmpty else { return nil }
        let base = site.hasSuffix("/") ? String(site.dropLast()) : site
        return URL(string: "\(base)/browse/\(issueKey)")
    }

    private static let shortDateFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "dd MMM"
        return fmt
    }()

    static func shortDate(_ dateStr: String) -> String {
        guard !dateStr.isEmpty else { return "" }
        // Try date-only format first (YYYY-MM-DD from Jira due dates)
        if dateStr.count == 10, let date = dateOnlyFormatter.date(from: dateStr) {
            return shortDateFormatter.string(from: date)
        }
        guard let date = isoFormatter.date(from: dateStr)
                ?? fallbackISOFormatter.date(from: dateStr) else {
            return String(dateStr.prefix(10))
        }
        return shortDateFormatter.string(from: date)
    }

    private static let dateOnlyFormatter: DateFormatter = {
        let fmt = DateFormatter()
        fmt.dateFormat = "yyyy-MM-dd"
        return fmt
    }()

    static func daysSince(_ dateStr: String) -> Int {
        guard !dateStr.isEmpty else { return 0 }
        let date: Date?
        if dateStr.count == 10 {
            date = dateOnlyFormatter.date(from: dateStr)
        } else {
            date = isoFormatter.date(from: dateStr)
                ?? fallbackISOFormatter.date(from: dateStr)
        }
        guard let date else { return 0 }
        return max(
            0,
            Calendar.current.dateComponents([.day], from: date, to: Date()).day ?? 0
        )
    }
}
