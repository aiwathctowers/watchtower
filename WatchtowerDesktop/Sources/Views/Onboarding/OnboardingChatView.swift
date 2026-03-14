import SwiftUI

/// Chat view for the onboarding flow — AI learns about the user.
/// Role questions appear as chat bubbles with quick-reply buttons,
/// then transitions to free-form LLM conversation.
struct OnboardingChatView: View {
    @Bindable var viewModel: OnboardingChatViewModel
    let onComplete: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            // Header
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

            // Chat messages + inline quick-reply buttons
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 12) {
                        ForEach(viewModel.messages) { msg in
                            MessageBubble(message: msg)
                                .id(msg.id)
                        }

                        // Quick-reply buttons inline, right after last message
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
                    }
                    .padding()
                }
                .onChange(of: viewModel.messages.count) {
                    if let last = viewModel.messages.last {
                        proxy.scrollTo(last.id, anchor: .bottom)
                    }
                }
                .onChange(of: viewModel.quickReplies.isEmpty) {
                    if !viewModel.quickReplies.isEmpty {
                        proxy.scrollTo("quick-replies", anchor: .bottom)
                    }
                }
            }

            // Bottom section: text input (during chat) or completion button (after chat)
            if !viewModel.chatReady && viewModel.quickReplies.isEmpty {
                HStack(spacing: 8) {
                    // Show ChatInput only if we can't skip yet
                    if !canSkip {
                        ChatInput(text: $viewModel.inputText, isStreaming: viewModel.isStreaming) {
                            viewModel.send()
                        }
                    }

                    // Show Continue button if we can skip
                    if canSkip {
                        Button(viewModel.loc("continue")) {
                            viewModel.finishChat()
                            onComplete()
                        }
                        .buttonStyle(.borderedProminent)
                        .padding(.trailing, 12)
                        .padding(.bottom, 8)
                    }
                }
            } else if viewModel.chatReady {
                VStack(spacing: 16) {
                    VStack(spacing: 8) {
                        Image(systemName: "checkmark.circle.fill")
                            .font(.system(size: 40))
                            .foregroundStyle(.green)

                        Text(viewModel.loc("header"))
                            .font(.title3)
                            .fontWeight(.semibold)

                        Text(viewModel.loc("subtitle"))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                    .padding(.top, 20)

                    Button {
                        viewModel.finishChat()
                        onComplete()
                    } label: {
                        Label(viewModel.loc("continue"), systemImage: "arrow.right.circle.fill")
                            .font(.headline)
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .padding(.horizontal, 40)
                    .padding(.bottom, 20)
                }
            }
        }
        .task {
            viewModel.startQuestionnaire()
        }
        .onChange(of: viewModel.chatReady) {
            if viewModel.chatReady {
                viewModel.finishChat()
                onComplete()
            }
        }
    }

    /// Show "Continue" after at least 1 user message to AI (not counting questionnaire answers)
    /// and no active stream.
    private var canSkip: Bool {
        viewModel.isRoleDetermined && !viewModel.isStreaming &&
        viewModel.messages.filter({ $0.role == .user }).count > questionAnswerCount
    }

    /// Number of user messages that are questionnaire answers (not free-form chat).
    private var questionAnswerCount: Int {
        if !viewModel.hasAnsweredRoleQ1 { return 0 }
        if !viewModel.hasAnsweredRoleQ2 { return 1 }
        if viewModel.shouldShowRoleQ3 && !viewModel.hasAnsweredRoleQ3 { return 2 }
        return viewModel.shouldShowRoleQ3 ? 3 : 2
    }
}
