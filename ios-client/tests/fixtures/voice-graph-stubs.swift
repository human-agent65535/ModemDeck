import Foundation

// These doubles model graph mutations and callbacks, not a physical audio device.
// Product startup, graph configuration, capture guards and failure methods are
// extracted unchanged by the JS runner. No AVFoundation or microphone is opened.
final class DispatchQueue {
    static let main = DispatchQueue()
    private var work: [() -> Void] = []
    func async(execute action: @escaping () -> Void) { work.append(action) }
    func drainNext() { precondition(!work.isEmpty); work.removeFirst()() }
    func drain() {
        while !work.isEmpty { work.removeFirst()() }
    }
}
enum StubError: Error { case enable, start }
enum AVAudioCommonFormat { case pcmFormatFloat32, pcmFormatInt16 }
typealias AVAudioFrameCount = UInt32
typealias AVAudioFramePosition = Int64
final class AVAudioFormat {
    let commonFormat: AVAudioCommonFormat
    let sampleRate: Double
    let channelCount: UInt32
    let isInterleaved: Bool
    init(commonFormat: AVAudioCommonFormat, sampleRate: Double, channels: UInt32, interleaved: Bool) {
        self.commonFormat = commonFormat; self.sampleRate = sampleRate
        channelCount = channels; isInterleaved = interleaved
    }
}
func hardwareFormat() -> AVAudioFormat {
    AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 48_000, channels: 4, interleaved: false)
}
func monoFormat() -> AVAudioFormat {
    AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 16_000, channels: 1, interleaved: false)
}
final class AVAudioPCMBuffer {
    let format: AVAudioFormat
    let frameLength: AVAudioFrameCount
    private let samples: UnsafeMutablePointer<Float>
    private let channels: UnsafeMutablePointer<UnsafeMutablePointer<Float>>
    var floatChannelData: UnsafePointer<UnsafeMutablePointer<Float>>? { UnsafePointer(channels) }
    init(format: AVAudioFormat = monoFormat(), frames: Int = 320, value: Float = 0.25) {
        self.format = format; frameLength = UInt32(frames)
        samples = .allocate(capacity: max(1, frames)); samples.initialize(repeating: value, count: max(1, frames))
        channels = .allocate(capacity: 1); channels.initialize(to: samples)
    }
    deinit {
        samples.deinitialize(count: max(1, Int(frameLength))); samples.deallocate()
        channels.deinitialize(count: 1); channels.deallocate()
    }
}
struct AVAudioTime: ExpressibleByIntegerLiteral {
    var hostTime: UInt64 = 0, sampleTime: Int64 = 0
    var sampleRate = 16_000.0, isHostTimeValid = false, isSampleTimeValid = false
    init(integerLiteral: Int) {}
    static func seconds(forHostTime value: UInt64) -> Double { Double(value) / 1_000_000_000 }
}
func mach_absolute_time() -> UInt64 { UInt64(ProcessInfo.processInfo.systemUptime * 1_000_000_000) }
typealias Tap = (AVAudioPCMBuffer, AVAudioTime) -> Void
class AVAudioNode {
    var input = hardwareFormat(), output = hardwareFormat()
    func inputFormat(forBus: Int) -> AVAudioFormat { input }
    func outputFormat(forBus: Int) -> AVAudioFormat { output }
}
final class AVAudioInputNode: AVAudioNode {
    var voiceProcessingEnables = 0, installedTaps = 0, removedTaps = 0
    var isVoiceProcessingAGCEnabled = false
    var enableFails = false, rejectTapFormat = false
    var tap: Tap?
    var lastBufferSize: AVAudioFrameCount?
    func setVoiceProcessingEnabled(_ enabled: Bool) throws {
        precondition(enabled); voiceProcessingEnables += 1
        if enableFails { throw StubError.enable }
    }
    func installTap(onBus: Int, bufferSize: AVAudioFrameCount, format: AVAudioFormat, block: @escaping Tap) {
        precondition(tap == nil, "Replacing a graph must remove the old tap")
        installedTaps += 1; lastBufferSize = bufferSize; tap = block
        if !rejectTapFormat { output = format }
    }
    func removeTap(onBus: Int) { precondition(tap != nil); tap = nil; removedTaps += 1 }
}
typealias OSStatus = Int32
let kAudio_ParamError: Int32 = -50, noErr: Int32 = 0
struct AudioTimeStampFlags: OptionSet { let rawValue: UInt32; static let hostTimeValid = Self(rawValue: 1) }
struct AudioTimeStamp { var mFlags: AudioTimeStampFlags = []; var mHostTime: UInt64 = 0 }
struct AudioBuffer { var mData: UnsafeMutableRawPointer? }
struct AudioBufferList { var mBuffers: AudioBuffer }
final class AVAudioSourceNode: AVAudioNode {
    typealias Render = (UnsafeMutablePointer<ObjCBool>, UnsafePointer<AudioTimeStamp>, UInt32, UnsafeMutablePointer<AudioBufferList>) -> Int32
    let block: Render
    init(format: AVAudioFormat, renderBlock: @escaping Render) { block = renderBlock; super.init(); output = format }
}
final class ModemDeckAudioRenderer {
    var resets = 0
    init() throws {}
    func resetRender() { resets += 1 }
    func render(nowUS: Int64, samples: UnsafeMutablePointer<Float>, count: Int) -> Int32 {
        samples.initialize(repeating: 0.25, count: count); return Int32(count)
    }
}
final class AVAudioEngine {
    enum Fault { case none, enable, configure, start, postStartFormat }
    static var nextFault = Fault.none
    static var instances = 0
    let inputNode = AVAudioInputNode(), outputNode = AVAudioNode(), mainMixerNode = AVAudioNode()
    var isRunning = false, starts = 0, stops = 0, prepares = 0
    var connections: [(AVAudioNode, AVAudioNode, AVAudioFormat)] = []
    var attached: [AVAudioNode] = []
    var startFails = false, changesFormatOnStart = false
    init() {
        Self.instances += 1
        inputNode.enableFails = Self.nextFault == .enable
        inputNode.rejectTapFormat = Self.nextFault == .configure
        startFails = Self.nextFault == .start; changesFormatOnStart = Self.nextFault == .postStartFormat
        Self.nextFault = .none
    }
    func attach(_ node: AVAudioNode) { attached.append(node) }
    func detach(_ node: AVAudioNode) { precondition(!isRunning); attached.removeAll { $0 === node } }
    func connect(_ from: AVAudioNode, to: AVAudioNode, format: AVAudioFormat) {
        precondition(!isRunning, "Graph changes must happen with the engine stopped")
        connections.append((from, to, format)); from.output = format; to.input = format
    }
    func prepare() { prepares += 1 }
    func start() throws {
        starts += 1
        if startFails { throw StubError.start }
        isRunning = true
        if changesFormatOnStart { outputNode.input = hardwareFormat() }
    }
    func stop() { stops += 1; isRunning = false }
}
func md_opus_encoder_create(_ rate: Int32, _ channels: Int32) -> OpaquePointer? { OpaquePointer(bitPattern: 1) }
final class Owner {
    private var dropCounts = ModemDeckAudioDropCounts()
    var stopped = false, activated = true, ready = true, connecting = false
    var engine: AVAudioEngine?, sourceNode: AVAudioSourceNode?, renderer: ModemDeckAudioRenderer?
    var encoder: OpaquePointer?
    var socket: Int? = 1
    var transportFailures = 0
    var renderedSampleCount: UInt64 = 0
    func transportFailed(task: Int?, error: Error) { transportFailures += 1; stopAudio() }
    let format = monoFormat(), queue = DispatchQueue(), captureLock = NSLock()
    var captureTapInstalled = false, captureGeneration = 0
    var captureSamples: [Float] = [], captured: [[Float]] = [], capturedSequences: [UInt32] = [], capturedEnds: [Double] = []
    var capturePendingSamples = 0, captureStreamGeneration = 0, droppedFrames = 0
    var captureSourceCursor: UInt64 = 0, captureSourceFrameStart: UInt64 = 0
    var captureDroppedSamples: UInt64 = 0, captureReportedDropFrames: UInt64 = 0
    var captureHardwareNext: AVAudioFramePosition?
    var sequence: UInt32 = 0, timestamp: UInt32 = 0
    var connectCompletion: ((Result<Void, Error>) -> Void)?
    var connectTimeout: DispatchWorkItem?
    var onFailed: ((Error) -> Void)?, onRemoteEnded: (() -> Void)?
    var failures: [Error] = [], remoteEnds = 0, stops: [Bool] = []
    var events: [String] = [], states: [String] = []
    var eventFields: [(String, [String: String])] = []
    init() {
        onFailed = { [weak self] in self?.failures.append($0) }
        onRemoteEnded = { [weak self] in self?.remoteEnds += 1 }
    }
    func start() { startAudioIfReady() }
    func resetStream() { resetCaptureStream() }
    func configurationChanged(_ changed: AVAudioEngine?) { audioConfigurationChanged(changed) }
    func deactivate() { activated = false; stopAudio() }
    func end() { stopped = true; stopAudio() }
    func capture(_ values: [Float], sampleEnd: Double) { captured.append(values); capturedSequences.append(sequence); capturedEnds.append(sampleEnd); captureSourceFrameStart += UInt64(values.count) }
    func recordConnectionEvent(_ event: String, fields: [String: String] = [:], error: Error? = nil) {
        events.append(event); eventFields.append((event, fields))
    }
    func publishMediaState(_ state: String) { states.append(state) }
    func stopLocked(notifyRemoteEnd: Bool) {
        stops.append(notifyRemoteEnd); stopped = true; stopAudio()
        if notifyRemoteEnd { DispatchQueue.main.async { [weak self] in self?.onRemoteEnded?() } }
    }
    // INSERT_PRODUCT_METHODS
}
func requireMono(_ format: AVAudioFormat) {
    precondition(format.sampleRate == 16_000 && format.channelCount == 1)
    precondition(format.commonFormat == .pcmFormatFloat32 && !format.isInterleaved)
}
@main struct Tests {
    static func main() {
        initialGraphAndDuplicateNotifications()
        stoppedAndChangedGraphs()
        obsoleteCaptureCallbacks()
        oldLargeCaptureDoesNotChangeTheNewClock()
        largeCurrentCapturePreservesAllSamples()
        oldCaptureCannotOccupyTheNewQueue()
        oldQueuedDeferCannotSubtractNewPendingSamples()
        failuresAreNotRemoteHangups()
        newerDroppedBatchCannotRelabelOlderQueuedCapture()
        reconnectStreamResetIsolatesAlreadyQueuedCapture()
        hardwareSampleAndHostTimesSurviveDispatch()
    }
    static func initialGraphAndDuplicateNotifications() {
        let owner = Owner(), before = AVAudioEngine.instances
        owner.start()
        let engine = owner.engine!, source = owner.sourceNode!
        precondition(AVAudioEngine.instances == before + 1)
        requireMono(engine.inputNode.output); requireMono(engine.outputNode.input)
        precondition(engine.inputNode.voiceProcessingEnables == 1 && engine.inputNode.isVoiceProcessingAGCEnabled)
        precondition(engine.connections.count == 2)
        precondition(engine.connections[0].0 === source && engine.connections[0].1 === engine.mainMixerNode)
        precondition(engine.connections[1].0 === engine.mainMixerNode && engine.connections[1].1 === engine.outputNode)
        engine.connections.forEach { requireMono($0.2) }
        precondition(engine.inputNode.lastBufferSize == 1600 && engine.starts == 1)
        for _ in 0..<5 { owner.configurationChanged(engine); owner.start() }
        precondition(owner.engine === engine && AVAudioEngine.instances == before + 1)
        precondition(engine.starts == 1 && engine.stops == 0 && engine.inputNode.installedTaps == 1)
        precondition(owner.events.filter { $0 == "voice_processing_started" }.count == 1)
        precondition(owner.failures.isEmpty && owner.remoteEnds == 0)
    }
    static func stoppedAndChangedGraphs() {
        let owner = Owner(); owner.start()
        let engine = owner.engine!, receiver = owner.renderer!, before = AVAudioEngine.instances
        engine.isRunning = false; engine.inputNode.output = hardwareFormat(); engine.outputNode.input = hardwareFormat()
        owner.configurationChanged(engine)
        precondition(owner.engine === engine && owner.renderer === receiver && AVAudioEngine.instances == before)
        precondition(engine.starts == 2 && engine.inputNode.voiceProcessingEnables == 1 && receiver.resets == 1)
        precondition(engine.inputNode.removedTaps == 1 && engine.inputNode.installedTaps == 2)
        requireMono(engine.inputNode.output); requireMono(engine.outputNode.input)
        owner.configurationChanged(engine)
        precondition(engine.starts == 2, "Reconfiguration's own queued notification must be inert")
        engine.outputNode.input = hardwareFormat() // A still-running graph with mismatched output also needs one reconfiguration.
        owner.configurationChanged(engine)
        precondition(engine.starts == 3 && engine.inputNode.voiceProcessingEnables == 1)
        requireMono(engine.outputNode.input)
        owner.configurationChanged(AVAudioEngine()); owner.configurationChanged(nil)
        precondition(engine.starts == 3)
        engine.isRunning = false
        owner.activated = false; owner.configurationChanged(engine)
        precondition(engine.starts == 3, "A queued notification must not restart deactivated audio")
        owner.activated = true; owner.stopped = true; owner.configurationChanged(engine)
        precondition(engine.starts == 3, "An ended call must ignore a queued configuration notification")
        owner.stopped = false
        owner.deactivate(); owner.configurationChanged(engine); owner.start()
        precondition(owner.engine == nil && engine.starts == 3)
        owner.activated = true; owner.start()
        let next = owner.engine!
        owner.configurationChanged(engine)
        precondition(next.starts == 1 && owner.engine === next, "An old engine must not affect its replacement")
        owner.end(); owner.configurationChanged(next); owner.start()
        precondition(owner.engine == nil && next.starts == 1)

        for changed in [
            AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 16_000, channels: 1, interleaved: true),
            AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16_000, channels: 1, interleaved: false)
        ] {
            let owner = Owner(); owner.start(); let engine = owner.engine!
            engine.inputNode.output = changed
            owner.configurationChanged(engine)
            precondition(engine.starts == 2, "Matching rate/channel count alone cannot validate the voice format")
            requireMono(engine.inputNode.output)
        }
    }
    static func obsoleteCaptureCallbacks() {
        let owner = Owner(); owner.start()
        let engine = owner.engine!, oldTap = engine.inputNode.tap!
        oldTap(AVAudioPCMBuffer(value: 0.1), 0) // Queue the old-generation work before reconfiguration.
        precondition(owner.capturePendingSamples == 320)
        engine.isRunning = false; owner.configurationChanged(engine)
        let newTap = engine.inputNode.tap!
        oldTap(AVAudioPCMBuffer(value: 0.2), 0) // An in-flight removed tap can still finish afterward.
        oldTap(AVAudioPCMBuffer(format: hardwareFormat()), 0) // It must not fail the replacement graph either.
        newTap(AVAudioPCMBuffer(value: 0.3), 0)
        owner.queue.drain(); DispatchQueue.main.drain()
        precondition(owner.capturePendingSamples == 0 && owner.captured.count == 1)
        precondition(owner.captured[0].allSatisfy { $0 == 0.3 })
        precondition(owner.failures.isEmpty && owner.remoteEnds == 0)
        newTap(AVAudioPCMBuffer(), 0); owner.deactivate(); owner.queue.drain()
        precondition(owner.captured.count == 1 && owner.capturePendingSamples == 0)
    }
    static func oldLargeCaptureDoesNotChangeTheNewClock() {
        let owner = Owner(); owner.start()
        let engine = owner.engine!, oldTap = engine.inputNode.tap!
        engine.isRunning = false; owner.configurationChanged(engine)
        let newTap = engine.inputNode.tap!
        oldTap(AVAudioPCMBuffer(frames: 3200, value: 0.1), 0)
        owner.queue.drain()
        precondition(owner.capturePendingSamples == 0 && owner.capturePendingSamples >= 0)
        newTap(AVAudioPCMBuffer(value: 0.8), 0); owner.queue.drain()
        precondition(owner.captured.count == 1 && owner.captured[0].allSatisfy { $0 == 0.8 })
        precondition(owner.sequence == 0 && owner.timestamp == 0 && owner.droppedFrames == 0,
                     "A removed tap's trimmed samples must not advance the replacement capture clock")
        precondition(owner.capturePendingSamples >= 0 && owner.capturePendingSamples == 0)
    }
    static func largeCurrentCapturePreservesAllSamples() {
        let owner = Owner(); owner.start()
        owner.engine!.inputNode.tap!(AVAudioPCMBuffer(frames: 3200, value: 0.5), 0)
        owner.queue.drain()
        precondition(owner.captured.count == 1 && owner.captured[0].count == 3200)
        precondition(owner.sequence == 0 && owner.timestamp == 0 && owner.droppedFrames == 0,
                     "Actual callback length must not be silently truncated to the requested tap size")
        precondition(owner.capturePendingSamples == 0 && owner.capturePendingSamples >= 0)
        owner.engine!.inputNode.tap!(AVAudioPCMBuffer(frames: 320), 0)
        owner.queue.drain()
        precondition(owner.captured.count == 2 && owner.captured[1].count == 320)
        precondition(owner.sequence == 0 && owner.timestamp == 0 && owner.droppedFrames == 0)
    }
    static func oldCaptureCannotOccupyTheNewQueue() {
        let owner = Owner(); owner.start()
        let engine = owner.engine!, oldTap = engine.inputNode.tap!
        engine.isRunning = false; owner.configurationChanged(engine)
        let newTap = engine.inputNode.tap!
        owner.queue.async {
            oldTap(AVAudioPCMBuffer(frames: 1600, value: 0.1), 0)
            newTap(AVAudioPCMBuffer(value: 0.7), 0)
            precondition(owner.capturePendingSamples == 320 && owner.capturePendingSamples >= 0,
                         "An obsolete tap cannot consume the current generation's 100 ms allowance")
        }
        owner.queue.drain()
        precondition(owner.captured.count == 1 && owner.captured[0].allSatisfy { $0 == 0.7 })
        precondition(owner.capturePendingSamples == 0 && owner.capturePendingSamples >= 0)
        precondition(owner.sequence == 0 && owner.timestamp == 0 && owner.droppedFrames == 0)
    }
    static func oldQueuedDeferCannotSubtractNewPendingSamples() {
        let owner = Owner(); owner.start()
        let engine = owner.engine!, oldTap = engine.inputNode.tap!
        oldTap(AVAudioPCMBuffer(frames: 1600, value: 0.1), 0)
        precondition(owner.capturePendingSamples == 1600)
        engine.isRunning = false; owner.configurationChanged(engine)
        precondition(owner.capturePendingSamples == 0 && owner.capturePendingSamples >= 0,
                     "Reconfiguration must start a fresh capture accounting generation")
        engine.inputNode.tap!(AVAudioPCMBuffer(value: 0.6), 0)
        precondition(owner.capturePendingSamples == 320)
        owner.queue.drainNext() // Old queued work returns without capturing; its defer must also be obsolete.
        precondition(owner.capturePendingSamples == 320 && owner.captured.isEmpty,
                     "An old generation's defer must not subtract the new generation's pending samples")
        owner.queue.drainNext()
        precondition(owner.capturePendingSamples == 0 && owner.captured.count == 1)
        precondition(owner.captured[0].allSatisfy { $0 == 0.6 })
        engine.inputNode.tap!(AVAudioPCMBuffer(frames: 1600), 0)
        precondition(owner.capturePendingSamples == 1600)
        owner.deactivate()
        precondition(owner.capturePendingSamples == 0 && owner.capturePendingSamples >= 0)
        owner.queue.drain()
        precondition(owner.capturePendingSamples == 0, "A stopped generation's defer must not create a negative count")
    }
    static func failuresAreNotRemoteHangups() {
        for fault in [AVAudioEngine.Fault.enable, .configure, .start, .postStartFormat] {
            AVAudioEngine.nextFault = fault
            let owner = Owner(); owner.start(); DispatchQueue.main.drain()
            precondition(owner.stopped && owner.engine == nil && owner.failures.count == 1)
            precondition(owner.stops == [false] && owner.remoteEnds == 0)
            precondition(!owner.states.contains("active"), "An invalid graph must never publish successful audio")
            if fault == .configure {
                precondition(owner.eventFields.contains { $0.0 == "voice_input_format" && $0.1 == ["sample_rate": "48000", "channels": "4"] })
                precondition(owner.eventFields.contains { $0.0 == "voice_output_format" && $0.1 == ["sample_rate": "16000", "channels": "1"] })
            }
            owner.start(); DispatchQueue.main.drain()
            precondition(owner.failures.count == 1)
        }
        for failsAtConfiguration in [true, false] {
            let owner = Owner(); owner.start()
            let engine = owner.engine!; engine.isRunning = false
            engine.inputNode.output = hardwareFormat()
            engine.inputNode.rejectTapFormat = failsAtConfiguration; engine.startFails = !failsAtConfiguration
            owner.configurationChanged(engine); DispatchQueue.main.drain()
            precondition(owner.failures.count == 1 && owner.remoteEnds == 0 && owner.stops == [false])
        }
        let invalidCapture = Owner(); invalidCapture.start()
        invalidCapture.engine!.inputNode.tap!(AVAudioPCMBuffer(format: hardwareFormat()), 0)
        invalidCapture.queue.drain(); DispatchQueue.main.drain()
        precondition(invalidCapture.failures.count == 1 && invalidCapture.remoteEnds == 0)
        let connecting = Owner(); var connectionFailures = 0
        connecting.connectCompletion = { if case .failure = $0 { connectionFailures += 1 } }
        AVAudioEngine.nextFault = .start; connecting.start(); DispatchQueue.main.drain()
        precondition(connectionFailures == 1 && connecting.failures.isEmpty && connecting.remoteEnds == 0)
    }
    static func newerDroppedBatchCannotRelabelOlderQueuedCapture() {
        let owner = Owner(); owner.start(); let tap = owner.engine!.inputNode.tap!
        tap(AVAudioPCMBuffer(frames: 1600, value: 0.1), 0)
        tap(AVAudioPCMBuffer(frames: 2400, value: 0.2), 0)
        owner.queue.drain()
        precondition(owner.capturedSequences == [0, 0])
        precondition(owner.captured.map(\.count) == [1600, 2400] && owner.droppedFrames == 0)
        precondition(owner.captureSourceCursor == 4000 && owner.capturePendingSamples == 0)
        tap(AVAudioPCMBuffer(frames: Int(md_audio_send_capacity()) * 320 + 1), 0)
        owner.queue.drain()
        precondition(owner.transportFailures == 1 && owner.engine == nil,
                     "Resource exhaustion reports backpressure instead of silently deleting speech")
    }
    static func reconnectStreamResetIsolatesAlreadyQueuedCapture() {
        let owner = Owner(); owner.start(); let tap = owner.engine!.inputNode.tap!
        tap(AVAudioPCMBuffer(frames: 1600, value: 0.1), 0)
        owner.resetStream()
        tap(AVAudioPCMBuffer(frames: 320, value: 0.4), 0)
        owner.queue.drainNext()
        precondition(owner.capturePendingSamples == 320 && owner.captured.isEmpty)
        owner.queue.drain()
        precondition(owner.capturePendingSamples == 0 && owner.captured.count == 1 && owner.captured[0][0] == 0.4)
        precondition(owner.sequence == 0 && owner.timestamp == 0 && owner.droppedFrames == 0)
    }

    static func hardwareSampleAndHostTimesSurviveDispatch() {
        let owner = Owner(); owner.start(); let tap = owner.engine!.inputNode.tap!
        var first: AVAudioTime = 0
        first.isSampleTimeValid = true; first.sampleTime = 0
        first.isHostTimeValid = true
        first.hostTime = mach_absolute_time() - 70_000_000 // 20 ms buffer ended 50 ms ago.
        let before = ProcessInfo.processInfo.systemUptime
        tap(AVAudioPCMBuffer(frames: 320), first); owner.queue.drain()
        precondition(abs(owner.capturedEnds[0] - (before - 0.05)) < 0.005,
                     "Dispatch must retain hardware capture time instead of relabeling old audio as now")
        var second: AVAudioTime = 0; second.isSampleTimeValid = true; second.sampleTime = 640
        tap(AVAudioPCMBuffer(frames: 320), second); owner.queue.drain()
        precondition(owner.capturedSequences == [0, 1] && owner.captureSourceCursor == 960 && owner.droppedFrames == 1,
                     "A genuine input sample-clock gap advances media ticks and is accounted once")
    }

}
