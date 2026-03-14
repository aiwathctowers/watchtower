import SwiftUI

/// Chat view for the onboarding flow — AI learns about the user.
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

                Text("Tell us about yourself")
                    .font(.title2)
                    .fontWeight(.semibold)

                Text("Watchtower will personalize your experience based on your role and needs.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .padding(.top, 24)
            .padding(.bottom, 12)
            .padding(.horizontal, 40)

            Divider()

            // Chat messages
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 12) {
                        if viewModel.messages.isEmpty {
                            welcomePrompts
                        }

                        ForEach(viewModel.messages) { msg in
                            MessageBubble(message: msg)
                                .id(msg.id)
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
            }

            Divider()

            // Input area
            HStack(spacing: 8) {
                ChatInput(text: $viewModel.inputText, isStreaming: viewModel.isStreaming) {
                    viewModel.send()
                }

                if canSkip {
                    Button("Continue") {
                        viewModel.finishChat()
                        onComplete()
                    }
                    .buttonStyle(.borderedProminent)
                    .padding(.trailing, 12)
                    .padding(.bottom, 8)
                }
            }
        }
    }

    /// Show "Continue" after at least 2 user messages and no active stream.
    private var canSkip: Bool {
        let userCount = viewModel.messages.filter { $0.role == .user }.count
        return userCount >= 2 && !viewModel.isStreaming
    }

    private var welcomePrompts: some View {
        VStack(spacing: 12) {
            Text("Start by telling the AI about your role")
                .font(.callout)
                .foregroundStyle(.secondary)
                .padding(.top, 20)

            HStack(spacing: 8) {
                quickButton("I'm an Engineering Manager")
                quickButton("I'm a Software Engineer")
                quickButton("I'm a Tech Lead")
            }
            HStack(spacing: 8) {
                quickButton("I'm a Product Manager")
                quickButton("I'm a Designer")
            }
        }
    }

    private func quickButton(_ text: String) -> some View {
        Button(text) {
            viewModel.inputText = text
            viewModel.send()
        }
        .buttonStyle(.bordered)
        .controlSize(.small)
    }
}
