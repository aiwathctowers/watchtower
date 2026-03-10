import Foundation

enum TimeFormatting {
    private static let isoFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static let isoFormatterNoFrac: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    /// Parse ISO8601 string to Date
    static func parseISO(_ str: String) -> Date? {
        isoFormatter.date(from: str) ?? isoFormatterNoFrac.date(from: str)
    }

    /// Relative time from ISO8601 string: "just now", "5m ago", "2h ago", etc.
    static func relativeTime(from isoString: String) -> String {
        guard let date = parseISO(isoString) else { return isoString }
        return relativeTime(from: date)
    }

    /// Relative time from unix timestamp
    static func relativeTimeFromUnix(_ ts: Double) -> String {
        relativeTime(from: Date(timeIntervalSince1970: ts))
    }

    /// Relative time from Date
    static func relativeTime(from date: Date) -> String {
        let now = Date()
        let interval = now.timeIntervalSince(date)

        if interval < 60 { return "just now" }
        if interval < 3600 { return "\(Int(interval / 60))m ago" }
        if interval < 86400 { return "\(Int(interval / 3600))h ago" }
        if interval < 172800 { return "yesterday" }
        if interval < 604800 { return "\(Int(interval / 86400))d ago" }

        return shortDateFormatter.string(from: date)
    }

    // M4: static DateFormatter to avoid per-call allocation
    private static let shortDateFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "MMM d"
        return f
    }()

    private static let mediumDateTimeFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .short
        return f
    }()

    /// Format unix timestamp to display string
    static func formatUnixTimestamp(_ ts: Double) -> String {
        let date = Date(timeIntervalSince1970: ts)
        return mediumDateTimeFormatter.string(from: date)
    }

    private static let shortTimeFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm"
        return f
    }()

    /// Short time only (e.g. "14:32") for grouped message hover
    static func shortTime(_ ts: Double) -> String {
        shortTimeFormatter.string(from: Date(timeIntervalSince1970: ts))
    }

    /// Short date from unix timestamp (e.g. "Mar 8")
    static func shortDate(fromUnix ts: Double) -> String {
        shortDateFormatter.string(from: Date(timeIntervalSince1970: ts))
    }

    private static let shortDateTimeFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "MMM d, HH:mm"
        return f
    }()

    /// Short date + time from unix timestamp (e.g. "Mar 8, 14:32")
    static func shortDateTime(fromUnix ts: Double) -> String {
        shortDateTimeFormatter.string(from: Date(timeIntervalSince1970: ts))
    }

    /// Short date + time from Date (e.g. "Mar 8, 14:32")
    static func shortDateTime(from date: Date) -> String {
        shortDateTimeFormatter.string(from: date)
    }
}
