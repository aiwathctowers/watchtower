import Foundation
import GRDB
import Yams

final class DatabaseManager: Sendable {
    let dbPool: DatabasePool

    /// Internal init for testing — accepts a pre-configured pool, skips validation.
    init(pool: DatabasePool) {
        self.dbPool = pool
    }

    init(path: String) throws {
        var config = Configuration()
        config.label = "watchtower"
        config.prepareDatabase { db in
            // M15: match Go CLI pragmas
            try db.execute(sql: "PRAGMA journal_mode = WAL")
            try db.execute(sql: "PRAGMA synchronous = NORMAL")
            try db.execute(sql: "PRAGMA busy_timeout = 5000")
            try db.execute(sql: "PRAGMA foreign_keys = ON")
        }
        dbPool = try DatabasePool(path: path, configuration: config)

        // H6 + M16: validate schema version and required tables
        try dbPool.read { db in
            let version = try Int.fetchOne(db, sql: "PRAGMA user_version") ?? 0
            guard version >= 3 else {
                throw WatchtowerDatabaseError.schemaVersionTooOld(version)
            }
            let tables = try String.fetchAll(db, sql: "SELECT name FROM sqlite_master WHERE type='table'")
            for required in ["workspace", "channels", "messages", "users"] {
                guard tables.contains(required) else {
                    throw WatchtowerDatabaseError.missingTable(required)
                }
            }
        }

        // Desktop-only tables (not managed by Go CLI schema versioning)
        try dbPool.write { db in
            try ChatConversationQueries.ensureTable(db)
            try ChatConversationQueries.ensureContextColumns(db)
            try ChatMessageQueries.ensureTable(db)
        }
    }

    /// Resolve the watchtower DB path from config.yaml
    static func resolveDBPath() throws -> String {
        let configPath = Constants.configPath
        let basePath = Constants.databasePath

        // M1: use Yams for proper YAML parsing
        if let configData = FileManager.default.contents(atPath: configPath),
           let configStr = String(data: configData, encoding: .utf8),
           let yaml = try? Yams.load(yaml: configStr) as? [String: Any],
           let workspace = yaml["active_workspace"] as? String,
           !workspace.isEmpty {
            // C2: validate workspace name to prevent path traversal
            guard isValidWorkspaceName(workspace) else {
                throw WatchtowerDatabaseError.invalidWorkspaceName(workspace)
            }
            let dbPath = "\(basePath)/\(workspace)/watchtower.db"
            if FileManager.default.fileExists(atPath: dbPath) {
                return dbPath
            }
        }

        // Fallback: find first workspace directory with a DB
        let fm = FileManager.default
        if let contents = try? fm.contentsOfDirectory(atPath: basePath) {
            for dir in contents.sorted() {
                guard isValidWorkspaceName(dir) else { continue }
                let dbPath = "\(basePath)/\(dir)/watchtower.db"
                if fm.fileExists(atPath: dbPath) {
                    return dbPath
                }
            }
        }

        throw WatchtowerDatabaseError.databaseNotFound
    }

    /// M17: DB file size including WAL and SHM
    var fileSize: Int64 {
        let path = dbPool.path
        let fm = FileManager.default
        var total: Int64 = 0
        for suffix in ["", "-wal", "-shm"] {
            let attrs = try? fm.attributesOfItem(atPath: path + suffix)
            total += (attrs?[.size] as? Int64) ?? 0
        }
        return total
    }

    // MARK: - Wipe LLM Data

    /// Delete all AI-generated data from the database, preserving raw Slack data, config, and user profile.
    func wipeLLMData() throws {
        try dbPool.write { db in
            // AI-generated content tables
            try db.execute(sql: "DELETE FROM digests")
            try db.execute(sql: "DELETE FROM user_analyses")
            try db.execute(sql: "DELETE FROM period_summaries")
            try db.execute(sql: "DELETE FROM tracks")
            try db.execute(sql: "DELETE FROM communication_guides")
            try db.execute(sql: "DELETE FROM people_cards")
            try db.execute(sql: "DELETE FROM chains")
            try db.execute(sql: "DELETE FROM chain_refs")

            // AI-generated summary tables
            try db.execute(sql: "DELETE FROM guide_summaries")
            try db.execute(sql: "DELETE FROM people_card_summaries")

            // Briefings
            try db.execute(sql: "DELETE FROM briefings")

            // Pipeline run history
            try db.execute(sql: "DELETE FROM pipeline_steps")
            try db.execute(sql: "DELETE FROM pipeline_runs")

            // Feedback & training signal (tied to wiped content)
            try db.execute(sql: "DELETE FROM feedback")
            try db.execute(sql: "DELETE FROM decision_importance_corrections")
            try db.execute(sql: "DELETE FROM decision_reads")
            try db.execute(sql: "DELETE FROM track_history")
            try db.execute(sql: "DELETE FROM user_interactions")
        }
    }

    // MARK: - CLI Migrations

    /// Run the bundled Go CLI to apply all pending DB migrations before opening the pool.
    /// The CLI owns all schema migrations — desktop app never writes migrations itself.
    static func runCLIMigrations() {
        guard let cliPath = Constants.findCLIPath() else { return }
        let process = Process()
        process.executableURL = URL(fileURLWithPath: cliPath)
        process.arguments = ["db", "migrate"]
        process.environment = Constants.resolvedEnvironment()
        process.standardOutput = nil
        process.standardError = Pipe() // capture for debugging
        guard (try? process.run()) != nil else { return }
        // C2: timeout to prevent indefinite hang on DB lock or broken CLI
        let timer = DispatchSource.makeTimerSource()
        timer.schedule(deadline: .now() + 30)
        timer.setEventHandler { process.terminate() }
        timer.resume()
        process.waitUntilExit()
        timer.cancel()
        if process.terminationStatus != 0 {
            NSLog("[Watchtower] CLI migration failed with exit code \(process.terminationStatus)")
        }
    }

    /// C2: only allow safe workspace names (alphanumeric, hyphens, underscores, dots)
    static func isValidWorkspaceName(_ name: String) -> Bool {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_."))
        return !name.isEmpty
            && name.unicodeScalars.allSatisfy { allowed.contains($0) }
            && !name.hasPrefix(".")
    }

    // MARK: - Starred Items Management

    /// Add a channel to the user's starred channels list
    func addStarredChannel(_ channelID: String, for userID: String) throws {
        try dbPool.write { db in
            let sql = "SELECT starred_channels FROM user_profile WHERE slack_user_id = ?"
            let result: String? = try String.fetchOne(db, sql: sql, arguments: [userID])

            var channels: [String] = []
            if let json = result, !json.isEmpty {
                if let data = json.data(using: .utf8) {
                    channels = (try? JSONDecoder().decode([String].self, from: data)) ?? []
                }
            }

            if !channels.contains(channelID) {
                channels.append(channelID)
            }

            let json = try JSONEncoder().encode(channels)
            let jsonStr = String(data: json, encoding: .utf8) ?? "[]"

            try db.execute(
                sql: "UPDATE user_profile SET starred_channels = ?, updated_at = ? WHERE slack_user_id = ?",
                arguments: [jsonStr, ISO8601DateFormatter().string(from: Date()), userID]
            )
        }
    }

    /// Remove a channel from the user's starred channels list
    func removeStarredChannel(_ channelID: String, for userID: String) throws {
        try dbPool.write { db in
            let sql = "SELECT starred_channels FROM user_profile WHERE slack_user_id = ?"
            let result: String? = try String.fetchOne(db, sql: sql, arguments: [userID])

            var channels: [String] = []
            if let json = result, !json.isEmpty {
                if let data = json.data(using: .utf8) {
                    channels = (try? JSONDecoder().decode([String].self, from: data)) ?? []
                }
            }

            channels.removeAll { $0 == channelID }

            let json = try JSONEncoder().encode(channels)
            let jsonStr = String(data: json, encoding: .utf8) ?? "[]"

            try db.execute(
                sql: "UPDATE user_profile SET starred_channels = ?, updated_at = ? WHERE slack_user_id = ?",
                arguments: [jsonStr, ISO8601DateFormatter().string(from: Date()), userID]
            )
        }
    }

    /// Add a person to the user's starred people list
    func addStarredPerson(_ personUserID: String, for userID: String) throws {
        try dbPool.write { db in
            let result: String? = try String.fetchOne(db, sql: "SELECT starred_people FROM user_profile WHERE slack_user_id = ?", arguments: [userID])

            var people: [String] = []
            if let json = result, !json.isEmpty {
                if let data = json.data(using: .utf8) {
                    people = (try? JSONDecoder().decode([String].self, from: data)) ?? []
                }
            }

            if !people.contains(personUserID) {
                people.append(personUserID)
            }

            let json = try JSONEncoder().encode(people)
            let jsonStr = String(data: json, encoding: .utf8) ?? "[]"

            try db.execute(
                sql: "UPDATE user_profile SET starred_people = ?, updated_at = ? WHERE slack_user_id = ?",
                arguments: [jsonStr, ISO8601DateFormatter().string(from: Date()), userID]
            )
        }
    }

    /// Remove a person from the user's starred people list
    func removeStarredPerson(_ personUserID: String, for userID: String) throws {
        try dbPool.write { db in
            let result: String? = try String.fetchOne(db, sql: "SELECT starred_people FROM user_profile WHERE slack_user_id = ?", arguments: [userID])

            var people: [String] = []
            if let json = result, !json.isEmpty {
                if let data = json.data(using: .utf8) {
                    people = (try? JSONDecoder().decode([String].self, from: data)) ?? []
                }
            }

            people.removeAll { $0 == personUserID }

            let json = try JSONEncoder().encode(people)
            let jsonStr = String(data: json, encoding: .utf8) ?? "[]"

            try db.execute(
                sql: "UPDATE user_profile SET starred_people = ?, updated_at = ? WHERE slack_user_id = ?",
                arguments: [jsonStr, ISO8601DateFormatter().string(from: Date()), userID]
            )
        }
    }
}

enum WatchtowerDatabaseError: LocalizedError {
    case databaseNotFound
    case invalidWorkspaceName(String)
    case schemaVersionTooOld(Int)
    case missingTable(String)

    var errorDescription: String? {
        switch self {
        case .databaseNotFound:
            "Watchtower database not found. Run 'watchtower auth login && watchtower sync' first."
        case .invalidWorkspaceName(let name):
            "Invalid workspace name: '\(name)'"
        case .schemaVersionTooOld(let ver):
            "Database schema version \(ver) is too old. Run 'watchtower sync' to upgrade."
        case .missingTable(let name):
            "Database is missing required table: \(name)"
        }
    }
}
