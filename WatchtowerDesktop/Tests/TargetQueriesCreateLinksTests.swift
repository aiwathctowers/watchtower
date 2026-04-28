import XCTest
import GRDB
@testable import WatchtowerDesktop

final class TargetQueriesCreateLinksTests: XCTestCase {

    func testCreate_PersistsValidLinks_DropsInvalid() throws {
        let queue = try TestDatabase.create()
        let newID = try queue.write { db -> Int in
            try TargetQueries.create(
                db,
                text: "with-links",
                periodStart: "2026-04-28",
                periodEnd: "2026-04-28",
                sourceType: "inbox",
                sourceID: "0",
                secondaryLinks: [
                    TargetPrefillLink(externalRef: "slack:Cabc/p1", relation: "related"),
                    TargetPrefillLink(externalRef: "jira:PROJ-42", relation: "blocks"),
                    TargetPrefillLink(externalRef: "http://invalid", relation: "related"),  // dropped
                    TargetPrefillLink(externalRef: "", relation: "related")                 // dropped (empty)
                ]
            )
        }

        let links = try queue.read { db in
            try TargetLink.fetchAll(
                db,
                sql: "SELECT * FROM target_links WHERE source_target_id = ? ORDER BY id ASC",
                arguments: [newID]
            )
        }
        XCTAssertEqual(links.count, 2)
        XCTAssertEqual(links[0].externalRef, "slack:Cabc/p1")
        XCTAssertEqual(links[0].relation, "related")
        XCTAssertEqual(links[1].externalRef, "jira:PROJ-42")
        XCTAssertEqual(links[1].relation, "blocks")
    }

    func testCreate_NoLinks_DefaultBehaviour() throws {
        let queue = try TestDatabase.create()
        let newID = try queue.write { db -> Int in
            try TargetQueries.create(
                db,
                text: "no-links",
                periodStart: "2026-04-28",
                periodEnd: "2026-04-28",
                sourceType: "manual",
                sourceID: ""
            )
        }
        let count = try queue.read { db in
            try Int.fetchOne(
                db,
                sql: "SELECT COUNT(*) FROM target_links WHERE source_target_id = ?",
                arguments: [newID]
            ) ?? 0
        }
        XCTAssertEqual(count, 0)
    }
}
