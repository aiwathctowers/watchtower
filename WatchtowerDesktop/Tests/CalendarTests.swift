import Foundation
import GRDB
import Testing
@testable import WatchtowerDesktop

// MARK: - CalendarCalendarItem Model Tests

@Suite("CalendarCalendarItem Model")
struct CalendarCalendarItemModelTests {

    @Test("Init from row reads all fields")
    func initFromRow() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try db.execute(sql: """
                INSERT INTO calendar_calendars (id, name, is_primary, is_selected, color, synced_at)
                VALUES (?, ?, ?, ?, ?, ?)
                """, arguments: ["cal_1", "Work", 1, 1, "#4285f4", "2026-04-01T10:00:00Z"])
        }

        let cal = try #require(try dbQueue.read { db in
            try CalendarCalendarItem.fetchOne(
                db,
                sql: "SELECT * FROM calendar_calendars WHERE id = ?",
                arguments: ["cal_1"]
            )
        })

        #expect(cal.id == "cal_1")
        #expect(cal.name == "Work")
        #expect(cal.isPrimary == true)
        #expect(cal.isSelected == true)
        #expect(cal.color == "#4285f4")
        #expect(cal.syncedAt == "2026-04-01T10:00:00Z")
    }

    @Test("Defaults for optional fields")
    func defaults() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try db.execute(sql: """
                INSERT INTO calendar_calendars (id, name) VALUES (?, ?)
                """, arguments: ["cal_2", "Personal"])
        }

        let cal = try #require(try dbQueue.read { db in
            try CalendarCalendarItem.fetchOne(
                db,
                sql: "SELECT * FROM calendar_calendars WHERE id = ?",
                arguments: ["cal_2"]
            )
        })

        #expect(cal.isPrimary == false)
        #expect(cal.isSelected == true)
        #expect(cal.color.isEmpty)
    }
}

// MARK: - CalendarEvent Model Tests

@Suite("CalendarEvent Model")
struct CalendarEventModelTests {

    @Test("Init from row reads all fields")
    func initFromRow() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "evt_1",
                title: "Sprint Planning",
                description: "Weekly sprint planning session",
                startTime: "2026-04-02T09:00:00Z",
                endTime: "2026-04-02T10:00:00Z",
                location: "Room 42",
                organizerEmail: "alice@example.com",
                isRecurring: true,
                eventStatus: "confirmed",
                eventType: "default",
                htmlLink: "https://calendar.google.com/event/abc"
            )
        }

        let event = try #require(try dbQueue.read { db in
            try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = ?",
                arguments: ["evt_1"]
            )
        })

        #expect(event.id == "evt_1")
        #expect(event.title == "Sprint Planning")
        #expect(event.description == "Weekly sprint planning session")
        #expect(event.startTime == "2026-04-02T09:00:00Z")
        #expect(event.endTime == "2026-04-02T10:00:00Z")
        #expect(event.location == "Room 42")
        #expect(event.organizerEmail == "alice@example.com")
        #expect(event.isRecurring == true)
        #expect(event.isAllDay == false)
        #expect(event.eventStatus == "confirmed")
        #expect(event.eventType == "default")
        #expect(event.htmlLink == "https://calendar.google.com/event/abc")
        #expect(event.calendarID == "primary")
    }

    @Test("Computed dates and duration")
    func datesAndDuration() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                startTime: "2023-11-14T22:13:20Z",
                endTime: "2023-11-14T23:13:20Z"
            )
        }

        let evt = try #require(try dbQueue.read { db in
            try CalendarEvent.fetchOne(db, sql: "SELECT * FROM calendar_events LIMIT 1")
        })

        let expectedStart = try #require(ISO8601DateFormatter().date(from: "2023-11-14T22:13:20Z"))
        let expectedEnd = try #require(ISO8601DateFormatter().date(from: "2023-11-14T23:13:20Z"))
        #expect(evt.startDate == expectedStart)
        #expect(evt.endDate == expectedEnd)
        #expect(evt.duration == 3600)
    }

    @Test("formattedTimeRange for all-day event")
    func allDayTimeRange() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(db, isAllDay: true)
        }
        let evt = try #require(try dbQueue.read { db in
            try CalendarEvent.fetchOne(db, sql: "SELECT * FROM calendar_events LIMIT 1")
        })
        #expect(evt.formattedTimeRange == "All day")
        #expect(evt.isAllDay == true)
    }

    @Test("formattedTimeRange for normal event contains dash")
    func normalTimeRange() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                startTime: "2026-04-02T09:00:00Z",
                endTime: "2026-04-02T10:00:00Z"
            )
        }
        let evt = try #require(try dbQueue.read { db in
            try CalendarEvent.fetchOne(db, sql: "SELECT * FROM calendar_events LIMIT 1")
        })
        #expect(evt.formattedTimeRange.contains(" - "))
    }

    @Test("durationText formatting")
    func durationText() throws {
        let dbQueue = try TestDatabase.create()

        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "short",
                startTime: "2026-04-02T09:00:00Z",
                endTime: "2026-04-02T09:30:00Z"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "hour",
                startTime: "2026-04-02T09:00:00Z",
                endTime: "2026-04-02T10:00:00Z"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "long",
                startTime: "2026-04-02T09:00:00Z",
                endTime: "2026-04-02T10:30:00Z"
            )
        }

        try dbQueue.read { db in
            let short = try #require(try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = 'short'"
            ))
            #expect(short.durationText == "30m")

            let hourEvt = try #require(try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = 'hour'"
            ))
            #expect(hourEvt.durationText == "1h")

            let longEvt = try #require(try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = 'long'"
            ))
            #expect(longEvt.durationText == "1h 30m")
        }
    }

    @Test("parsedAttendees decodes JSON")
    func parsedAttendees() throws {
        let dbQueue = try TestDatabase.create()
        let json = """
            [{"email":"bob@test.com","display_name":"Bob","response_status":"accepted","slack_user_id":"U123"}]
            """
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(db, attendees: json)
        }
        let evt = try #require(try dbQueue.read { db in
            try CalendarEvent.fetchOne(db, sql: "SELECT * FROM calendar_events LIMIT 1")
        })

        #expect(evt.parsedAttendees.count == 1)
        #expect(evt.parsedAttendees[0].email == "bob@test.com")
        #expect(evt.parsedAttendees[0].displayName == "Bob")
        #expect(evt.parsedAttendees[0].slackUserID == "U123")
    }

    @Test("parsedAttendees returns empty for invalid JSON")
    func parsedAttendeesInvalid() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(db, attendees: "not json")
        }
        let evt = try #require(try dbQueue.read { db in
            try CalendarEvent.fetchOne(db, sql: "SELECT * FROM calendar_events LIMIT 1")
        })
        #expect(evt.parsedAttendees.isEmpty)
    }

    @Test("Response icon and color based on eventStatus")
    func responseDisplay() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "conf",
                eventStatus: "confirmed"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "tent",
                eventStatus: "tentative"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "canc",
                eventStatus: "cancelled"
            )
        }

        try dbQueue.read { db in
            let confirmed = try #require(try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = 'conf'"
            ))
            #expect(confirmed.responseIcon == "checkmark.circle.fill")
            #expect(confirmed.responseColor == "green")

            let tentative = try #require(try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = 'tent'"
            ))
            #expect(tentative.responseIcon == "questionmark.circle")
            #expect(tentative.responseColor == "orange")

            let cancelled = try #require(try CalendarEvent.fetchOne(
                db,
                sql: "SELECT * FROM calendar_events WHERE id = 'canc'"
            ))
            #expect(cancelled.responseIcon == "xmark.circle")
            #expect(cancelled.responseColor == "red")
        }
    }
}

// MARK: - CalendarQueries Tests

@Suite("CalendarQueries")
struct CalendarQueriesTests {

    private static let iso8601: ISO8601DateFormatter = {
        let fmt = ISO8601DateFormatter()
        fmt.formatOptions = [.withInternetDateTime]
        return fmt
    }()

    @Test("fetchTodayEvents returns only today's events")
    func fetchToday() throws {
        let dbQueue = try TestDatabase.create()
        let cal = Calendar.current
        let today = cal.startOfDay(for: Date())
        let todayNoon = today.addingTimeInterval(43200)
        let yesterdayNoon = todayNoon.addingTimeInterval(-86400)
        let tomorrowNoon = todayNoon.addingTimeInterval(86400)

        let fmt = Self.iso8601

        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "today",
                startTime: fmt.string(from: todayNoon),
                endTime: fmt.string(from: todayNoon.addingTimeInterval(3600))
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "yesterday",
                startTime: fmt.string(from: yesterdayNoon),
                endTime: fmt.string(from: yesterdayNoon.addingTimeInterval(3600))
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "tomorrow",
                startTime: fmt.string(from: tomorrowNoon),
                endTime: fmt.string(from: tomorrowNoon.addingTimeInterval(3600))
            )
        }

        let events = try dbQueue.read { db in
            try CalendarQueries.fetchTodayEvents(db)
        }

        #expect(events.count == 1)
        #expect(events[0].id == "today")
    }

    @Test("fetchEvents by date range")
    func fetchByRange() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "evtA",
                startTime: "2026-01-01T01:00:00Z",
                endTime: "2026-01-01T02:00:00Z"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "evtB",
                startTime: "2026-01-01T03:00:00Z",
                endTime: "2026-01-01T04:00:00Z"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "evtC",
                startTime: "2026-01-01T05:00:00Z",
                endTime: "2026-01-01T06:00:00Z"
            )
        }

        let from = try #require(Self.iso8601.date(from: "2026-01-01T02:30:00Z"))
        let to = try #require(Self.iso8601.date(from: "2026-01-01T04:30:00Z"))

        let events = try dbQueue.read { db in
            try CalendarQueries.fetchEvents(db, from: from, to: to)
        }

        // evtB (03:00-04:00) overlaps [02:30, 04:30]
        #expect(events.count == 1)
        #expect(events[0].id == "evtB")
    }

    @Test("fetchNextEvent returns soonest future event")
    func fetchNext() throws {
        let dbQueue = try TestDatabase.create()
        let now = Date()
        let fmt = Self.iso8601

        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "past",
                startTime: fmt.string(from: now.addingTimeInterval(-7200)),
                endTime: fmt.string(from: now.addingTimeInterval(-3600))
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "soon",
                startTime: fmt.string(from: now.addingTimeInterval(1800)),
                endTime: fmt.string(from: now.addingTimeInterval(5400))
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "later",
                startTime: fmt.string(from: now.addingTimeInterval(86400)),
                endTime: fmt.string(from: now.addingTimeInterval(90000))
            )
        }

        let next = try dbQueue.read { db in
            try CalendarQueries.fetchNextEvent(db)
        }

        #expect(next?.id == "soon")
    }

    @Test("fetchEvent by ID")
    func fetchByID() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(db, id: "target", title: "Important")
        }

        let evt = try dbQueue.read { db in
            try CalendarQueries.fetchEvent(db, id: "target")
        }

        #expect(evt?.title == "Important")
    }

    @Test("fetchEvent returns nil for missing ID")
    func fetchByIDMissing() throws {
        let dbQueue = try TestDatabase.create()
        let evt = try dbQueue.read { db in
            try CalendarQueries.fetchEvent(db, id: "nonexistent")
        }
        #expect(evt == nil)
    }

    @Test("eventCount")
    func eventCount() throws {
        let dbQueue = try TestDatabase.create()

        let empty = try dbQueue.read { db in try CalendarQueries.eventCount(db) }
        #expect(empty == 0)

        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(db, id: "one")
            try TestDatabase.insertCalendarEvent(db, id: "two")
            try TestDatabase.insertCalendarEvent(db, id: "three")
        }

        let count = try dbQueue.read { db in try CalendarQueries.eventCount(db) }
        #expect(count == 3)
    }

    @Test("Events ordered by start_time ASC")
    func orderByStart() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try TestDatabase.insertCalendarEvent(
                db,
                id: "late",
                startTime: "2026-04-02T15:00:00Z",
                endTime: "2026-04-02T16:00:00Z"
            )
            try TestDatabase.insertCalendarEvent(
                db,
                id: "early",
                startTime: "2026-04-02T09:00:00Z",
                endTime: "2026-04-02T10:00:00Z"
            )
        }

        let events = try dbQueue.read { db in
            try CalendarQueries.fetchEvents(
                db,
                from: try #require(Self.iso8601.date(from: "2026-04-02T00:00:00Z")),
                to: try #require(Self.iso8601.date(from: "2026-04-02T23:59:59Z"))
            )
        }

        #expect(events.count == 2)
        #expect(events[0].id == "early")
        #expect(events[1].id == "late")
    }

    @Test("fetchCalendars returns all calendars ordered")
    func fetchCalendars() throws {
        let dbQueue = try TestDatabase.create()
        try dbQueue.write { db in
            try db.execute(sql: """
                INSERT INTO calendar_calendars (id, name, is_primary, is_selected)
                VALUES (?, ?, ?, ?)
                """, arguments: ["secondary", "Personal", 0, 1])
            try db.execute(sql: """
                INSERT INTO calendar_calendars (id, name, is_primary, is_selected)
                VALUES (?, ?, ?, ?)
                """, arguments: ["primary", "Work", 1, 1])
        }

        let calendars = try dbQueue.read { db in
            try CalendarQueries.fetchCalendars(db)
        }

        #expect(calendars.count == 2)
        #expect(calendars[0].id == "primary") // primary first
        #expect(calendars[1].id == "secondary")
    }

    @Test("calendarCount")
    func calendarCount() throws {
        let dbQueue = try TestDatabase.create()

        let empty = try dbQueue.read { db in try CalendarQueries.calendarCount(db) }
        #expect(empty == 0)

        try dbQueue.write { db in
            try TestDatabase.ensureCalendar(db, id: "cal1")
            try TestDatabase.ensureCalendar(db, id: "cal2")
        }

        let count = try dbQueue.read { db in try CalendarQueries.calendarCount(db) }
        #expect(count == 2)
    }
}

// MARK: - MeetingPrepResult Tests

@Suite("MeetingPrepResult")
struct MeetingPrepResultTests {

    @Test("Decode MeetingPrepResult from JSON")
    func decodeJSON() throws {
        let json = """
            {
                "event_id": "abc123",
                "title": "Sprint Planning",
                "start_time": "2026-04-02T09:00:00Z",
                "talking_points": [
                    {"text": "Review velocity", "source_type": "track", "source_id": "42", "priority": "high"},
                    {"text": "Discuss blockers", "source_type": "digest", "source_id": "10", "priority": "medium"}
                ],
                "open_items": [
                    {"text": "Pending PR review", "type": "track", "id": "55", "person_name": "@alice", "person_id": "U123"}
                ],
                "people_notes": [
                    {"user_id": "U123", "name": "Alice", "communication_tip": "Prefers async", "recent_context": "Working on auth"}
                ],
                "suggested_prep": ["Review track #42", "Check digest #10"]
            }
            """
        let data = try #require(json.data(using: .utf8))
        let result = try JSONDecoder().decode(MeetingPrepResult.self, from: data)

        #expect(result.eventID == "abc123")
        #expect(result.title == "Sprint Planning")
        #expect(result.startTime == "2026-04-02T09:00:00Z")
        #expect(result.talkingPoints.count == 2)
        #expect(result.talkingPoints[0].text == "Review velocity")
        #expect(result.talkingPoints[0].sourceType == "track")
        #expect(result.talkingPoints[0].sourceID == "42")
        #expect(result.talkingPoints[0].priority == "high")
        #expect(result.openItems.count == 1)
        #expect(result.openItems[0].text == "Pending PR review")
        #expect(result.openItems[0].type == "track")
        #expect(result.openItems[0].itemID == "55")
        #expect(result.openItems[0].personName == "@alice")
        #expect(result.openItems[0].personID == "U123")
        #expect(result.peopleNotes.count == 1)
        #expect(result.peopleNotes[0].userID == "U123")
        #expect(result.peopleNotes[0].name == "Alice")
        #expect(result.peopleNotes[0].communicationTip == "Prefers async")
        #expect(result.peopleNotes[0].recentContext == "Working on auth")
        #expect(result.suggestedPrep.count == 2)
        #expect(result.suggestedPrep[0] == "Review track #42")
    }

    @Test("TalkingPoint identity uses text")
    func talkingPointID() throws {
        let point = TalkingPoint(
            text: "Test",
            sourceType: "track",
            sourceID: "1",
            priority: "low"
        )
        #expect(point.id == "Test")
    }

    @Test("OpenItem identity uses type-id")
    func openItemID() throws {
        let item = OpenItem(
            text: "Review",
            type: "inbox",
            itemID: "99",
            personName: "Bob",
            personID: "U456"
        )
        #expect(item.id == "inbox-99")
    }

    @Test("PersonNote identity uses userID")
    func personNoteID() throws {
        let note = PersonNote(
            userID: "U789",
            name: "Charlie",
            communicationTip: "Direct",
            recentContext: "Onboarding"
        )
        #expect(note.id == "U789")
    }
}

// MARK: - EventAttendee Tests

@Suite("EventAttendee")
struct EventAttendeeTests {

    @Test("Decode from JSON with snake_case keys")
    func decode() throws {
        let json = """
            {"email":"bob@test.com","display_name":"Bob Smith","response_status":"tentative","slack_user_id":"U456"}
            """
        let data = try #require(json.data(using: .utf8))
        let attendee = try JSONDecoder().decode(EventAttendee.self, from: data)

        #expect(attendee.email == "bob@test.com")
        #expect(attendee.displayName == "Bob Smith")
        #expect(attendee.responseStatus == "tentative")
        #expect(attendee.slackUserID == "U456")
        #expect(attendee.id == "bob@test.com")
    }
}
