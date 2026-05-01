import SwiftUI

enum SidebarDestination: String, CaseIterable, Identifiable {
    case chat
    case briefings
    case dayPlan
    case inbox
    case calendar
    case targets
    case tracks
    case digests
    case people
    case workload
    case blockers
    case projectMap
    case releases
    case statistics
    case search
    case boards
    case usage
    case training

    var id: String { rawValue }

    var title: String {
        switch self {
        case .chat: "AI Chat"
        case .briefings: "Briefings"
        case .dayPlan: "Day Plan"
        case .inbox: "Inbox"
        case .calendar: "Calendar"
        case .targets: "Targets"
        case .tracks: "Tracks"
        case .digests: "Digests"
        case .people: "People"
        case .workload: "Workload"
        case .blockers: "Blockers"
        case .projectMap: "Project Map"
        case .releases: "Releases"
        case .statistics: "Statistics"
        case .search: "Search"
        case .boards: "Boards"
        case .usage: "Usage"
        case .training: "Training"
        }
    }

    var icon: String {
        switch self {
        case .chat: "bubble.left.and.bubble.right"
        case .briefings: "sun.max"
        case .dayPlan: "calendar.day.timeline.left"
        case .inbox: "tray"
        case .calendar: "calendar"
        case .targets: "scope"
        case .tracks: "binoculars"
        case .digests: "doc.text.magnifyingglass"
        case .people: "person.2"
        case .workload: "gauge.with.dots.needle.33percent"
        case .blockers: "exclamationmark.triangle"
        case .projectMap: "map"
        case .releases: "shippingbox"
        case .statistics: "chart.bar.xaxis"
        case .search: "magnifyingglass"
        case .boards: "rectangle.on.rectangle.angled"
        case .usage: "chart.bar"
        case .training: "brain.head.profile"
        }
    }

    /// Main navigation items (shown above the separator).
    static var mainItems: [Self] {
        [.chat, .briefings, .dayPlan, .inbox, .calendar, .targets, .tracks, .digests, .people, .workload, .blockers, .projectMap, .releases, .statistics, .search]
    }

    /// Tool items (shown below the separator).
    static var toolItems: [Self] {
        [.boards, .usage, .training]
    }
}
