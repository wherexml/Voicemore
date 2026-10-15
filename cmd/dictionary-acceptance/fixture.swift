import AppKit
let dir = CommandLine.arguments[1]
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let menu = NSMenu(); let item = NSMenuItem(); let edit = NSMenu(title: "Edit")
edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
item.submenu = edit; menu.addItem(item); app.mainMenu = menu
let window = NSWindow(contentRect: NSRect(x: 300,y: 300,width: 600,height: 150), styleMask: [.titled], backing: .buffered, defer: false)
window.title = "Dictionary acceptance"
let field = NSTextField(frame: NSRect(x: 20,y: 60,width: 560,height: 40))
window.contentView!.addSubview(field); window.makeKeyAndOrderFront(nil); app.activate(ignoringOtherApps: true); window.makeFirstResponder(field)
let original = "这个产品的 mark 的 demo 地址是多少？"
let corrected = "这个产品的 mock 的 demo 地址是多少？"
var scenario = ""; var pasted: Date?; var step = 0
func setText(_ text: String) { field.stringValue = text; field.currentEditor()?.string = text; field.currentEditor()?.selectedRange = NSRange(location: text.utf16.count, length: 0) }
let timer = Timer.scheduledTimer(withTimeInterval: 0.01, repeats: true) { _ in
 let cmd = (try? String(contentsOfFile: dir + "/command", encoding: .utf8)) ?? ""
 if cmd == "quit" { app.terminate(nil); return }
 if cmd != scenario {
  scenario = cmd; pasted = nil; step = 0; app.activate(ignoringOtherApps: true); window.makeKeyAndOrderFront(nil); window.makeFirstResponder(nil); setText(""); window.makeFirstResponder(field)
  try! Data(cmd.utf8).write(to: URL(fileURLWithPath: dir + "/ready"), options: .atomic)
 }
 let value = field.currentEditor()?.string ?? field.stringValue
 if pasted == nil && value == original { pasted = Date() }
 guard let started = pasted else { return }
 let elapsed = Date().timeIntervalSince(started)
 if step == 0 && elapsed > 0.15 {
  if scenario != "unchanged" { setText(corrected) }; step = 1
 }
 if step == 1 && elapsed > 0.30 {
  if scenario == "append" { setText(corrected + "另外我还要继续输入一大段新的内容，这些内容不应该作为词典纠正。") }
  if scenario == "undo" { setText(original) }; step = 2
 }
 if step == 2 && elapsed > 0.45 { setText(""); step = 3 }
}
app.run()
