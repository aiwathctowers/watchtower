#!/usr/bin/env swift
// Generate macOS-style squircle .icns from a source PNG.
// Usage: swift generate-icon.swift <source_1024.png> <output.icns>

import AppKit
import Foundation

guard CommandLine.arguments.count == 3 else {
    fputs("Usage: swift generate-icon.swift <source.png> <output.icns>\n", stderr)
    exit(1)
}

let sourcePath = CommandLine.arguments[1]
let outputPath = CommandLine.arguments[2]

guard let sourceImage = NSImage(contentsOfFile: sourcePath) else {
    fputs("ERROR: Cannot load \(sourcePath)\n", stderr)
    exit(1)
}

func createSquircleIcon(source: NSImage, size: Int) -> NSImage {
    let s = CGFloat(size)
    let img = NSImage(size: NSSize(width: s, height: s))
    img.lockFocus()

    // Padding ~10% each side so the icon doesn't bleed to the edge
    let padding = s * 0.10
    let iconSize = s - padding * 2
    let radius = iconSize * 0.225  // macOS-style corner radius

    let rect = NSRect(x: padding, y: padding, width: iconSize, height: iconSize)
    let path = NSBezierPath(roundedRect: rect, xRadius: radius, yRadius: radius)

    // Add subtle drop shadow (like Apple's icons)
    let shadow = NSShadow()
    shadow.shadowColor = NSColor.black.withAlphaComponent(0.3)
    shadow.shadowOffset = NSSize(width: 0, height: -s * 0.01)
    shadow.shadowBlurRadius = s * 0.02
    shadow.set()

    path.addClip()
    source.draw(in: rect, from: .zero, operation: .sourceOver, fraction: 1.0)

    img.unlockFocus()
    return img
}

// Generate all iconset sizes
let iconsetSizes: [(String, Int)] = [
    ("icon_16x16.png", 16),
    ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256),
    ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512),
    ("icon_512x512@2x.png", 1024),
]

let tempDir = FileManager.default.temporaryDirectory
    .appendingPathComponent("AppIcon.iconset")
try? FileManager.default.removeItem(at: tempDir)
try FileManager.default.createDirectory(at: tempDir, withIntermediateDirectories: true)

for (filename, size) in iconsetSizes {
    let icon = createSquircleIcon(source: sourceImage, size: size)
    guard let tiff = icon.tiffRepresentation,
          let rep = NSBitmapImageRep(data: tiff),
          let png = rep.representation(using: .png, properties: [:]) else {
        fputs("ERROR: Failed to create PNG for \(filename)\n", stderr)
        exit(1)
    }
    let filePath = tempDir.appendingPathComponent(filename)
    try png.write(to: filePath)
    print("  Generated \(filename) (\(size)x\(size))")
}

// Convert to .icns using iconutil
let process = Process()
process.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
process.arguments = ["-c", "icns", tempDir.path, "-o", outputPath]
try process.run()
process.waitUntilExit()

if process.terminationStatus != 0 {
    fputs("ERROR: iconutil failed\n", stderr)
    exit(1)
}

try? FileManager.default.removeItem(at: tempDir)
print("Done! Saved to \(outputPath)")
