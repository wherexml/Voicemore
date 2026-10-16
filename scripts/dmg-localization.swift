// Native Finder localized label, without executable installer code in the DMG.
import AppKit

let arguments = CommandLine.arguments
if arguments[1] == "--display-name" {
    print(FileManager.default.displayName(atPath: arguments[2]))
    exit(0)
}
let destination = arguments[1]
let name = "Drag Voicemore to Applications to install"
let path = destination + "/" + name + ".localized"
try FileManager.default.createDirectory(atPath: path + "/.localized", withIntermediateDirectories: true)
let translations = [
    "en": name,
    "zh": "将 Voicemore 拖入应用程序即可安装",
    "zh_Hans": "将 Voicemore 拖入应用程序即可安装",
    "zh_CN": "将 Voicemore 拖入应用程序即可安装",
    "zh_Hant": "將 Voicemore 拖入應用程式即可安裝",
    "zh_TW": "將 Voicemore 拖入應用程式即可安裝",
    "zh_HK": "將 Voicemore 拖入應用程式即可安裝",
]
for (language, text) in translations {
    try "\"\(name)\" = \"\(text)\";\n".write(
        toFile: path + "/.localized/" + language + ".strings",
        atomically: true, encoding: .utf16)
}
// A transparent native icon lets Finder render only the localized label.
let blank = NSImage(size: NSSize(width: 256, height: 256))
blank.lockFocus()
NSColor.clear.setFill()
NSRect(x: 0, y: 0, width: 256, height: 256).fill()
blank.unlockFocus()
guard NSWorkspace.shared.setIcon(blank, forFile: path, options: []) else {
    fatalError("Cannot set installation hint icon")
}
