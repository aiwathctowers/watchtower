import SwiftUI

struct SearchView: View {
    @Environment(AppState.self) private var appState
    @State private var viewModel: SearchViewModel?

    var body: some View {
        Group {
            if let vm = viewModel {
                searchContent(vm)
            } else {
                ProgressView()
            }
        }
        .navigationTitle("Search")
        .onAppear {
            if let db = appState.databaseManager, viewModel == nil {
                viewModel = SearchViewModel(dbManager: db)
            }
        }
    }

    private func searchContent(_ vm: SearchViewModel) -> some View {
        @Bindable var vm = vm
        return VStack(spacing: 0) {
            // Search field
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                SearchField(text: $vm.query, placeholder: "Search messages...")
                    .frame(height: 22)
                    .onChange(of: vm.query) { vm.search() }

                if vm.isSearching {
                    ProgressView()
                        .controlSize(.small)
                }
            }
            .padding(12)
            .background(Color(nsColor: .windowBackgroundColor))

            Divider()

            // Results
            if vm.results.isEmpty && !vm.query.isEmpty && !vm.isSearching {
                VStack(spacing: 8) {
                    Image(systemName: "magnifyingglass")
                        .font(.title)
                        .foregroundStyle(.secondary)
                    Text("No results for \"\(vm.query)\"")
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if vm.results.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "text.magnifyingglass")
                        .font(.title)
                        .foregroundStyle(.secondary)
                    Text("Type to search across all messages")
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List(vm.results) { result in
                    SearchResultRow(
                        result: result,
                        slackChannelURL: vm.slackChannelURL(channelID: result.channelID)
                    )
                }

                if vm.results.count >= 100 {
                    Text("Showing first 100 results")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(8)
                }
            }
        }
    }
}
