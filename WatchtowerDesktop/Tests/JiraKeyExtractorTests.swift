import Foundation
import Testing
@testable import WatchtowerDesktop

@Suite("JiraKeyExtractor")
struct JiraKeyExtractorTests {
    @Test("extracts a single key")
    func single() {
        let got = JiraKeyExtractor.extractKeys(from: "see ABC-123 for details")
        #expect(got == ["ABC-123"])
    }

    @Test("extracts multiple distinct keys preserving order")
    func multiple() {
        let got = JiraKeyExtractor.extractKeys(from: "Linked PROJ-1, then ABC-2 and PROJ-1 again")
        #expect(got == ["PROJ-1", "ABC-2"])
    }

    @Test("returns empty list when no keys present")
    func empty() {
        #expect(JiraKeyExtractor.extractKeys(from: "no jira here").isEmpty)
        #expect(JiraKeyExtractor.extractKeys(from: "").isEmpty)
    }

    @Test("ignores lowercase or invalid prefixes")
    func ignoresInvalid() {
        // Lowercase prefix is rejected.
        #expect(JiraKeyExtractor.extractKeys(from: "abc-123 here").isEmpty)
        // Single-letter prefix is rejected by [A-Z][A-Z0-9_]+
        #expect(JiraKeyExtractor.extractKeys(from: "A-1").isEmpty)
        // Trailing letters are not part of the key.
        let got = JiraKeyExtractor.extractKeys(from: "ABC-12X")
        #expect(got.isEmpty, "letter immediately after digits should not be a key")
    }

    @Test("supports underscores and digits in project")
    func underscoresDigits() {
        let got = JiraKeyExtractor.extractKeys(from: "PROJ_2-100 plus AB12-3")
        #expect(got == ["PROJ_2-100", "AB12-3"])
    }

    @Test("String extension delegates to extractor")
    func stringExtension() {
        #expect("PROJ-1 and ABC-2".extractJiraKeys() == ["PROJ-1", "ABC-2"])
    }
}
