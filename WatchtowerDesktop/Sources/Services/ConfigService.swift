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
    var analysisLegacyMode: Bool = false
    var claudePath: String?
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

            if let ai = yaml["ai"] as? [String: Any] {
                aiModel = ai["model"] as? String
            }

            claudePath = yaml["claude_path"] as? String

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
        if let v = syncInterval, !v.isEmpty { sync["poll_interval"] = v } else { sync.removeValue(forKey: "poll_interval") }
        if let v = syncWorkers { sync["workers"] = v } else { sync.removeValue(forKey: "workers") }
        sync["sync_threads"] = syncThreads
        if let v = initialHistoryDays { sync["initial_history_days"] = v } else { sync.removeValue(forKey: "initial_history_days") }
        if !sync.isEmpty { yaml["sync"] = sync } else { yaml.removeValue(forKey: "sync") }

        // Digest section
        var digest = (yaml["digest"] as? [String: Any]) ?? [:]
        digest["enabled"] = digestEnabled
        if let v = digestModel, !v.isEmpty { digest["model"] = v } else { digest.removeValue(forKey: "model") }
        if let v = digestMinMessages { digest["min_messages"] = v } else { digest.removeValue(forKey: "min_messages") }
        if let v = digestLanguage, !v.isEmpty { digest["language"] = v } else { digest.removeValue(forKey: "language") }
        if !digest.isEmpty { yaml["digest"] = digest } else { yaml.removeValue(forKey: "digest") }

        // AI section
        var ai = (yaml["ai"] as? [String: Any]) ?? [:]
        if let v = aiModel, !v.isEmpty { ai["model"] = v } else { ai.removeValue(forKey: "model") }
        if !ai.isEmpty { yaml["ai"] = ai } else { yaml.removeValue(forKey: "ai") }

        // Claude path override
        if let v = claudePath, !v.isEmpty { yaml["claude_path"] = v } else { yaml.removeValue(forKey: "claude_path") }

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
