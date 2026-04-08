import Foundation
import SwiftUI
import Yams

// MARK: - Jira Key Extraction

/// Shared regex and utility for extracting Jira issue keys from text.
enum JiraKeyExtractor {
    // swiftlint:disable:next force_try
    static let pattern = try! NSRegularExpression(
        pattern: "\\b([A-Z][A-Z0-9_]+-\\d+)\\b"
    )

    /// Extract unique Jira issue keys from text, preserving order of first appearance.
    static func extractKeys(from text: String) -> [String] {
        let range = NSRange(text.startIndex..., in: text)
        let matches = pattern.matches(in: text, range: range)
        var seen = Set<String>()
        var keys: [String] = []
        for match in matches {
            if let keyRange = Range(match.range(at: 1), in: text) {
                let key = String(text[keyRange])
                if seen.insert(key).inserted {
                    keys.append(key)
                }
            }
        }
        return keys
    }
}

extension String {
    /// Extract unique Jira issue keys (e.g. "PROJ-123") from this string.
    func extractJiraKeys() -> [String] {
        JiraKeyExtractor.extractKeys(from: self)
    }
}

// MARK: - Shared Jira Badges View

/// Renders Jira key badges for keys found in text.
/// Uses JiraBadgeView for keys with loaded issue data, falls back to plain link badges.
struct JiraKeyBadgesView: View {
    let text: String
    let issues: [String: JiraIssue]
    let siteURL: String?
    let isConnected: Bool

    var body: some View {
        let keys = text.extractJiraKeys()
        if isConnected, !keys.isEmpty {
            ForEach(keys, id: \.self) { key in
                if let issue = issues[key] {
                    JiraBadgeView(
                        issue: issue,
                        siteURL: siteURL
                    )
                } else if let siteURL,
                          let url = URL(
                              string: "\(siteURL)/browse/\(key)"
                          ) {
                    Link(destination: url) {
                        HStack(spacing: 3) {
                            Image(systemName: "link")
                                .font(.system(size: 8))
                            Text(key)
                                .font(.caption2)
                                .fontWeight(.medium)
                        }
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            Color.blue.opacity(0.10),
                            in: Capsule()
                        )
                        .foregroundStyle(.blue)
                    }
                    .buttonStyle(.plain)
                    .help("Open \(key) in Jira")
                }
            }
        }
    }
}

/// Variant without issue data — shows plain link badges only.
struct JiraKeyLinkBadgesView: View {
    let text: String
    let siteURL: String?
    let isConnected: Bool

    var body: some View {
        let keys = text.extractJiraKeys()
        if isConnected, !keys.isEmpty, let baseURL = siteURL {
            ForEach(keys, id: \.self) { key in
                if let url = URL(
                    string: "\(baseURL)/browse/\(key)"
                ) {
                    Link(destination: url) {
                        HStack(spacing: 3) {
                            Image(systemName: "link")
                                .font(.system(size: 8))
                            Text(key)
                                .font(.caption2)
                                .fontWeight(.medium)
                        }
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            Color.blue.opacity(0.12),
                            in: Capsule()
                        )
                        .foregroundStyle(.blue)
                    }
                }
            }
        }
    }
}

// MARK: - Jira Config Helpers

enum JiraConfigHelper {
    /// Read siteURL from config YAML without creating a full ConfigService.
    static func readSiteURL() -> String? {
        let configPath = Constants.configPath
        guard let data = FileManager.default.contents(atPath: configPath),
              let str = String(data: data, encoding: .utf8),
              let yaml = try? Yams.load(yaml: str) as? [String: Any],
              let jira = yaml["jira"] as? [String: Any] else {
            return nil
        }
        return jira["site_url"] as? String
    }

    /// Read without_jira_detection feature toggle from config YAML.
    static func readWithoutJiraDetection() -> Bool {
        let configPath = Constants.configPath
        guard let data = FileManager.default.contents(atPath: configPath),
              let str = String(data: data, encoding: .utf8),
              let yaml = try? Yams.load(yaml: str) as? [String: Any],
              let jira = yaml["jira"] as? [String: Any],
              let features = jira["features"] as? [String: Bool] else {
            return false
        }
        return features["without_jira_detection"] ?? false
    }
}
