import Foundation
import GRDB

@MainActor
@Observable
final class PeopleViewModel {
    var cards: [PeopleCard] = []
    var cardSummary: PeopleCardSummary?
    var searchText = ""
    var isLoading = false
    var errorMessage: String?
    var selectedWindow: Int = 0  // index into availableWindows
    var availableWindows: [(from: Double, to: Double)] = []

    private(set) var userNameCache: [String: String] = [:]
    private(set) var starredPeopleIDs: Set<String> = []
    private(set) var currentUserID: String?
    private(set) var currentProfile: UserProfile?
    private(set) var interactions: [UserInteraction] = []
    private let dbManager: DatabaseManager
    private var observationTask: Task<Void, Never>?

    init(dbManager: DatabaseManager) {
        self.dbManager = dbManager
    }

    /// Start observing the people_cards table for live updates.
    func startObserving() {
        guard observationTask == nil else { return }
        load()
        let dbPool = dbManager.dbPool
        observationTask = Task { [weak self] in
            let observation = ValueObservation.tracking { db in
                try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM people_cards") ?? 0
            }
            do {
                for try await _ in observation.values(in: dbPool).dropFirst() {
                    guard !Task.isCancelled else { break }
                    self?.load()
                }
            } catch {}
        }
    }

    /// The current user's card from the loaded cards list.
    var myCard: PeopleCard? {
        guard let uid = currentUserID else { return nil }
        return cards.first { $0.userID == uid }
    }

    func load() {
        isLoading = true
        do {
            let result = try dbManager.dbPool.read { db in
                let windows = try PeopleCardQueries.fetchAvailableWindows(db)
                let users = try UserQueries.fetchAll(db, activeOnly: false)

                var nameMap: [String: String] = [:]
                for user in users {
                    let name = user.displayName.isEmpty ? user.name : user.displayName
                    nameMap[user.id] = name
                }

                let cards: [PeopleCard]
                let cs: PeopleCardSummary?
                if let window = windows.first {
                    cards = try PeopleCardQueries.fetchForWindow(
                        db, from: window.from, to: window.to
                    )
                    cs = try PeopleCardQueries.fetchSummary(
                        db, from: window.from, to: window.to
                    )
                } else {
                    cards = []
                    cs = nil
                }
                let profile = try ProfileQueries.fetchCurrentProfile(db)
                let starred = Set(profile?.decodedStarredPeople ?? [])
                let uid = profile?.slackUserID

                // Load interactions for social graph
                var ints: [UserInteraction] = []
                if let uid, let window = windows.first {
                    ints = try InteractionQueries.fetchForUser(
                        db, userID: uid, periodFrom: window.from, periodTo: window.to
                    )
                }

                return (cards, windows, nameMap, cs, starred, uid, profile, ints)
            }
            cards = result.0
            availableWindows = result.1
            userNameCache = result.2
            cardSummary = result.3
            starredPeopleIDs = result.4
            currentUserID = result.5
            currentProfile = result.6
            interactions = result.7
            errorMessage = nil
        } catch {
            cards = []
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
                let cards = try PeopleCardQueries.fetchForWindow(
                    db, from: window.from, to: window.to
                )
                let cs = try PeopleCardQueries.fetchSummary(
                    db, from: window.from, to: window.to
                )
                var ints: [UserInteraction] = []
                if let uid = currentUserID {
                    ints = try InteractionQueries.fetchForUser(
                        db, userID: uid, periodFrom: window.from, periodTo: window.to
                    )
                }
                return (cards, cs, ints)
            }
            cards = result.0
            cardSummary = result.1
            interactions = result.2
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func userName(for userID: String) -> String {
        userNameCache[userID] ?? userID
    }

    func cardHistory(userID: String) -> [PeopleCard] {
        do {
            return try dbManager.dbPool.read { db in
                try PeopleCardQueries.fetchByUser(db, userID: userID)
            }
        } catch {
            return []
        }
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

    var redFlagCount: Int {
        cards.filter { $0.hasRedFlags }.count
    }

    // MARK: - Starred People Management

    func isPersonStarred(_ userID: String) -> Bool {
        starredPeopleIDs.contains(userID)
    }

    /// Look up the card for a specific user in the current window.
    func cardFor(userID: String) -> PeopleCard? {
        cards.first { $0.userID == userID }
    }

    /// Update profile connections (reports, peers, manager).
    func updateConnections(reports: [String], peers: [String], manager: String) {
        guard let userID = currentUserID else { return }
        do {
            let encoder = JSONEncoder()
            let reportsJSON = String(data: try encoder.encode(reports), encoding: .utf8) ?? "[]"
            let peersJSON = String(data: try encoder.encode(peers), encoding: .utf8) ?? "[]"
            try dbManager.dbPool.write { db in
                try ProfileQueries.updateField(db, slackUserID: userID, field: "reports", value: reportsJSON)
                try ProfileQueries.updateField(db, slackUserID: userID, field: "peers", value: peersJSON)
                try ProfileQueries.updateField(db, slackUserID: userID, field: "manager", value: manager)
            }
            // Reload profile
            currentProfile = try dbManager.dbPool.read { db in
                try ProfileQueries.fetchCurrentProfile(db)
            }
        } catch {
            errorMessage = "Failed to update connections: \(error.localizedDescription)"
        }
    }

    func toggleStarredPerson(_ personUserID: String) {
        guard let userID = currentUserID else { return }
        let wasStarred = starredPeopleIDs.contains(personUserID)
        // Optimistic update
        if wasStarred {
            starredPeopleIDs.remove(personUserID)
        } else {
            starredPeopleIDs.insert(personUserID)
        }
        do {
            if wasStarred {
                try dbManager.removeStarredPerson(personUserID, for: userID)
            } else {
                try dbManager.addStarredPerson(personUserID, for: userID)
            }
        } catch {
            // Revert on failure
            if wasStarred {
                starredPeopleIDs.insert(personUserID)
            } else {
                starredPeopleIDs.remove(personUserID)
            }
            errorMessage = "Failed to update starred person: \(error.localizedDescription)"
        }
    }
}
