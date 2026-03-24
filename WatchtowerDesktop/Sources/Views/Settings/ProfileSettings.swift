import SwiftUI

struct ProfileSettings: View {
    @Environment(AppState.self) private var appState

    @State private var profile: UserProfile?
    @State private var allUsers: [User] = []
    @State private var allChannels: [Channel] = []
    @State private var isLoading = false
    @State private var isSaving = false
    @State private var errorMessage: String?
    @State private var showSaved = false

    // Editable fields
    @State private var role = ""
    @State private var team = ""
    @State private var manager = ""
    @State private var reports: [String] = []
    @State private var peers: [String] = []
    @State private var starredChannels: [String] = []
    @State private var starredPeople: [String] = []

    var body: some View {
        Form {
            if isLoading {
                HStack {
                    Spacer()
                    ProgressView()
                    Spacer()
                }
            } else if let error = errorMessage {
                Section {
                    Text(error)
                        .foregroundStyle(.red)
                }
            } else {
                aboutSection
                teamSection
                starredSection
                onboardingSection
            }
        }
        .formStyle(.grouped)
        .padding(.horizontal)
        .padding(.top, 4)
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 0) {
                Divider()
                HStack {
                    Spacer()
                    if showSaved {
                        Text("Saved")
                            .foregroundStyle(.green)
                            .transition(.opacity)
                    }
                    Button("Save") {
                        save()
                    }
                    .keyboardShortcut("s", modifiers: .command)
                    .buttonStyle(.borderedProminent)
                    .disabled(isSaving)
                }
                .padding(.horizontal)
                .padding(.vertical, 10)
            }
            .background(Color(nsColor: .windowBackgroundColor))
        }
        .onAppear { load() }
    }

    // MARK: - Sections

    @ViewBuilder
    private var aboutSection: some View {
        Section("About You") {
            TextField("Role", text: $role, prompt: Text("e.g. Engineering Manager, IC, Tech Lead"))
            TextField("Team", text: $team, prompt: Text("e.g. Platform, Backend, Mobile"))
        }
    }

    @ViewBuilder
    private var teamSection: some View {
        Section("Team") {
            // Manager (single user picker)
            HStack {
                Text("Manager")
                    .font(.headline)
                Spacer()
                Picker("", selection: $manager) {
                    Text("None").tag("")
                    ForEach(allUsers.filter { !$0.isBot }) { user in
                        Text(user.bestName).tag(user.id)
                    }
                }
                .frame(maxWidth: 200)
            }

            SlackUserPicker(title: "My Reports", allUsers: allUsers, selectedIDs: $reports)
            SlackUserPicker(title: "Key Peers", allUsers: allUsers, selectedIDs: $peers)
        }
    }

    @ViewBuilder
    private var starredSection: some View {
        Section("Starred (Extra Attention)") {
            ChannelPicker(title: "Starred Channels", allChannels: allChannels, selectedIDs: $starredChannels)
            SlackUserPicker(title: "Starred People", allUsers: allUsers, selectedIDs: $starredPeople)
        }
    }

    @ViewBuilder
    private var onboardingSection: some View {
        Section {
            Button("Re-run Onboarding") {
                appState.startOnboarding()
            }
            .foregroundStyle(.secondary)
        } footer: {
            Text("Re-run the onboarding chat to update your role, pain points, and tracking preferences.")
        }
    }

    // MARK: - Data Loading

    private func load() {
        guard let db = appState.databaseManager else { return }
        isLoading = true
        Task {
            do {
                let result: (UserProfile?, [User], [Channel]) = try await db.dbPool.read { dbConn in
                    let prof = try ProfileQueries.fetchCurrentProfile(dbConn)
                    let users = try UserQueries.fetchAll(dbConn)
                    let channels = try ChannelQueries.fetchAll(dbConn)
                    return (prof, users, channels)
                }
                allUsers = result.1
                allChannels = result.2
                if let prof = result.0 {
                    profile = prof
                    role = prof.role
                    team = prof.team
                    manager = prof.manager
                    reports = prof.decodedReports
                    peers = prof.decodedPeers
                    starredChannels = prof.decodedStarredChannels
                    starredPeople = prof.decodedStarredPeople
                }
                isLoading = false
            } catch {
                errorMessage = error.localizedDescription
                isLoading = false
            }
        }
    }

    // MARK: - Save

    private func save() {
        guard let db = appState.databaseManager else { return }

        // Get current user ID from workspace
        guard let slackUserID = getCurrentUserID(db: db) else {
            errorMessage = "No workspace found. Run sync first."
            return
        }

        isSaving = true
        let encoder = JSONEncoder()
        func encode(_ arr: [String]) -> String {
            (try? String(data: encoder.encode(arr), encoding: .utf8)) ?? "[]"
        }

        let updated = UserProfile(
            id: profile?.id ?? 0,
            slackUserID: slackUserID,
            role: role,
            team: team,
            responsibilities: profile?.responsibilities ?? "[]",
            reports: encode(reports),
            peers: encode(peers),
            manager: manager,
            starredChannels: encode(starredChannels),
            starredPeople: encode(starredPeople),
            painPoints: profile?.painPoints ?? "[]",
            trackFocus: profile?.trackFocus ?? "[]",
            onboardingDone: profile?.onboardingDone ?? false,
            customPromptContext: profile?.customPromptContext ?? ""
        )

        Task {
            do {
                try await db.dbPool.write { dbConn in
                    try ProfileQueries.upsertProfile(dbConn, profile: updated)
                }
                isSaving = false
                withAnimation { showSaved = true }
                try? await Task.sleep(for: .seconds(2))
                withAnimation { showSaved = false }
            } catch {
                errorMessage = "Save failed: \(error.localizedDescription)"
                isSaving = false
            }
        }
    }

    private func getCurrentUserID(db: DatabaseManager) -> String? {
        try? db.dbPool.read { dbConn in
            try String.fetchOne(dbConn, sql: "SELECT current_user_id FROM workspace LIMIT 1")
        }
    }

}
