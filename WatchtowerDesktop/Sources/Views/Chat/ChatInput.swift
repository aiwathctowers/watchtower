import SwiftUI
import AppKit

struct ChatInput: View {
    @Binding var text: String
    let isStreaming: Bool
    let onSend: () -> Void
    var onStop: (() -> Void)? = nil
    var placeholder: String = "Ask about your workspace..."
    @State private var inputHeight: CGFloat = 22

    var body: some View {
        HStack(alignment: .bottom, spacing: 6) {
            ZStack(alignment: .topLeading) {
                if text.isEmpty {
                    Text(placeholder)
                        .foregroundStyle(.placeholder)
                        .padding(.leading, 5)
                        .padding(.top, 4)
                        .allowsHitTesting(false)
                }

                ExpandingTextInput(
                    text: $text,
                    height: $inputHeight,
                    onSubmit: {
                        guard canSend else { return }
                        onSend()
                    }
                )
                .frame(height: inputHeight)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .background(
                RoundedRectangle(cornerRadius: 18)
                    .fill(Color(.textBackgroundColor))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 18)
                    .strokeBorder(Color(.separatorColor).opacity(0.3), lineWidth: 0.5)
            )

            Button {
                if isStreaming {
                    onStop?()
                } else {
                    onSend()
                }
            } label: {
                Image(systemName: isStreaming ? "stop.circle.fill" : "arrow.up.circle.fill")
                    .font(.system(size: 24))
                    .foregroundStyle(buttonActive ? Color.accentColor : Color(.tertiaryLabelColor))
            }
            .buttonStyle(.borderless)
            .disabled(!buttonActive)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private var buttonActive: Bool {
        isStreaming ? onStop != nil : canSend
    }
}

// MARK: - Auto-expanding native text input
// Enter sends, Shift+Enter inserts a newline.

private struct ExpandingTextInput: NSViewRepresentable {
    @Binding var text: String
    @Binding var height: CGFloat
    var onSubmit: () -> Void

    private let minHeight: CGFloat = 20
    private let maxHeight: CGFloat = 120

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSTextView.scrollableTextView()
        let textView = scrollView.documentView as! NSTextView

        textView.delegate = context.coordinator
        textView.font = .systemFont(ofSize: NSFont.systemFontSize)
        textView.isRichText = false
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false
        textView.allowsUndo = true
        textView.textContainerInset = NSSize(width: 0, height: 2)
        textView.textContainer?.lineFragmentPadding = 4
        textView.drawsBackground = false

        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        scrollView.drawsBackground = false
        scrollView.borderType = .noBorder

        textView.postsFrameChangedNotifications = true
        NotificationCenter.default.addObserver(
            context.coordinator,
            selector: #selector(Coordinator.frameDidChange(_:)),
            name: NSView.frameDidChangeNotification,
            object: textView
        )

        return scrollView
    }

    func updateNSView(_ scrollView: NSScrollView, context: Context) {
        let textView = scrollView.documentView as! NSTextView
        if textView.string != text {
            textView.string = text
            DispatchQueue.main.async {
                context.coordinator.recalculateHeight(textView)
            }
        }
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: ExpandingTextInput

        init(_ parent: ExpandingTextInput) {
            self.parent = parent
        }

        deinit {
            NotificationCenter.default.removeObserver(self)
        }

        func textDidChange(_ notification: Notification) {
            guard let textView = notification.object as? NSTextView else { return }
            parent.text = textView.string
            recalculateHeight(textView)
        }

        func textView(_ textView: NSTextView, doCommandBy sel: Selector) -> Bool {
            if sel == #selector(NSResponder.insertNewline(_:)) {
                let event = NSApp.currentEvent
                if event?.modifierFlags.contains(.shift) == true {
                    textView.insertNewlineIgnoringFieldEditor(nil)
                    return true
                }
                // Plain Enter → send
                parent.onSubmit()
                return true
            }
            return false
        }

        func recalculateHeight(_ textView: NSTextView) {
            guard let layoutManager = textView.layoutManager,
                  let textContainer = textView.textContainer else { return }
            layoutManager.ensureLayout(for: textContainer)
            let usedRect = layoutManager.usedRect(for: textContainer)
            let inset = textView.textContainerInset
            let newHeight = usedRect.height + inset.height * 2
            let clamped = min(max(newHeight, parent.minHeight), parent.maxHeight)
            if abs(parent.height - clamped) > 0.5 {
                parent.height = clamped
            }
        }

        @objc func frameDidChange(_ notification: Notification) {
            guard let textView = notification.object as? NSTextView else { return }
            recalculateHeight(textView)
        }
    }
}
