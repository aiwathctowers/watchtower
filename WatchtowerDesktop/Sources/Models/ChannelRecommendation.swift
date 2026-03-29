import Foundation

struct ChannelRecommendation: Identifiable {
    enum Action: String {
        case mute
        case leave
        case favorite
    }

    var id: String { "\(action.rawValue)-\(channelID)" }
    let channelID: String
    let channelName: String
    let action: Action
    let reason: String
}
