import Foundation
struct AVAudioTime { static func seconds(forHostTime: UInt64) -> Double { Double(forHostTime) / 1_000_000_000 } }
func mach_absolute_time() -> UInt64 { UInt64(ProcessInfo.processInfo.systemUptime * 1_000_000_000) }
final class Engine { var isRunning = true }
final class URLSessionWebSocketTask {
    enum Message { case data(Data) }
    var packets: [Data] = [], completions: [(Error?) -> Void] = []
    func send(_ message: Message, completionHandler: @escaping (Error?) -> Void) {
        if case .data(let data) = message { packets.append(data) }
        completions.append(completionHandler)
    }
}
// INSERT_PRODUCT_GLOBALS
private final class Owner {
    let queue = DispatchQueue(label: "neteq-regression")
    var ready = true, activated = true, stopped = false, muted = false
    var engine: Engine? = Engine(), socket: URLSessionWebSocketTask? = URLSessionWebSocketTask()
    var renderer: ModemDeckAudioRenderer? = try! ModemDeckAudioRenderer()
    var encoder = md_opus_encoder_create(16000, 1)
    var captureSamples: [Float] = [], captureSourceFrameStart: UInt64 = 0
    var sendQueue: [Data] = [], sending = false, sendStarted = 0.0
    var sequence: UInt32 = 0, timestamp: UInt32 = 0, receivedSequence: UInt32?, receivedTimestamp: UInt32?
    var capturedFrames = 0, sentPackets = 0, receivedPackets = 0, renderedPackets = 0, droppedFrames = 0
    var renderedSampleCount: UInt64 = 0
    var microphoneLevel = -96.0, lastSentAudioAt = -Double.infinity, lastRenderedAudioAt = -Double.infinity
    private var dropCounts = ModemDeckAudioDropCounts()
    var failures = 0, transportFailures = 0
    deinit { if let encoder { md_opus_encoder_destroy(encoder) } }
    func captureValues(_ values: [Float]) { queue.sync { capture(values, sampleEnd: ProcessInfo.processInfo.systemUptime - 1.5) } }
    func deliver(_ data: Data) { queue.sync { receiveAudio(data) } }
    func transportFailed(task: URLSessionWebSocketTask?, error: Error) { transportFailures += 1; ready = false }
    func failMedia(_ error: Error) { failures += 1; stopped = true }
    func recordConnectionEvent(_ event: String, fields: [String:String] = [:], error: Error? = nil) {}
    func stopAudio() { engine = nil; renderer = nil }
    func resetCaptureStream() { captureSamples.removeAll(); captureSourceFrameStart = 0 }
    func drainSends() {
        while queue.sync(execute: { !(socket?.completions.isEmpty ?? true) }) {
            queue.sync { socket!.completions.removeFirst()(nil) }
            queue.sync {} // Actual product completion enqueues the next send on this owner.
        }
    }
    func checkDeadline(_ now: Double) { queue.sync { checkSendDeadline(now: now) } }
    func resetReady() -> Bool { queue.sync { resetMediaForReady() } }
    func updateStats() { queue.sync { updateRenderedAudioStatistics() } }
    func statsFields() -> [String:String] { queue.sync { neteqStatisticsFields(renderer!.statistics()!) } }
    // INSERT_PRODUCT_METHODS
}
func speech(_ count: Int) -> [Float] { (0..<count).map { Float(sin(Double($0) * 2 * .pi * 330 / 16000) * 0.3) } }
private func render(_ receiver: ModemDeckAudioRenderer, now: Int64, count: Int) -> [Float] {
    var samples = [Float](repeating: -2, count: count)
    precondition(samples.withUnsafeMutableBufferPointer { receiver.render(nowUS: now, samples: $0.baseAddress!, count: $0.count) } == count)
    precondition(samples.allSatisfy { $0.isFinite && abs($0) <= 1 })
    return samples
}
@main struct Tests {
    static func main() {
        actualCaptureFramingAndMute()
        metadataAndReadyReset()
        libraryDemandAndWrap()
        queueBackpressureIsExplicit()
        delayedSendCompletionDoesNotDiscardSpeech()
        sourceGapAndArrivalBehindRenderAreLibraryInputs()
    }
    static func actualCaptureFramingAndMute() {
        let owner = Owner()
        owner.captureValues(speech(1600)); owner.captureValues(speech(2400)); owner.drainSends()
        precondition(owner.capturedFrames == 12 && owner.captureSamples.count == 160)
        precondition(owner.sentPackets == 12 && owner.droppedFrames == 0,
                     "100/150ms actual capture batches and dispatch latency are not audio expiry")
        let packets = owner.socket!.packets.compactMap(ModemDeckAudioPacket.decode)
        precondition(packets.count == 12)
        for (index, packet) in packets.enumerated() {
            precondition(packet.sequence == index && packet.timestamp == index * 320)
            precondition(packet.payload.withUnsafeBytes { md_opus_packet_samples($0.bindMemory(to: UInt8.self).baseAddress, $0.count, 16000) } == 320)
        }
        let muted = Owner(); muted.muted = true; muted.captureValues([Float](repeating: 0.5, count: 320)); muted.drainSends()
        let packet = ModemDeckAudioPacket.decode(muted.socket!.packets[0])!
        let decoder = md_opus_decoder_create(16000, 1)!
        defer { md_opus_decoder_destroy(decoder) }
        var decoded = [Int16](repeating: 0, count: 320)
        let count = packet.payload.withUnsafeBytes { md_opus_decode(decoder, $0.bindMemory(to: UInt8.self).baseAddress, $0.count, &decoded, 320) }
        precondition(count == 320 && decoded.allSatisfy { abs(Int($0)) < 10 })
        precondition(muted.sequence == 1 && muted.timestamp == 320, "Mute still transmits real silence frames")
    }
    static func metadataAndReadyReset() {
        let sender = Owner(); sender.sequence = UInt32.max; sender.timestamp = UInt32.max - 319
        sender.captureValues(speech(640)); sender.drainSends()
        let receiver = Owner()
        sender.socket!.packets.forEach(receiver.deliver)
        precondition(receiver.receivedPackets == 2 && receiver.droppedFrames == 0)
        receiver.deliver(sender.socket!.packets.last!) // Duplicate is invalid, not a clock reanchor.
        precondition(receiver.receivedPackets == 2 && receiver.droppedFrames == 1)
        receiver.deliver(ModemDeckAudioPacket.encode(sequence: 2, timestamp: 999, payload: Data([1])))
        precondition(receiver.receivedPackets == 2 && receiver.droppedFrames == 2)
        precondition(sender.resetReady() && sender.sequence == 0 && sender.timestamp == 0)
        precondition(sender.engine == nil && sender.renderer == nil && !sender.sending && sender.sendQueue.isEmpty)
        sender.ready = true; sender.captureValues(speech(320)); sender.drainSends()
        precondition(ModemDeckAudioPacket.decode(sender.socket!.packets.last!)!.sequence == 0)
        receiver.activated = false; receiver.deliver(ModemDeckAudioPacket.encode(sequence: 1, timestamp: 320, payload: Data([1])))
        precondition(receiver.failures == 0 && receiver.transportFailures == 0)
    }
    static func libraryDemandAndWrap() {
        let sender = Owner(); sender.captureValues(speech(16000)); sender.drainSends()
        let packets = sender.socket!.packets.compactMap(ModemDeckAudioPacket.decode)
        var checksums: [UInt64] = []
        for wrapped in [false, true] {
            let renderer = try! ModemDeckAudioRenderer()
            let start = wrapped ? UInt32.max - 24 * 320 + 1 : 0
            for packet in packets {
                let translated = ModemDeckAudioPacket(sequence: packet.sequence,
                    timestamp: start &+ packet.timestamp, payload: packet.payload)
                precondition(renderer.enqueue(translated, arrivalUS: 0) == 0)
            }
            var sum: UInt64 = 1469598103934665603, consumed = 0, nonzero = 0
            for count in [127, 513, 64, 320, 79, 160, 1000, 17, 13720] {
                // The final large request is split only by this test's fake device
                // maximum slice. It creates no PCM queue or additional timer.
                var remaining = count
                while remaining > 0 {
                    let demand = min(4096, remaining)
                    let samples = render(renderer, now: Int64(consumed * 1_000_000 / 16000), count: demand)
                    for sample in samples {
                        if sample != 0 { nonzero += 1 }
                        sum ^= UInt64(sample.bitPattern); sum &*= 1099511628211
                    }
                    consumed += demand; remaining -= demand
                }
            }
            let before = renderer.statistics()!
            precondition(before.packets_received == 50 && before.internal_sample_rate == 48000 && before.render_errors == 0)
            precondition(nonzero > 0 && before.real_output_samples > 0)
            renderer.resetRender()
            _ = render(renderer, now: Int64(consumed * 1_000_000 / 16000 + 500000), count: 160)
            let after = renderer.statistics()!
            precondition(after.packets_received == before.packets_received && after.render_errors == 0,
                         "Engine reset drops only the device remainder and preserves library state")
            checksums.append(sum)
        }
        precondition(checksums[0] == checksums[1], "RTP wrap translation must not change real rendered PCM")
        let recovery = Owner(); recovery.captureValues(speech(320)); recovery.drainSends()
        recovery.deliver(recovery.socket!.packets[0])
        let start = Int64(AVAudioTime.seconds(forHostTime: mach_absolute_time()) * 1_000_000)
        for tick in 0..<30 { _ = render(recovery.renderer!, now: start + Int64(tick * 10000), count: 160) }
        recovery.updateStats(); let fields = recovery.statsFields()
        precondition(fields.count == 12 && fields["neteq_output_sample_rate"] == "16000")
        precondition(fields["neteq_render_errors"] == "0" && recovery.renderedPackets > 0)
        precondition(recovery.lastRenderedAudioAt < Double(start + 300000) / 1_000_000,
                     "Concealment after the real packet cannot keep media health fresh")
    }
    static func sourceGapAndArrivalBehindRenderAreLibraryInputs() {
        let sender = Owner(); sender.captureValues(speech(1600)); sender.drainSends()
        let packets = sender.socket!.packets
        let owner = Owner(); owner.deliver(packets[0]); owner.deliver(packets[2])
        precondition(owner.receivedPackets == 2 && owner.droppedFrames == 0,
                     "A real source gap stays in RTP metadata for NetEq concealment")
        let renderer = try! ModemDeckAudioRenderer()
        _ = render(renderer, now: 100000, count: 127)
        let packet = ModemDeckAudioPacket.decode(packets[0])!
        precondition(renderer.enqueue(packet, arrivalUS: 90000) == 0,
                     "Queued network arrival predates a render pull without invalidating the packet")
        _ = render(renderer, now: 110000, count: 513)
        precondition(renderer.statistics()!.packets_received == 1 && renderer.statistics()!.render_errors == 0)
        renderer.resetRender()
        _ = render(renderer, now: 110000, count: 160)
        precondition(renderer.statistics()!.render_errors == 0,
                     "Immediate engine restart uses the actual callback clock, not a synthesized future pull time")
    }
    static func delayedSendCompletionDoesNotDiscardSpeech() {
        let owner = Owner(); owner.captureValues(speech(320))
        let began = owner.sendStarted
        // 75 new 20ms capture frames arrive while the first URLSession
        // completion is delayed by 1500ms. The actual async completion later
        // retires each original packet in order on the session queue.
        owner.captureValues(speech(75 * 320))
        owner.checkDeadline(began + 1.5)
        precondition(owner.transportFailures == 0 && owner.sentPackets == 0 && owner.sendQueue.count == 75)
        owner.drainSends()
        precondition(owner.sentPackets == 76 && owner.sendQueue.isEmpty && owner.droppedFrames == 0)
        let packets = owner.socket!.packets.compactMap(ModemDeckAudioPacket.decode)
        precondition(packets.enumerated().allSatisfy { $0.element.sequence == $0.offset && $0.element.timestamp == $0.offset * 320 })
        let stalled = Owner(); stalled.captureValues(speech(320))
        stalled.checkDeadline(stalled.sendStarted + 2)
        precondition(stalled.transportFailures == 1, "A truly blocked send remains bounded by the shared resource budget")
    }
    static func queueBackpressureIsExplicit() {
        let owner = Owner()
        owner.captureValues(speech((Int(md_audio_send_capacity()) + 1) * 320))
        precondition(owner.sendQueue.count == md_audio_send_capacity() && owner.transportFailures == 0)
        let first = ModemDeckAudioPacket.decode(owner.sendQueue[0])!
        precondition(first.sequence == 1, "Queued voice is not silently replaced by newer frames")
        owner.captureValues(speech(320))
        precondition(owner.transportFailures == 1 && !owner.ready)
        let renderer = try! ModemDeckAudioRenderer()
        let packet = ModemDeckAudioPacket.decode(owner.socket!.packets[0])!
        for _ in 0..<md_audio_ingress_capacity() { precondition(renderer.enqueue(packet, arrivalUS: 0) == 0) }
        precondition(renderer.enqueue(packet, arrivalUS: 0) == MD_AUDIO_BACKPRESSURE)
    }
}
