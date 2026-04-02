import Foundation
import GRDB

// MARK: - Attendee

struct EventAttendee: Codable, Identifiable, Equatable {
    var id: String { email }
    let email: String
    let displayName: String
    let responseStatus: String
    let slackUserID: String

    enum CodingKeys: String, CodingKey {
        case email
        case displayName = "display_name"
        case responseStatus = "response_status"
        case slackUserID = "slack_user_id"
    }
}

// MARK: - CalendarCalendarItem

struct CalendarCalendarItem: FetchableRecord, Identifiable, Equatable {
    let id: String
    let name: String
    let isPrimary: Bool
    let isSelected: Bool
    let color: String
    let syncedAt: String

    init(row: Row) {
        id = row["id"]
        name = row["name"] ?? ""
        isPrimary = (row["is_primary"] as Int? ?? 0) != 0
        isSelected = (row["is_selected"] as Int? ?? 1) != 0
        color = row["color"] ?? ""
        syncedAt = row["synced_at"] ?? ""
    }
}

// MARK: - CalendarEvent

struct CalendarEvent: FetchableRecord, Identifiable, Equatable {
    let id: String
    let calendarID: String
    let title: String
    let description: String
    let location: String
    let startTime: String       // ISO8601
    let endTime: String         // ISO8601
    let organizerEmail: String
    let attendees: String       // JSON array
    let isRecurring: Bool
    let isAllDay: Bool
    let eventStatus: String
    let eventType: String
    let htmlLink: String
    let rawJSON: String
    let syncedAt: String
    let updatedAt: String

    init(row: Row) {
        id = row["id"]
        calendarID = row["calendar_id"] ?? ""
        title = row["title"] ?? ""
        description = row["description"] ?? ""
        location = row["location"] ?? ""
        startTime = row["start_time"] ?? ""
        endTime = row["end_time"] ?? ""
        organizerEmail = row["organizer_email"] ?? ""
        attendees = row["attendees"] ?? "[]"
        isRecurring = (row["is_recurring"] as Int? ?? 0) != 0
        isAllDay = (row["is_all_day"] as Int? ?? 0) != 0
        eventStatus = row["event_status"] ?? "confirmed"
        eventType = row["event_type"] ?? ""
        htmlLink = row["html_link"] ?? ""
        rawJSON = row["raw_json"] ?? "{}"
        syncedAt = row["synced_at"] ?? ""
        updatedAt = row["updated_at"] ?? ""
    }

    // MARK: - ISO8601 Parsing

    private static let iso8601Formatter: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    // MARK: - Computed Dates

    var startDate: Date {
        Self.iso8601Formatter.date(from: startTime) ?? Date.distantPast
    }

    var endDate: Date {
        Self.iso8601Formatter.date(from: endTime) ?? Date.distantPast
    }

    var duration: TimeInterval {
        endDate.timeIntervalSince(startDate)
    }

    // MARK: - Attendees

    var parsedAttendees: [EventAttendee] {
        guard let data = attendees.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([EventAttendee].self, from: data)) ?? []
    }

    // MARK: - Status

    var isHappeningNow: Bool {
        let now = Date()
        return now >= startDate && now < endDate
    }

    var isUpcoming: Bool {
        let now = Date()
        return startDate > now && startDate <= now.addingTimeInterval(3600)
    }

    // MARK: - Display

    var formattedTimeRange: String {
        if isAllDay { return "All day" }
        let fmt = DateFormatter()
        fmt.dateFormat = "HH:mm"
        return "\(fmt.string(from: startDate)) - \(fmt.string(from: endDate))"
    }

    var durationText: String {
        let minutes = Int(duration / 60)
        if minutes < 60 { return "\(minutes)m" }
        let hours = minutes / 60
        let rem = minutes % 60
        if rem == 0 { return "\(hours)h" }
        return "\(hours)h \(rem)m"
    }

    var responseIcon: String {
        switch eventStatus {
        case "confirmed": return "checkmark.circle.fill"
        case "tentative": return "questionmark.circle"
        case "cancelled": return "xmark.circle"
        default: return "circle"
        }
    }

    var responseColor: String {
        switch eventStatus {
        case "confirmed": return "green"
        case "tentative": return "orange"
        case "cancelled": return "red"
        default: return "secondary"
        }
    }
}
