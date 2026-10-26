// VoicemoreAudioTap: live system-audio capture for subtitles (macOS 14.2+), adapted from the
// verified tools/system-audio-prototype/capture/main.swift. Core Audio process tap (all output
// except this helper) → private aggregate device → mono 16 kHz PCM16 little-endian on stdout,
// continuously, until SIGTERM/SIGINT, stdin EOF (the app went away) or a closed stdout.
// Status is JSON lines on stderr. No files, no virtual driver, the output device is untouched,
// and playback stays audible.
import AVFoundation
import AudioToolbox
import CoreAudio
import Darwin
import Foundation

let TARGET_RATE = 16000.0

// MARK: - Status (stderr JSON lines)

let statusLock = NSLock()

func emit(_ event: String, _ fields: [String: Any] = [:]) {
    var d = fields
    d["event"] = event
    d["t_mono_ns"] = NSNumber(value: clock_gettime_nsec_np(CLOCK_UPTIME_RAW))
    guard var data = try? JSONSerialization.data(withJSONObject: d, options: [.sortedKeys]) else { return }
    data.append(0x0A)
    statusLock.lock()
    data.withUnsafeBytes { _ = write(2, $0.baseAddress, $0.count) }
    statusLock.unlock()
}

enum CaptureError: Error, CustomStringConvertible {
    case os(String, OSStatus)
    case msg(String)
    var description: String {
        switch self {
        case let .os(what, st): return "\(what) failed: OSStatus \(st) (\(fourcc(st)))"
        case let .msg(m): return m
        }
    }
}

func fourcc(_ st: OSStatus) -> String {
    let b = withUnsafeBytes(of: UInt32(bitPattern: st).bigEndian) { Array($0) }
    return b.allSatisfy { $0 >= 0x20 && $0 < 0x7F } ? String(bytes: b, encoding: .ascii)! : String(st)
}

func check(_ st: OSStatus, _ what: String) throws {
    if st != noErr { throw CaptureError.os(what, st) }
}

func globalAddress(_ sel: AudioObjectPropertySelector) -> AudioObjectPropertyAddress {
    AudioObjectPropertyAddress(mSelector: sel, mScope: kAudioObjectPropertyScopeGlobal, mElement: kAudioObjectPropertyElementMain)
}

func processObject(_ pid: pid_t) throws -> AudioObjectID {
    var addr = globalAddress(kAudioHardwarePropertyTranslatePIDToProcessObject)
    var qualifier = pid
    var obj = AudioObjectID(kAudioObjectUnknown)
    var size = UInt32(MemoryLayout<AudioObjectID>.size)
    try check(AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr,
                                         UInt32(MemoryLayout<pid_t>.size), &qualifier, &size, &obj),
              "translate PID \(pid)")
    return obj
}

func tapFormat(_ tap: AudioObjectID) throws -> AudioStreamBasicDescription {
    var addr = globalAddress(kAudioTapPropertyFormat)
    var asbd = AudioStreamBasicDescription()
    var size = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
    try check(AudioObjectGetPropertyData(tap, &addr, 0, nil, &size, &asbd), "tap format")
    return asbd
}

/// Writes all of the bytes to stdout; false once the reader is gone.
func writeAll(_ p: UnsafeRawPointer, _ count: Int) -> Bool {
    var off = 0
    while off < count {
        let n = write(1, p + off, count - off)
        if n < 0 {
            if errno == EINTR { continue }
            return false
        }
        off += n
    }
    return true
}

// MARK: - Capture

final class Capture {
    let ioQueue = DispatchQueue(label: "voicemore.audiotap.io", qos: .userInteractive)
    var tapID = AudioObjectID(kAudioObjectUnknown)
    var aggID = AudioObjectID(kAudioObjectUnknown)
    var procID: AudioDeviceIOProcID?
    var stopped = false

    // ioQueue state
    var srcFormat: AVAudioFormat!
    let dstFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: TARGET_RATE, channels: 1, interleaved: true)!
    var converter: AVAudioConverter!
    var tapBufferCount = 1
    var callbacks = 0
    var outFrames: UInt64 = 0
    var nonzero: UInt64 = 0
    var secPeak: Int16 = 0
    var secNonzero = 0
    var nextProgress: UInt64 = 16000
    var zeroWarned = false
    var convertErrors = 0
    var closed = false

    func start() throws {
        emit("start", ["pid": Int(getpid()), "bundle_id": Bundle.main.bundleIdentifier ?? "none",
                       "os": ProcessInfo.processInfo.operatingSystemVersionString])
        let me = try processObject(getpid())
        let desc = CATapDescription(monoGlobalTapButExcludeProcesses: me == kAudioObjectUnknown ? [] : [me])
        desc.name = "Voicemore Subtitles"
        desc.uuid = UUID()
        desc.muteBehavior = .unmuted
        desc.isPrivate = true
        try check(AudioHardwareCreateProcessTap(desc, &tapID), "AudioHardwareCreateProcessTap")

        var asbd = try tapFormat(tapID)
        emit("tap_format", ["sample_rate": asbd.mSampleRate, "channels": Int(asbd.mChannelsPerFrame)])
        guard let src = AVAudioFormat(streamDescription: &asbd) else { throw CaptureError.msg("unsupported tap format") }
        guard let conv = AVAudioConverter(from: src, to: dstFormat) else { throw CaptureError.msg("no converter from tap format") }
        srcFormat = src
        converter = conv
        tapBufferCount = src.isInterleaved ? 1 : Int(src.channelCount)

        let dict: [String: Any] = [
            kAudioAggregateDeviceNameKey: "Voicemore Subtitles",
            kAudioAggregateDeviceUIDKey: "voicemore.subtitles.aggregate.\(UUID().uuidString)",
            kAudioAggregateDeviceIsPrivateKey: true,
            kAudioAggregateDeviceIsStackedKey: false,
            kAudioAggregateDeviceTapAutoStartKey: true,
            kAudioAggregateDeviceTapListKey: [[kAudioSubTapDriftCompensationKey: true, kAudioSubTapUIDKey: desc.uuid.uuidString]],
        ]
        try check(AudioHardwareCreateAggregateDevice(dict as CFDictionary, &aggID), "AudioHardwareCreateAggregateDevice")
        try check(AudioDeviceCreateIOProcIDWithBlock(&procID, aggID, ioQueue) { [weak self] _, input, _, _, _ in
            self?.process(input)
        }, "AudioDeviceCreateIOProcIDWithBlock")
        try check(AudioDeviceStart(aggID, procID), "AudioDeviceStart")
        emit("io_started")
    }

    /// ioQueue only.
    func process(_ input: UnsafePointer<AudioBufferList>) {
        guard !closed else { return }
        callbacks += 1
        let all = UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: input))
        guard all.count >= tapBufferCount else { return }
        let list = AudioBufferList.allocate(maximumBuffers: tapBufferCount)
        defer { free(list.unsafeMutablePointer) }
        for i in 0..<tapBufferCount { list[i] = all[all.count - tapBufferCount + i] }
        guard let inBuf = AVAudioPCMBuffer(pcmFormat: srcFormat, bufferListNoCopy: list.unsafePointer, deallocator: nil),
              inBuf.frameLength > 0 else { return }
        let cap = AVAudioFrameCount(Double(inBuf.frameLength) * TARGET_RATE / srcFormat.sampleRate) + 64
        guard let outBuf = AVAudioPCMBuffer(pcmFormat: dstFormat, frameCapacity: cap) else { return }
        var fed = false
        var err: NSError?
        let status = converter.convert(to: outBuf, error: &err) { _, outStatus in
            if fed { outStatus.pointee = .noDataNow; return nil }
            fed = true
            outStatus.pointee = .haveData
            return inBuf
        }
        if status == .error {
            convertErrors += 1
            if convertErrors <= 3 { emit("error", ["stage": "convert", "message": err?.localizedDescription ?? "unknown"]) }
            return
        }
        let n = Int(outBuf.frameLength)
        guard n > 0, let p = outBuf.int16ChannelData?[0] else { return }
        for i in 0..<n where p[i] != 0 {
            nonzero += 1
            secNonzero += 1
            let a = p[i] == Int16.min ? Int16.max : abs(p[i])
            if a > secPeak { secPeak = a }
            if nonzero == 1 { emit("first_nonzero_audio", ["frame": Int(outFrames) + i]) }
        }
        if !writeAll(p, n * 2) {
            closed = true
            DispatchQueue.main.async { self.stop("stdout_closed") }
            return
        }
        outFrames += UInt64(n)
        while outFrames >= nextProgress {
            emit("progress", ["frames": NSNumber(value: outFrames), "callbacks": callbacks, "nonzero": NSNumber(value: nonzero),
                              "peak_dbfs_1s": secPeak == 0 ? -120 : 20 * log10(Double(secPeak) / 32767),
                              "nonzero_ratio_1s": Double(secNonzero) / TARGET_RATE])
            secPeak = 0
            secNonzero = 0
            nextProgress += 16000
        }
        if !zeroWarned, nonzero == 0, outFrames >= 5 * 16000 {
            zeroWarned = true
            emit("zero_audio_warning", ["frames": NSNumber(value: outFrames)])
        }
    }

    func teardown() {
        if aggID != kAudioObjectUnknown {
            if let procID {
                AudioDeviceStop(aggID, procID)
                AudioDeviceDestroyIOProcID(aggID, procID)
            }
            AudioHardwareDestroyAggregateDevice(aggID)
            aggID = AudioObjectID(kAudioObjectUnknown)
        }
        if tapID != kAudioObjectUnknown {
            AudioHardwareDestroyProcessTap(tapID)
            tapID = AudioObjectID(kAudioObjectUnknown)
        }
    }

    /// Main queue.
    func stop(_ reason: String) {
        guard !stopped else { return }
        stopped = true
        teardown()
        let summary: [String: Any] = ioQueue.sync {
            ["reason": reason, "frames": NSNumber(value: outFrames), "callbacks": callbacks,
             "nonzero": NSNumber(value: nonzero), "convert_errors": convertErrors]
        }
        emit("summary", summary)
        exit(reason == "no_callbacks" ? 4 : 0)
    }
}

// MARK: - Main

signal(SIGPIPE, SIG_IGN)
let capture = Capture()
do { try capture.start() } catch {
    emit("error", ["stage": "setup", "message": "\(error)"])
    capture.teardown()
    exit(2)
}

var signalSources: [DispatchSourceSignal] = []
for sig in [SIGINT, SIGTERM] {
    signal(sig, SIG_IGN)
    let s = DispatchSource.makeSignalSource(signal: sig, queue: .main)
    s.setEventHandler { capture.stop(sig == SIGINT ? "sigint" : "sigterm") }
    s.resume()
    signalSources.append(s)
}

// The app holds stdin open; EOF means it is gone, and capture must not outlive it.
let stdinSource = DispatchSource.makeReadSource(fileDescriptor: 0, queue: .main)
stdinSource.setEventHandler {
    var b = [UInt8](repeating: 0, count: 64)
    if read(0, &b, b.count) <= 0 { capture.stop("stdin_closed") }
}
stdinSource.resume()

DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
    if capture.ioQueue.sync(execute: { capture.callbacks }) == 0 {
        emit("no_callbacks")
        capture.stop("no_callbacks")
    }
}

dispatchMain()
