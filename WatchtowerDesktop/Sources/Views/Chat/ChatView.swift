import SwiftUI

struct ChatView: View {
    @Environment(AppState.self) private var appState
    @State private var chatVM: ChatViewModel?
    @State private var historyVM: ChatHistoryViewModel?
    @State private var showHistory = true
    @State private var historyWidth: CGFloat = 240

    var body: some View {
        Group {
            if let chatVM, let historyVM {
                chatSplitView(chatVM: chatVM, historyVM: historyVM)
            } else {
                ProgressView()
            }
        }
        .onAppear { setup() }
    }

    private func setup() {
        guard let db = appState.databaseManager, chatVM == nil else { return }
        let cvm = ChatViewModel(claudeService: ClaudeService(), dbManager: db)
        let hvm = ChatHistoryViewModel(dbManager: db)
        hvm.load()

        cvm.onConversationUpdated = { [weak hvm] convID, title, sessionID in
            guard let hvm else { return }
            if let title { hvm.updateTitle(convID, title: title) }
            if let sessionID { hvm.updateSessionID(convID, sessionID: sessionID) }
            if title == nil && sessionID == nil { hvm.touch(convID) }
        }

        chatVM = cvm
        historyVM = hvm
    }

    private func chatSplitView(chatVM: ChatViewModel, historyVM: ChatHistoryViewModel) -> some View {
        HStack(spacing: 0) {
            // Main chat area (left)
            VStack(spacing: 0) {
                // Toolbar row
                HStack(spacing: 8) {
                    Picker("Model", selection: Bindable(chatVM).selectedModel) {
                        ForEach(ChatModel.allCases) { model in
                            Text(model.displayName).tag(model)
                        }
                    }
                    .pickerStyle(.menu)
                    .frame(width: 130)
                    .disabled(chatVM.isStreaming)

                    Button {
                        createNewChat(chatVM: chatVM, historyVM: historyVM)
                    } label: {
                        Image(systemName: "plus.message")
                    }
                    .keyboardShortcut("n", modifiers: .command)
                    .help("New Chat")

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

                chatContent(chatVM)
            }
            .frame(maxWidth: .infinity)

            // Right panel: chat history
            if showHistory {
                // Resize handle
                ResizeHandle { delta in
                    historyWidth = min(max(historyWidth - delta, 160), 400)
                }

                Divider()

                ChatHistoryView(historyVM: historyVM) {
                    createNewChat(chatVM: chatVM, historyVM: historyVM)
                }
                .frame(width: historyWidth)
                .transition(.move(edge: .trailing).combined(with: .opacity))
            }
        }
        .onChange(of: historyVM.selectedConversationID) { oldID, newID in
            switchConversation(chatVM: chatVM, historyVM: historyVM, from: oldID, to: newID)
        }
    }

    private func switchConversation(chatVM: ChatViewModel, historyVM: ChatHistoryViewModel, from oldID: Int64?, to newID: Int64?) {
        guard let newID, let conv = historyVM.conversations.first(where: { $0.id == newID }) else {
            return
        }
        chatVM.bind(to: conv)
    }

    private func createNewChat(chatVM: ChatViewModel, historyVM: ChatHistoryViewModel) {
        guard let conv = historyVM.createConversation() else { return }
        chatVM.newChat()
        chatVM.bind(to: conv)
    }

    private func chatContent(_ vm: ChatViewModel) -> some View {
        @Bindable var vm = vm
        return VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 12) {
                        if vm.messages.isEmpty && vm.conversationID != nil {
                            quickPrompts(vm)
                        } else if vm.messages.isEmpty {
                            emptyState
                        }

                        ForEach(vm.messages) { msg in
                            MessageBubble(message: msg)
                                .id(msg.id)
                        }

                        if let error = vm.errorMessage {
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
                .onChange(of: vm.messages.count) {
                    if let last = vm.messages.last {
                        proxy.scrollTo(last.id, anchor: .bottom)
                    }
                }
            }

            Divider()
            ChatInput(text: $vm.inputText, isStreaming: vm.isStreaming) {
                if vm.conversationID == nil {
                    if let historyVM, let conv = historyVM.createConversation() {
                        vm.bind(to: conv)
                    }
                }
                vm.send()
            }
            .disabled(vm.conversationID == nil && historyVM == nil)
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

    private func quickPrompts(_ vm: ChatViewModel) -> some View {
        VStack(spacing: 8) {
            Text("Ask about your workspace")
                .font(.title2)
                .foregroundStyle(.secondary)
                .padding(.top, 40)

            HStack(spacing: 8) {
                quickPromptButton("What happened today?", vm: vm)
                quickPromptButton("Any decisions?", vm: vm)
                quickPromptButton("Summarize activity", vm: vm)
            }
            .padding(.bottom, 20)
        }
    }

    private func quickPromptButton(_ text: String, vm: ChatViewModel) -> some View {
        Button(text) {
            vm.inputText = text
            vm.send()
        }
        .buttonStyle(.bordered)
    }
}
