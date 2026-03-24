import SwiftUI

/// Team form shown after the onboarding chat.
/// Lets the user pick their reports, manager, and key peers from synced Slack users.
struct OnboardingTeamFormView: View {
    @Bindable var viewModel: OnboardingChatViewModel
    let onComplete: () -> Void

    // Manager as single-select (wrap in array for picker, then extract)
    @State private var managerIDs: [String] = []

    var body: some View {
        VStack(spacing: 0) {
            // Header
            VStack(spacing: 8) {
                Image(systemName: "person.3.fill")
                    .font(.system(size: 36))
                    .foregroundStyle(.secondary)

                Text("Set up your team")
                    .font(.title2)
                    .fontWeight(.semibold)

                Text("This helps Watchtower understand who's who and assign track ownership correctly.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .padding(.vertical, 24)
            .padding(.horizontal, 40)

            Divider()

            // Form
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    // Role & Team (editable, pre-filled from chat)
                    GroupBox("About You") {
                        VStack(alignment: .leading, spacing: 12) {
                            LabeledContent("Role") {
                                TextField("e.g. Engineering Manager", text: $viewModel.role)
                                    .textFieldStyle(.roundedBorder)
                                    .frame(maxWidth: 250)
                            }
                            LabeledContent("Team") {
                                TextField("e.g. Platform", text: $viewModel.team)
                                    .textFieldStyle(.roundedBorder)
                                    .frame(maxWidth: 250)
                            }
                        }
                        .padding(8)
                    }

                    // People pickers
                    GroupBox {
                        VStack(alignment: .leading, spacing: 20) {
                            SlackUserPicker(
                                title: "My Reports",
                                allUsers: viewModel.allUsers,
                                selectedIDs: $viewModel.reportIDs
                            )

                            Divider()

                            // Manager — single select via array binding
                            SlackUserPicker(
                                title: "I Report To",
                                allUsers: viewModel.allUsers,
                                selectedIDs: $managerIDs
                            )

                            Divider()

                            SlackUserPicker(
                                title: "Key Peers",
                                allUsers: viewModel.allUsers,
                                selectedIDs: $viewModel.peerIDs
                            )
                        }
                        .padding(8)
                    }

                    if viewModel.allUsers.isEmpty {
                        HStack(spacing: 6) {
                            ProgressView()
                                .controlSize(.small)
                            Text("Waiting for Slack sync to load users...")
                                .font(.callout)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .padding(24)
                .frame(maxWidth: 500)
            }
            .frame(maxWidth: .infinity)

            Divider()

            // Bottom bar
            HStack {
                Spacer()
                Button("Done") {
                    // Sync manager from array to single ID
                    viewModel.managerID = managerIDs.first ?? ""
                    onComplete()
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .keyboardShortcut(.return, modifiers: .command)
            }
            .padding(16)
        }
        .onAppear {
            if !viewModel.managerID.isEmpty {
                managerIDs = [viewModel.managerID]
            }
        }
        .onChange(of: managerIDs) { _, newValue in
            // Keep only the last selected (single-select behavior)
            if newValue.count > 1, let last = newValue.last {
                managerIDs = [last]
            }
        }
    }
}
