import Foundation

// Deterministic rendering and downstream-device clocks; no audio device opens.
// The fixture executes the product receive/flush/capture methods unchanged.
final class ProcessInfo {
    static let processInfo = ProcessInfo()
    var systemUptime: Double { Double(Simulation.now) / 1000 }
}
final class Queue {
    var work: [() -> Void] = []
    func async(execute action: @escaping () -> Void) { work.append(action) }
    func drain() { while !work.isEmpty { work.removeFirst()() } }
}
final class Simulation {
    static var now = 0
    static var events: [(Int, () -> Void)] = []
    static func reset() { now = 0; events = [] }
    static func advance(to time: Int) {
        while let first = events.enumerated().filter({ $0.element.0 <= time }).min(by: { $0.element.0 < $1.element.0 }) {
            now = first.element.0
            events.remove(at: first.offset).1()
        }
        now = time
    }
}
enum CallbackType { case dataPlayedBack, dataRendered }
typealias AVAudioFramePosition = Int64
func mach_absolute_time() -> UInt64 { UInt64(Simulation.now) * 1_000_000 }
final class AVAudioTime {
    var hostTime: UInt64 = 0, sampleTime: Int64 = 0
    init(hostTime: UInt64) { self.hostTime = hostTime }
    init(sampleTime: Int64, atRate: Double) { precondition(atRate == 16_000); self.sampleTime = sampleTime }
    static func hostTime(forSeconds seconds: Double) -> UInt64 { UInt64((seconds * 1_000_000_000).rounded()) }
}
final class AVAudioFormat {}
final class AVAudioPCMBuffer {
    var frameLength: UInt32 = 0
    let samples: UnsafeMutablePointer<Float>
    let channels: UnsafeMutablePointer<UnsafeMutablePointer<Float>>
    let capacity: Int
    var floatChannelData: UnsafePointer<UnsafeMutablePointer<Float>>? { UnsafePointer(channels) }
    init?(pcmFormat: AVAudioFormat, frameCapacity: UInt32) {
        capacity = Int(frameCapacity)
        samples = .allocate(capacity: capacity); samples.initialize(repeating: 0, count: capacity)
        channels = .allocate(capacity: 1); channels.initialize(to: samples)
    }
    deinit { samples.deinitialize(count: capacity); samples.deallocate(); channels.deinitialize(count: 1); channels.deallocate() }
}
final class AVAudioEngine { var isRunning = true }
final class AVAudioPlayerNode {
    var isPlaying = false, renderStall = 0, hardwareLatency = 0, renderCallbackDelay = 0
    var stopTimes: [Int] = [], playStarts: [Int] = [], scheduledSlots: [Int64] = []
    var reportsNegativeBeforeStart = false
    var completions: [() -> Void] = []
    private var waiting: [(Int64, CallbackType, () -> Void)] = []
    private var epochStart = 0
    let queue: Queue
    init(queue: Queue) { self.queue = queue }
    func scheduleBuffer(_ buffer: AVAudioPCMBuffer, at time: AVAudioTime, options: [Int],
                        completionCallbackType type: CallbackType, completionHandler: @escaping (CallbackType) -> Void) {
        precondition(buffer.frameLength == 320)
        scheduledSlots.append(time.sampleTime)
        let completion = { completionHandler(type); self.queue.drain() }
        completions.append(completion)
        if isPlaying { scheduleCompletion(time.sampleTime, type, completion) }
        else { waiting.append((time.sampleTime, type, completion)) }
    }
    private func scheduleCompletion(_ sampleTime: Int64, _ type: CallbackType, _ completion: @escaping () -> Void) {
        let renderEnd = epochStart + Int(sampleTime) / 16 + 20 + renderStall
        let callbackAt = renderEnd + renderCallbackDelay + (type == .dataPlayedBack ? hardwareLatency : 0)
        precondition(callbackAt >= Simulation.now, "Do not schedule audio into the past")
        // Keep old delayed completions to exercise product generation isolation.
        Simulation.events.append((callbackAt, completion))
    }
    var lastRenderTime: AVAudioTime? { isPlaying ? AVAudioTime(sampleTime: 0, atRate: 16_000) : nil }
    func playerTime(forNodeTime time: AVAudioTime) -> AVAudioTime? {
        guard isPlaying else { return nil }
        if Simulation.now < epochStart {
            return reportsNegativeBeforeStart ? AVAudioTime(sampleTime: Int64(Simulation.now - epochStart) * 16, atRate: 16_000) : nil
        }
        return AVAudioTime(sampleTime: Int64(max(0, Simulation.now - epochStart - renderStall)) * 16, atRate: 16_000)
    }
    func stop() { stopTimes.append(Simulation.now); isPlaying = false; waiting = [] }
    func play(at time: AVAudioTime) {
        epochStart = Int(time.hostTime / 1_000_000); playStarts.append(epochStart); isPlaying = true
        for (sampleTime, type, completion) in waiting { scheduleCompletion(sampleTime, type, completion) }
        waiting = []
    }
}
var encodedFrames: [[Float]] = []
var failEncodeAttempt: Int?
func opus_decode_float(_ decoder: OpaquePointer, _ input: UnsafePointer<UInt8>?, _ count: Int32,
                       _ output: inout [Float], _ capacity: Int32, _ fec: Int32) -> Int32 { 320 }
func opus_encode_float(_ encoder: OpaquePointer, _ frame: inout [Float], _ size: Int32,
                       _ payload: inout [UInt8], _ capacity: Int32) -> Int32 {
    precondition(size == 320 && frame.count == 320)
    encodedFrames.append(frame)
    if encodedFrames.count == failEncodeAttempt { return -1 }
    payload[0] = 1; return 1
}
final class Socket {
    enum Message { case data(Data) }
    let queue: Queue
    var delay = 0, sent: [Data] = [], sendTimes: [Int] = []
    init(queue: Queue) { self.queue = queue }
    func send(_ message: Message, completionHandler: @escaping (Error?) -> Void) {
        if case .data(let data) = message { sent.append(data); sendTimes.append(Simulation.now) }
        Simulation.events.append((Simulation.now + delay, { completionHandler(nil); self.queue.drain() }))
    }
}
final class Owner {
    private var dropCounts = ModemDeckAudioDropCounts()
    var decoder: OpaquePointer? = OpaquePointer(bitPattern: 1), encoder: OpaquePointer? = OpaquePointer(bitPattern: 2)
    let format = AVAudioFormat(), engine: AVAudioEngine? = AVAudioEngine(), queue = Queue()
    var player: AVAudioPlayerNode?
    var receiveClock = ModemDeckAudioClock(), receivedPackets = 0, droppedFrames = 0, renderedPackets = 0, capturedFrames = 0
    var activated = true, ready = true, muted = false, playbackPending = 0, playbackGeneration = 0
    var lastRenderedAudioAt = 0.0, microphoneLevel = -96.0
    var playbackEpochSourceSamples: Double?
    var playbackFrameEnds: [AVAudioFramePosition] = []
    var playbackLastSampleEnd: AVAudioFramePosition = 0
    var playbackEpochStartedAt = 0.0, playbackQueuedUntil = -Double.infinity
    var playbackUnderruns = 0, playbackResets = 0
    var captureSourceFrameStart: UInt64 = 0
    var captureSamples: [Float] = [], sendQueue: [(Data, Double)] = [], sending = false
    var socket: Socket?, stopped = false, sendStarted = 0.0, sentPackets = 0
    var lastSentAudioAt = -Double.infinity
    var sent: [Data] { socket!.sent }
    var sequence: UInt32 = 0, timestamp: UInt32 = 0
    init(latency: Int = 0) { player = AVAudioPlayerNode(queue: queue); player!.hardwareLatency = latency; socket = Socket(queue: queue) }
    func receive(_ sequence: UInt32) {
        receiveAudio(ModemDeckAudioPacket.encode(sequence: sequence, timestamp: sequence &* 320, payload: Data([1])))
        queue.drain()
    }
    func resetPlayback() { flushPlayback() }
    func captureWithEnd(_ samples: [Float], end: Double) { capture(samples, sampleEnd: end) }
    func captureIngress(_ samples: [Float]) { capture(samples, sampleEnd: ProcessInfo.processInfo.systemUptime) }
    func completeSends() {
        while sending {
            Simulation.advance(to: Simulation.events.map(\.0).min()!); queue.drain()
        }
    }
    func transportFailed(task: Socket?, error: Error) { preconditionFailure("Unexpected transport failure") }
    func recordConnectionEvent(_ name: String) { preconditionFailure(name) }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        deviceLatencyIsNotUnrenderedBacklog()
        unrenderedBacklogIsStillBounded()
        oldCompletionsCannotReleaseNewBuffers()
        capturePreservesTwentyMillisecondBoundaries()
        encodingFailureKeepsSourceTimeAndMuteKeepsCadence()
        sendQueueUsesSampleAgeNotCompletionCount()
        expiredCaptureUsesEachFramesOwnSampleEnd()
        expandedSourceSlotsPreserveTimestampWrap()
        shortJitterAndBatchesKeepTheSamePlaybackEpoch()
        delayedCompletionDeliveryIsNotRenderingBacklog()
        explicitSourceSlotsPreserveMissingFrames()
        aLongPauseCannotWashOldAudioIntoAFreshEpoch()
        aFutureStartCannotRetireUnplayedFrames()
        sourceClockReanchorStartsOneNewPlaybackEpoch()
    }
    static func deviceLatencyIsNotUnrenderedBacklog() {
        for latency in [120, 200] {
            Simulation.reset()
            let owner = Owner(latency: latency)
            for frame in 0..<1500 {
                Simulation.advance(to: frame * 20); owner.receive(UInt32(frame))
                precondition(owner.playbackPending <= 3)
            }
            Simulation.advance(to: 30_100)
            precondition(owner.player!.stopTimes.isEmpty, "Device latency must not cause periodic stop/restart")
            precondition(owner.droppedFrames == 0 && owner.playbackGeneration == 0)
            precondition(owner.playbackUnderruns == 0 && owner.playbackResets == 0)
            precondition(owner.player!.playStarts == [40], "The first source slot starts with exactly 40 ms prebuffer")
            precondition(owner.receivedPackets == 1500 && owner.renderedPackets == 1500 && owner.playbackPending == 0)
        }
    }
    static func unrenderedBacklogIsStillBounded() {
        Simulation.reset()
        let owner = Owner(latency: 200)
        owner.player!.renderStall = 200 // Actual player rendering is stalled, not merely device playback.
        for frame in 0..<50 {
            Simulation.advance(to: frame * 20); owner.receive(UInt32(frame))
            precondition((1...7).contains(owner.playbackPending))
        }
        precondition(!owner.player!.stopTimes.isEmpty && owner.player!.stopTimes.first == 140)
        precondition(owner.droppedFrames == 7 * owner.player!.stopTimes.count)
        precondition(owner.playbackGeneration == owner.player!.stopTimes.count)
        precondition(owner.playbackResets == owner.player!.stopTimes.count && owner.playbackUnderruns == 0)
    }
    static func oldCompletionsCannotReleaseNewBuffers() {
        Simulation.reset()
        let owner = Owner()
        for frame in 0..<5 { owner.receive(UInt32(frame)) }
        let oldCompletions = owner.player!.completions
        precondition(owner.playbackPending == 5)
        owner.resetPlayback(); owner.receive(5)
        precondition(owner.playbackPending == 1)
        for completion in oldCompletions {
            completion()
            precondition(owner.playbackPending == 1, "A stopped generation cannot decrement replacement buffers")
        }
        owner.player!.completions.last!()
        precondition(owner.playbackPending == 0)
        oldCompletions.forEach { $0() }
        precondition(owner.playbackPending == 0)
    }
    static func shortJitterAndBatchesKeepTheSamePlaybackEpoch() {
        Simulation.reset()
        let owner = Owner(latency: 200)
        // Five frames delivered every 100 ms. One batch is 40 ms late, then
        // cadence catches up; the prebuffer plus explicit slots stays continuous.
        for batch in 0..<20 {
            let arrival = batch * 100 + (batch == 5 ? 40 : 0)
            Simulation.advance(to: arrival)
            for offset in 0..<5 { owner.receive(UInt32(batch * 5 + offset)) }
            precondition(owner.playbackPending <= 7)
        }
        Simulation.advance(to: 2100)
        precondition(owner.player!.stopTimes.isEmpty && owner.player!.playStarts == [40])
        precondition(owner.renderedPackets == 100 && owner.droppedFrames == 0)
        precondition(owner.playbackUnderruns == 0 && owner.playbackResets == 0 && owner.playbackPending == 0)
    }
    static func delayedCompletionDeliveryIsNotRenderingBacklog() {
        for callbackDelay in [2, 200] {
            Simulation.reset()
            let owner = Owner(latency: 200)
            owner.player!.renderCallbackDelay = callbackDelay
            for batch in 0..<20 {
                Simulation.advance(to: batch * 100)
                for offset in 0..<5 { owner.receive(UInt32(batch * 5 + offset)) }
                precondition(owner.playbackPending <= 7)
            }
            Simulation.advance(to: 2400)
            precondition(owner.player!.stopTimes.isEmpty && owner.playbackResets == 0 && owner.droppedFrames == 0,
                         "A rendered frame awaiting its callback must not overflow a legitimate batch")
            precondition(owner.playbackPending == 0)
        }
    }
    static func explicitSourceSlotsPreserveMissingFrames() {
        Simulation.reset()
        let owner = Owner()
        owner.receive(0)
        Simulation.advance(to: 20); owner.receive(2) // One absent 20 ms source frame.
        precondition(owner.player!.scheduledSlots == [0, 640], "Missing source ticks must remain a silent sample-time gap")
        precondition(abs(owner.playbackQueuedUntil - 0.1) < 0.000001)
        precondition(owner.player!.playStarts == [40] && owner.player!.stopTimes.isEmpty)
        Simulation.advance(to: 100)
        precondition(owner.playbackPending == 0)
    }
    static func aLongPauseCannotWashOldAudioIntoAFreshEpoch() {
        Simulation.reset()
        let owner = Owner()
        for frame in 0..<5 { owner.receive(UInt32(frame)) }
        Simulation.advance(to: 500)
        precondition(owner.playbackPending == 0)
        let rendered = owner.renderedPackets
        for frame in 5..<15 { owner.receive(UInt32(frame)) }
        precondition(owner.renderedPackets == rendered && owner.player!.playStarts == [40],
                     "An empty player cannot make old TCP audio fresh")
        precondition(owner.playbackUnderruns == 0)
        // Source time has caught up: the next fresh sequence establishes a new
        // playback epoch, without reanchoring the source freshness clock.
        Simulation.advance(to: 600); owner.receive(30)
        precondition(owner.receiveClock.generation == 1)
        precondition(owner.player!.playStarts == [40, 640])
        precondition(owner.playbackUnderruns == 1 && owner.playbackResets == 0)
        let pending = owner.playbackPending
        owner.player!.completions.prefix(5).forEach { $0() }
        precondition(owner.playbackPending == pending)
        Simulation.advance(to: 680)
        precondition(owner.playbackPending == 0)
    }
    static func aFutureStartCannotRetireUnplayedFrames() {
        for negativePlayerTime in [false, true] {
            Simulation.reset()
            let owner = Owner()
            owner.player!.reportsNegativeBeforeStart = negativePlayerTime
            owner.receive(0)
            Simulation.advance(to: 20); owner.receive(1)
            Simulation.advance(to: 39); owner.receive(2)
            precondition(owner.player!.playStarts == [40] && owner.playbackPending == 3,
                         "Nil or negative player time before future host start must not retire prebuffered frames")
            precondition(owner.playbackFrameEnds == [320, 640, 960])
            Simulation.advance(to: 120)
            precondition(owner.playbackPending == 0 && owner.droppedFrames == 0)
        }
    }
    static func sourceClockReanchorStartsOneNewPlaybackEpoch() {
        Simulation.reset()
        let owner = Owner()
        for frame in 0..<20 {
            Simulation.advance(to: frame * 20); owner.receive(UInt32(frame))
        }
        for frame in 20...35 {
            Simulation.advance(to: frame * 20 + 200); owner.receive(UInt32(frame))
        }
        precondition(owner.receiveClock.generation == 2)
        precondition(owner.playbackResets == 1 && owner.playbackUnderruns == 0)
        precondition(owner.player!.playStarts == [40, 940])
        let pending = owner.playbackPending
        owner.player!.completions.prefix(20).forEach { $0() }
        precondition(owner.playbackPending == pending)
        Simulation.advance(to: 1000)
        precondition(owner.playbackPending == 0)
    }
    static func capturePreservesTwentyMillisecondBoundaries() {
        Simulation.reset(); encodedFrames = []
        let owner = Owner(), samples = (0..<1600).map { Float($0) / 1600 }
        owner.captureIngress(samples)
        precondition(owner.capturedFrames == 5 && encodedFrames.count == 5 && owner.captureSamples.isEmpty)
        precondition(encodedFrames.flatMap { $0 } == samples)
        precondition(owner.sent.count == 1 && owner.sendQueue.count == 4 && owner.droppedFrames == 0,
                     "A 100 ms tap batch must fit one in-flight plus four queued 20 ms packets")
        owner.completeSends()
        for (index, data) in owner.sent.enumerated() {
            let packet = Owner.ModemDeckAudioPacket.decode(data)!
            precondition(packet.sequence == UInt32(index) && packet.timestamp == UInt32(index * 320))
        }
        precondition(owner.sequence == 5 && owner.timestamp == 1600)
        encodedFrames = []
        let split = Owner(), next = (0..<640).map { Float($0) / 640 }
        split.captureIngress(Array(next.prefix(127)))
        precondition(encodedFrames.isEmpty && split.captureSamples.count == 127)
        split.captureIngress(Array(next.dropFirst(127).prefix(224)))
        precondition(encodedFrames.count == 1 && split.captureSamples.count == 31)
        split.captureIngress(Array(next.dropFirst(351)))
        precondition(encodedFrames.count == 2 && split.captureSamples.isEmpty)
        precondition(encodedFrames.flatMap { $0 } == next && split.timestamp == 640 && split.droppedFrames == 0)
    }
    static func encodingFailureKeepsSourceTimeAndMuteKeepsCadence() {
        Simulation.reset(); encodedFrames = []; failEncodeAttempt = 2
        let owner = Owner()
        owner.captureIngress(Array(repeating: 0.5, count: 960)); owner.completeSends()
        precondition(owner.capturedFrames == 3 && owner.droppedFrames == 1)
        precondition(owner.sequence == 3 && owner.timestamp == 960)
        let packets = owner.sent.map { Owner.ModemDeckAudioPacket.decode($0)! }
        precondition(packets.map(\.sequence) == [0, 2] && packets.map(\.timestamp) == [0, 640],
                     "A codec failure must leave a source-time hole rather than compressing later speech")
        failEncodeAttempt = nil; encodedFrames = []
        let muted = Owner(); muted.muted = true
        muted.captureIngress(Array(repeating: 0.5, count: 1600)); muted.completeSends()
        precondition(muted.sent.count == 5 && muted.droppedFrames == 0 && muted.timestamp == 1600)
        precondition(encodedFrames.flatMap { $0 }.allSatisfy { $0 == 0 },
                     "Mute replaces samples with silence without stopping the media clock")
    }

    static func sendQueueUsesSampleAgeNotCompletionCount() {
        for delay in [19, 20, 21, 25] {
            Simulation.reset(); encodedFrames = []; failEncodeAttempt = nil
            let owner = Owner(); owner.socket!.delay = delay
            for batch in 1...300 {
                let arrival = batch * 100
                Simulation.advance(to: arrival - 1)
                // Capture arrives first at an exactly coincident completion.
                Simulation.now = arrival
                owner.captureIngress(Array(repeating: 0.5, count: 1600))
                precondition(owner.sendQueue.count <= Int(md_audio_send_queue_capacity()))
                Simulation.advance(to: arrival); owner.queue.drain()
            }
            owner.completeSends()
            if delay <= 20 {
                precondition(owner.sent.count == 1500 && owner.droppedFrames == 0,
                             "20 ms completions must not manufacture a periodic 200 ms capture dropout")
            } else {
                precondition(owner.droppedFrames > 0 && owner.sent.count + owner.droppedFrames == 1500,
                             "Insufficient throughput must discard old media, without extending its deadline")
            }
            for (data, sentAt) in zip(owner.sent, owner.socket!.sendTimes) {
                let packet = Owner.ModemDeckAudioPacket.decode(data)!
                let sampleEnd = 20 + Int(packet.sequence) * 20
                precondition(sentAt - sampleEnd <= 100, "Never send a frame over 100 ms old")
                precondition(packet.timestamp == packet.sequence &* 320)
            }
        }
    }

    static func expiredCaptureUsesEachFramesOwnSampleEnd() {
        Simulation.reset(); encodedFrames = []
        let owner = Owner()
        owner.captureWithEnd(Array(repeating: 0.5, count: 1600), end: -0.04); owner.completeSends()
        precondition(owner.capturedFrames == 5 && owner.droppedFrames == 1 && owner.sent.count == 4)
        let packets = owner.sent.map { Owner.ModemDeckAudioPacket.decode($0)! }
        precondition(packets.map(\.sequence) == [1, 2, 3, 4] && packets.map(\.timestamp) == [320, 640, 960, 1280],
                     "A partially old input batch drops only its truly expired head, preserving source gaps")
        precondition(owner.sequence == 5 && owner.timestamp == 1600)
        let expired = Owner()
        expired.captureWithEnd(Array(repeating: 0.5, count: 1600), end: -0.12)
        precondition(expired.sent.isEmpty && expired.droppedFrames == 5 && expired.timestamp == 1600)
    }

    static func expandedSourceSlotsPreserveTimestampWrap() {
        Simulation.reset(); let owner = Owner(); owner.receive(0)
        let advance = UInt32.max / 320 + 2
        let arrival = Int(advance) * 20
        Simulation.advance(to: arrival)
        owner.playbackQueuedUntil = Double(arrival + 1000) / 1000
        owner.player!.renderStall = arrival
        owner.receive(advance)
        precondition(owner.receiveClock.generation == 1 && owner.player!.playStarts == [40])
        precondition(owner.player!.scheduledSlots == [0, Int64(advance) * 320],
                     "The fixed device epoch uses expanded samples through uint32 timestamp wrap")
    }

}
