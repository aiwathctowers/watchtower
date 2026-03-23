import SwiftUI

struct PeopleListView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: PeopleViewModel?
    @State private var selectedUserID: String?
    @State private var searchText = ""

    var body: some View {
        HStack(spacing: 0) {
            if let vm = viewModel {
                listPanel(vm)

                if let userID = selectedUserID,
                   let analysis = vm.analyses.first(where: { $0.userID == userID }) {
                    Divider()
                    PersonDetailView(
                        analysis: analysis,
                        userName: vm.userName(for: userID),
                        history: vm.userHistory(userID: userID),
                        userNameResolver: { vm.userName(for: $0) },
                        onClose: { selectedUserID = nil }
                    )
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
        .navigationTitle("People")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = PeopleViewModel(dbManager: db)
                viewModel?.load()
            }
        }
    }

    private var filteredAnalyses: [UserAnalysis] {
        guard let vm = viewModel else { return [] }
        if searchText.isEmpty { return vm.analyses }
        let q = searchText.lowercased()
        return vm.analyses.filter { a in
            let name = vm.userName(for: a.userID).lowercased()
            if name.contains(q) { return true }
            if a.summary.lowercased().contains(q) { return true }
            if a.communicationStyle.lowercased().contains(q) { return true }
            if a.decisionRole.lowercased().contains(q) { return true }
            return false
        }
    }

    private func listPanel(_ vm: PeopleViewModel) -> some View {
        VStack(spacing: 0) {
            // Window picker
            if vm.availableWindows.count > 1 {
                HStack {
                    Button(action: {
                        let next = vm.selectedWindow + 1
                        if next < vm.availableWindows.count {
                            vm.loadWindow(at: next)
                        }
                    }) {
                        Image(systemName: "chevron.left")
                    }
                    .disabled(vm.selectedWindow >= vm.availableWindows.count - 1)

                    Spacer()
                    Text(vm.currentWindowLabel)
                        .font(.headline)
                    Spacer()

                    Button(action: {
                        let prev = vm.selectedWindow - 1
                        if prev >= 0 {
                            vm.loadWindow(at: prev)
                        }
                    }) {
                        Image(systemName: "chevron.right")
                    }
                    .disabled(vm.selectedWindow <= 0)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
            } else {
                Text(vm.currentWindowLabel)
                    .font(.headline)
                    .padding(.vertical, 8)
            }

            // Summary stats
            HStack(spacing: 16) {
                StatBadge(value: "\(vm.analyses.count)", label: "users")
                if vm.redFlagCount > 0 {
                    StatBadge(value: "\(vm.redFlagCount)", label: "flags", color: .red)
                }
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 6)

            // Search
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                SearchField(
                    text: $searchText,
                    placeholder: "Filter people..."
                )
                .frame(height: 22)
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            // User list
            ScrollView {
                LazyVStack(spacing: 0) {
                    // Period summary
                    if let ps = vm.periodSummary, searchText.isEmpty {
                        VStack(alignment: .leading, spacing: 8) {
                            Text("Team Summary")
                                .font(.caption)
                                .fontWeight(.bold)
                                .foregroundStyle(.secondary)

                            Text(ps.summary)
                                .font(.subheadline)
                                .lineLimit(4)

                            let attention = ps.parsedAttention
                            if !attention.isEmpty {
                                ForEach(attention, id: \.self) { item in
                                    HStack(alignment: .top, spacing: 4) {
                                        Image(systemName: "exclamationmark.circle.fill")
                                            .foregroundStyle(.orange)
                                            .font(.caption)
                                        Text(item)
                                            .font(.caption)
                                    }
                                }
                            }
                        }
                        .padding(10)
                        .background(Color.orange.opacity(0.05), in: RoundedRectangle(cornerRadius: 8))
                        .padding(.horizontal, 8)
                        .padding(.bottom, 8)

                        Divider()
                            .padding(.horizontal, 8)
                    }

                    ForEach(filteredAnalyses) { analysis in
                        personRow(analysis, vm: vm)
                            .contentShape(Rectangle())
                            .onTapGesture {
                                selectedUserID = analysis.userID
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(
                                selectedUserID == analysis.userID
                                    ? Color.accentColor.opacity(0.15)
                                    : Color.clear,
                                in: RoundedRectangle(cornerRadius: 6)
                            )
                            .padding(.horizontal, 4)
                    }
                }
                .padding(.vertical, 4)
            }
        }
        .frame(minWidth: 300, idealWidth: 360)
    }

    private func personRow(_ analysis: UserAnalysis, vm: PeopleViewModel) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(analysis.styleEmoji)
                Text("@\(vm.userName(for: analysis.userID))")
                    .fontWeight(.medium)
                Spacer()

                if analysis.hasConcerns {
                    Image(systemName: "exclamationmark.circle.fill")
                        .foregroundStyle(.orange)
                        .font(.caption)
                }
                if analysis.hasRedFlags {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .font(.caption)
                }

                Text("\(analysis.messageCount) msgs")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            HStack(spacing: 8) {
                Text(analysis.communicationStyle)
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(Color.accentColor.opacity(0.1), in: Capsule())

                Text(analysis.decisionRole)
                    .font(.caption)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(Color.secondary.opacity(0.1), in: Capsule())

                if analysis.volumeChangePct != 0 {
                    Text(String(format: "%+.0f%%", analysis.volumeChangePct))
                        .font(.caption)
                        .foregroundStyle(analysis.volumeChangePct < -30 ? .red : .secondary)
                }
            }

            if !analysis.summary.isEmpty {
                Text(analysis.summary)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .padding(.vertical, 4)
    }
}

struct StatBadge: View {
    let value: String
    let label: String
    var color: Color = .accentColor

    var body: some View {
        HStack(spacing: 4) {
            Text(value)
                .fontWeight(.bold)
                .foregroundStyle(color)
            Text(label)
                .foregroundStyle(.secondary)
        }
        .font(.caption)
    }
}
