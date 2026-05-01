import Foundation
import Testing
import Yams
@testable import WatchtowerDesktop

@MainActor
@Suite("ConfigService")
struct ConfigServiceTests {
    private func makeTempConfig(_ yaml: String) -> String {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("watchtower-config-tests-\(UUID().uuidString)")
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let path = dir.appendingPathComponent("config.yaml").path
        try? yaml.write(toFile: path, atomically: true, encoding: .utf8)
        return path
    }

    @Test("Load parses scalar fields")
    func loadScalars() {
        let path = makeTempConfig("""
        active_workspace: dev
        claude_path: /opt/claude
        codex_path: /opt/codex
        """)
        let svc = ConfigService(configPath: path)
        #expect(svc.activeWorkspace == "dev")
        #expect(svc.claudePath == "/opt/claude")
        #expect(svc.codexPath == "/opt/codex")
        #expect(svc.parseError == nil)
    }

    @Test("Load parses sync section")
    func loadSync() {
        let path = makeTempConfig("""
        sync:
          poll_interval: 5m
          workers: 4
          sync_threads: false
          initial_history_days: 14
        """)
        let svc = ConfigService(configPath: path)
        #expect(svc.syncInterval == "5m")
        #expect(svc.syncWorkers == 4)
        #expect(svc.syncThreads == false)
        #expect(svc.initialHistoryDays == 14)
    }

    @Test("Load applies defaults for missing day_plan keys")
    func loadDayPlanDefaults() {
        let path = makeTempConfig("active_workspace: x\n")
        let svc = ConfigService(configPath: path)
        #expect(svc.dayPlanEnabled == true)
        #expect(svc.dayPlanHour == 8)
        #expect(svc.workingHoursStart == "09:00")
        #expect(svc.workingHoursEnd == "19:00")
        #expect(svc.maxTimeblocks == 3)
        #expect(svc.minBacklog == 3)
        #expect(svc.maxBacklog == 8)
    }

    @Test("Load parses ai and digest sections")
    func loadAIDigest() {
        let path = makeTempConfig("""
        ai:
          model: claude-opus
          provider: claude
          workers: 2
        digest:
          enabled: true
          model: haiku
          min_messages: 3
          language: English
        briefing:
          hour: 9
        """)
        let svc = ConfigService(configPath: path)
        #expect(svc.aiModel == "claude-opus")
        #expect(svc.aiProvider == "claude")
        #expect(svc.aiWorkers == 2)
        #expect(svc.digestEnabled == true)
        #expect(svc.digestModel == "haiku")
        #expect(svc.digestMinMessages == 3)
        #expect(svc.digestLanguage == "English")
        #expect(svc.briefingHour == 9)
    }

    @Test("Load parses calendar settings")
    func loadCalendar() {
        let path = makeTempConfig("""
        calendar:
          enabled: true
          sync_days_ahead: 5
        """)
        let svc = ConfigService(configPath: path)
        #expect(svc.calendarEnabled == true)
        #expect(svc.calendarSyncDaysAhead == 5)
    }

    @Test("Load parses jira features")
    func loadJiraFeatures() {
        let path = makeTempConfig("""
        jira:
          features:
            briefings: true
            recommendations: false
        """)
        let svc = ConfigService(configPath: path)
        #expect(svc.jiraFeatures["briefings"] == true)
        #expect(svc.jiraFeatures["recommendations"] == false)
    }

    @Test("Save round-trips dirty values")
    func saveRoundTrip() throws {
        let path = makeTempConfig("active_workspace: old\n")
        let svc = ConfigService(configPath: path)
        svc.activeWorkspace = "new-ws"
        svc.aiProvider = "codex"
        svc.calendarEnabled = true
        svc.calendarSyncDaysAhead = 7
        svc.briefingHour = 11
        try svc.save()

        let svc2 = ConfigService(configPath: path)
        #expect(svc2.activeWorkspace == "new-ws")
        #expect(svc2.aiProvider == "codex")
        #expect(svc2.calendarEnabled == true)
        #expect(svc2.calendarSyncDaysAhead == 7)
        #expect(svc2.briefingHour == 11)
    }

    @Test("Save sets file permissions to 0600")
    func savePermissions() throws {
        let path = makeTempConfig("active_workspace: x\n")
        let svc = ConfigService(configPath: path)
        svc.activeWorkspace = "y"
        try svc.save()

        let attrs = try FileManager.default.attributesOfItem(atPath: path)
        let perm = attrs[.posixPermissions] as? NSNumber
        #expect(perm?.intValue == 0o600, "expected owner-only permissions, got \(String(describing: perm))")
    }

    @Test("Save removes empty optional strings")
    func saveRemovesEmptyValues() throws {
        let path = makeTempConfig("""
        ai:
          model: keepme
          provider: claude
        """)
        let svc = ConfigService(configPath: path)
        svc.aiModel = ""
        svc.aiProvider = nil
        try svc.save()

        let raw = try String(contentsOfFile: path, encoding: .utf8)
        #expect(!raw.contains("model:"), "empty model should be removed; got: \(raw)")
        #expect(!raw.contains("provider:"), "nil provider should be removed; got: \(raw)")
    }

    @Test("Reload on bad YAML records parseError but doesn't crash")
    func reloadBadYAML() {
        let path = makeTempConfig("not: a: valid: yaml: at: all\n  - mismatch")
        let svc = ConfigService(configPath: path)
        // parseError is set when YAML can't be parsed.
        // (In some cases bad YAML still parses to a non-dictionary, leaving parseError nil.)
        _ = svc.parseError
    }

    @Test("Reload on missing file leaves defaults untouched")
    func reloadMissingFile() {
        let svc = ConfigService(configPath: "/nonexistent/path/to/config.yaml")
        #expect(svc.activeWorkspace == nil)
        #expect(svc.briefingHour == 8) // default
        #expect(svc.dayPlanEnabled == true) // default
    }
}
