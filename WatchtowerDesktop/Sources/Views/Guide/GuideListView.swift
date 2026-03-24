import SwiftUI

// NOTE: This view is no longer used — the Guide tab has been merged into People.
// Kept for backwards compatibility.
struct GuideListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: GuideViewModel?
    @State private var selectedUserID: String?
    @State private var searchText = ""

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let userID = selectedUserID,
                   let guide = vm.guides.first(where: { $0.userID == userID }) {
                    Divider()
                    GuideDetailView(
                        guide: guide,
                        userName: vm.userName(for: userID),
                    ) { selectedUserID = nil }
                    .id(userID)
                    .frame(minWidth: 400, idealWidth: 500)
                    .transition(.move(edge: .trailing).combined(with: .opacity))
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .animation(.easeInOut(duration: 0.25), value: selectedUserID)
        .navigationTitle("Communication Guide")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = GuideViewModel(dbManager: db)
                viewModel?.load()
            }
        }
    }

    private var filteredGuides: [CommunicationGuide] {
        guard let vm = viewModel else { return [] }
        let excluding = vm.guides.filter { $0.userID != vm.currentUserID }
        if searchText.isEmpty { return excluding }
        let query = searchText.lowercased()
        return excluding.filter { guide in
            let name = vm.userName(for: guide.userID).lowercased()
            return name.contains(query)
        }
    }

    @ViewBuilder
    private func listPanel(_ vm: GuideViewModel) -> some View {
        VStack(spacing: 0) {
            // Window picker
            if vm.availableWindows.count > 1 {
                HStack {
                    Button {
                        if vm.selectedWindow < vm.availableWindows.count - 1 {
                            vm.loadWindow(at: vm.selectedWindow + 1)
                        }
                    } label: {
                        Image(systemName: "chevron.left")
                    }
                    .disabled(vm.selectedWindow >= vm.availableWindows.count - 1)

                    Text(vm.currentWindowLabel)
                        .font(.headline)
                        .frame(maxWidth: .infinity)

                    Button {
                        if vm.selectedWindow > 0 {
                            vm.loadWindow(at: vm.selectedWindow - 1)
                        }
                    } label: {
                        Image(systemName: "chevron.right")
                    }
                    .disabled(vm.selectedWindow <= 0)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
            }

            // Team summary
            if let gs = vm.guideSummary {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Team Communication")
                        .font(.subheadline.bold())
                    Text(gs.summary)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                    if !gs.parsedTips.isEmpty {
                        ForEach(gs.parsedTips.prefix(2), id: \.self) { tip in
                            Label(tip, systemImage: "lightbulb")
                                .font(.caption)
                                .foregroundStyle(.orange)
                                .lineLimit(2)
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(.ultraThinMaterial)

                Divider()
            }

            // Search
            TextField("Search people...", text: $searchText)
                .textFieldStyle(.roundedBorder)
                .padding(.horizontal, 16)
                .padding(.vertical, 8)

            // Guide list
            List(selection: $selectedUserID) {
                ForEach(filteredGuides) { guide in
                    guideRow(guide, vm: vm)
                        .tag(guide.userID)
                }
            }
            .listStyle(.sidebar)
        }
        .frame(minWidth: 280, idealWidth: 320)
    }

    @ViewBuilder
    private func guideRow(_ guide: CommunicationGuide, vm: GuideViewModel) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("@\(vm.userName(for: guide.userID))")
                    .font(.body.bold())
                Spacer()
                Text("\(guide.messageCount) msgs")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if !guide.summary.isEmpty {
                Text(guide.summary)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            if !guide.parsedSituationalTactics.isEmpty {
                Label("\(guide.parsedSituationalTactics.count) tactics", systemImage: "lightbulb")
                    .font(.caption2)
                    .foregroundStyle(.orange)
            }
        }
        .padding(.vertical, 2)
    }
}
