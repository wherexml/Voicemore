import AppKit
import CoreGraphics
let root = CommandLine.arguments[1]
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let menu = NSMenu()
let appItem = NSMenuItem(); appItem.submenu = NSMenu(title: "Fixture"); menu.addItem(appItem)
let editItem = NSMenuItem(); let editMenu = NSMenu(title: "Edit")
editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
editItem.submenu = editMenu; menu.addItem(editItem); app.mainMenu = menu
let window = NSWindow(contentRect: NSRect(x: 220, y: 280, width: 720, height: 260), styleMask: [.titled], backing: .buffered, defer: false)
window.title = "Manual correction acceptance — synthetic text only"
let field = NSTextView(frame: NSRect(x: 20, y: 110, width: 680, height: 120))
field.isRichText = false; field.allowsUndo = true; field.font = NSFont.systemFont(ofSize: 19)
let second = NSTextView(frame: NSRect(x: 20, y: 20, width: 680, height: 65)); second.isRichText = false
window.contentView!.addSubview(field); window.contentView!.addSubview(second)
window.makeKeyAndOrderFront(nil); app.activate(ignoringOtherApps: true); window.makeFirstResponder(field)
let original = "这个产品的 mark 的 demo 地址是多少？"
let corrected = "这个产品的 mock 的 demo 地址是多少？"
var command = "", scenario = "", generation = 0, keyCount = 0, pasted = false
var log: [[String: Any]] = []
var finished = false
func record(_ label: String) { log.append(["event": label, "text": field.string, "marked": field.hasMarkedText(), "keys": keyCount, "time": Date().timeIntervalSince1970]) }
let monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
 keyCount += 1
 if event.keyCode == 36 && window.firstResponder === field { record("send"); field.string = ""; return nil }
 return event
}
var keyID = 0
func key(_ code: CGKeyCode, _ text: String? = nil, flags: CGEventFlags = []) {
 keyID += 1
 let request: [String: Any] = ["id": keyID, "code": Int(code), "text": text ?? "", "flags": flags.rawValue]
 if let data = try? JSONSerialization.data(withJSONObject: request) { try? data.write(to: URL(fileURLWithPath: root + "/key.json"), options: .atomic) }
}
func replaceWord(_ from: String, _ to: String) {
 let range = (field.string as NSString).range(of: from, options: scenario == "duplicate" ? .backwards : [])
 if range.location == NSNotFound { record("missing-word"); return }
 field.setSelectedRange(range); key(0, to)
}
func later(_ seconds: Double, _ body: @escaping () -> Void) {
 let g = generation
 DispatchQueue.main.asyncAfter(deadline: .now() + seconds) { if g == generation { body(); record("step") } }
}
let timer = Timer.scheduledTimer(withTimeInterval: 0.02, repeats: true) { _ in
 let next = (try? String(contentsOfFile: root + "/command", encoding: .utf8)) ?? ""
 if next != command {
  command = next
  if next == "quit" { app.terminate(nil); return }
  if next.hasPrefix("setup:") {
   finished = false; generation += 1; scenario = String(next.dropFirst(6)); keyCount = 0; pasted = false; log = []
   window.makeKeyAndOrderFront(nil); app.activate(ignoringOtherApps: true); window.makeFirstResponder(field)
   field.unmarkText(); field.string = scenario == "surrounding" ? "前缀：\n\n：后缀" : scenario == "duplicate" ? original + "\n" : ""
   field.setSelectedRange(NSRange(location: scenario == "surrounding" ? 4 : scenario == "duplicate" ? field.string.utf16.count : 0, length: 0))
   field.undoManager?.removeAllActions(); record("ready")
  }
  if next.hasPrefix("edit:") {
   record("editing")
   later(1.25) { finished = true }
   if scenario == "programmatic" { later(0.15) { field.string = corrected } }
   else if scenario == "append_only" { later(0.15) { field.setSelectedRange(NSRange(location: field.string.utf16.count, length: 0)); key(0, "接下来谈另外一件事。") } }
   else if scenario == "focus_loss" { later(0.15) { window.makeFirstResponder(second) }; later(0.35) { field.string = corrected } }
   else if (scenario == "marked_only" || scenario == "marked_commit") { later(0.15) { replaceWord("mark", "mo") }; later(0.30) { field.setMarkedText("mo", selectedRange: NSRange(location: 2, length: 0), replacementRange: (field.string as NSString).range(of: "mo")) } }
   else if scenario != "unchanged" { later(0.15) { replaceWord("mark", "mock") } }
   if scenario == "undo" { later(0.50) { key(6, flags: .maskCommand) } }
   if scenario == "marked_commit" { later(0.65) { field.insertText("mock", replacementRange: field.markedRange()); field.unmarkText() } }
   if scenario == "undo_immediate" { later(0.82) { key(6, flags: .maskCommand) } }
   if scenario == "append" { later(0.50) { field.setSelectedRange(NSRange(location: field.string.utf16.count, length: 0)); key(0, "追加内容不应该学习。") } }
   if scenario != "marked_only" && scenario != "focus_loss" && scenario != "timeout" { later(0.90) { key(36) } }
  }
 }
 if !pasted && field.string.contains(original) { pasted = true; record("paste-consumed") }
 let state: [String: Any] = ["command": command, "finished": finished, "text": field.string, "pasted": pasted, "focused": window.firstResponder === field, "active": app.isActive, "keyWindow": window.isKeyWindow, "keys": keyCount, "marked": field.hasMarkedText(), "events": log]
 if let data = try? JSONSerialization.data(withJSONObject: state, options: [.prettyPrinted, .sortedKeys]) { try? data.write(to: URL(fileURLWithPath: root + "/state.json"), options: .atomic) }
}
app.run()
