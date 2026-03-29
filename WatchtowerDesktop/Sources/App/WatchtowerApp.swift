import SwiftUI
import AppKit
import UserNotifications

/// Allows notifications to display as banners even when the app is in the foreground,
/// and handles notification click actions to navigate within the running app.
/// H5 fix: uses a static shared reference to AppState that is set once the SwiftUI-managed
/// state is available (in .onAppear), avoiding the stale-copy problem with @State in init().
class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    /// Set from the SwiftUI body once the real managed AppState is live.
    static var sharedAppState: AppState?

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let userInfo = response.notification.request.content.userInfo
        let type = userInfo["type"] as? String

        // Bring the running app to front
        NSApplication.shared.activate(ignoringOtherApps: true)

        Task { @MainActor in
            let appState = Self.sharedAppState
            switch type {
            case "decision":
                if let digestID = userInfo["digestId"] as? Int {
                    appState?.navigateToDigest(digestID)
                } else {
                    appState?.selectedDestination = .digests
                }
            case "track", "track_update":
                appState?.selectedDestination = .tracks
            case "task_overdue":
                appState?.selectedDestination = .tasks
            case "daily_summary":
                appState?.selectedDestination = .digests
            default:
                break
            }
        }

        completionHandler()
    }
}

@main
struct WatchtowerApp: App {
    @State private var appState = AppState()
    private let notificationDelegate = NotificationDelegate()

    init() {
        NSApplication.shared.setActivationPolicy(.regular)
        NSApplication.shared.activate(ignoringOtherApps: true)
        UNUserNotificationCenter.current().delegate = notificationDelegate
    }

    var body: some Scene {
        WindowGroup {
            NavigationRoot()
                .environment(appState)
                .frame(minWidth: 800, minHeight: 600)
                .background(OpaqueWindowBackground())
                .onAppear {
                    // H5 fix: connect the live SwiftUI-managed appState to the notification delegate
                    NotificationDelegate.sharedAppState = appState
                    appState.initialize()
                }
                .onOpenURL { url in
                    // Handle watchtower-auth:// callback — just bring app to front
                    if url.scheme == "watchtower-auth" {
                        NSApplication.shared.activate(ignoringOtherApps: true)
                    }
                }
        }
        .defaultSize(width: 1200, height: 800)

        Window("Pipeline Progress", id: "progress-detail") {
            ProgressDetailView()
                .environment(appState)
        }
        .defaultSize(width: 600, height: 500)

        Settings {
            SettingsView()
                .environment(appState)
                .background(SettingsWindowAccessor())
        }
    }
}

/// Makes the Settings window appear on the same fullscreen Space as the main window.
struct SettingsWindowAccessor: NSViewRepresentable {
    func makeNSView(context: Context) -> SettingsWindowView { SettingsWindowView() }
    func updateNSView(_ nsView: SettingsWindowView, context: Context) {}
}

class SettingsWindowView: NSView {
    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window else { return }
        window.collectionBehavior.insert(.canJoinAllSpaces)
        window.level = .floating
        // Reset to normal level after a brief delay so the window doesn't stay always-on-top
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [weak window] in
            window?.level = .normal
            window?.collectionBehavior.remove(.canJoinAllSpaces)
            window?.collectionBehavior.insert(.fullScreenAuxiliary)
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

        // Persist window frame (position + size) across launches
        window.setFrameAutosaveName("WatchtowerMainWindow")

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
