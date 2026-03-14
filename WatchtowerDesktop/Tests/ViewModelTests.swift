import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - DashboardViewModel

final class DashboardViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testLoadWorkspaceAndStats() async throws {
        try await dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, name: "Acme Corp", domain: "acme")
            try TestDatabase.insertChannel(db, id: "C001")
            try TestDatabase.insertChannel(db, id: "C002")
            try TestDatabase.insertUser(db, id: "U001")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: "1700000001.000100")
            try TestDatabase.insertDigest(db)
        }

        let vm = DashboardViewModel(dbManager: dbManager)
        await vm.load()

        XCTAssertEqual(vm.workspace?.name, "Acme Corp")
        XCTAssertEqual(vm.stats.channelCount, 2)
        XCTAssertEqual(vm.stats.userCount, 1)
        XCTAssertEqual(vm.stats.messageCount, 1)
        XCTAssertEqual(vm.stats.digestCount, 1)
        XCTAssertFalse(vm.isLoading)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testLoadEmptyDB() async {
        let vm = DashboardViewModel(dbManager: dbManager)
        await vm.load()

        XCTAssertNil(vm.workspace)
        XCTAssertEqual(vm.stats.channelCount, 0)
        XCTAssertTrue(vm.recentActivity.isEmpty)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testLoadRecentActivity() async throws {
        let recentTS = String(format: "%.6f", Date().timeIntervalSince1970 - 3600)
        try await dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertUser(db, id: "U001", displayName: "Alice")
            try TestDatabase.insertWatchItem(db, entityType: "channel", entityID: "C001")
            try TestDatabase.insertMessage(db, channelID: "C001", ts: recentTS, userID: "U001", text: "Recent msg")
        }

        let vm = DashboardViewModel(dbManager: dbManager)
        await vm.load()

        XCTAssertEqual(vm.recentActivity.count, 1)
        XCTAssertEqual(vm.recentActivity.first?.text, "Recent msg")
    }
}

// MARK: - DigestViewModel

final class DigestViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testLoadDigests() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "acme")
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertDigest(db, channelID: "C001", summary: "Daily standup recap")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.digests.count, 1)
        XCTAssertEqual(vm.digests[0].summary, "Daily standup recap")
        XCTAssertEqual(vm.workspaceDomain, "acme")
        XCTAssertFalse(vm.isLoading)
    }

    @MainActor
    func testLoadWithTypeFilter() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 100, periodTo: 200, type: "channel")
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 100, periodTo: 200, type: "daily")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.selectedType = "daily"
        vm.load()

        XCTAssertEqual(vm.digests.count, 1)
        XCTAssertEqual(vm.digests[0].type, "daily")
    }

    @MainActor
    func testDecisionEntries() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertDigest(db, channelID: "C001",
                                          decisions: #"[{"text":"Use Go","by":"Alice","importance":"high"},{"text":"Deploy Friday"}]"#)
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.decisionEntries.count, 2)
        XCTAssertEqual(vm.decisionEntries[0].decision.text, "Use Go")
        XCTAssertEqual(vm.decisionEntries[0].channelName, "general")
        XCTAssertEqual(vm.decisionEntries[0].digestType, "channel")
    }

    @MainActor
    func testChannelName() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertDigest(db, channelID: "C001")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.channelName(for: vm.digests[0]), "general")
    }

    @MainActor
    func testChannelNameForDM() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUser(db, id: "U001", displayName: "Alice")
            try TestDatabase.insertChannel(db, id: "D001", name: "dm-alice", type: "dm", dmUserID: "U001")
            try TestDatabase.insertDigest(db, channelID: "D001")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.channelName(for: vm.digests[0]), "DM: Alice")
    }

    @MainActor
    func testChannelNameNilForCrossChannel() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertDigest(db, channelID: "", type: "daily")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.channelName(for: vm.digests[0]))
    }

    @MainActor
    func testSlackChannelURL() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "acme")
            try TestDatabase.insertDigest(db, channelID: "C001")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.slackChannelURL(channelID: "C001")?.absoluteString,
                       "https://acme.slack.com/archives/C001")
    }

    @MainActor
    func testSlackChannelURLNilWithoutDomain() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "")
            try TestDatabase.insertDigest(db)
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.slackChannelURL(channelID: "C001"))
    }

    @MainActor
    func testSlackMessageURL() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "acme")
            try TestDatabase.insertDigest(db)
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        let url = vm.slackMessageURL(channelID: "C001", messageTS: "1740577800.000100")
        XCTAssertEqual(url?.absoluteString, "https://acme.slack.com/archives/C001/p1740577800000100")
    }

    @MainActor
    func testContributingChannels() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertChannel(db, id: "C002", name: "engineering")
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400, type: "channel", summary: "ch1")
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 1700000000, periodTo: 1700086400, type: "channel", summary: "ch2")
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700000000, periodTo: 1700086400, type: "daily", summary: "daily")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        let dailyDigest = vm.digests.first { $0.type == "daily" }!
        let contributing = vm.contributingChannels(for: dailyDigest)
        XCTAssertEqual(contributing.count, 2)
        XCTAssertTrue(contributing.contains { $0.name == "general" })
        XCTAssertTrue(contributing.contains { $0.name == "engineering" })
    }

    @MainActor
    func testContributingChannelsEmptyForChannelDigest() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", type: "channel")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.contributingChannels(for: vm.digests[0]).isEmpty)
    }

    @MainActor
    func testDigestByID() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertDigest(db, summary: "Target digest")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        XCTAssertEqual(vm.digestByID(1)?.summary, "Target digest")
    }

    @MainActor
    func testLoadEmptyDB() {
        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.digests.isEmpty)
        XCTAssertTrue(vm.decisionEntries.isEmpty)
        XCTAssertNil(vm.errorMessage)
    }
}

// MARK: - PeopleViewModel

final class PeopleViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testLoad() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUser(db, id: "U001", name: "alice", displayName: "Alice")
            try TestDatabase.insertUser(db, id: "U002", name: "bob", displayName: "Bob")
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200, messageCount: 30)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.errorMessage, "load() error: \(vm.errorMessage ?? "")")
        XCTAssertFalse(vm.availableWindows.isEmpty, "no windows found")
        XCTAssertEqual(vm.analyses.count, 2)
        XCTAssertEqual(vm.analyses[0].userID, "U001")
        XCTAssertEqual(vm.availableWindows.count, 1)
        XCTAssertEqual(vm.userNameCache["U001"], "Alice")
        XCTAssertEqual(vm.userNameCache["U002"], "Bob")
        XCTAssertFalse(vm.isLoading)
    }

    @MainActor
    func testLoadEmptyDB() {
        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.analyses.isEmpty)
        XCTAssertTrue(vm.availableWindows.isEmpty)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testLoadWindow() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 200, periodTo: 300, messageCount: 30)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.analyses.count, 1)
        XCTAssertEqual(vm.analyses[0].periodFrom, 200)

        vm.loadWindow(at: 1)
        XCTAssertEqual(vm.selectedWindow, 1)
        XCTAssertEqual(vm.analyses.count, 1)
        XCTAssertEqual(vm.analyses[0].periodFrom, 100)
    }

    @MainActor
    func testLoadWindowOutOfBounds() {
        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        vm.loadWindow(at: 99)
        XCTAssertEqual(vm.selectedWindow, 0)
    }

    @MainActor
    func testUserName() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUser(db, id: "U001", displayName: "Alice")
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.userName(for: "U001"), "Alice")
        XCTAssertEqual(vm.userName(for: "U999"), "U999")
    }

    @MainActor
    func testCurrentWindowLabel() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 1700000000, periodTo: 1700604800)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        let label = vm.currentWindowLabel
        XCTAssertFalse(label.isEmpty)
        XCTAssertNotEqual(label, "No data")
        XCTAssertTrue(label.contains("–"))
    }

    @MainActor
    func testCurrentWindowLabelNoData() {
        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()
        XCTAssertEqual(vm.currentWindowLabel, "No data")
    }

    @MainActor
    func testRedFlagCount() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200,
                                                 redFlags: #"["Issue"]"#)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200,
                                                 redFlags: "[]")
            try TestDatabase.insertUserAnalysis(db, userID: "U003", periodFrom: 100, periodTo: 200,
                                                 redFlags: #"["A","B"]"#)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.redFlagCount, 2)
    }

    @MainActor
    func testUserHistory() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 200, periodTo: 300, messageCount: 30)
            try TestDatabase.insertUserAnalysis(db, userID: "U002", periodFrom: 100, periodTo: 200, messageCount: 10)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        let history = vm.userHistory(userID: "U001")

        XCTAssertEqual(history.count, 2)
        XCTAssertTrue(history.allSatisfy { $0.userID == "U001" })
    }

    @MainActor
    func testUserNameCachePrefersDisplayName() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUser(db, id: "U001", name: "alice", displayName: "Alice Wonder")
            try TestDatabase.insertUser(db, id: "U002", name: "bob", displayName: "")
            try TestDatabase.insertUserAnalysis(db, userID: "U001", periodFrom: 100, periodTo: 200)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.userNameCache["U001"], "Alice Wonder")
        XCTAssertEqual(vm.userNameCache["U002"], "bob")
    }
}

// MARK: - ChatViewModel

final class ChatViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testNewChat() {
        let vm = ChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.messages = [
            ChatMessage(id: UUID(), role: .user, text: "Hi", timestamp: Date(), isStreaming: false),
            ChatMessage(id: UUID(), role: .assistant, text: "Hello!", timestamp: Date(), isStreaming: false),
        ]
        vm.newChat()

        XCTAssertTrue(vm.messages.isEmpty)
        XCTAssertFalse(vm.isStreaming)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testCancelStream() {
        let vm = ChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.isStreaming = true
        vm.messages = [
            ChatMessage(id: UUID(), role: .assistant, text: "Partial...", timestamp: Date(), isStreaming: true),
        ]

        vm.cancelStream()

        XCTAssertFalse(vm.isStreaming)
        XCTAssertFalse(vm.messages.last!.isStreaming)
    }

    @MainActor
    func testSendCreatesMessages() async throws {
        let mock = MockClaudeService(events: [.text("Hello "), .text("world"), .done])
        let vm = ChatViewModel(claudeService: mock, dbManager: dbManager)

        vm.inputText = "Hi there"
        vm.send()

        try await Task.sleep(for: .milliseconds(300))

        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertEqual(vm.messages[0].role, .user)
        XCTAssertEqual(vm.messages[0].text, "Hi there")
        XCTAssertEqual(vm.messages[1].role, .assistant)
        XCTAssertEqual(vm.messages[1].text, "Hello world")
        XCTAssertFalse(vm.isStreaming)
    }

    @MainActor
    func testSendEmptyDoesNothing() {
        let vm = ChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.inputText = "   "
        vm.send()

        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testSendWhileStreamingDoesNothing() {
        let vm = ChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.isStreaming = true
        vm.inputText = "Hello"
        vm.send()

        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testSendClearsInputText() {
        let vm = ChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.inputText = "Hello"
        vm.send()

        XCTAssertEqual(vm.inputText, "")
    }

    @MainActor
    func testSendWithError() async throws {
        let mock = MockClaudeService(error: ClaudeError.notFound)
        let vm = ChatViewModel(claudeService: mock, dbManager: dbManager)

        vm.inputText = "Hello"
        vm.send()
        try await Task.sleep(for: .milliseconds(300))

        XCTAssertNotNil(vm.errorMessage)
        XCTAssertFalse(vm.isStreaming)
    }

    func testBuildSystemPrompt() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, name: "Acme Corp", domain: "acme")
        }

        let prompt = ChatViewModel.buildSystemPrompt(dbPool: dbManager.dbPool)

        XCTAssertTrue(prompt.contains("Acme Corp"))
        XCTAssertTrue(prompt.contains("acme.slack.com"))
        XCTAssertTrue(prompt.contains("DATABASE SCHEMA"))
        XCTAssertTrue(prompt.contains("CREATE TABLE"))
    }

    func testBuildSystemPromptEmptyDB() {
        let prompt = ChatViewModel.buildSystemPrompt(dbPool: dbManager.dbPool)

        XCTAssertTrue(prompt.contains("unknown"))
        XCTAssertTrue(prompt.contains("Watchtower"))
    }

    func testFetchSchema() throws {
        let schema = try dbManager.dbPool.read { db in
            try ChatViewModel.fetchSchema(db)
        }
        XCTAssertTrue(schema.contains("CREATE TABLE"))
        XCTAssertTrue(schema.contains("workspace"))
        XCTAssertTrue(schema.contains("messages"))
    }
}

// MARK: - SearchViewModel

final class SearchViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testEmptyQueryClearsResults() {
        let vm = SearchViewModel(dbManager: dbManager)
        vm.query = "   "
        vm.search()

        XCTAssertTrue(vm.results.isEmpty)
    }

    @MainActor
    func testInitialState() {
        let vm = SearchViewModel(dbManager: dbManager)

        XCTAssertEqual(vm.query, "")
        XCTAssertTrue(vm.results.isEmpty)
        XCTAssertFalse(vm.isSearching)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testSearchSetsIsSearching() async throws {
        let vm = SearchViewModel(dbManager: dbManager)
        vm.query = "hello"
        vm.search()

        // After debounce completes (300ms), isSearching should be set then cleared
        try await Task.sleep(for: .milliseconds(500))

        // After completion, isSearching should be false
        XCTAssertFalse(vm.isSearching)
    }

    @MainActor
    func testSearchCancelsOnNewQuery() async throws {
        let vm = SearchViewModel(dbManager: dbManager)
        vm.query = "first"
        vm.search()

        // Immediately issue new search, cancelling previous
        vm.query = "  "
        vm.search()

        XCTAssertTrue(vm.results.isEmpty)
    }

    @MainActor
    func testSearchCancelsPreviousTask() async throws {
        let vm = SearchViewModel(dbManager: dbManager)
        vm.query = "alpha"
        vm.search()

        // Issue second query before debounce completes
        vm.query = "beta"
        vm.search()

        // Wait for debounce
        try await Task.sleep(for: .milliseconds(500))

        XCTAssertFalse(vm.isSearching)
    }
}

// MARK: - TracksViewModel

final class TracksViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testLoadTracks() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "acme")
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Fix the bug", status: "inbox", priority: "high")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Write docs", status: "inbox", priority: "low")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.errorMessage, "load() error: \(vm.errorMessage ?? "")")
        XCTAssertEqual(vm.items.count, 2)
        // High priority first
        XCTAssertEqual(vm.items[0].text, "Fix the bug")
        XCTAssertEqual(vm.items[1].text, "Write docs")
        XCTAssertEqual(vm.openCount, 2)
        XCTAssertEqual(vm.workspaceDomain, "acme")
        XCTAssertFalse(vm.isLoading)
    }

    @MainActor
    func testLoadEmptyDB() {
        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.items.isEmpty)
        XCTAssertEqual(vm.openCount, 0)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testLoadWithStatusFilter() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Open task", status: "inbox")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Done task", status: "done",
                                               priority: "medium", periodFrom: 1700100000, periodTo: 1700200000)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = "done"
        vm.load()

        XCTAssertEqual(vm.items.count, 1)
        XCTAssertEqual(vm.items[0].text, "Done task")
    }

    @MainActor
    func testLoadWithPriorityFilter() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "High", priority: "high")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Low", priority: "low",
                                               periodFrom: 1700100000, periodTo: 1700200000)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.priorityFilter = "high"
        vm.load()

        XCTAssertEqual(vm.items.count, 1)
        XCTAssertEqual(vm.items[0].text, "High")
    }

    @MainActor
    func testMarkDone() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Fix it", status: "inbox")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.load()
        XCTAssertEqual(vm.items.count, 1)

        let item = vm.items[0]
        vm.markDone(item)

        // After markDone, reload happens. Status filter nil shows inbox+active, so done item is filtered out.
        XCTAssertTrue(vm.items.isEmpty)
        // Verify the item is actually done via direct lookup.
        let updated = vm.itemByID(item.id)
        XCTAssertEqual(updated?.status, "done")
    }

    @MainActor
    func testDismiss() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Task", status: "inbox")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.load()

        let item = vm.items[0]
        vm.dismiss(item)

        // After dismiss, reload happens. Status filter nil shows inbox+active, so dismissed item is filtered out.
        XCTAssertTrue(vm.items.isEmpty)
        let updated = vm.itemByID(item.id)
        XCTAssertEqual(updated?.status, "dismissed")
    }

    @MainActor
    func testReopen() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Task", status: "done")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = "done"
        vm.load()
        XCTAssertEqual(vm.items.count, 1)

        let item = vm.items[0]
        vm.reopen(item)

        // After reopen to "inbox", it won't appear under "done" filter anymore.
        // Switch to nil filter (inbox+active) to verify.
        vm.statusFilter = nil
        vm.load()
        XCTAssertEqual(vm.items.count, 1)
        XCTAssertEqual(vm.items[0].status, "inbox")
    }

    @MainActor
    func testSnooze() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Task", status: "inbox")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.load()

        let item = vm.items[0]
        let tomorrow = Date().addingTimeInterval(86400)
        vm.snooze(item, until: tomorrow)

        // After snooze, reload happens. Status filter nil shows inbox+active, so snoozed item is filtered out.
        XCTAssertTrue(vm.items.isEmpty)
        let updated = vm.itemByID(item.id)
        XCTAssertEqual(updated?.status, "snoozed")
        XCTAssertNotNil(updated?.snoozeUntil)
    }

    @MainActor
    func testStatusChangeLogsHistory() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Fix it", status: "inbox")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.load()
        let item = vm.items[0]

        // Mark done — should log history.
        vm.markDone(item)

        let history = vm.fetchHistory(for: item.id)
        XCTAssertFalse(history.isEmpty, "Status change should create a history entry")
        XCTAssertEqual(history.last?.event, "status_changed")
        XCTAssertEqual(history.last?.oldValue, "inbox")
        XCTAssertEqual(history.last?.newValue, "done")
    }

    @MainActor
    func testAcceptNonInboxDoesNotLogHistory() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001", text: "Task", status: "active")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = "active"
        vm.load()
        let item = vm.items[0]

        // Accept an already-active item — should NOT log phantom history.
        vm.accept(item)

        let history = vm.fetchHistory(for: item.id)
        XCTAssertTrue(history.isEmpty, "Accepting a non-inbox item should not create phantom history")
    }

    @MainActor
    func testSlackMessageURL() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "acme")
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, assigneeUserID: "U001")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        let url = vm.slackMessageURL(channelID: "C001", messageTS: "1740577800.000100")
        XCTAssertEqual(url?.absoluteString, "https://acme.slack.com/archives/C001/p1740577800000100")
    }

    @MainActor
    func testSlackMessageURLNilWithoutDomain() {
        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.slackMessageURL(channelID: "C001", messageTS: "123.456"))
    }

    @MainActor
    func testAvailableChannels() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Task 1",
                                               sourceChannelName: "general")
            try TestDatabase.insertTrack(db, channelID: "C002", assigneeUserID: "U001", text: "Task 2",
                                               sourceChannelName: "engineering",
                                               periodFrom: 1700100000, periodTo: 1700200000)
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Task 3",
                                               sourceChannelName: "general",
                                               periodFrom: 1700200000, periodTo: 1700300000)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.load()

        let channels = vm.availableChannels
        XCTAssertEqual(channels.count, 2)
        // Sorted by name
        XCTAssertEqual(channels[0].name, "engineering")
        XCTAssertEqual(channels[1].name, "general")
    }

    @MainActor
    func testLoadWithChannelFilter() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001", text: "Task 1")
            try TestDatabase.insertTrack(db, channelID: "C002", assigneeUserID: "U001", text: "Task 2",
                                               periodFrom: 1700100000, periodTo: 1700200000)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.channelFilter = "C002"
        vm.load()

        XCTAssertEqual(vm.items.count, 1)
        XCTAssertEqual(vm.items[0].channelID, "C002")
    }

    @MainActor
    func testAvailableChannelsFallbackToID() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, channelID: "C001", assigneeUserID: "U001",
                                               text: "Task", sourceChannelName: "")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.statusFilter = nil
        vm.load()

        let channels = vm.availableChannels
        XCTAssertEqual(channels.count, 1)
        XCTAssertEqual(channels[0].name, "C001")
    }
}

// MARK: - ChatHistoryViewModel

final class ChatHistoryViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
        // Ensure chat_conversations table exists
        try! dbManager.dbPool.write { db in
            try ChatConversationQueries.ensureTable(db)
        }
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testCreateConversation() {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv = vm.createConversation()

        XCTAssertNotNil(conv)
        XCTAssertEqual(vm.conversations.count, 1)
        XCTAssertEqual(vm.selectedConversationID, conv?.id)
    }

    @MainActor
    func testDeleteConversation() {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv = vm.createConversation()!
        XCTAssertEqual(vm.conversations.count, 1)

        vm.deleteConversation(conv.id)
        XCTAssertTrue(vm.conversations.isEmpty)
        XCTAssertNil(vm.selectedConversationID)
    }

    @MainActor
    func testDeleteSelectedSwitchesToFirst() {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv1 = vm.createConversation()!
        let conv2 = vm.createConversation()!
        vm.selectedConversationID = conv2.id

        vm.deleteConversation(conv2.id)

        XCTAssertEqual(vm.conversations.count, 1)
        XCTAssertEqual(vm.selectedConversationID, conv1.id)
    }

    @MainActor
    func testFilteredConversations() {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        vm.createConversation()
        vm.updateTitle(vm.conversations[0].id, title: "Slack discussion")
        vm.createConversation()
        vm.updateTitle(vm.conversations[0].id, title: "Meeting notes")

        vm.searchText = "slack"
        XCTAssertEqual(vm.filteredConversations.count, 1)
        XCTAssertEqual(vm.filteredConversations[0].title, "Slack discussion")
    }

    @MainActor
    func testFilteredConversationsEmptySearch() {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        vm.createConversation()
        vm.createConversation()

        vm.searchText = ""
        XCTAssertEqual(vm.filteredConversations.count, 2)
    }

    @MainActor
    func testUpdateSessionID() {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv = vm.createConversation()!

        vm.updateSessionID(conv.id, sessionID: "sess-abc")

        let updated = vm.conversations.first { $0.id == conv.id }
        XCTAssertEqual(updated?.sessionID, "sess-abc")
    }

    @MainActor
    func testLoad() {
        // Create conversations directly in DB
        try! dbManager.dbPool.write { db in
            try ChatConversationQueries.create(db, title: "Chat A")
            try ChatConversationQueries.create(db, title: "Chat B")
        }

        let vm = ChatHistoryViewModel(dbManager: dbManager)
        XCTAssertTrue(vm.conversations.isEmpty)

        vm.load()

        // load() is async via Task.detached, give it a moment
        let expectation = XCTestExpectation(description: "load completes")
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
            XCTAssertEqual(vm.conversations.count, 2)
            expectation.fulfill()
        }
        wait(for: [expectation], timeout: 2)
    }
}

// MARK: - DigestViewModel (additional coverage)

final class DigestViewModelAdditionalTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testMarkDigestRead() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", summary: "Test")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.unreadDigestCount, 1)
        XCTAssertFalse(vm.digests[0].isRead)

        vm.markDigestRead(vm.digests[0].id)

        XCTAssertEqual(vm.unreadDigestCount, 0)
        XCTAssertTrue(vm.digests[0].isRead)
    }

    @MainActor
    func testMarkDecisionRead() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertDigest(db, channelID: "C001",
                                          decisions: #"[{"text":"Decision A"},{"text":"Decision B"}]"#)
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.decisionEntries.count, 2)
        XCTAssertEqual(vm.unreadDecisionCount, 2)

        let entry = vm.decisionEntries[0]
        vm.markDecisionRead(digestID: entry.digestID, decisionIdx: entry.decisionIdx)

        XCTAssertEqual(vm.unreadDecisionCount, 1)
        let updated = vm.decisionEntries.first { $0.digestID == entry.digestID && $0.decisionIdx == entry.decisionIdx }
        XCTAssertTrue(updated?.isRead ?? false)
    }

    @MainActor
    func testDecisionDedup() throws {
        // Two digests with similar decisions — channel and daily rollup
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400, type: "channel",
                                          decisions: #"[{"text":"We decided to migrate the database to PostgreSQL immediately"}]"#)
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700000000, periodTo: 1700086400, type: "daily",
                                          decisions: #"[{"text":"Team decided to migrate the database to PostgreSQL soon"}]"#)
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        // Daily/weekly decisions are preferred; the channel duplicate should be deduped
        XCTAssertEqual(vm.decisionEntries.count, 1)
        XCTAssertEqual(vm.decisionEntries[0].digestType, "daily")
    }

    @MainActor
    func testDecisionsDailyPreferred() throws {
        // Unique decisions from both channel and daily
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "general")
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700086400, type: "channel",
                                          decisions: #"[{"text":"Use Redis for caching"}]"#)
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700000000, periodTo: 1700086400, type: "daily",
                                          decisions: #"[{"text":"Adopt TypeScript for frontend"}]"#)
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        // Both are unique, both should appear
        XCTAssertEqual(vm.decisionEntries.count, 2)
    }

    @MainActor
    func testUnreadDigestCountMultiple() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 100, periodTo: 200, summary: "D1")
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 100, periodTo: 200, summary: "D2")
            try TestDatabase.insertDigest(db, channelID: "C003", periodFrom: 100, periodTo: 200, summary: "D3")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.unreadDigestCount, 3)

        vm.markDigestRead(vm.digests[0].id)
        XCTAssertEqual(vm.unreadDigestCount, 2)

        vm.markDigestRead(vm.digests[1].id)
        XCTAssertEqual(vm.unreadDigestCount, 1)
    }

    @MainActor
    func testDigestByIDNotFound() {
        let vm = DigestViewModel(dbManager: dbManager)
        XCTAssertNil(vm.digestByID(999))
    }
}

// MARK: - UpdateService Version Comparison

final class UpdateServiceTests: XCTestCase {
    func testNewerMajor() {
        XCTAssertTrue(UpdateService.isNewer("1.0.0", than: "0.2.0"))
    }

    func testNewerMinor() {
        XCTAssertTrue(UpdateService.isNewer("0.3.0", than: "0.2.0"))
    }

    func testNewerPatch() {
        XCTAssertTrue(UpdateService.isNewer("0.2.1", than: "0.2.0"))
    }

    func testSameVersion() {
        XCTAssertFalse(UpdateService.isNewer("0.2.0", than: "0.2.0"))
    }

    func testOlderVersion() {
        XCTAssertFalse(UpdateService.isNewer("0.1.0", than: "0.2.0"))
    }

    func testVPrefix() {
        XCTAssertTrue(UpdateService.isNewer("v0.3.0", than: "0.2.0"))
        XCTAssertTrue(UpdateService.isNewer("v0.3.0", than: "v0.2.0"))
        XCTAssertFalse(UpdateService.isNewer("v0.2.0", than: "v0.2.0"))
    }

    func testDifferentLengths() {
        XCTAssertTrue(UpdateService.isNewer("0.2.1", than: "0.2"))
        XCTAssertFalse(UpdateService.isNewer("0.2", than: "0.2.0"))
    }
}

// MARK: - OnboardingChatViewModel

final class OnboardingChatViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        (dbManager, dbPath) = try! TestDatabase.createDatabaseManager()
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testInitialState() {
        let vm = OnboardingChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        XCTAssertTrue(vm.messages.isEmpty)
        XCTAssertFalse(vm.isStreaming)
        XCTAssertEqual(vm.inputText, "")
        XCTAssertNil(vm.errorMessage)
        XCTAssertEqual(vm.role, "")
        XCTAssertEqual(vm.team, "")
        XCTAssertTrue(vm.painPoints.isEmpty)
        XCTAssertTrue(vm.trackFocus.isEmpty)
        XCTAssertTrue(vm.reportIDs.isEmpty)
        XCTAssertEqual(vm.managerID, "")
        XCTAssertTrue(vm.peerIDs.isEmpty)
    }

    @MainActor
    func testSendCreatesMessages() async throws {
        let mock = MockClaudeService(events: [.text("Great! "), .text("Tell me more."), .done])
        let vm = OnboardingChatViewModel(claudeService: mock, dbManager: dbManager)

        vm.inputText = "I'm an Engineering Manager"
        vm.send()

        try await Task.sleep(for: .milliseconds(300))

        XCTAssertEqual(vm.messages.count, 2)
        XCTAssertEqual(vm.messages[0].role, .user)
        XCTAssertEqual(vm.messages[0].text, "I'm an Engineering Manager")
        XCTAssertEqual(vm.messages[1].role, .assistant)
        XCTAssertEqual(vm.messages[1].text, "Great! Tell me more.")
        XCTAssertFalse(vm.isStreaming)
    }

    @MainActor
    func testSendEmptyDoesNothing() {
        let vm = OnboardingChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.inputText = "   "
        vm.send()
        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testSendWhileStreamingDoesNothing() {
        let vm = OnboardingChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        vm.isStreaming = true
        vm.inputText = "Hello"
        vm.send()
        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testFinishChatParsesRole() async throws {
        let mock = MockClaudeService(events: [.text("Got it!"), .done])
        let vm = OnboardingChatViewModel(claudeService: mock, dbManager: dbManager)

        // Simulate user saying their role
        vm.inputText = "I'm an engineering manager at Platform team"
        vm.send()
        try await Task.sleep(for: .milliseconds(300))

        vm.finishChat()

        XCTAssertEqual(vm.role, "Engineering Manager")
        XCTAssertFalse(vm.isStreaming)
    }

    @MainActor
    func testFinishChatParsesPainPoints() async throws {
        let mock = MockClaudeService(events: [.text("I understand."), .done])
        let vm = OnboardingChatViewModel(claudeService: mock, dbManager: dbManager)

        vm.inputText = "I often miss important decisions in threads and lose track of deadlines"
        vm.send()
        try await Task.sleep(for: .milliseconds(300))

        vm.finishChat()

        XCTAssertTrue(vm.painPoints.contains(where: { $0.lowercased().contains("decision") }))
        XCTAssertTrue(vm.painPoints.contains(where: { $0.lowercased().contains("deadline") }))
    }

    @MainActor
    func testMarkOnboardingDone() async throws {
        try await dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, id: "T001")
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertProfile(db, slackUserID: "U001", onboardingDone: false)
        }

        let vm = OnboardingChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        await vm.markOnboardingDone()

        let profile = try await dbManager.dbPool.read { db in
            try ProfileQueries.fetchProfile(db, slackUserID: "U001")
        }
        XCTAssertEqual(profile?.onboardingDone, true)
    }

    @MainActor
    func testSendWithError() async throws {
        let mock = MockClaudeService(error: ClaudeError.notFound)
        let vm = OnboardingChatViewModel(claudeService: mock, dbManager: dbManager)

        vm.inputText = "Hello"
        vm.send()
        try await Task.sleep(for: .milliseconds(300))

        XCTAssertNotNil(vm.errorMessage)
        XCTAssertFalse(vm.isStreaming)
    }

    @MainActor
    func testLoadUsersFromDB() async throws {
        try await dbManager.dbPool.write { db in
            try TestDatabase.insertUser(db, id: "U001", displayName: "Alice")
            try TestDatabase.insertUser(db, id: "U002", displayName: "Bob")
        }

        let vm = OnboardingChatViewModel(claudeService: MockClaudeService(), dbManager: dbManager)
        XCTAssertEqual(vm.allUsers.count, 2)
    }

    @MainActor
    func testOnboardingSystemPromptContent() {
        let prompt = OnboardingChatViewModel.onboardingSystemPrompt
        XCTAssertTrue(prompt.contains("onboarding"))
        XCTAssertTrue(prompt.contains("Role"))
        XCTAssertTrue(prompt.contains("Pain Points"))
    }
}
