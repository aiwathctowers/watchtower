import XCTest
import GRDB
@testable import WatchtowerDesktop

final class ChainQueryTests: XCTestCase {

    func testFetchAll() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Alpha", slug: "alpha", lastSeen: 1700086400)
            try TestDatabase.insertChain(db, title: "Beta", slug: "beta", lastSeen: 1700172800)
        }
        let chains = try db.read { try ChainQueries.fetchAll($0) }
        XCTAssertEqual(chains.count, 2)
        // Ordered by last_seen DESC
        XCTAssertEqual(chains[0].title, "Beta")
        XCTAssertEqual(chains[1].title, "Alpha")
    }

    func testFetchAllWithStatusFilter() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Active", slug: "active", status: "active")
            try TestDatabase.insertChain(db, title: "Resolved", slug: "resolved", status: "resolved")
            try TestDatabase.insertChain(db, title: "Stale", slug: "stale", status: "stale")
        }
        let active = try db.read { try ChainQueries.fetchAll($0, status: "active") }
        XCTAssertEqual(active.count, 1)
        XCTAssertEqual(active[0].title, "Active")
    }

    func testFetchByID() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Test Chain", slug: "test")
        }
        let chain = try db.read { try ChainQueries.fetchByID($0, id: 1) }
        XCTAssertNotNil(chain)
        XCTAssertEqual(chain?.title, "Test Chain")
    }

    func testFetchByIDNotFound() throws {
        let db = try TestDatabase.create()
        let chain = try db.read { try ChainQueries.fetchByID($0, id: 999) }
        XCTAssertNil(chain)
    }

    func testFetchRefs() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Chain 1", slug: "c1")
            try TestDatabase.insertChainRef(db, chainID: 1, refType: "decision", digestID: 10, timestamp: 1700000000)
            try TestDatabase.insertChainRef(db, chainID: 1, refType: "track", trackID: 5, timestamp: 1700050000)
        }
        let refs = try db.read { try ChainQueries.fetchRefs($0, chainID: 1) }
        XCTAssertEqual(refs.count, 2)
        // Ordered by timestamp ASC
        XCTAssertTrue(refs[0].isDecision)
        XCTAssertTrue(refs[1].isTrack)
    }

    func testFetchActiveCount() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "A", slug: "a", status: "active")
            try TestDatabase.insertChain(db, title: "B", slug: "b", status: "active")
            try TestDatabase.insertChain(db, title: "C", slug: "c", status: "resolved")
        }
        let count = try db.read { try ChainQueries.fetchActiveCount($0) }
        XCTAssertEqual(count, 2)
    }

    func testUpdateStatus() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Test", slug: "test", status: "active")
            try ChainQueries.updateStatus(db, id: 1, status: "resolved")
        }
        let chain = try db.read { try ChainQueries.fetchByID($0, id: 1) }
        XCTAssertEqual(chain?.status, "resolved")
        XCTAssertTrue(chain!.isResolved)
    }

    func testFetchChainsForDigest() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Related", slug: "related")
            try TestDatabase.insertChain(db, title: "Unrelated", slug: "unrelated")
            try TestDatabase.insertChainRef(db, chainID: 1, refType: "decision", digestID: 42, timestamp: 1700000000)
        }
        let chains = try db.read { try ChainQueries.fetchChainsForDigest($0, digestID: 42) }
        XCTAssertEqual(chains.count, 1)
        XCTAssertEqual(chains[0].title, "Related")
    }

    func testFetchChainsForTrack() throws {
        let db = try TestDatabase.create()
        try db.write { db in
            try TestDatabase.insertChain(db, title: "Track Chain", slug: "tc")
            try TestDatabase.insertChainRef(db, chainID: 1, refType: "track", trackID: 7, timestamp: 1700000000)
        }
        let chains = try db.read { try ChainQueries.fetchChainsForTrack($0, trackID: 7) }
        XCTAssertEqual(chains.count, 1)
        XCTAssertEqual(chains[0].title, "Track Chain")
    }

    func testFetchChainsForTrackEmpty() throws {
        let db = try TestDatabase.create()
        let chains = try db.read { try ChainQueries.fetchChainsForTrack($0, trackID: 999) }
        XCTAssertTrue(chains.isEmpty)
    }
}
