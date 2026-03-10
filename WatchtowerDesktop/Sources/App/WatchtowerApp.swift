import SwiftUI
import AppKit

@main
struct WatchtowerApp: App {
    @State private var appState = AppState()

    init() {
        NSApplication.shared.setActivationPolicy(.regular)
        NSApplication.shared.activate(ignoringOtherApps: true)
    }

    var body: some Scene {
        WindowGroup {
            NavigationRoot()
                .environment(appState)
                .frame(minWidth: 800, minHeight: 600)
                .background(OpaqueWindowBackground())
                .onAppear {
                    appState.initialize()
                }
        }
        .defaultSize(width: 1200, height: 800)

        Settings {
            SettingsView()
                .environment(appState)
        }
    }
}

/// Inserts an opaque NSView that fills the entire window behind all SwiftUI content.
struct OpaqueWindowBackground: NSViewRepresentable {
    func makeNSView(context: Context) -> OpaqueBackgroundView {
        let view = OpaqueBackgroundView()
        return view
    }

    func updateNSView(_ nsView: OpaqueBackgroundView, context: Context) {}
}

class OpaqueBackgroundView: NSView {
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window else { return }

        window.isOpaque = true
        window.backgroundColor = .windowBackgroundColor

        // Insert an opaque layer behind the entire content view hierarchy
        if let contentView = window.contentView {
            contentView.wantsLayer = true

            // Add opaque background layer at the bottom of the layer stack
            let bgLayer = CALayer()
            bgLayer.backgroundColor = NSColor.windowBackgroundColor.cgColor
            bgLayer.zPosition = -1000
            bgLayer.frame = contentView.bounds
            bgLayer.autoresizingMask = [.layerWidthSizable, .layerHeightSizable]
            contentView.layer?.insertSublayer(bgLayer, at: 0)

            // Also set layer itself opaque
            contentView.layer?.isOpaque = true
            contentView.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        }
    }
}
