import SwiftUI

struct ChatView: View {
    @Environment(AppState.self) private var appState

    var body: some View {
        Group {
            if let chatVM = appState.chatViewModel, let historyVM = appState.chatHistoryViewModel {
                ChatSplitView(chatVM: chatVM, historyVM: historyVM)
            } else {
                ProgressView()
            }
        }
        .onAppear { appState.ensureChatViewModels() }
    }
}

/// Extracted so that @State (historyWidth, showHistory) lives here — survives tab switches
/// because AppState keeps the VMs alive and this view just re-renders around them.
private struct ChatSplitView: View {
    @Bindable var chatVM: ChatViewModel
    let historyVM: ChatHistoryViewModel
    @State private var showHistory = true
    @State private var historyWidth: CGFloat = 240
    @State private var showDeleteConfirmation = false

    var body: some View {
        HStack(spacing: 0) {
            // Main chat area (left)
            VStack(spacing: 0) {
                // Toolbar row
                HStack(spacing: 8) {
                    Picker("Model", selection: $chatVM.selectedModel) {
                        ForEach(ChatModel.allCases) { model in
                            Text(model.displayName).tag(model)
                        }
                    }
                    .pickerStyle(.menu)
                    .frame(width: 130)
                    .disabled(chatVM.isStreaming)

                    Button {
                        createNewChat()
                    } label: {
                        Image(systemName: "plus.message")
                    }
                    .keyboardShortcut("n", modifiers: .command)
                    .help("New Chat")

                    if chatVM.conversationID != nil {
                        Button(role: .destructive) {
                            showDeleteConfirmation = true
                        } label: {
                            Image(systemName: "trash")
                                .foregroundStyle(.secondary)
                        }
                        .buttonStyle(.borderless)
                        .keyboardShortcut(.delete, modifiers: .command)
                        .help("Delete Chat")
                    }

                    Spacer()

                    Button {
                        withAnimation(.easeInOut(duration: 0.2)) {
                            showHistory.toggle()
                        }
                    } label: {
                        Image(systemName: "sidebar.trailing")
                            .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                    .help("Toggle Chat History")
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 6)

                Divider()

                chatContent
            }
            .frame(maxWidth: .infinity)

            // Right panel: chat history
            if showHistory {
                ResizeHandle { delta in
                    historyWidth = min(max(historyWidth - delta, 160), 400)
                }

                Divider()

                ChatHistoryView(historyVM: historyVM) {
                    createNewChat()
                }
                .frame(width: historyWidth)
                .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        }
        .onChange(of: historyVM.selectedConversationID) { _, newID in
            if let newID, let conv = historyVM.conversations.first(where: { $0.id == newID }) {
                chatVM.bind(to: conv)
            }
        }
        // L6: confirmation dialog before deleting chat
        .alert("Delete Chat?", isPresented: $showDeleteConfirmation) {
            Button("Delete", role: .destructive) { deleteCurrentChat() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This conversation will be permanently deleted.")
        }
    }

    private func createNewChat() {
        guard let conv = historyVM.createConversation() else { return }
        chatVM.newChat()
        chatVM.bind(to: conv)
    }

    private func deleteCurrentChat() {
        guard let id = chatVM.conversationID else { return }
        chatVM.cancelStream()
        historyVM.deleteConversation(id)
        chatVM.newChat()
        // Switch to the next available conversation, or leave empty
        if let next = historyVM.conversations.first {
            chatVM.bind(to: next)
        }
    }

    private var chatContent: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 12) {
                        if chatVM.messages.isEmpty && chatVM.conversationID != nil {
                            quickPrompts
                        } else if chatVM.messages.isEmpty {
                            emptyState
                        }

                        ForEach(chatVM.messages) { msg in
                            MessageBubble(message: msg)
                                .id(msg.id)
                        }

                        if let error = chatVM.errorMessage {
                            Text(error)
                                .font(.callout)
                                .foregroundStyle(.red)
                                .padding(8)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(.red.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
                        }
                    }
                    .padding()
                }
                .onChange(of: chatVM.messages.count) {
                    if let last = chatVM.messages.last {
                        proxy.scrollTo(last.id, anchor: .bottom)
                    }
                }
            }

            Divider()
            ChatInput(
                text: $chatVM.inputText,
                isStreaming: chatVM.isStreaming,
                onSend: {
                    if chatVM.conversationID == nil {
                        if let conv = historyVM.createConversation() {
                            chatVM.bind(to: conv)
                        }
                    }
                    chatVM.send()
                },
                onStop: { chatVM.cancelStream() }
            )
        }
    }

    private var emptyState: some View {
        VStack(spacing: 12) {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 40))
                .foregroundStyle(.secondary)
            Text("Start a new chat")
                .font(.title3)
                .foregroundStyle(.secondary)
            Text("Press \(Image(systemName: "command")) N or click \"+\" to begin")
                .font(.callout)
                .foregroundStyle(.tertiary)
        }
        .padding(.top, 60)
    }

    private var quickPrompts: some View {
        VStack(spacing: 8) {
            Text("Ask about your workspace")
                .font(.title2)
                .foregroundStyle(.secondary)
                .padding(.top, 40)

            HStack(spacing: 8) {
                quickPromptButton("What happened today?")
                quickPromptButton("Any decisions?")
                quickPromptButton("Summarize activity")
            }
            .padding(.bottom, 20)
        }
    }

    private func quickPromptButton(_ text: String) -> some View {
        Button(text) {
            chatVM.inputText = text
            chatVM.send()
        }
        .buttonStyle(.bordered)
    }
}
