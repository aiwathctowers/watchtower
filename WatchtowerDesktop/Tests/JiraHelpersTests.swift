import Foundation
import SwiftUI
import Testing
@testable import WatchtowerDesktop

@Suite("JiraHelpers")
struct JiraHelpersTests {
    @Test("avatarColor is deterministic for the same user")
    func avatarColorDeterministic() {
        let c1 = JiraHelpers.avatarColor(for: "U123")
        let c2 = JiraHelpers.avatarColor(for: "U123")
        #expect(c1 == c2, "same input → same color")
    }

    @Test("avatarColor handles empty input without crashing")
    func avatarColorEmpty() {
        _ = JiraHelpers.avatarColor(for: "")
    }

    @Test("browseURL constructs canonical Jira URL")
    func browseURLValid() {
        let url = JiraHelpers.browseURL(siteURL: "https://acme.atlassian.net", issueKey: "ABC-1")
        #expect(url?.absoluteString == "https://acme.atlassian.net/browse/ABC-1")
    }

    @Test("browseURL trims trailing slash")
    func browseURLStripsTrailingSlash() {
        let url = JiraHelpers.browseURL(siteURL: "https://acme.atlassian.net/", issueKey: "PROJ-9")
        #expect(url?.absoluteString == "https://acme.atlassian.net/browse/PROJ-9")
    }

    @Test("browseURL returns nil for empty site")
    func browseURLEmpty() {
        #expect(JiraHelpers.browseURL(siteURL: nil, issueKey: "X-1") == nil)
        #expect(JiraHelpers.browseURL(siteURL: "", issueKey: "X-1") == nil)
    }

    @Test("shortDate handles YYYY-MM-DD")
    func shortDateDateOnly() {
        let got = JiraHelpers.shortDate("2026-04-02")
        #expect(got == "02 Apr")
    }

    @Test("shortDate handles ISO8601 with fractional seconds")
    func shortDateISOFraction() {
        let got = JiraHelpers.shortDate("2026-04-02T10:00:00.123+0000")
        #expect(got == "02 Apr")
    }

    @Test("shortDate handles ISO8601 without fractional seconds")
    func shortDateISO() {
        let got = JiraHelpers.shortDate("2026-04-02T10:00:00Z")
        #expect(got == "02 Apr")
    }

    @Test("shortDate falls back to first 10 chars on garbage")
    func shortDateFallback() {
        let got = JiraHelpers.shortDate("garbage-input")
        #expect(got == "garbage-in")
    }

    @Test("shortDate returns empty for empty input")
    func shortDateEmpty() {
        #expect(JiraHelpers.shortDate("") == "")
    }

    @Test("daysSince returns non-negative count")
    func daysSinceNonNegative() {
        let got = JiraHelpers.daysSince("2020-01-01")
        #expect(got > 0, "should be a positive number of days")
    }

    @Test("daysSince clamps future dates to 0")
    func daysSinceFutureClamped() {
        let future = ISO8601DateFormatter().string(from: Date().addingTimeInterval(86400 * 30))
        let got = JiraHelpers.daysSince(future)
        #expect(got == 0)
    }

    @Test("daysSince empty string is 0")
    func daysSinceEmpty() {
        #expect(JiraHelpers.daysSince("") == 0)
    }

    @Test("daysSince invalid format is 0")
    func daysSinceInvalid() {
        #expect(JiraHelpers.daysSince("garbage") == 0)
    }

    @Test("constants are sane")
    func constants() {
        #expect(JiraHelpers.staleThresholdDays > 0)
        #expect(JiraHelpers.velocityWindowDays > 0)
        #expect(JiraHelpers.blockedRatioThreshold > 0 && JiraHelpers.blockedRatioThreshold <= 1)
        #expect(JiraHelpers.progressAtRiskThreshold > 0 && JiraHelpers.progressAtRiskThreshold <= 1)
    }
}
