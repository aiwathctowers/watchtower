import XCTest
import SwiftUI
import ViewInspector
@testable import WatchtowerDesktop

@MainActor
final class JiraBadgeViewTests: XCTestCase {

    // MARK: - Compact mode

    /// Compact mode рендерит issue key как Text.
    func testCompactShowsIssueKey() throws {
        let view = JiraBadgeView(
            issueKey: "PROJ-123",
            status: "In Progress",
            statusCategory: "in_progress",
            priority: "medium",
            siteURL: nil
        )

        XCTAssertNoThrow(try view.inspect().find(text: "PROJ-123"))
    }

    /// Compact mode без siteURL — нет Link, только обычный label.
    func testCompactWithoutSiteURLHasNoLink() throws {
        let view = JiraBadgeView(
            issueKey: "PROJ-1",
            status: "Done",
            statusCategory: "done",
            priority: "low",
            siteURL: nil
        )

        // Нет Link в дереве — find бросает.
        XCTAssertThrowsError(try view.inspect().find(ViewType.Link.self))
    }

    /// Compact mode c siteURL — рендерит Link на /browse/<key>.
    func testCompactWithSiteURLRendersLink() throws {
        let view = JiraBadgeView(
            issueKey: "PROJ-42",
            status: "To Do",
            statusCategory: "new",
            priority: "high",
            siteURL: "https://acme.atlassian.net"
        )

        let link = try view.inspect().find(ViewType.Link.self)
        XCTAssertEqual(try link.url().absoluteString,
                       "https://acme.atlassian.net/browse/PROJ-42")
    }

    /// Trailing slash в siteURL не должен ломать URL.
    func testCompactSiteURLWithTrailingSlash() throws {
        let view = JiraBadgeView(
            issueKey: "X-1",
            status: "Open",
            statusCategory: "new",
            priority: "",
            siteURL: "https://acme.atlassian.net/"
        )

        let link = try view.inspect().find(ViewType.Link.self)
        XCTAssertEqual(try link.url().absoluteString,
                       "https://acme.atlassian.net/browse/X-1")
    }

    // MARK: - Expanded mode

    /// Expanded mode показывает и issue key, и текст статуса.
    func testExpandedShowsKeyAndStatus() throws {
        let view = JiraBadgeView(
            issueKey: "PROJ-7",
            status: "In Review",
            statusCategory: "in_progress",
            priority: "high",
            siteURL: nil,
            isExpanded: true
        )
        let inspected = try view.inspect()

        XCTAssertNoThrow(try inspected.find(text: "PROJ-7"))
        XCTAssertNoThrow(try inspected.find(text: "In Review"))
    }

    /// Expanded mode без приоритета — priority text "" — иконка приоритета не появляется.
    /// (negative-test для priorityIcon)
    func testExpandedWithoutPriorityHasNoPriorityIcon() throws {
        let view = JiraBadgeView(
            issueKey: "PROJ-1",
            status: "Open",
            statusCategory: "new",
            priority: "",
            siteURL: nil,
            isExpanded: true
        )
        // Priority empty → блок `if !priority.isEmpty` не отрабатывает, иконка не строится.
        // Image со статус-категорией specific systemNames не должно быть кроме statusDot,
        // но statusDot — это Circle, не Image, так что Images быть не должно.
        XCTAssertThrowsError(try view.inspect().find(ViewType.Image.self))
    }
}
