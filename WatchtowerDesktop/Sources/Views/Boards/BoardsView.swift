import SwiftUI

struct BoardsView: View {
    @Environment(AppState.self) private var appState
    @State private var jiraConnected = JiraQueries.isConnected()
    @State private var activeDetail: BoardsNavItem?

    var body: some View {
        if jiraConnected {
            connectedContent
        } else {
            notConnectedPlaceholder
        }
    }

    // MARK: - Connected

    @ViewBuilder
    private var connectedContent: some View {
        if let detail = activeDetail {
            detailContent(detail)
                .transition(.move(edge: .trailing).combined(with: .opacity))
        } else {
            mainList
                .transition(.move(edge: .leading).combined(with: .opacity))
        }
    }

    private var mainList: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Boards")
                    .font(.title2)
                    .fontWeight(.bold)
                Spacer()
            }
            .padding()
            .background(Color(nsColor: .controlBackgroundColor))

            Divider()

            Form {
                JiraBoardsSettingsView(
                    onSelectBoard: { board in
                        withAnimation(.easeInOut(duration: 0.2)) {
                            activeDetail = .board(board)
                        }
                    }
                )
                .environment(appState)

                JiraFeaturesSettingsView(
                    onSelectFeatures: {
                        withAnimation(.easeInOut(duration: 0.2)) {
                            activeDetail = .features
                        }
                    }
                )

                JiraUserMappingSettingsView()
                    .environment(appState)
                JiraSyncInfoView()
                    .environment(appState)
            }
            .formStyle(.grouped)
            .scrollContentBackground(.hidden)
            .background(Color(nsColor: .controlBackgroundColor))
        }
    }

    @ViewBuilder
    private func detailContent(_ item: BoardsNavItem) -> some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        activeDetail = nil
                    }
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: "chevron.left")
                        Text("Boards")
                    }
                    .foregroundStyle(Color.accentColor)
                }
                .buttonStyle(.plain)

                Text(detailTitle(item))
                    .font(.title2)
                    .fontWeight(.bold)

                Spacer()
            }
            .padding()
            .background(Color(nsColor: .controlBackgroundColor))

            Divider()

            switch item {
            case .board(let board):
                JiraBoardProfileView(board: board)
                    .scrollContentBackground(.hidden)
            case .features:
                JiraFeaturesDetailView()
                    .scrollContentBackground(.hidden)
            }
        }
        .background(Color(nsColor: .controlBackgroundColor))
    }

    private func detailTitle(_ item: BoardsNavItem) -> String {
        switch item {
        case .board(let board): board.name
        case .features: "Jira Features"
        }
    }

    // MARK: - Not Connected

    private var notConnectedPlaceholder: some View {
        VStack(spacing: 16) {
            Spacer()

            Image(systemName: "rectangle.on.rectangle.angled")
                .font(.system(size: 48))
                .foregroundStyle(.secondary)

            Text("No board sources connected")
                .font(.title3)
                .fontWeight(.medium)

            Text("Connect Jira in Settings to start tracking boards.")
                .font(.callout)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)

            Button("Open Settings") {
                NSApp.sendAction(
                    Selector(("showSettingsWindow:")),
                    to: nil, from: nil
                )
            }
            .buttonStyle(.borderedProminent)

            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }
}

// MARK: - Navigation

enum BoardsNavItem: Hashable {
    case board(JiraBoard)
    case features
}
