import XCTest
import GRDB
@testable import WatchtowerDesktop

// MARK: - DashboardViewModel

final class DashboardViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
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
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
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
            try TestDatabase.insertDigest(
                db,
                channelID: "C001",
                decisions: #"[{"text":"Use Go","by":"Alice","importance":"high"},{"text":"Deploy Friday"}]"#
            )
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

        XCTAssertEqual(vm.slackChannelURL(channelID: "C001")?.absoluteString, "slack://channel?team=T001&id=C001")
    }

    @MainActor
    func testSlackChannelURLNilWithoutTeamID() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, id: "", domain: "")
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
        XCTAssertEqual(url?.absoluteString, "slack://channel?team=T001&id=C001&message=1740577800.000100")
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

        let dailyDigest = try XCTUnwrap(vm.digests.first { $0.type == "daily" })
        let contributing = vm.contributingChannels(for: dailyDigest)
        XCTAssertEqual(contributing.count, 2)
        XCTAssertTrue(contributing.contains { $0.name == "general" })
        XCTAssertTrue(contributing.contains { $0.name == "engineering" })
    }

    @MainActor
    func testContributingChannelsDeduplicates() throws {
        // Same channel can have multiple channel digests within a daily/weekly window
        // (e.g. one per sync cycle). The list must show each channel only once,
        // sorted alphabetically.
        try dbManager.dbPool.write { db in
            try TestDatabase.insertChannel(db, id: "C001", name: "zeta")
            try TestDatabase.insertChannel(db, id: "C002", name: "alpha")
            // Three channel digests for C001, one for C002 — all in the daily window
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700000000, periodTo: 1700020000, type: "channel", summary: "z1")
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700020001, periodTo: 1700040000, type: "channel", summary: "z2")
            try TestDatabase.insertDigest(db, channelID: "C001", periodFrom: 1700040001, periodTo: 1700060000, type: "channel", summary: "z3")
            try TestDatabase.insertDigest(db, channelID: "C002", periodFrom: 1700000000, periodTo: 1700086400, type: "channel", summary: "a1")
            try TestDatabase.insertDigest(db, channelID: "", periodFrom: 1700000000, periodTo: 1700086400, type: "daily", summary: "daily")
        }

        let vm = DigestViewModel(dbManager: dbManager)
        vm.load()

        let daily = try XCTUnwrap(vm.digests.first { $0.type == "daily" })
        let contributing = vm.contributingChannels(for: daily)
        XCTAssertEqual(contributing.count, 2, "C001 must collapse to a single entry")
        XCTAssertEqual(contributing[0].name, "alpha", "results must be sorted by name")
        XCTAssertEqual(contributing[1].name, "zeta")
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
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
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
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertPeopleCard(db, userID: "U002", periodFrom: 100, periodTo: 200, messageCount: 30)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.errorMessage, "load() error: \(vm.errorMessage ?? "")")
        XCTAssertFalse(vm.availableWindows.isEmpty, "no windows found")
        XCTAssertEqual(vm.cards.count, 2)
        XCTAssertEqual(vm.cards[0].userID, "U001")
        XCTAssertEqual(vm.availableWindows.count, 1)
        XCTAssertEqual(vm.userNameCache["U001"], "Alice")
        XCTAssertEqual(vm.userNameCache["U002"], "Bob")
        XCTAssertFalse(vm.isLoading)
    }

    @MainActor
    func testLoadEmptyDB() {
        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.cards.isEmpty)
        XCTAssertTrue(vm.availableWindows.isEmpty)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testLoadWindow() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 200, periodTo: 300, messageCount: 30)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.cards.count, 1)
        XCTAssertEqual(vm.cards[0].periodFrom, 200)

        vm.loadWindow(at: 1)
        XCTAssertEqual(vm.selectedWindow, 1)
        XCTAssertEqual(vm.cards.count, 1)
        XCTAssertEqual(vm.cards[0].periodFrom, 100)
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
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 100, periodTo: 200)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.userName(for: "U001"), "Alice")
        XCTAssertEqual(vm.userName(for: "U999"), "U999")
    }

    @MainActor
    func testCurrentWindowLabel() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 1700000000, periodTo: 1700604800)
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
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 100, periodTo: 200, redFlags: #"["Issue"]"#)
            try TestDatabase.insertPeopleCard(db, userID: "U002", periodFrom: 100, periodTo: 200, redFlags: "[]")
            try TestDatabase.insertPeopleCard(db, userID: "U003", periodFrom: 100, periodTo: 200, redFlags: #"["A","B"]"#)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertEqual(vm.redFlagCount, 2)
    }

    @MainActor
    func testCardHistory() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 100, periodTo: 200, messageCount: 50)
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 200, periodTo: 300, messageCount: 30)
            try TestDatabase.insertPeopleCard(db, userID: "U002", periodFrom: 100, periodTo: 200, messageCount: 10)
        }

        let vm = PeopleViewModel(dbManager: dbManager)
        let history = vm.cardHistory(userID: "U001")

        XCTAssertEqual(history.count, 2)
        XCTAssertTrue(history.allSatisfy { $0.userID == "U001" })
    }

    @MainActor
    func testUserNameCachePrefersDisplayName() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertUser(db, id: "U001", name: "alice", displayName: "Alice Wonder")
            try TestDatabase.insertUser(db, id: "U002", name: "bob", displayName: "")
            try TestDatabase.insertPeopleCard(db, userID: "U001", periodFrom: 100, periodTo: 200)
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
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testNewChat() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.messages = [
            ChatMessage(id: UUID(), role: .user, text: "Hi", timestamp: Date(), isStreaming: false),
            ChatMessage(id: UUID(), role: .assistant, text: "Hello!", timestamp: Date(), isStreaming: false)
        ]
        vm.newChat()

        XCTAssertTrue(vm.messages.isEmpty)
        XCTAssertFalse(vm.isStreaming)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testCancelStream() throws {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.isStreaming = true
        vm.messages = [
            ChatMessage(id: UUID(), role: .assistant, text: "Partial...", timestamp: Date(), isStreaming: true)
        ]

        vm.cancelStream()

        XCTAssertFalse(vm.isStreaming)
        XCTAssertFalse(try XCTUnwrap(vm.messages.last).isStreaming)
    }

    @MainActor
    func testSendCreatesMessages() async throws {
        let mock = MockClaudeService(events: [.text("Hello "), .text("world"), .done])
        let vm = ChatViewModel(aiService: mock, dbManager: dbManager)

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
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.inputText = "   "
        vm.send()

        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testSendWhileStreamingDoesNothing() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.isStreaming = true
        vm.inputText = "Hello"
        vm.send()

        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testSendClearsInputText() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.inputText = "Hello"
        vm.send()

        XCTAssertEqual(vm.inputText, "")
    }

    @MainActor
    func testSendWithError() async throws {
        let mock = MockClaudeService(error: WatchtowerAIError.cliNotFound)
        let vm = ChatViewModel(aiService: mock, dbManager: dbManager)

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

// MARK: - AIProvider & ChatModel Tests

final class AIProviderTests: XCTestCase {
    func testProviderDisplayNames() {
        XCTAssertEqual(AIProvider.claude.displayName, "Claude")
        XCTAssertEqual(AIProvider.codex.displayName, "Codex")
    }

    func testProviderAllCases() {
        XCTAssertEqual(AIProvider.allCases.count, 2)
    }

    func testClaudeModelsProvider() {
        XCTAssertEqual(ChatModel.sonnet.provider, .claude)
        XCTAssertEqual(ChatModel.haiku.provider, .claude)
        XCTAssertEqual(ChatModel.opus.provider, .claude)
    }

    func testCodexModelsProvider() {
        XCTAssertEqual(ChatModel.gpt54.provider, .codex)
        XCTAssertEqual(ChatModel.gpt54mini.provider, .codex)
        XCTAssertEqual(ChatModel.gpt53codex.provider, .codex)
    }

    func testModelsForProvider() {
        let claudeModels = ChatModel.models(for: .claude)
        XCTAssertEqual(claudeModels.count, 3)
        XCTAssertTrue(claudeModels.allSatisfy { $0.provider == .claude })

        let codexModels = ChatModel.models(for: .codex)
        XCTAssertEqual(codexModels.count, 3)
        XCTAssertTrue(codexModels.allSatisfy { $0.provider == .codex })
    }

    func testModelDisplayNames() {
        XCTAssertEqual(ChatModel.gpt54.displayName, "GPT-5.4")
        XCTAssertEqual(ChatModel.gpt54mini.displayName, "GPT-5.4 Mini")
        XCTAssertEqual(ChatModel.gpt53codex.displayName, "GPT-5.3 Codex")
    }

    func testModelRawValues() {
        XCTAssertEqual(ChatModel.gpt54.rawValue, "gpt-5.4")
        XCTAssertEqual(ChatModel.gpt54mini.rawValue, "gpt-5.4-mini")
        XCTAssertEqual(ChatModel.gpt53codex.rawValue, "gpt-5.3-codex")
    }
}

// MARK: - ChatViewModel Provider Switching Tests

final class ChatViewModelProviderTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testDefaultProviderIsClaude() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        XCTAssertEqual(vm.selectedProvider, .claude)
        XCTAssertEqual(vm.selectedModel.provider, .claude)
    }

    @MainActor
    func testInitWithCodexProvider() {
        let vm = ChatViewModel(
            aiService: MockClaudeService(),
            dbManager: dbManager,
            provider: .codex
        )
        XCTAssertEqual(vm.selectedProvider, .codex)
        XCTAssertEqual(vm.selectedModel.provider, .codex)
    }

    @MainActor
    func testSwitchProviderChangesModel() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        XCTAssertEqual(vm.selectedProvider, .claude)

        vm.switchProvider(.codex)

        XCTAssertEqual(vm.selectedProvider, .codex)
        XCTAssertEqual(vm.selectedModel.provider, .codex)
    }

    @MainActor
    func testSwitchToSameProviderNoOp() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        let originalModel = vm.selectedModel

        vm.switchProvider(.claude)

        XCTAssertEqual(vm.selectedModel, originalModel)
    }

    @MainActor
    func testSwitchProviderBackAndForth() {
        let vm = ChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)

        vm.switchProvider(.codex)
        XCTAssertEqual(vm.selectedProvider, .codex)

        vm.switchProvider(.claude)
        XCTAssertEqual(vm.selectedProvider, .claude)
        XCTAssertEqual(vm.selectedModel.provider, .claude)
    }

    @MainActor
    func testCreateServiceForClaude() {
        let service = ChatViewModel.createService(for: .claude)
        XCTAssertTrue(service is WatchtowerAIService)
    }

    @MainActor
    func testCreateServiceForCodex() {
        let service = ChatViewModel.createService(for: .codex)
        XCTAssertTrue(service is WatchtowerAIService)
    }
}

// MARK: - SearchViewModel

final class SearchViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
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
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
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
            try TestDatabase.insertTrack(db, text: "Fix the bug", priority: "high", hasUpdates: true)
            try TestDatabase.insertTrack(db, text: "Write docs", priority: "low")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertNil(vm.errorMessage, "load() error: \(vm.errorMessage ?? "")")
        // Has updates goes to updatedTracks, rest to allTracks
        XCTAssertEqual(vm.updatedTracks.count, 1)
        XCTAssertEqual(vm.allTracks.count, 1)
        XCTAssertEqual(vm.updatedTracks[0].text, "Fix the bug")
        XCTAssertEqual(vm.allTracks[0].text, "Write docs")
        XCTAssertEqual(vm.totalCount, 2)
        XCTAssertEqual(vm.updatedCount, 1)
        XCTAssertEqual(vm.workspaceDomain, "acme")
        XCTAssertFalse(vm.isLoading)
    }

    @MainActor
    func testLoadEmptyDB() {
        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        XCTAssertTrue(vm.updatedTracks.isEmpty)
        XCTAssertTrue(vm.allTracks.isEmpty)
        XCTAssertEqual(vm.totalCount, 0)
        XCTAssertNil(vm.errorMessage)
    }

    @MainActor
    func testLoadWithPriorityFilter() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, text: "High", priority: "high")
            try TestDatabase.insertTrack(db, text: "Low", priority: "low")
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.priorityFilter = "high"
        vm.load()

        XCTAssertEqual(vm.allTracks.count, 1)
        XCTAssertEqual(vm.allTracks[0].text, "High")
    }

    @MainActor
    func testMarkRead() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, text: "Fix it", hasUpdates: true)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.showRead = true // show read tracks to verify they move correctly
        vm.load()
        XCTAssertEqual(vm.updatedTracks.count, 1)

        let item = vm.updatedTracks[0]
        vm.markRead(item)

        // After markRead, the track moves from updatedTracks to allTracks
        XCTAssertTrue(vm.updatedTracks.isEmpty)
        XCTAssertEqual(vm.allTracks.count, 1)
        let updated = vm.itemByID(item.id)
        XCTAssertTrue(updated?.isRead ?? false)
    }

    @MainActor
    func testSlackMessageURL() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, domain: "acme")
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        let url = vm.slackMessageURL(channelID: "C001", messageTS: "1740577800.000100")
        XCTAssertEqual(url?.absoluteString, "slack://channel?team=T001&id=C001&message=1740577800.000100")
    }

    @MainActor
    func testSlackMessageURLWithoutDomain() {
        let vm = TracksViewModel(dbManager: dbManager)
        vm.load()

        // No workspace loaded — teamID is nil, so URL should be nil
        let url = vm.slackMessageURL(channelID: "C001", messageTS: "123.456")
        XCTAssertNil(url)
    }

    @MainActor
    func testLoadWithChannelFilter() throws {
        try dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db)
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertTrack(db, text: "Task 1", channelIDs: #"["C001"]"#)
            try TestDatabase.insertTrack(db, text: "Task 2", channelIDs: #"["C002"]"#)
        }

        let vm = TracksViewModel(dbManager: dbManager)
        vm.channelFilter = "C002"
        vm.load()

        let total = vm.updatedTracks.count + vm.allTracks.count
        XCTAssertEqual(total, 1)
    }
}

// MARK: - ChatHistoryViewModel

final class ChatHistoryViewModelTests: XCTestCase {
    private var dbManager: DatabaseManager!
    private var dbPath: String!

    override func setUp() {
        super.setUp()
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
        // Ensure chat_conversations table exists
        do {
            try dbManager.dbPool.write { db in
                try ChatConversationQueries.ensureTable(db)
            }
        } catch {
            XCTFail("setUp ensureTable failed: \(error)")
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
    func testDeleteConversation() throws {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv = try XCTUnwrap(vm.createConversation())
        XCTAssertEqual(vm.conversations.count, 1)

        vm.deleteConversation(conv.id)
        XCTAssertTrue(vm.conversations.isEmpty)
        XCTAssertNil(vm.selectedConversationID)
    }

    @MainActor
    func testDeleteSelectedSwitchesToFirst() throws {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv1 = try XCTUnwrap(vm.createConversation())
        let conv2 = try XCTUnwrap(vm.createConversation())
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
    func testUpdateSessionID() throws {
        let vm = ChatHistoryViewModel(dbManager: dbManager)
        let conv = try XCTUnwrap(vm.createConversation())

        vm.updateSessionID(conv.id, sessionID: "sess-abc")

        let updated = vm.conversations.first { $0.id == conv.id }
        XCTAssertEqual(updated?.sessionID, "sess-abc")
    }

    @MainActor
    func testLoad() throws {
        // Create conversations directly in DB
        try dbManager.dbPool.write { db in
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
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
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
            try TestDatabase.insertDigest(db, channelID: "C001", decisions: #"[{"text":"Decision A"},{"text":"Decision B"}]"#)
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
            try TestDatabase.insertDigest(
                db,
                channelID: "C001",
                periodFrom: 1700000000,
                periodTo: 1700086400,
                type: "channel",
                decisions: #"[{"text":"We decided to migrate the database to PostgreSQL immediately"}]"#
            )
            try TestDatabase.insertDigest(
                db,
                channelID: "",
                periodFrom: 1700000000,
                periodTo: 1700086400,
                type: "daily",
                decisions: #"[{"text":"Team decided to migrate the database to PostgreSQL soon"}]"#
            )
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
            try TestDatabase.insertDigest(
                db,
                channelID: "C001",
                periodFrom: 1700000000,
                periodTo: 1700086400,
                type: "channel",
                decisions: #"[{"text":"Use Redis for caching"}]"#
            )
            try TestDatabase.insertDigest(
                db,
                channelID: "",
                periodFrom: 1700000000,
                periodTo: 1700086400,
                type: "daily",
                decisions: #"[{"text":"Adopt TypeScript for frontend"}]"#
            )
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
        do {
            (dbManager, dbPath) = try TestDatabase.createDatabaseManager()
        } catch {
            XCTFail("setUp failed: \(error)")
        }
    }

    override func tearDown() {
        TestDatabase.cleanup(path: dbPath)
        super.tearDown()
    }

    @MainActor
    func testInitialState() {
        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
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
        let vm = OnboardingChatViewModel(aiService: mock, dbManager: dbManager)

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
        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.inputText = "   "
        vm.send()
        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testSendWhileStreamingDoesNothing() {
        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        vm.isStreaming = true
        vm.inputText = "Hello"
        vm.send()
        XCTAssertTrue(vm.messages.isEmpty)
    }

    @MainActor
    func testFinishChatParsesRole() async throws {
        let extractionJSON = """
        {"role": "Engineering Manager", "team": "Platform", "pain_points": []}
        """
        let mock = MockClaudeService(eventSequence: [
            [.text("Got it!"), .done],                    // send() response
            [.text(extractionJSON), .done]               // parseProfileFromChat() response
        ])
        let vm = OnboardingChatViewModel(aiService: mock, dbManager: dbManager)

        // Simulate user saying their role
        vm.inputText = "I'm an engineering manager at Platform team"
        vm.send()
        try await Task.sleep(for: .milliseconds(300))

        await vm.finishChat()

        XCTAssertEqual(vm.role, "Engineering Manager")
        XCTAssertEqual(vm.team, "Platform")
        XCTAssertFalse(vm.isStreaming)
    }

    @MainActor
    func testFinishChatParsesPainPoints() async throws {
        let extractionJSON = """
        {"role": "", "team": "", "pain_points": ["Decisions getting lost in threads", "Deadlines discussed in chat get forgotten"]}
        """
        let mock = MockClaudeService(eventSequence: [
            [.text("I understand."), .done],              // send() response
            [.text(extractionJSON), .done]               // parseProfileFromChat() response
        ])
        let vm = OnboardingChatViewModel(aiService: mock, dbManager: dbManager)

        vm.inputText = "I often miss important decisions in threads and lose track of deadlines"
        vm.send()
        try await Task.sleep(for: .milliseconds(300))

        await vm.finishChat()

        XCTAssertTrue(vm.painPoints.contains { $0.lowercased().contains("decision") })
        XCTAssertTrue(vm.painPoints.contains { $0.lowercased().contains("deadline") })
    }

    @MainActor
    func testMarkOnboardingDone() async throws {
        try await dbManager.dbPool.write { db in
            try TestDatabase.insertWorkspace(db, id: "T001")
            try db.execute(sql: "UPDATE workspace SET current_user_id = 'U001'")
            try TestDatabase.insertProfile(db, slackUserID: "U001", onboardingDone: false)
        }

        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        await vm.markOnboardingDone()

        let profile = try await dbManager.dbPool.read { db in
            try ProfileQueries.fetchProfile(db, slackUserID: "U001")
        }
        XCTAssertEqual(profile?.onboardingDone, true)
    }

    @MainActor
    func testSendWithError() async throws {
        let mock = MockClaudeService(error: WatchtowerAIError.cliNotFound)
        let vm = OnboardingChatViewModel(aiService: mock, dbManager: dbManager)

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

        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), dbManager: dbManager)
        XCTAssertEqual(vm.allUsers.count, 2)
    }

    @MainActor
    func testOnboardingSystemPromptContent() {
        let prompt = OnboardingChatViewModel.onboardingSystemPrompt(language: "English")
        XCTAssertTrue(prompt.contains("onboarding"))
        XCTAssertTrue(prompt.contains("Watchtower"))
        XCTAssertTrue(prompt.contains("[READY]"))
        XCTAssertTrue(prompt.contains("Respond in English"))
    }

    @MainActor
    func testOnboardingSystemPromptRussian() {
        let prompt = OnboardingChatViewModel.onboardingSystemPrompt(language: "Russian")
        XCTAssertTrue(prompt.contains("Respond in Russian"))
    }

    @MainActor
    func testOnboardingSystemPromptUkrainian() {
        let prompt = OnboardingChatViewModel.onboardingSystemPrompt(language: "Ukrainian")
        XCTAssertTrue(prompt.contains("Respond in Ukrainian"))
    }

    @MainActor
    func testQuestionnaireLocalizationRussian() {
        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), language: "Russian", dbManager: dbManager)
        vm.startQuestionnaire()
        XCTAssertEqual(vm.messages.count, 1)
        XCTAssertTrue(vm.messages[0].text.contains("роль"))
        XCTAssertEqual(vm.quickReplies.count, 2)
        XCTAssertEqual(vm.quickReplies[0].label, "Да")
        XCTAssertEqual(vm.quickReplies[1].label, "Нет")
    }

    @MainActor
    func testQuestionnaireLocalizationEnglish() {
        let vm = OnboardingChatViewModel(aiService: MockClaudeService(), language: "English", dbManager: dbManager)
        vm.startQuestionnaire()
        XCTAssertEqual(vm.messages[0].text, "Let's understand your role. Do people report to you?")
        XCTAssertEqual(vm.quickReplies[0].label, "Yes")
    }
}

// MARK: - OnboardingStateMachine

final class OnboardingStateMachineTests: XCTestCase {
    private let stepKey = "onboarding_current_step"
    private let syncKey = "onboarding_sync_completed"

    override func tearDown() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        UserDefaults.standard.removeObject(forKey: syncKey)
        super.tearDown()
    }

    @MainActor
    func testInitialStateDefaultsToConnect() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        XCTAssertEqual(sm.currentStep, .connect)
        XCTAssertFalse(sm.syncCompleted)
    }

    @MainActor
    func testAdvanceMovesToNextStep() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.advance()
        XCTAssertEqual(sm.currentStep, .settings)
        sm.advance()
        XCTAssertEqual(sm.currentStep, .claude)
        sm.advance()
        XCTAssertEqual(sm.currentStep, .chat)
        sm.advance()
        XCTAssertEqual(sm.currentStep, .teamForm)
        sm.advance()
        XCTAssertEqual(sm.currentStep, .generating)
        sm.advance()
        XCTAssertEqual(sm.currentStep, .complete)
    }

    @MainActor
    func testAdvancePastCompleteDoesNothing() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.goTo(.complete)
        sm.advance()
        XCTAssertEqual(sm.currentStep, .complete)
    }

    @MainActor
    func testGoToJumpsToStep() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.goTo(.chat)
        XCTAssertEqual(sm.currentStep, .chat)
    }

    @MainActor
    func testPersistenceInUserDefaults() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.goTo(.claude)
        XCTAssertEqual(UserDefaults.standard.integer(forKey: stepKey), OnboardingStep.claude.rawValue)

        // Create new instance — should read persisted step
        let sm2 = OnboardingStateMachine()
        XCTAssertEqual(sm2.currentStep, .claude)
    }

    @MainActor
    func testSyncCompletedPersistence() {
        UserDefaults.standard.removeObject(forKey: syncKey)
        let sm = OnboardingStateMachine()
        XCTAssertFalse(sm.syncCompleted)
        sm.syncCompleted = true
        XCTAssertTrue(UserDefaults.standard.bool(forKey: syncKey))

        let sm2 = OnboardingStateMachine()
        XCTAssertTrue(sm2.syncCompleted)
    }

    @MainActor
    func testResetGoesToStepAndClearsSyncIfBeforeChat() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.goTo(.generating)
        sm.syncCompleted = true
        sm.reset(to: .chat)
        XCTAssertEqual(sm.currentStep, .chat)
        XCTAssertFalse(sm.syncCompleted)
    }

    @MainActor
    func testResetToTeamFormKeepsSyncCompleted() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.syncCompleted = true
        sm.reset(to: .teamForm)
        XCTAssertEqual(sm.currentStep, .teamForm)
        XCTAssertTrue(sm.syncCompleted)
    }

    @MainActor
    func testMarkCompleteRemovesUserDefaults() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        sm.goTo(.generating)
        sm.syncCompleted = true
        sm.markComplete()
        XCTAssertEqual(sm.currentStep, .complete)
        XCTAssertNil(UserDefaults.standard.object(forKey: stepKey))
        XCTAssertNil(UserDefaults.standard.object(forKey: syncKey))
    }

    @MainActor
    func testSkipCompletedSkipsConnectWhenConfigExists() {
        UserDefaults.standard.removeObject(forKey: stepKey)
        let sm = OnboardingStateMachine()
        // shouldSkip(.connect) checks if config file exists — depends on test env
        // We test the skip logic by verifying it doesn't skip settings
        sm.goTo(.settings)
        let result = sm.skipCompleted()
        XCTAssertEqual(result, .settings)
    }

    @MainActor
    func testStepComparable() {
        XCTAssertTrue(OnboardingStep.connect < .settings)
        XCTAssertTrue(OnboardingStep.settings < .claude)
        XCTAssertTrue(OnboardingStep.claude < .chat)
        XCTAssertTrue(OnboardingStep.chat < .teamForm)
        XCTAssertTrue(OnboardingStep.teamForm < .generating)
        XCTAssertTrue(OnboardingStep.generating < .complete)
    }

    @MainActor
    func testIndicatorSteps() {
        XCTAssertEqual(OnboardingStep.indicatorSteps.count, 4)
        XCTAssertEqual(OnboardingStep.indicatorSteps, [.connect, .settings, .claude, .chat])
    }

    @MainActor
    func testIndicatorTitles() {
        XCTAssertEqual(OnboardingStep.connect.indicatorTitle, "Connect")
        XCTAssertEqual(OnboardingStep.settings.indicatorTitle, "Settings")
        XCTAssertEqual(OnboardingStep.claude.indicatorTitle, "AI Setup")
        XCTAssertEqual(OnboardingStep.chat.indicatorTitle, "Setup")
        XCTAssertEqual(OnboardingStep.teamForm.indicatorTitle, "Setup")
        XCTAssertEqual(OnboardingStep.generating.indicatorTitle, "Setup")
        XCTAssertNil(OnboardingStep.complete.indicatorTitle)
    }
}

// MARK: - BackgroundTaskManager

final class BackgroundTaskManagerTests: XCTestCase {

    @MainActor
    func testStepRecordEquality() {
        let r1 = BackgroundTaskManager.StepRecord(
            timestamp: Date(timeIntervalSince1970: 1000),
            pipeline: "digests",
            step: 1,
            total: 10,
            status: "Processing #general",
            inputTokens: 100,
            outputTokens: 50,
            costUsd: 0.001,
            durationSeconds: 5.0
        )
        let r2 = BackgroundTaskManager.StepRecord(
            timestamp: Date(timeIntervalSince1970: 1000),
            pipeline: "digests",
            step: 1,
            total: 10,
            status: "Processing #general",
            inputTokens: 100,
            outputTokens: 50,
            costUsd: 0.001,
            durationSeconds: 5.0
        )
        // Different UUIDs, so not equal
        XCTAssertNotEqual(r1, r2)
        // But same id is equal
        XCTAssertEqual(r1, r1)
    }

    @MainActor
    func testTotalTokensAndCost() throws {
        let manager = BackgroundTaskManager()

        // Totals now come from accumulated progress (not step history sum).
        var digestState = BackgroundTaskManager.TaskState()
        digestState.progress = try decodeProgress("""
            {"pipeline":"digests","done":2,"total":5,"status":"","input_tokens":300,"output_tokens":150,"cost_usd":0.003,"finished":false}
            """)
        var peopleState = BackgroundTaskManager.TaskState()
        peopleState.progress = try decodeProgress("""
            {"pipeline":"people","done":1,"total":3,"status":"","input_tokens":300,"output_tokens":150,"cost_usd":0.003,"finished":false}
            """)
        manager.tasks[.digests] = digestState
        manager.tasks[.people] = peopleState

        XCTAssertEqual(manager.totalInputTokens, 600)
        XCTAssertEqual(manager.totalOutputTokens, 300)

    }

    private func decodeProgress(_ json: String) throws -> InsightProgressData {
        try JSONDecoder().decode(InsightProgressData.self, from: Data(json.utf8))
    }

    @MainActor
    func testTotalTokensEmptyTasks() {
        let manager = BackgroundTaskManager()
        XCTAssertEqual(manager.totalInputTokens, 0)
        XCTAssertEqual(manager.totalOutputTokens, 0)

    }

    @MainActor
    func testHasActiveTasks() {
        let manager = BackgroundTaskManager()
        XCTAssertFalse(manager.hasActiveTasks)

        manager.tasks[.digests] = .init(status: .running)
        XCTAssertTrue(manager.hasActiveTasks)

        manager.tasks[.digests] = .init(status: .done)
        XCTAssertFalse(manager.hasActiveTasks)
    }

    @MainActor
    func testAllFinished() {
        let manager = BackgroundTaskManager()
        XCTAssertTrue(manager.allFinished) // empty is finished

        manager.tasks[.digests] = .init(status: .done)
        manager.tasks[.people] = .init(status: .error("fail"))
        XCTAssertTrue(manager.allFinished)

        manager.tasks[.people] = .init(status: .running)
        XCTAssertFalse(manager.allFinished)
    }

    @MainActor
    func testHasVisibleTasks() {
        let manager = BackgroundTaskManager()
        XCTAssertFalse(manager.hasVisibleTasks)

        manager.tasks[.digests] = .init(status: .done)
        XCTAssertFalse(manager.hasVisibleTasks)

        manager.tasks[.people] = .init(status: .error("oops"))
        XCTAssertTrue(manager.hasVisibleTasks)

        manager.tasks[.people] = .init(status: .pending)
        XCTAssertTrue(manager.hasVisibleTasks)
    }
}
