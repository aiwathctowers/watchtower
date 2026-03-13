import AuthenticationServices
import SwiftUI

/// Constants for the OAuth redirect.
/// The HTTPS redirect URI must be registered in the Slack app settings.
enum OAuthConstants {
    static let redirectHost = "127.0.0.1"
    static let redirectPort = 18491
    static let callbackPath = "/callback"
    static let redirectURI = "https://127.0.0.1:18491/callback"
}

/// Presentation context provider for ASWebAuthenticationSession.
/// Held as a static to ensure the weak reference from the session stays alive.
final class OAuthPresentationContext: NSObject, ASWebAuthenticationPresentationContextProviding {
    static let shared = OAuthPresentationContext()

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        NSApp.keyWindow ?? NSApp.windows.first ?? ASPresentationAnchor()
    }
}
