import AppKit
import Foundation
import Yams

@MainActor
@Observable
final class ConfigService {
    var activeWorkspace: String?
    var syncInterval: String?
    var syncWorkers: Int?
    var syncThreads: Bool = true
    var initialHistoryDays: Int?
    var digestEnabled: Bool = false
    var digestModel: String?
    var digestMinMessages: Int?
    var digestLanguage: String?
    var aiModel: String?
    var aiWorkers: Int?
    var analysisLegacyMode: Bool = false
    var briefingHour: Int = 8
    var aiProvider: String?
    var claudePath: String?
    var codexPath: String?
    var calendarEnabled: Bool = false
    var calendarSyncDaysAhead: Int = 2
    var jiraFeatures: [String: Bool] = [:]
    var parseError: String?

    private let configPath: String
    /// Raw YAML dictionary — preserved for round-trip editing
    private var rawYAML: [String: Any] = [:]

    init() {
        configPath = Constants.configPath
        reload()
    }

    func reload() {
        guard let data = FileManager.default.contents(atPath: configPath),
              let str = String(data: data, encoding: .utf8) else {
            return
        }

        do {
            guard let yaml = try Yams.load(yaml: str) as? [String: Any] else { return }
            rawYAML = yaml

            activeWorkspace = yaml["active_workspace"] as? String

            if let sync = yaml["sync"] as? [String: Any] {
                syncInterval = sync["poll_interval"] as? String
                    ?? (sync["poll_interval"].flatMap { "\($0)" })
                syncWorkers = sync["workers"] as? Int
                syncThreads = (sync["sync_threads"] as? Bool) ?? true
                initialHistoryDays = sync["initial_history_days"] as? Int
            }

            if let digest = yaml["digest"] as? [String: Any] {
                digestEnabled = (digest["enabled"] as? Bool) ?? false
                digestModel = digest["model"] as? String
                digestMinMessages = digest["min_messages"] as? Int
                digestLanguage = digest["language"] as? String
            }

            if let analysis = yaml["analysis"] as? [String: Any] {
                analysisLegacyMode = (analysis["legacy_mode"] as? Bool) ?? false
            }

            if let briefing = yaml["briefing"] as? [String: Any] {
                briefingHour = (briefing["hour"] as? Int) ?? 8
            }

            if let ai = yaml["ai"] as? [String: Any] {
                aiModel = ai["model"] as? String
                aiWorkers = ai["workers"] as? Int
                aiProvider = ai["provider"] as? String
            }

            claudePath = yaml["claude_path"] as? String
            codexPath = yaml["codex_path"] as? String

            if let calendar = yaml["calendar"] as? [String: Any] {
                calendarEnabled = (calendar["enabled"] as? Bool) ?? false
                calendarSyncDaysAhead = (calendar["sync_days_ahead"] as? Int) ?? 2
            }

            if let jira = yaml["jira"] as? [String: Any],
               let features = jira["features"] as? [String: Bool] {
                jiraFeatures = features
            } else {
                jiraFeatures = [:]
            }

            parseError = nil
        } catch {
            parseError = error.localizedDescription
        }
    }

    func save() throws {
        var yaml = rawYAML

        yaml["active_workspace"] = activeWorkspace

        // Sync section
        var sync = (yaml["sync"] as? [String: Any]) ?? [:]
        if let val = syncInterval, !val.isEmpty { sync["poll_interval"] = val } else { sync.removeValue(forKey: "poll_interval") }
        if let val = syncWorkers { sync["workers"] = val } else { sync.removeValue(forKey: "workers") }
        sync["sync_threads"] = syncThreads
        if let val = initialHistoryDays { sync["initial_history_days"] = val } else { sync.removeValue(forKey: "initial_history_days") }
        if !sync.isEmpty { yaml["sync"] = sync } else { yaml.removeValue(forKey: "sync") }

        // Digest section
        var digest = (yaml["digest"] as? [String: Any]) ?? [:]
        digest["enabled"] = digestEnabled
        if let val = digestModel, !val.isEmpty { digest["model"] = val } else { digest.removeValue(forKey: "model") }
        if let val = digestMinMessages { digest["min_messages"] = val } else { digest.removeValue(forKey: "min_messages") }
        if let val = digestLanguage, !val.isEmpty { digest["language"] = val } else { digest.removeValue(forKey: "language") }
        if !digest.isEmpty { yaml["digest"] = digest } else { yaml.removeValue(forKey: "digest") }

        // Briefing section
        var briefing = (yaml["briefing"] as? [String: Any]) ?? [:]
        briefing["hour"] = briefingHour
        yaml["briefing"] = briefing

        // AI section
        var ai = (yaml["ai"] as? [String: Any]) ?? [:]
        if let val = aiModel, !val.isEmpty { ai["model"] = val } else { ai.removeValue(forKey: "model") }
        if let val = aiWorkers { ai["workers"] = val } else { ai.removeValue(forKey: "workers") }
        if let val = aiProvider, !val.isEmpty { ai["provider"] = val } else { ai.removeValue(forKey: "provider") }
        if !ai.isEmpty { yaml["ai"] = ai } else { yaml.removeValue(forKey: "ai") }

        // Calendar section
        var calendarDict = (yaml["calendar"] as? [String: Any]) ?? [:]
        calendarDict["enabled"] = calendarEnabled
        calendarDict["sync_days_ahead"] = calendarSyncDaysAhead
        yaml["calendar"] = calendarDict

        // Claude path override
        if let val = claudePath, !val.isEmpty { yaml["claude_path"] = val } else { yaml.removeValue(forKey: "claude_path") }

        // Codex path override
        if let val = codexPath, !val.isEmpty { yaml["codex_path"] = val } else { yaml.removeValue(forKey: "codex_path") }

        let output = try Yams.dump(object: yaml, allowUnicode: true)

        let dir = (configPath as NSString).deletingLastPathComponent
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try output.write(toFile: configPath, atomically: true, encoding: .utf8)
        // Restrict permissions to owner-only (config contains Slack token)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: configPath)

        rawYAML = yaml
    }

    func openInEditor() {
        let url = URL(fileURLWithPath: configPath)
        NSWorkspace.shared.open(url)
    }

    func revealInFinder() {
        let url = URL(fileURLWithPath: configPath)
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }
}
