import XCTest
import GRDB
@testable import WatchtowerDesktop

final class PromptQueryTests: XCTestCase {

    func testFetchAllEmpty() throws {
        let db = try TestDatabase.create()
        let prompts = try db.read { try PromptQueries.fetchAll($0) }
        XCTAssertTrue(prompts.isEmpty)
    }

    func testFetchAll() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO prompts (id, template, version, language) VALUES ('digest.channel', 'Template A', 1, 'en')
                """)
            try db.execute(sql: """
                INSERT INTO prompts (id, template, version, language) VALUES ('digest.daily', 'Template B', 2, 'ru')
                """)
        }
        let prompts = try db.read { try PromptQueries.fetchAll($0) }
        XCTAssertEqual(prompts.count, 2)
        // Ordered by id
        XCTAssertEqual(prompts[0].id, "digest.channel")
        XCTAssertEqual(prompts[1].id, "digest.daily")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO prompts (id, template, version, language) VALUES ('digest.channel', 'Template', 3, 'en')
                """)
        }
        let prompt = try db.read { try PromptQueries.fetchByID($0, id: "digest.channel") }
        XCTAssertNotNil(prompt)
        XCTAssertEqual(prompt?.template, "Template")
        XCTAssertEqual(prompt?.version, 3)
        XCTAssertEqual(prompt?.language, "en")
    }

    func testFetchByIDNotFound() throws {
        let db = try TestDatabase.create()
        let prompt = try db.read { try PromptQueries.fetchByID($0, id: "nonexistent") }
        XCTAssertNil(prompt)
    }

    func testFetchHistory() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO prompts (id, template, version) VALUES ('digest.channel', 'v3', 3)
                """)
            try db.execute(sql: """
                INSERT INTO prompt_history (prompt_id, version, template, reason) VALUES ('digest.channel', 1, 'v1', 'Initial')
                """)
            try db.execute(sql: """
                INSERT INTO prompt_history (prompt_id, version, template, reason) VALUES ('digest.channel', 2, 'v2', 'Improved')
                """)
        }
        let history = try db.read { try PromptQueries.fetchHistory($0, promptID: "digest.channel") }
        XCTAssertEqual(history.count, 2)
        // Ordered by version DESC
        XCTAssertEqual(history[0].version, 2)
        XCTAssertEqual(history[0].reason, "Improved")
        XCTAssertEqual(history[1].version, 1)
    }

    func testFetchHistoryEmpty() throws {
        let db = try TestDatabase.create()
        let history = try db.read { try PromptQueries.fetchHistory($0, promptID: "nonexistent") }
        XCTAssertTrue(history.isEmpty)
    }
}
