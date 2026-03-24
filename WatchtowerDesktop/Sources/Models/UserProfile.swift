import Foundation
import GRDB

struct UserProfile: FetchableRecord, Identifiable {
    let id: Int
    let slackUserID: String
    var role: String
    var team: String
    var responsibilities: String   // JSON array of strings
    var reports: String            // JSON array of Slack user_ids
    var peers: String              // JSON array of Slack user_ids
    var manager: String            // Slack user_id
    var starredChannels: String    // JSON array of channel_ids
    var starredPeople: String      // JSON array of Slack user_ids
    var painPoints: String         // JSON array from onboarding
    var trackFocus: String         // JSON array of focus areas
    var onboardingDone: Bool
    var customPromptContext: String
    let createdAt: String
    let updatedAt: String

    init(row: Row) {
        id = row["id"]
        slackUserID = row["slack_user_id"]
        role = row["role"] ?? ""
        team = row["team"] ?? ""
        responsibilities = row["responsibilities"] ?? "[]"
        reports = row["reports"] ?? "[]"
        peers = row["peers"] ?? "[]"
        manager = row["manager"] ?? ""
        starredChannels = row["starred_channels"] ?? "[]"
        starredPeople = row["starred_people"] ?? "[]"
        painPoints = row["pain_points"] ?? "[]"
        trackFocus = row["track_focus"] ?? "[]"
        onboardingDone = row["onboarding_done"] ?? false
        customPromptContext = row["custom_prompt_context"] ?? ""
        createdAt = row["created_at"] ?? ""
        updatedAt = row["updated_at"] ?? ""
    }

    init(
        id: Int = 0,
        slackUserID: String,
        role: String = "",
        team: String = "",
        responsibilities: String = "[]",
        reports: String = "[]",
        peers: String = "[]",
        manager: String = "",
        starredChannels: String = "[]",
        starredPeople: String = "[]",
        painPoints: String = "[]",
        trackFocus: String = "[]",
        onboardingDone: Bool = false,
        customPromptContext: String = "",
        createdAt: String = "",
        updatedAt: String = ""
    ) {
        self.id = id
        self.slackUserID = slackUserID
        self.role = role
        self.team = team
        self.responsibilities = responsibilities
        self.reports = reports
        self.peers = peers
        self.manager = manager
        self.starredChannels = starredChannels
        self.starredPeople = starredPeople
        self.painPoints = painPoints
        self.trackFocus = trackFocus
        self.onboardingDone = onboardingDone
        self.customPromptContext = customPromptContext
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    // MARK: - JSON Helpers

    var decodedReports: [String] {
        decodeJSONArray(reports)
    }

    var decodedPeers: [String] {
        decodeJSONArray(peers)
    }

    var decodedStarredChannels: [String] {
        decodeJSONArray(starredChannels)
    }

    var decodedStarredPeople: [String] {
        decodeJSONArray(starredPeople)
    }

    var decodedResponsibilities: [String] {
        decodeJSONArray(responsibilities)
    }

    var decodedPainPoints: [String] {
        decodeJSONArray(painPoints)
    }

    var decodedTrackFocus: [String] {
        decodeJSONArray(trackFocus)
    }

    private func decodeJSONArray(_ json: String) -> [String] {
        guard !json.isEmpty,
              let data = json.data(using: .utf8) else { return [] }
        return (try? JSONDecoder().decode([String].self, from: data)) ?? []
    }
}

extension UserProfile: Equatable {
    static func == (lhs: UserProfile, rhs: UserProfile) -> Bool {
        lhs.slackUserID == rhs.slackUserID &&
        lhs.role == rhs.role &&
        lhs.team == rhs.team &&
        lhs.responsibilities == rhs.responsibilities &&
        lhs.reports == rhs.reports &&
        lhs.peers == rhs.peers &&
        lhs.manager == rhs.manager &&
        lhs.starredChannels == rhs.starredChannels &&
        lhs.starredPeople == rhs.starredPeople &&
        lhs.painPoints == rhs.painPoints &&
        lhs.trackFocus == rhs.trackFocus &&
        lhs.onboardingDone == rhs.onboardingDone &&
        lhs.customPromptContext == rhs.customPromptContext
    }
}

// MARK: - Role Level Enum

enum RoleLevel: String, Codable {
    case topManagement = "top_management"
    case directionOwner = "direction_owner"
    case middleManagement = "middle_management"
    case seniorIC = "senior_ic"
    case ic = "ic"

    var displayName: String {
        switch self {
        case .topManagement: "Top Management"
        case .directionOwner: "Direction Owner"
        case .middleManagement: "Middle Management"
        case .seniorIC: "Senior IC"
        case .ic: "Individual Contributor"
        }
    }

    var shortDescription: String {
        switch self {
        case .topManagement: "Sets organizational strategy"
        case .directionOwner: "Owns and executes strategy in an area"
        case .middleManagement: "Manages team and coordinates execution"
        case .seniorIC: "High technical influence, no direct reports"
        case .ic: "Solves tasks in their domain"
        }
    }
}

// Helper to determine role from onboarding answers
struct RoleDetermination {
    let reportsToThem: Bool      // Q1: "People report to you?"
    let setStrategy: Bool        // Q2: "You set strategy?" (only if Q1=true)
    let manageManagers: Bool     // Q3: "Do you manage other managers?" (only if Q1=true AND Q2=true)
    let influenceType: String?   // Q2b: "expertise" or "tasks" (only if Q1=false)

    var roleLevel: RoleLevel {
        if reportsToThem {
            if setStrategy {
                // Q1=true, Q2=true → need Q3
                return manageManagers ? .topManagement : .directionOwner
            } else {
                // Q1=true, Q2=false
                return .middleManagement
            }
        } else {
            // Q1=false → check Q2b influence type
            if let influenceType = influenceType {
                return influenceType == "expertise" ? .seniorIC : .ic
            }
            // Q2b not answered yet, default to IC
            return .ic
        }
    }

    static func forIC() -> Self {
        Self(reportsToThem: false, setStrategy: false, manageManagers: false, influenceType: "tasks")
    }
}
