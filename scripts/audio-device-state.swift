// Read-only snapshot for Bluetooth recovery acceptance. Run with swift.
import CoreAudio
import Foundation
func read<T>(_ id: AudioObjectID, _ selector: AudioObjectPropertySelector, _ result: inout T, _ scope: AudioObjectPropertyScope = kAudioObjectPropertyScopeGlobal, _ channel: UInt32 = 0) -> Bool {
 var address = AudioObjectPropertyAddress(mSelector: selector, mScope: scope, mElement: channel)
 var size = UInt32(MemoryLayout<T>.size)
 return AudioObjectGetPropertyData(id, &address, 0, nil, &size, &result) == 0
}
let samples = CommandLine.arguments.count == 3 && CommandLine.arguments[1] == "--samples" ? max(1, min(6000, Int(CommandLine.arguments[2]) ?? 1)) : 1
for sample in 0..<samples {
var result: [String: Any] = [:]
if samples > 1 { result["time"] = Date().timeIntervalSince1970 }
for (name, selector) in [("input", kAudioHardwarePropertyDefaultInputDevice), ("output", kAudioHardwarePropertyDefaultOutputDevice)] {
 var device: UInt32 = 0
 guard read(1, selector, &device) else { continue }
 var rate: Double = 0
 var running: UInt32 = 0
 var muted: UInt32 = 0
 var row: [String: Any] = ["device": device]
 if read(device, kAudioDevicePropertyNominalSampleRate, &rate) { row["rate"] = rate }
 if read(device, kAudioDevicePropertyDeviceIsRunningSomewhere, &running) { row["running"] = running }
 if read(device, kAudioDevicePropertyMute, &muted, kAudioDevicePropertyScopeOutput) { row["mute"] = muted }
 for channel: UInt32 in 0...2 {
 var volume: Float = 0
 if read(device, kAudioDevicePropertyVolumeScalar, &volume, kAudioDevicePropertyScopeOutput, channel) { row["volume\(channel)"] = volume }
 }
 result[name] = row
}
let data = try JSONSerialization.data(withJSONObject: result, options: [.sortedKeys])
print(String(decoding: data, as: UTF8.self))

if sample + 1 < samples { Thread.sleep(forTimeInterval: 0.02) }
}
