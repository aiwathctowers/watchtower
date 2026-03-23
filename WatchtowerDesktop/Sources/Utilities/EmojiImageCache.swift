import AppKit
import Foundation

/// Caches custom emoji images downloaded from URLs.
/// Images are loaded on-demand and cached in memory.
@MainActor
@Observable
final class EmojiImageCache {
    private var cache: [String: NSImage] = [:]
    private var loading: Set<String> = []
    private var failed: Set<String> = []

    /// Get a cached image for the given emoji name, or start loading it.
    /// Returns nil if not yet loaded (view should show placeholder).
    func image(for name: String, url: String) -> NSImage? {
        if let cached = cache[name] {
            return cached
        }
        if !loading.contains(name) && !failed.contains(name) {
            loading.insert(name)
            Task {
                await loadImage(name: name, urlString: url)
            }
        }
        return nil
    }

    private func loadImage(name: String, urlString: String) async {
        guard let url = URL(string: urlString) else {
            loading.remove(name)
            failed.insert(name)
            return
        }

        do {
            let (data, _) = try await URLSession.shared.data(from: url)
            if let image = NSImage(data: data) {
                // Normalize to emoji display size
                image.size = NSSize(width: 20, height: 20)
                cache[name] = image
            } else {
                failed.insert(name)
            }
        } catch {
            failed.insert(name)
        }
        loading.remove(name)
    }

    /// Pre-warm the cache with known emoji URLs.
    func preload(emojis: [String: String]) {
        for (name, url) in emojis {
            if cache[name] == nil && !loading.contains(name) {
                loading.insert(name)
                Task {
                    await loadImage(name: name, urlString: url)
                }
            }
        }
    }
}
