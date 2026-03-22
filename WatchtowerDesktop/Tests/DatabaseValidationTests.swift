import XCTest
@testable import WatchtowerDesktop

final class DatabaseValidationTests: XCTestCase {

    // MARK: - isValidWorkspaceName

    func testValidWorkspaceNames() {
        XCTAssertTrue(DatabaseManager.isValidWorkspaceName("my-workspace"))
        XCTAssertTrue(DatabaseManager.isValidWorkspaceName("workspace_v2"))
        XCTAssertTrue(DatabaseManager.isValidWorkspaceName("MyCompany"))
        XCTAssertTrue(DatabaseManager.isValidWorkspaceName("test123"))
        XCTAssertTrue(DatabaseManager.isValidWorkspaceName("a"))
        XCTAssertTrue(DatabaseManager.isValidWorkspaceName("workspace.v2"))
    }

    func testEmptyWorkspaceName() {
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName(""))
    }

    func testDotPrefixRejected() {
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName(".hidden"))
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName(".."))
    }

    func testPathTraversalRejected() {
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("../etc"))
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("foo/bar"))
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("workspace name"))
    }

    func testSpecialCharsRejected() {
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("work space"))
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("work@space"))
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("work$pace"))
        XCTAssertFalse(DatabaseManager.isValidWorkspaceName("work\nspace"))
    }

    // MARK: - sanitizeFTS5Query

    func testSanitizeBasicTerms() {
        let result = SearchQueries.sanitizeFTS5Query("hello world")
        XCTAssertEqual(result, #""hello" "world""#)
    }

    func testSanitizeStripsOperators() {
        let result = SearchQueries.sanitizeFTS5Query("hello AND world")
        XCTAssertEqual(result, #""hello" "world""#)
    }

    func testSanitizeStripsAllOperators() {
        let result = SearchQueries.sanitizeFTS5Query("NOT this OR that NEAR here")
        XCTAssertEqual(result, #""this" "that" "here""#)
    }

    func testSanitizeEmptyInput() {
        let result = SearchQueries.sanitizeFTS5Query("")
        XCTAssertEqual(result, "")
    }

    func testSanitizeOnlyOperators() {
        let result = SearchQueries.sanitizeFTS5Query("AND OR NOT")
        XCTAssertEqual(result, "")
    }

    func testSanitizeStripsQuotes() {
        let result = SearchQueries.sanitizeFTS5Query(#"hello "world""#)
        XCTAssertEqual(result, #""hello" "world""#)
    }

    func testSanitizeSingleTerm() {
        let result = SearchQueries.sanitizeFTS5Query("deploy")
        XCTAssertEqual(result, #""deploy""#)
    }

    func testSanitizeWhitespace() {
        let result = SearchQueries.sanitizeFTS5Query("  hello   world  ")
        XCTAssertEqual(result, #""hello" "world""#)
    }

    func testSanitizeCaseInsensitiveOperators() {
        let result = SearchQueries.sanitizeFTS5Query("hello and world or test")
        // Operators are filtered case-insensitively (uppercased before check)
        XCTAssertEqual(result, #""hello" "world" "test""#)
    }

    // MARK: - WatchtowerDatabaseError descriptions

    func testErrorDescriptions() {
        XCTAssertNotNil(WatchtowerDatabaseError.databaseNotFound.errorDescription)
        XCTAssertTrue(WatchtowerDatabaseError.invalidWorkspaceName("bad").errorDescription?.contains("bad") == true)
        XCTAssertTrue(WatchtowerDatabaseError.schemaVersionTooOld(1).errorDescription?.contains("1") == true)
        XCTAssertTrue(WatchtowerDatabaseError.missingTable("users").errorDescription?.contains("users") == true)
    }
}
