import CryptoKit
import Foundation
import GRDB

struct JiraBoard: Codable, FetchableRecord, TableRecord {
    static let databaseTableName = "jira_boards"
    let id: Int
    var name: String
    var projectKey: String
    var boardType: String        // "scrum" | "kanban" | "simple"
    var isSelected: Bool
    var issueCount: Int
    var syncedAt: String
    // Phase 0b — profile columns
    var rawColumnsJSON: String
    var rawConfigJSON: String
    var llmProfileJSON: String
    var workflowSummary: String
    var userOverridesJSON: String
    var configHash: String
    var profileGeneratedAt: String

    enum CodingKeys: String, CodingKey {
        case id, name
        case projectKey = "project_key"
        case boardType = "board_type"
        case isSelected = "is_selected"
        case issueCount = "issue_count"
        case syncedAt = "synced_at"
        case rawColumnsJSON = "raw_columns_json"
        case rawConfigJSON = "raw_config_json"
        case llmProfileJSON = "llm_profile_json"
        case workflowSummary = "workflow_summary"
        case userOverridesJSON = "user_overrides_json"
        case configHash = "config_hash"
        case profileGeneratedAt = "profile_generated_at"
    }
}

// MARK: - Board Profile Display Models

struct BoardProfileDisplay: Codable {
    let workflowStages: [WorkflowStageDisplay]
    let estimationApproach: EstimationApproachDisplay
    let iterationInfo: IterationInfoDisplay
    let workflowSummary: String
    let staleThresholds: [String: Int]
    let healthSignals: [String]
}

struct WorkflowStageDisplay: Codable, Identifiable {
    var id: String { name }
    let name: String
    let originalStatuses: [String]
    let phase: String   // "backlog"|"active_work"|"review"|"testing"|"done"|"other"
    let isTerminal: Bool
    let typicalDurationSignal: String
}

struct EstimationApproachDisplay: Codable {
    let type: String
    let field: String?
}

struct IterationInfoDisplay: Codable {
    let hasIterations: Bool
    let typicalLengthDays: Int
    let avgThroughput: Int
}

// MARK: - Config Change Detection

extension JiraBoard {
    /// Whether the board's raw configuration has changed since the last analysis.
    /// Mirrors Go's `ComputeConfigHash` algorithm: canonical columns + estimation → SHA256.
    var isConfigChanged: Bool {
        // No profile yet — not a "changed config" case.
        guard !configHash.isEmpty, !llmProfileJSON.isEmpty else {
            return false
        }
        let computed = Self.computeConfigHash(
            rawColumnsJSON: rawColumnsJSON,
            rawConfigJSON: rawConfigJSON
        )
        return !computed.isEmpty && computed != configHash
    }

    /// Compute SHA256 hash of board config, matching Go's ComputeConfigHash.
    /// Input: raw_columns_json = JSON array of {name, statuses: [{name, ...}]}
    ///        raw_config_json  = JSON object with {columns, estimation: {field_id}}
    static func computeConfigHash(
        rawColumnsJSON: String,
        rawConfigJSON: String
    ) -> String {
        guard let colData = rawColumnsJSON.data(using: .utf8),
              let columns = try? JSONDecoder().decode(
                  [RawBoardColumn].self, from: colData
              ) else {
            return ""
        }

        var parts: [String] = []

        // Canonicalize columns — sort status names within each column.
        for col in columns {
            let sortedStatuses = col.statuses
                .map(\.name)
                .sorted()
                .joined(separator: ",")
            parts.append("\(col.name):\(sortedStatuses)")
        }

        // Add estimation field from config.
        if let cfgData = rawConfigJSON.data(using: .utf8),
           let config = try? JSONDecoder().decode(
               RawBoardConfig.self, from: cfgData
           ),
           let est = config.estimation {
            parts.append("est:\(est.fieldID)")
        }

        let data = parts.joined(separator: "|")
        let hash = SHA256.hash(data: Data(data.utf8))
        return hash.map { String(format: "%02x", $0) }.joined()
    }
}

// MARK: - Raw config models for hash computation

private struct RawBoardColumn: Codable {
    let name: String
    let statuses: [RawBoardColumnStatus]
}

private struct RawBoardColumnStatus: Codable {
    let name: String
}

private struct RawBoardConfig: Codable {
    let estimation: RawEstimation?
}

private struct RawEstimation: Codable {
    let fieldID: String

    enum CodingKeys: String, CodingKey {
        case fieldID = "field_id"
    }
}
