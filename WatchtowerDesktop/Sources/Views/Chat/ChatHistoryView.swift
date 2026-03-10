import SwiftUI

struct ChatHistoryView: View {
    @Bindable var historyVM: ChatHistoryViewModel
    let onNewChat: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            searchBar
            Divider()
            chatList
        }
        .frame(minWidth: 200, idealWidth: 240)
    }

    private var searchBar: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
                .font(.caption)
            SearchField(text: $historyVM.searchText, placeholder: "Search chats")
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
    }

    private var chatList: some View {
        List(selection: $historyVM.selectedConversationID) {
            ForEach(historyVM.filteredConversations) { conv in
                chatRow(conv)
                    .tag(conv.id)
                    .contextMenu {
                        Button("Delete", role: .destructive) {
                            historyVM.deleteConversation(conv.id)
                        }
                    }
            }
        }
        .listStyle(.plain)
    }

    private func chatRow(_ conv: ChatConversation) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(conv.displayTitle)
                .lineLimit(1)
                .font(.body)
            Text(formatDate(conv.updatedDate))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }

    private func formatDate(_ date: Date) -> String {
        let cal = Calendar.current
        if cal.isDateInToday(date) {
            let fmt = DateFormatter()
            fmt.dateFormat = "HH:mm"
            return fmt.string(from: date)
        } else if cal.isDateInYesterday(date) {
            return "Yesterday"
        } else {
            let fmt = DateFormatter()
            fmt.dateFormat = "MMM d"
            return fmt.string(from: date)
        }
    }
}
