import GRDB

struct CustomEmoji: FetchableRecord, Decodable, Identifiable {
    let name: String
    let url: String
    let aliasFor: String

    var id: String { name }

    /// True if this emoji is an alias for another custom emoji.
    var isAlias: Bool { !aliasFor.isEmpty }

    enum CodingKeys: String, CodingKey {
        case name, url
        case aliasFor = "alias_for"
    }
}
