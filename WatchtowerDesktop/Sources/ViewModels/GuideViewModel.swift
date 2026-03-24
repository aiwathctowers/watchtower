import Foundation
import GRDB

// NOTE: This ViewModel is no longer actively used — the Guide tab has been merged into People.
// Kept for backwards compatibility with GuideListView.
@MainActor
@Observable
final class GuideViewModel {
    var guides: [CommunicationGuide] = []
    var guideSummary: GuideSummary?
    var searchText = ""
    var isLoading = false
    var errorMessage: String?
    var selectedWindow: Int = 0  // index into availableWindows
    var availableWindows: [(from: Double, to: Double)] = []

    private(set) var userNameCache: [String: String] = [:]
    private(set) var currentUserID: String?
    private let dbManager: DatabaseManager

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db in
                let windows = try GuideQueries.fetchAvailableWindows(db)
                let users = try UserQueries.fetchAll(db, activeOnly: false)

                var nameMap: [String: String] = [:]
                for user in users {
                    let name = user.displayName.isEmpty ? user.name : user.displayName
                    nameMap[user.id] = name
                }

                let guides: [CommunicationGuide]
                let gs: GuideSummary?
                if let window = windows.first {
                    guides = try GuideQueries.fetchForWindow(
                        db, periodFrom: window.from, periodTo: window.to
                    )
                    gs = try GuideQueries.fetchGuideSummary(
                        db, periodFrom: window.from, periodTo: window.to
                    )
                } else {
                    guides = []
                    gs = nil
                }
                let profile = try ProfileQueries.fetchCurrentProfile(db)
                let uid = profile?.slackUserID

                return (guides, windows, nameMap, gs, uid)
            }
            guides = result.0
            availableWindows = result.1
            userNameCache = result.2
            guideSummary = result.3
            currentUserID = result.4
            errorMessage = nil
        } catch {
            guides = []
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func loadWindow(at index: Int) {
        guard index >= 0, index < availableWindows.count else { return }
        selectedWindow = index
        let window = availableWindows[index]
        do {
            let result = try dbManager.dbPool.read { db in
                let guides = try GuideQueries.fetchForWindow(
                    db, periodFrom: window.from, periodTo: window.to
                )
                let gs = try GuideQueries.fetchGuideSummary(
                    db, periodFrom: window.from, periodTo: window.to
                )
                return (guides, gs)
            }
            guides = result.0
            guideSummary = result.1
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func userName(for userID: String) -> String {
        userNameCache[userID] ?? userID
    }

    var currentWindowLabel: String {
        guard !availableWindows.isEmpty, selectedWindow < availableWindows.count else {
            return "No data"
        }
        let w = availableWindows[selectedWindow]
        let from = Date(timeIntervalSince1970: w.from)
        let to = Date(timeIntervalSince1970: w.to)
        let fmt = DateFormatter()
        fmt.dateFormat = "MMM d"
        return "\(fmt.string(from: from)) – \(fmt.string(from: to))"
    }
}
