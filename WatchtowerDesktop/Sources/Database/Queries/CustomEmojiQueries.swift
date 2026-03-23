import GRDB

enum CustomEmojiQueries {
    /// Fetch all custom emojis as a map of name → image URL, resolving aliases.
    static func fetchEmojiMap(_ db: Database) throws -> [String: String] {
        let emojis = try CustomEmoji.fetchAll(db, sql: "SELECT name, url, alias_for FROM custom_emojis")

        // First pass: build name → URL map
        var result = [String: String](minimumCapacity: emojis.count)
        for e in emojis {
            result[e.name] = e.url
        }

        // Second pass: resolve aliases to target URL
        for e in emojis where e.isAlias {
            if let targetURL = result[e.aliasFor] {
                result[e.name] = targetURL
            }
        }

        return result
    }
}
