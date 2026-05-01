import Foundation
import GRDB
import Testing
@testable import WatchtowerDesktop

@Suite("SearchViewModel Helpers")
@MainActor
struct SearchViewModelHelperTests {

    private func makeManager() throws -> DatabaseManager {
        let (manager, _) = try TestDatabase.createDatabaseManager()
        return manager
    }

    @Test("workspaceTeamID populated from DB")
    func workspaceTeamIDLoaded() throws {
        let manager = try makeManager()
        try manager.dbPool.write { db in
            try db.execute(sql: "INSERT OR REPLACE INTO workspace (id, name) VALUES ('T1', 'test')")
        }

        let vm = SearchViewModel(dbManager: manager)
        #expect(vm.workspaceTeamID == "T1")
    }

    @Test("slackChannelURL returns nil without team id")
    func slackChannelURLNoTeam() throws {
        let manager = try makeManager()
        let vm = SearchViewModel(dbManager: manager)
        #expect(vm.slackChannelURL(channelID: "C1") == nil)
    }

    @Test("slackChannelURL constructs slack:// URL when team id present")
    func slackChannelURLWithTeam() throws {
        let manager = try makeManager()
        try manager.dbPool.write { db in
            try db.execute(sql: "INSERT OR REPLACE INTO workspace (id, name) VALUES ('T42', 'test')")
        }
        let vm = SearchViewModel(dbManager: manager)
        let url = vm.slackChannelURL(channelID: "C123")
        #expect(url?.absoluteString == "slack://channel?team=T42&id=C123")
    }

    @Test("Empty query clears results immediately")
    func emptyQueryClearsResults() async throws {
        let manager = try makeManager()
        let vm = SearchViewModel(dbManager: manager)
        vm.query = "   "
        vm.search()
        #expect(vm.results.isEmpty)
    }

    @Test("Whitespace-only query results in empty result list (no debounce)")
    func whitespaceOnlyQuery() async throws {
        let manager = try makeManager()
        let vm = SearchViewModel(dbManager: manager)
        vm.query = "\t\n  "
        vm.search()
        #expect(vm.results.isEmpty)
    }
}
