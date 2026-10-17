// Run on macOS: swift scripts/export-action-symbols.swift
// Capture genuine SF Symbols as white alpha masks for MyGo's tinted bitmap renderer.
import AppKit
import Foundation

let output = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "resources/symbols", isDirectory: true)
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
let pixels = 128
for name in ["doc.on.doc", "trash"] {
    guard let symbol = NSImage(systemSymbolName: name, accessibilityDescription: nil)?
        .withSymbolConfiguration(.init(pointSize: 16, weight: .regular, scale: .medium)),
        let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
        let context = NSGraphicsContext(bitmapImageRep: rep) else {
        fatalError("Cannot render SF Symbol: \(name)")
    }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = context
    let scale = CGFloat(pixels) / max(symbol.size.width, symbol.size.height)
    let width = symbol.size.width * scale, height = symbol.size.height * scale
    symbol.draw(in: NSRect(x: (CGFloat(pixels) - width) / 2,
                          y: (CGFloat(pixels) - height) / 2, width: width, height: height),
                from: .zero, operation: .sourceOver, fraction: 1)
    // Keep the native coverage/antialiasing; color is supplied by the current UI theme.
    context.cgContext.setBlendMode(.sourceIn)
    context.cgContext.setFillColor(NSColor.white.cgColor)
    context.cgContext.fill(CGRect(x: 0, y: 0, width: pixels, height: pixels))
    NSGraphicsContext.restoreGraphicsState()
    guard let png = rep.representation(using: .png, properties: [:]) else {
        fatalError("Cannot encode SF Symbol: \(name)")
    }
    try png.write(to: output.appendingPathComponent(name + ".png"), options: .atomic)
    print("Exported \(name): \(pixels)×\(pixels), Regular / Medium")
}
