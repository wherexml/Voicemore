import AppKit
class OpaqueEditor: NSView {
 var text = ""
 override var acceptsFirstResponder: Bool { true }
 override func isAccessibilityElement() -> Bool { false }
 @objc func paste(_ sender: Any?) { text += NSPasteboard.general.string(forType: .string) ?? "" }
}
let dir = CommandLine.arguments[1]
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let menu = NSMenu()
let appItem = NSMenuItem()
appItem.submenu = NSMenu(title: "Fixture")
menu.addItem(appItem)
let edit = NSMenuItem(title: "Edit", action: nil, keyEquivalent: "")
let submenu = NSMenu(title: "Edit")
submenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
edit.submenu = submenu
menu.addItem(edit)
app.mainMenu = menu
let window = NSWindow(contentRect: NSRect(x: 200,y: 300,width: 500,height: 220), styleMask: [.titled], backing: .buffered, defer: false)
window.title = "Voice Input acceptance fixture"
let first = NSTextField(frame: NSRect(x: 20,y: 150,width: 450,height: 30))
let second = NSTextField(frame: NSRect(x: 20,y: 100,width: 450,height: 30))
let opaque = OpaqueEditor(frame: NSRect(x: 250,y: 30,width: 200,height: 30))
let button = NSButton(frame: NSRect(x: 20,y: 30,width: 200,height: 30))
button.title = "No text input"
for v in [first, second, button, opaque] { window.contentView!.addSubview(v) }
window.makeKeyAndOrderFront(nil)
app.activate(ignoringOtherApps: true)
// The fixture has a stable identity so production profile matching is used.
var keyEventCount = 0
var sendEvents = 0
var sentText = ""
var sendFlags = ""
let monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
 keyEventCount += 1
 if event.keyCode == 36 {
  sendEvents += 1
  sentText = second.stringValue
  sendFlags = event.modifierFlags.contains(.command) ? "cmd-enter" : event.modifierFlags.contains(.control) ? "ctrl-enter" : "enter"
  second.stringValue = ""
  return nil
 }
 if window.firstResponder === opaque && event.keyCode == 9 && event.modifierFlags.contains(.control) {
  opaque.paste(nil)
  return nil
 }
 return event
}
var last = ""
let timer = Timer.scheduledTimer(withTimeInterval: 0.05, repeats: true) { _ in
 guard let cmd = try? String(contentsOfFile: dir + "/command", encoding: .utf8), cmd != last else { return }
 last = cmd
 if cmd.hasPrefix("send-reset-") { second.isEditable=true;second.stringValue="";sendEvents=0;sentText="";sendFlags="";window.makeFirstResponder(second) }
 switch cmd {
 case "first": window.makeFirstResponder(first)
 case "second": second.isEditable = true; window.makeFirstResponder(second)
 case "opaque": window.makeFirstResponder(opaque)
 case "none": window.makeFirstResponder(button)
 case "readonly": second.isEditable = false; window.makeFirstResponder(second)
 case "quit": app.terminate(nil)
 default:
  if cmd.hasPrefix("remote-") { opaque.text = ""; window.makeFirstResponder(opaque) }
  break
 }
 submenu.update()
 let state = ["sendEvents": String(sendEvents), "sentText": sentText, "sendFlags": sendFlags, "command": cmd, "first": first.stringValue, "second": second.stringValue, "opaque": opaque.text, "keyWindow": String(window.isKeyWindow), "active": String(app.isActive), "firstResponder": String(describing: window.firstResponder), "keyEventCount": String(keyEventCount)]
 let data = try! JSONSerialization.data(withJSONObject: state)
 try! data.write(to: URL(fileURLWithPath: dir + "/state"), options: .atomic)
}
app.run()
