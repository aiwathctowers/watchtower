import GRDB
import Foundation

enum WorkspaceQueries {
    static func fetchWorkspace(_ db: Database) throws -> Workspace? {
        try Workspace.fetchOne(db, sql: "SELECT * FROM workspace LIMIT 1")
    }

    static func fetchStats(_ db: Database) throws -> WorkspaceStats {
        try WorkspaceStats.fetch(db)
    }
}
