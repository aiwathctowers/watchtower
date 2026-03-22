import SwiftUI

/// Chat view for the onboarding flow — AI learns about the user.
/// Role questions appear as chat bubbles with quick-reply buttons,
/// then transitions to free-form LLM conversation.
struct OnboardingChatView: View {
    @Bindable var viewModel: OnboardingChatViewModel
    let onComplete: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            chatHeader
            chatScrollArea
            chatBottomSection
        }
        .task {
            viewModel.startQuestionnaire()
        }
    }

    private var chatHeader: some View {
        VStack(spacing: 8) {
            Image(systemName: "person.crop.circle.badge.questionmark")
                .font(.system(size: 36))
                .foregroundStyle(.secondary)

            Text(viewModel.loc("header"))
                .font(.title2)
                .fontWeight(.semibold)

            Text(viewModel.loc("subtitle"))
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .padding(.top, 24)
        .padding(.bottom, 20)
        .padding(.horizontal, 40)
    }

    private var chatScrollArea: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    ForEach(viewModel.messages) { msg in
                        MessageBubble(message: msg)
                            .id(msg.id)
                    }

                    if !viewModel.quickReplies.isEmpty {
                        HStack(spacing: 8) {
                            ForEach(viewModel.quickReplies) { reply in
                                Button(reply.label) {
                                    reply.action()
                                }
                                .buttonStyle(.bordered)
                                .controlSize(.regular)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.leading, 12)
                        .id("quick-replies")
                    }

                    if let error = viewModel.errorMessage {
                        Text(error)
                            .font(.callout)
                            .foregroundStyle(.red)
                            .padding(8)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(.red.opacity(0.1), in: RoundedRectangle(cornerRadius: 8))
                    }

                    Spacer()
                        .id("bottom")
                }
                .padding()
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .onChange(of: viewModel.messages.count) {
                withAnimation {
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }
            .onChange(of: viewModel.quickReplies.isEmpty) {
                if !viewModel.quickReplies.isEmpty {
                    withAnimation {
                        proxy.scrollTo("quick-replies", anchor: .bottom)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var chatBottomSection: some View {
        if viewModel.quickReplies.isEmpty {
            VStack(spacing: 0) {
                if viewModel.chatReady {
                    Button {
                        Task {
                            await viewModel.finishChat()
                            onComplete()
                        }
                    } label: {
                        if viewModel.isExtractingProfile {
                            HStack(spacing: 8) {
                                ProgressView()
                                    .controlSize(.small)
                                Text("Analyzing...")
                            }
                            .font(.headline)
                            .frame(maxWidth: .infinity)
                        } else {
                            Label(viewModel.loc("continue"), systemImage: "arrow.right.circle.fill")
                                .font(.headline)
                                .frame(maxWidth: .infinity)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .disabled(viewModel.isExtractingProfile)
                    .padding(.horizontal, 40)
                    .padding(.top, 12)
                    .padding(.bottom, 4)
                }

                ChatInput(text: $viewModel.inputText, isStreaming: viewModel.isStreaming) {
                    viewModel.send()
                }
            }
        }
    }

}
