import SwiftUI

struct BannerImage: View {
    var maxWidth: CGFloat = 360

    private var nsImage: NSImage? {
        // Try resource bundle first (SPM / .app)
        if let url = AppBundle.resources.url(forResource: "banner", withExtension: "png"),
           let img = NSImage(contentsOf: url) {
            return img
        }
        // Try Bundle.main directly
        if let url = Bundle.main.url(forResource: "banner", withExtension: "png"),
           let img = NSImage(contentsOf: url) {
            return img
        }
        // Try next to executable
        if let execURL = Bundle.main.executableURL?.deletingLastPathComponent() {
            let bundlePath = execURL.appendingPathComponent("WatchtowerDesktop_WatchtowerDesktop.bundle/banner.png")
            if let img = NSImage(contentsOf: bundlePath) {
                return img
            }
        }
        return nil
    }

    var body: some View {
        if let nsImage {
            Image(nsImage: nsImage)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: maxWidth)
        } else {
            // Fallback: show app name as text
            Text("WATCHTOWER")
                .font(.system(size: 32, weight: .bold))
                .foregroundStyle(.orange)
                .frame(maxWidth: maxWidth)
        }
    }
}
