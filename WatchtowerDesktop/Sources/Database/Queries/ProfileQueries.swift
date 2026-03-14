import GRDB

enum ProfileQueries {
    static func fetchProfile(_ db: Database, slackUserID: String) throws -> UserProfile? {
        guard try db.tableExists("user_profile") else { return nil }
        return try UserProfile.fetchOne(db, sql: """
            SELECT * FROM user_profile WHERE slack_user_id = ?
            """, arguments: [slackUserID])
    }

    static func fetchCurrentProfile(_ db: Database) throws -> UserProfile? {
        guard try db.tableExists("user_profile") else { return nil }
        guard try db.tableExists("workspace") else { return nil }
        guard let userID = try String.fetchOne(db, sql: """
            SELECT current_user_id FROM workspace LIMIT 1
            """), !userID.isEmpty else { return nil }
        return try fetchProfile(db, slackUserID: userID)
    }

    static func upsertProfile(_ db: Database, profile: UserProfile) throws {
        try db.execute(sql: """
            INSERT INTO user_profile
                (slack_user_id, role, team, responsibilities, reports, peers, manager,
                 starred_channels, starred_people, pain_points, track_focus,
                 onboarding_done, custom_prompt_context, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
            ON CONFLICT(slack_user_id) DO UPDATE SET
                role = excluded.role,
                team = excluded.team,
                responsibilities = excluded.responsibilities,
                reports = excluded.reports,
                peers = excluded.peers,
                manager = excluded.manager,
                starred_channels = excluded.starred_channels,
                starred_people = excluded.starred_people,
                pain_points = excluded.pain_points,
                track_focus = excluded.track_focus,
                onboarding_done = excluded.onboarding_done,
                custom_prompt_context = excluded.custom_prompt_context,
                updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
            """, arguments: [
                profile.slackUserID, profile.role, profile.team,
                profile.responsibilities, profile.reports, profile.peers, profile.manager,
                profile.starredChannels, profile.starredPeople,
                profile.painPoints, profile.trackFocus,
                profile.onboardingDone, profile.customPromptContext,
            ])
    }

    /// Updates a single field on the user profile.
    /// Uses a switch statement mapping field names to explicit SQL to prevent injection.
    static func updateField(_ db: Database, slackUserID: String, field: String, value: String) throws {
        let column: String
        switch field {
        case "role": column = "role"
        case "team": column = "team"
        case "responsibilities": column = "responsibilities"
        case "reports": column = "reports"
        case "peers": column = "peers"
        case "manager": column = "manager"
        case "starred_channels": column = "starred_channels"
        case "starred_people": column = "starred_people"
        case "pain_points": column = "pain_points"
        case "track_focus": column = "track_focus"
        case "custom_prompt_context": column = "custom_prompt_context"
        default: return
        }
        try db.execute(sql: """
            UPDATE user_profile SET \(column) = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
            WHERE slack_user_id = ?
            """, arguments: [value, slackUserID])
    }
}
