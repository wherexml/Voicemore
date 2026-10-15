// Run: swift scripts/export-demo-app-icons.swift mock/assets/apps
// Exports only application identities/icons, never dictation text or credentials.
import AppKit
import Foundation

let output = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "mock/assets/apps", isDirectory: true)
try FileManager.default.createDirectory(at: output, withIntermediateDirectories: true)
let workspace = NSWorkspace.shared
let demoIDs = ["com.openai.codex", "com.tencent.xinWeChat", "com.electron.lark", "com.google.Chrome", "com.apple.Safari", "com.apple.Notes", "dev.steve.voice-input", "com.anysphere.sand"]
var urls: [String: URL] = [:]
for id in demoIDs { if let url = workspace.urlForApplication(withBundleIdentifier: id) { urls[id] = url } }
for app in workspace.runningApplications where app.activationPolicy == .regular {
    if let id = app.bundleIdentifier, let url = app.bundleURL { urls[id] = url }
}
var apps: [[String: String]] = []
for (id, url) in urls.sorted(by: { $0.key < $1.key }) {
    let running = workspace.runningApplications.first { $0.bundleIdentifier == id }
    let bundle = Bundle(url: url)
    let name = running?.localizedName ?? bundle?.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String ?? bundle?.object(forInfoDictionaryKey: "CFBundleName") as? String ?? url.deletingPathExtension().lastPathComponent
    let icon = running?.icon ?? workspace.icon(forFile: url.path)
    guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 128, pixelsHigh: 128, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0), let context = NSGraphicsContext(bitmapImageRep: rep) else { continue }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = context
    icon.draw(in: NSRect(x: 0, y: 0, width: 128, height: 128), from: .zero, operation: .copy, fraction: 1)
    NSGraphicsContext.restoreGraphicsState()
    guard let png = rep.representation(using: .png, properties: [:]) else { continue }
    let filename = id + ".png"
    try png.write(to: output.appendingPathComponent(filename), options: .atomic)
    apps.append(["bundleID": id, "name": name, "icon": filename])
}
let snapshot: [String: Any] = ["generatedAt": ISO8601DateFormatter().string(from: Date()), "apps": apps]
let data = try JSONSerialization.data(withJSONObject: snapshot, options: [.prettyPrinted, .sortedKeys])
try data.write(to: output.appendingPathComponent("manifest.json"), options: .atomic)
try ("window.VOICEMORE_APPS = " + String(decoding: data, as: UTF8.self) + ";\n").write(to: output.appendingPathComponent("manifest.js"), atomically: true, encoding: .utf8)
print("Exported \(apps.count) macOS application identities and icons to \(output.path)")

let source = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("demo-app-icons")
for filename in ["app-identity.js", "app-identity.css"] {
    let contents = try Data(contentsOf: source.appendingPathComponent(filename))
    try contents.write(to: output.deletingLastPathComponent().appendingPathComponent(filename), options: .atomic)
}
