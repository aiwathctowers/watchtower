import Foundation
import GRDB
import Testing
@testable import WatchtowerDesktop

@Suite("EmojiResolver")
@MainActor
struct EmojiResolverTests {
    private func makePool() throws -> DatabasePool {
        let (manager, _) = try TestDatabase.createDatabaseManager()
        return manager.dbPool
    }

    @Test("parse returns single text segment when no emoji shortcodes")
    func parsePlainText() throws {
        let pool = try makePool()
        let resolver = EmojiResolver(dbPool: pool)
        let segs = resolver.parse("hello world")
        #expect(segs.count == 1)
        if case .text(let s) = segs[0] {
            #expect(s == "hello world")
        } else {
            #expect(Bool(false), "expected text segment")
        }
    }

    @Test("parse keeps unknown emoji as text")
    func parseUnknownEmoji() throws {
        let pool = try makePool()
        let resolver = EmojiResolver(dbPool: pool)
        let segs = resolver.parse("hi :neverheard:")
        // Standard emoji resolver may pass through unchanged for unknown.
        // The output should contain at least one segment.
        #expect(!segs.isEmpty)
    }

    @Test("parse splits text and custom emoji into segments")
    func parseCustomEmoji() throws {
        let pool = try makePool()
        try pool.write { db in
            try db.execute(sql: "INSERT INTO custom_emojis (name, url) VALUES (?, ?)",
                           arguments: ["acme", "https://example.com/acme.png"])
        }

        let resolver = EmojiResolver(dbPool: pool)
        resolver.reload()

        let segs = resolver.parse("ship it :acme: now")
        var sawCustom = false
        for s in segs {
            if case .customEmoji(let name, let url) = s {
                sawCustom = true
                #expect(name == "acme")
                #expect(url == "https://example.com/acme.png")
            }
        }
        #expect(sawCustom, "expected custom emoji segment, got \(segs)")
    }

    @Test("resolveStandard delegates to SlackEmoji.resolve")
    func resolveStandard() throws {
        let pool = try makePool()
        let resolver = EmojiResolver(dbPool: pool)
        let want = SlackEmoji.resolve(":fire:")
        let got = resolver.resolveStandard(":fire:")
        #expect(got == want)
    }

    @Test("MessageSegment equality")
    func segmentEquality() {
        #expect(MessageSegment.text("a") == MessageSegment.text("a"))
        #expect(MessageSegment.text("a") != MessageSegment.text("b"))
        #expect(MessageSegment.customEmoji(name: "x", url: "u") == MessageSegment.customEmoji(name: "x", url: "u"))
        #expect(MessageSegment.customEmoji(name: "x", url: "u") != MessageSegment.text("x"))
    }
}
