import Foundation

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
