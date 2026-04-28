import Foundation
import GRDB

/// Builds `TargetPrefill` values from in-app source records. Async methods
/// open a single short read transaction; the sub-item method is sync because
/// the parent is already loaded in memory.
///
/// On a missing related entity (channel renamed, user not synced yet, etc.)
/// builders fall back to a softer label (channel id, raw user id) instead of
/// throwing. They only throw on hard DB errors.
enum TargetPrefillBuilder {

    // MARK: - fromSubItem

    /// Synchronous — the `parent` target is already loaded by the caller.
    static func fromSubItem(parent: Target, subItem: TargetSubItem, index: Int) -> TargetPrefill {
        var lines: [String] = []
        lines.append("Sub-target of #\(parent.id) «\(parent.text)».")

        let parentIntent = parent.intent.trimmingCharacters(in: .whitespacesAndNewlines)
        if !parentIntent.isEmpty {
            lines.append("Parent context: \(parentIntent)")
        }

        let siblings = parent.decodedSubItems.enumerated().compactMap { (i, item) -> String? in
            guard i != index, !item.done else { return nil }
            let trimmed = item.text.trimmingCharacters(in: .whitespacesAndNewlines)
            return trimmed.isEmpty ? nil : trimmed
        }
        if !siblings.isEmpty {
            let bulleted = siblings.prefix(5).map { "  • \($0)" }.joined(separator: "\n")
            lines.append("Sibling sub-items:\n\(bulleted)")
        }

        return TargetPrefill(
            text: subItem.text,
            intent: lines.joined(separator: "\n"),
            sourceType: "promoted_subitem",
            sourceID: "\(parent.id):\(index)",
            secondaryLinks: [],
            parentID: parent.id
        )
    }
}
