// System-audio capture prototype (macOS 14.2+). Core Audio process tap → private aggregate
// device → mono 16 kHz PCM16 WAV + JSONL timeline. Isolated development tool: no production
// identity, data or update channel; no virtual driver; never changes the system output device.
import AVFoundation
import AudioToolbox
import CoreAudio
import CryptoKit
import Darwin
import Foundation

let PROTO_BUNDLE_ID = "dev.steve.voice-input.debug.sysaudio-proto"
let TARGET_RATE = 16000.0
let MAX_DURATION = 600.0

// MARK: - Options

struct Options {
    var out: URL?
    var log: URL?
    var duration = 60.0
    var pids: [pid_t] = []
    var zeroWarnAfter = 5.0
    var withOutputSubdevice = false
}

func usage() -> Never {
    FileHandle.standardError.write("""
    usage: SysAudioCapture --out /abs/capture.wav [--log /abs/capture.jsonl] [--duration 60]
                           [--pid N]... [--zero-warn 5] [--with-output-subdevice]
      --duration   seconds, 1...\(Int(MAX_DURATION)) (default 60); WAV length is exact
      --pid        capture only these processes (repeatable); default: all system output except this helper
      --with-output-subdevice  fallback aggregate that also lists the default output device

    """.data(using: .utf8)!)
    exit(2)
}

func need(_ v: String?) -> String {
    guard let v else { usage() }
    return v
}

func absoluteURL(_ s: String) -> URL {
    // Launched through `open`, the working directory is "/": relative paths would be surprising.
    guard s.hasPrefix("/") else {
        FileHandle.standardError.write("path must be absolute: \(s)\n".data(using: .utf8)!)
        exit(2)
    }
    return URL(fileURLWithPath: s)
}

func parseOptions() -> Options {
    var o = Options()
    var it = CommandLine.arguments.dropFirst().makeIterator()
    while let a = it.next() {
        switch a {
        case "--out": o.out = absoluteURL(need(it.next()))
        case "--log": o.log = absoluteURL(need(it.next()))
        case "--duration":
            guard let d = Double(need(it.next())), d >= 1, d <= MAX_DURATION else { usage() }
            o.duration = d
        case "--pid":
            guard let p = Int32(need(it.next())), p > 0 else { usage() }
            o.pids.append(p)
        case "--zero-warn":
            guard let z = Double(need(it.next())), z > 0 else { usage() }
            o.zeroWarnAfter = z
        case "--with-output-subdevice": o.withOutputSubdevice = true
        case "-h", "--help": usage()
        default: usage()
        }
    }
    if o.out == nil { usage() }
    if o.log == nil { o.log = o.out!.deletingPathExtension().appendingPathExtension("jsonl") }
    return o
}

// MARK: - JSONL timeline

let timebase: mach_timebase_info_data_t = {
    var tb = mach_timebase_info_data_t()
    mach_timebase_info(&tb)
    return tb
}()

/// Monotonic clock shared with AudioTimeStamp.mHostTime (both mach absolute time), in ns.
func monoNs() -> UInt64 { clock_gettime_nsec_np(CLOCK_UPTIME_RAW) }
func hostTimeNs(_ ticks: UInt64) -> UInt64 { ticks * UInt64(timebase.numer) / UInt64(timebase.denom) }

final class Timeline {
    private let fh: FileHandle
    private let lock = NSLock()
    private let iso = ISO8601DateFormatter()

    init(_ url: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        guard FileManager.default.createFile(atPath: url.path, contents: nil) else {
            throw CaptureError.msg("cannot create log \(url.path)")
        }
        fh = try FileHandle(forWritingTo: url)
    }

    func emit(_ event: String, _ fields: [String: Any] = [:], stderr: Bool = false) {
        var d = fields
        d["event"] = event
        d["t_mono_ns"] = NSNumber(value: monoNs())
        d["t_wall"] = iso.string(from: Date())
        guard let data = try? JSONSerialization.data(withJSONObject: d, options: [.sortedKeys]) else { return }
        lock.lock()
        fh.write(data)
        fh.write(Data([0x0A]))
        lock.unlock()
        if stderr {
            FileHandle.standardError.write(data)
            FileHandle.standardError.write(Data([0x0A]))
        }
    }

    func close() { lock.lock(); try? fh.synchronize(); try? fh.close(); lock.unlock() }
}

// MARK: - WAV writer (mono, 16 kHz, signed 16-bit little-endian)

final class WavWriter {
    private let fh: FileHandle
    private(set) var frames: UInt32 = 0

    init(_ url: URL) throws {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        guard FileManager.default.createFile(atPath: url.path, contents: nil) else {
            throw CaptureError.msg("cannot create WAV \(url.path)")
        }
        fh = try FileHandle(forWritingTo: url)
        fh.write(Self.header(dataBytes: 0))
    }

    func append(_ p: UnsafePointer<Int16>, count: Int) {
        guard count > 0 else { return }
        fh.write(Data(bytes: p, count: count * 2))
        frames += UInt32(count)
    }

    func finish() throws {
        try fh.seek(toOffset: 0)
        fh.write(Self.header(dataBytes: frames * 2))
        try fh.synchronize()
        try fh.close()
    }

    static func header(dataBytes: UInt32) -> Data {
        var d = Data()
        func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { d.append(contentsOf: $0) } }
        func u16(_ v: UInt16) { withUnsafeBytes(of: v.littleEndian) { d.append(contentsOf: $0) } }
        d.append(contentsOf: Array("RIFF".utf8)); u32(36 + dataBytes); d.append(contentsOf: Array("WAVE".utf8))
        d.append(contentsOf: Array("fmt ".utf8)); u32(16); u16(1); u16(1); u32(16000); u32(32000); u16(2); u16(16)
        d.append(contentsOf: Array("data".utf8)); u32(dataBytes)
        return d
    }
}

// MARK: - Core Audio helpers

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

/// Core Audio process object for a PID; kAudioObjectUnknown when the process has no audio client.
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

func defaultOutputUID() throws -> String {
    var addr = globalAddress(kAudioHardwarePropertyDefaultSystemOutputDevice)
    var dev = AudioDeviceID(kAudioObjectUnknown)
    var size = UInt32(MemoryLayout<AudioDeviceID>.size)
    try check(AudioObjectGetPropertyData(AudioObjectID(kAudioObjectSystemObject), &addr, 0, nil, &size, &dev),
              "default system output device")
    var uidAddr = globalAddress(kAudioDevicePropertyDeviceUID)
    var uid: Unmanaged<CFString>?
    size = UInt32(MemoryLayout<Unmanaged<CFString>?>.size)
    try check(AudioObjectGetPropertyData(dev, &uidAddr, 0, nil, &size, &uid), "output device UID")
    guard let uid else { throw CaptureError.msg("output device has no UID") }
    return uid.takeRetainedValue() as String
}

func tapFormat(_ tap: AudioObjectID) throws -> AudioStreamBasicDescription {
    var addr = globalAddress(kAudioTapPropertyFormat)
    var asbd = AudioStreamBasicDescription()
    var size = UInt32(MemoryLayout<AudioStreamBasicDescription>.size)
    try check(AudioObjectGetPropertyData(tap, &addr, 0, nil, &size, &asbd), "tap format")
    return asbd
}

// MARK: - Capture

final class Capture {
    let opts: Options
    let log: Timeline
    let ioQueue = DispatchQueue(label: "sysaudio-proto.io", qos: .userInteractive)

    var tapID = AudioObjectID(kAudioObjectUnknown)
    var aggID = AudioObjectID(kAudioObjectUnknown)
    var procID: AudioDeviceIOProcID?
    var stopped = false

    // ioQueue state
    var wav: WavWriter?
    var srcFormat: AVAudioFormat!
    let dstFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: TARGET_RATE, channels: 1, interleaved: true)!
    var converter: AVAudioConverter!
    var tapBufferCount = 1
    var targetFrames: UInt32 = 0
    var callbacks = 0
    var srcFrames: UInt64 = 0
    var nonzero: UInt64 = 0
    var peak: Int16 = 0
    var secPeak: Int16 = 0
    var secNonzero = 0
    var nextProgress: UInt32 = 16000
    var zeroWarned = false
    var reachedTarget = false
    var layoutLogged = false
    var convertErrors = 0

    init(_ opts: Options, _ log: Timeline) {
        self.opts = opts
        self.log = log
    }

    func start() throws {
        targetFrames = UInt32(opts.duration * TARGET_RATE)
        log.emit("start", ["bundle_id": Bundle.main.bundleIdentifier ?? "none", "expected_bundle_id": PROTO_BUNDLE_ID,
                           "pid": Int(getpid()), "os": ProcessInfo.processInfo.operatingSystemVersionString,
                           "out": opts.out!.path, "duration_s": opts.duration, "target_frames": Int(targetFrames),
                           "pids": opts.pids.map { Int($0) }, "with_output_subdevice": opts.withOutputSubdevice,
                           "usage_description": Bundle.main.object(forInfoDictionaryKey: "NSAudioCaptureUsageDescription") != nil])
        if Bundle.main.bundleIdentifier != PROTO_BUNDLE_ID {
            log.emit("warning", ["message": "not running from the prototype app bundle; TCC may attribute capture to the launching app (e.g. Terminal). Use run-capture.sh / open -W."], stderr: true)
        }

        // Tap description: global (minus this helper) or an explicit PID mixdown. Mono, unmuted, private.
        let desc: CATapDescription
        if opts.pids.isEmpty {
            let me = try processObject(getpid())
            let exclude: [AudioObjectID] = me == kAudioObjectUnknown ? [] : [me]
            desc = CATapDescription(monoGlobalTapButExcludeProcesses: exclude)
            log.emit("tap_scope", ["mode": "global_excluding_self", "excluded_objects": exclude.map { Int($0) }])
        } else {
            var objs: [AudioObjectID] = []
            for pid in opts.pids {
                let obj = try processObject(pid)
                guard obj != kAudioObjectUnknown else {
                    throw CaptureError.msg("PID \(pid) has no Core Audio process object (not running or never opened audio output); start playback first")
                }
                objs.append(obj)
                log.emit("process_object", ["pid": Int(pid), "object": Int(obj)])
            }
            desc = CATapDescription(monoMixdownOfProcesses: objs)
            log.emit("tap_scope", ["mode": "pid_mixdown", "objects": objs.map { Int($0) }])
        }
        desc.name = "Voicemore SysAudio Proto"
        desc.uuid = UUID()
        desc.muteBehavior = .unmuted // playback stays audible
        desc.isPrivate = true

        try check(AudioHardwareCreateProcessTap(desc, &tapID), "AudioHardwareCreateProcessTap")
        log.emit("tap_created", ["tap_id": Int(tapID), "tap_uid": desc.uuid.uuidString])

        var asbd = try tapFormat(tapID)
        log.emit("tap_format", ["sample_rate": asbd.mSampleRate, "channels": Int(asbd.mChannelsPerFrame),
                                "format_id": fourcc(OSStatus(bitPattern: asbd.mFormatID)), "flags": Int(asbd.mFormatFlags),
                                "bits": Int(asbd.mBitsPerChannel), "bytes_per_frame": Int(asbd.mBytesPerFrame)])
        guard let src = AVAudioFormat(streamDescription: &asbd) else { throw CaptureError.msg("unsupported tap format") }
        guard let conv = AVAudioConverter(from: src, to: dstFormat) else { throw CaptureError.msg("no converter from tap format") }
        srcFormat = src
        converter = conv
        tapBufferCount = src.isInterleaved ? 1 : Int(src.channelCount)

        // Private aggregate whose only input is the tap. Listing the output device is a fallback:
        // its input streams (e.g. an AirPods microphone) would then also be opened.
        let aggUID = "\(PROTO_BUNDLE_ID).aggregate.\(UUID().uuidString)"
        var dict: [String: Any] = [
            kAudioAggregateDeviceNameKey: "Voicemore SysAudio Proto",
            kAudioAggregateDeviceUIDKey: aggUID,
            kAudioAggregateDeviceIsPrivateKey: true,
            kAudioAggregateDeviceIsStackedKey: false,
            kAudioAggregateDeviceTapAutoStartKey: true,
            kAudioAggregateDeviceTapListKey: [[kAudioSubTapDriftCompensationKey: true, kAudioSubTapUIDKey: desc.uuid.uuidString]],
        ]
        if opts.withOutputSubdevice {
            let outUID = try defaultOutputUID()
            dict[kAudioAggregateDeviceMainSubDeviceKey] = outUID
            dict[kAudioAggregateDeviceSubDeviceListKey] = [[kAudioSubDeviceUIDKey: outUID]]
            log.emit("output_subdevice", ["uid": outUID])
        }
        try check(AudioHardwareCreateAggregateDevice(dict as CFDictionary, &aggID), "AudioHardwareCreateAggregateDevice")
        log.emit("aggregate_created", ["aggregate_id": Int(aggID), "uid": aggUID])

        wav = try WavWriter(opts.out!)

        try check(AudioDeviceCreateIOProcIDWithBlock(&procID, aggID, ioQueue) { [weak self] _, input, inputTime, _, _ in
            self?.process(input, inputTime)
        }, "AudioDeviceCreateIOProcIDWithBlock")
        try check(AudioDeviceStart(aggID, procID), "AudioDeviceStart")
        log.emit("io_started", ["note": "first start may show the system audio recording permission prompt; denied permission yields silent buffers"])
    }

    /// ioQueue only.
    func process(_ input: UnsafePointer<AudioBufferList>, _ time: UnsafePointer<AudioTimeStamp>) {
        guard !reachedTarget, let wav else { return }
        callbacks += 1
        let all = UnsafeMutableAudioBufferListPointer(UnsafeMutablePointer(mutating: input))
        if !layoutLogged {
            layoutLogged = true
            log.emit("first_buffer", ["host_time_ns": NSNumber(value: hostTimeNs(time.pointee.mHostTime)),
                                      "sample_time": time.pointee.mSampleTime, "buffers": all.count,
                                      "bytes": all.map { Int($0.mDataByteSize) }, "tap_buffers": tapBufferCount])
        }
        guard all.count >= tapBufferCount else { return }

        // Tap streams follow any sub-device streams; use the last tapBufferCount buffers.
        let list = AudioBufferList.allocate(maximumBuffers: tapBufferCount)
        defer { free(list.unsafeMutablePointer) }
        for i in 0..<tapBufferCount { list[i] = all[all.count - tapBufferCount + i] }
        guard let inBuf = AVAudioPCMBuffer(pcmFormat: srcFormat, bufferListNoCopy: list.unsafePointer, deallocator: nil),
              inBuf.frameLength > 0 else { return }
        srcFrames += UInt64(inBuf.frameLength)

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
            if convertErrors <= 3 { log.emit("error", ["stage": "convert", "message": err?.localizedDescription ?? "unknown"], stderr: true) }
            return
        }

        let remaining = Int(targetFrames - wav.frames)
        let n = min(Int(outBuf.frameLength), remaining)
        guard n > 0, let p = outBuf.int16ChannelData?[0] else { return }
        for i in 0..<n {
            let v = p[i]
            if v != 0 {
                nonzero += 1
                secNonzero += 1
                let a = v == Int16.min ? Int16.max : abs(v)
                if a > peak { peak = a }
                if a > secPeak { secPeak = a }
                if nonzero == 1 {
                    log.emit("first_nonzero_audio", ["frame": Int(wav.frames) + i,
                                                     "audio_s": Double(Int(wav.frames) + i) / TARGET_RATE,
                                                     "host_time_ns": NSNumber(value: hostTimeNs(time.pointee.mHostTime))])
                }
            }
        }
        wav.append(p, count: n)

        while wav.frames >= nextProgress {
            log.emit("progress", ["out_frames": Int(wav.frames), "audio_s": Double(wav.frames) / TARGET_RATE,
                                  "src_frames": NSNumber(value: srcFrames), "callbacks": callbacks,
                                  "host_time_ns": NSNumber(value: hostTimeNs(time.pointee.mHostTime)),
                                  "sample_time": time.pointee.mSampleTime,
                                  "peak_dbfs_1s": dbfs(secPeak), "nonzero_ratio_1s": Double(secNonzero) / TARGET_RATE])
            secPeak = 0
            secNonzero = 0
            nextProgress += 16000
        }
        if !zeroWarned, nonzero == 0, Double(wav.frames) >= opts.zeroWarnAfter * TARGET_RATE {
            zeroWarned = true
            log.emit("zero_audio_warning", ["audio_s": Double(wav.frames) / TARGET_RATE,
                                            "message": "only digital silence so far: system audio recording permission may be denied (System Settings → Privacy & Security → Screen & System Audio Recording), nothing is playing, or the selected PID is silent"],
                     stderr: true)
        }
        if wav.frames >= targetFrames {
            reachedTarget = true
            DispatchQueue.main.async { self.stop("duration_reached") }
        }
    }

    func dbfs(_ v: Int16) -> Double { v == 0 ? -120 : 20 * log10(Double(v) / 32767) }

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
        log.emit("stopping", ["reason": reason])
        teardown()
        var summary: [String: Any] = ["reason": reason]
        var code: Int32 = 0
        ioQueue.sync {
            let frames = wav?.frames ?? 0
            do { try wav?.finish() } catch {
                summary["wav_error"] = "\(error)"
                code = 2
            }
            summary["frames"] = Int(frames)
            summary["audio_s"] = Double(frames) / TARGET_RATE
            summary["complete"] = frames >= targetFrames
            summary["callbacks"] = callbacks
            summary["src_frames"] = NSNumber(value: srcFrames)
            summary["src_rate"] = srcFormat?.sampleRate ?? 0
            summary["nonzero_samples"] = NSNumber(value: nonzero)
            summary["peak_dbfs"] = dbfs(peak)
            summary["convert_errors"] = convertErrors
            if code == 0 {
                if callbacks == 0 { code = 4; summary["status"] = "failed_no_callbacks" }
                else if nonzero == 0 { code = 3; summary["status"] = "failed_zero_audio" }
                else if frames < targetFrames { summary["status"] = "partial" }
                else { summary["status"] = "ok" }
            }
        }
        if let out = opts.out, let data = try? Data(contentsOf: out) {
            summary["wav"] = out.path
            summary["wav_sha256"] = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
            summary["wav_bytes"] = data.count
        }
        summary["exit_code"] = Int(code)
        log.emit("summary", summary, stderr: true)
        log.close()
        exit(code)
    }
}

// MARK: - Main

let opts = parseOptions()
let timeline: Timeline
do { timeline = try Timeline(opts.log!) } catch {
    FileHandle.standardError.write("cannot open log: \(error)\n".data(using: .utf8)!)
    exit(2)
}
let capture = Capture(opts, timeline)
do { try capture.start() } catch {
    timeline.emit("error", ["stage": "setup", "message": "\(error)"], stderr: true)
    capture.teardown()
    timeline.close()
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

DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
    let count = capture.ioQueue.sync { capture.callbacks }
    if count == 0 {
        timeline.emit("no_callbacks", ["message": "no audio buffers within 3 s: tap/aggregate not running; check macOS ≥ 14.2, permission prompt, or retry with --with-output-subdevice"], stderr: true)
        capture.stop("no_callbacks")
    }
}
// Safety net if the device clock stalls: stop shortly after the bounded duration.
DispatchQueue.main.asyncAfter(deadline: .now() + opts.duration + 5) { capture.stop("wall_timeout") }

dispatchMain()
