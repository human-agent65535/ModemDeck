import AVFoundation
import Foundation
import Copus
import ModemDeckAudioCore

enum ModemDeckCallAudioError: LocalizedError {
    case callEnded
    case callNotActive
    case mediaUnavailable
    case invalidResponse
    case negotiationFailed
    case timedOut
    case audioFormatUnavailable

    var errorDescription: String? {
        switch self {
        case .callEnded:
            return "The call has already ended."
        case .callNotActive:
            return "The call did not become active."
        case .mediaUnavailable:
            return "Call audio is unavailable for this line."
        case .invalidResponse:
            return "ModemDeck returned invalid call audio data."
        case .negotiationFailed:
            return "The iOS device could not establish call audio."
        case .timedOut:
            return "Call audio took too long to connect."
        case .audioFormatUnavailable:
            return "The device could not configure microphone and playback audio."
        }
    }
}

struct ModemDeckTestAudioStatus: Decodable {
    let phase: String
    let remainingMS: Int
    let capturedFrames: Int
    let capturedDBFS: Int
    let capturedPeakDBFS: Int
    let receivedPackets: Int
    let inputDBFS: Int
    let outputDBFS: Int
    enum CodingKeys: String, CodingKey {
        case phase
        case remainingMS = "remaining_ms", capturedFrames = "captured_frames"
        case capturedDBFS = "captured_dbfs", capturedPeakDBFS = "captured_peak_dbfs"
        case receivedPackets = "received_packets", inputDBFS = "input_dbfs", outputDBFS = "output_dbfs"
    }
}

struct ModemDeckActiveCallsResponse: Decodable {
    struct Call: Decodable {
        let id: String
        let phase: String
        let mediaAvailable: Bool
        let controlState: String

        enum CodingKeys: String, CodingKey {
            case id
            case phase
            case mediaAvailable = "media_available"
            case controlState = "control_state"
        }
    }

    let calls: [Call]
    let testPhase: String?
    let testAudio: ModemDeckTestAudioStatus?

    enum CodingKeys: String, CodingKey {
        case calls
        case testPhase = "test_phase"
        case testAudio = "test_audio"
    }
}

private struct ModemDeckHTTPError: LocalizedError {
    let status: Int

    var errorDescription: String? {
        "ModemDeck returned HTTP \(status)."
    }
}

struct ModemDeckAudioPacket {
    let sequence: UInt32
    let timestamp: UInt32
    let payload: Data
    static func encode(sequence: UInt32, timestamp: UInt32, payload: Data) -> Data {
        var data = Data([0x4d, 0x44, 1, 0])
        for value in [sequence, timestamp] {
            data.append(contentsOf: [UInt8(value >> 24), UInt8((value >> 16) & 255), UInt8((value >> 8) & 255), UInt8(value & 255)])
        }
        data.append(payload); return data
    }
    static func decode(_ data: Data) -> ModemDeckAudioPacket? {
        let bytes = Array(data)
        guard (13...1287).contains(bytes.count), Array(bytes.prefix(4)) == [0x4d, 0x44, 1, 0] else { return nil }
        func uint32(_ offset: Int) -> UInt32 {
            (0..<4).reduce(UInt32(0)) { ($0 << 8) | UInt32(bytes[offset + $1]) }
        }
        return ModemDeckAudioPacket(sequence: uint32(4), timestamp: uint32(8), payload: Data(bytes.dropFirst(12)))
    }
}

private enum ModemDeckAudioDropReason { case capture, send, receiveStale, receiveInvalid, playback }
private struct ModemDeckAudioDropCounts {
    var capture = 0, send = 0, receiveStale = 0, receiveInvalid = 0, playback = 0
    var fields: [String: String] {
        ["capture_dropped_frames": String(capture), "send_dropped_frames": String(send),
         "receive_stale_frames": String(receiveStale), "receive_invalid_frames": String(receiveInvalid),
         "playback_dropped_frames": String(playback)]
    }
    mutating func add(_ count: Int, reason: ModemDeckAudioDropReason) {
        guard count > 0 else { return }
        switch reason {
        case .capture: capture += count
        case .send: send += count
        case .receiveStale: receiveStale += count
        case .receiveInvalid: receiveInvalid += count
        case .playback: playback += count
        }
    }
}

/// Remote sample ticks define playout age, independent of receive bursts.
struct ModemDeckAudioClock {
    private var state = md_audio_clock()
    var generation: UInt32 { state.generation }
    var playAt: Double { withUnsafePointer(to: state) { md_audio_clock_play_at($0) } }
    var sourceSamples: Double { withUnsafePointer(to: state) { md_audio_clock_source_samples($0) } }
    var age: Double { withUnsafePointer(to: state) { md_audio_clock_age($0) } }
    mutating func accept(sequence: UInt32, timestamp: UInt32, now: Double) -> Bool? {
        switch md_audio_clock_accept(&state, sequence, timestamp, now) {
        case 1: return true
        case 0: return false
        default: return nil
        }
    }
}

/// Ready is transport negotiation only; stable duplex media retires a retry budget.
struct ModemDeckMediaRecoveryHealth {
    private var readyAt = Double.infinity
    private var sentBase = 0
    private var renderedBase = 0
    mutating func markReady(now: Double, sent: Int, rendered: Int) {
        readyAt = now; sentBase = sent; renderedBase = rendered
    }
    func isStable(now: Double, sent: Int, rendered: Int, lastSent: Double, lastRendered: Double) -> Bool {
        now - readyAt >= 1 && sent - sentBase >= 25 && rendered - renderedBase >= 25 &&
            now - lastSent <= 0.1 && now - lastRendered <= 0.1
    }
}

/// One media owner for real and server test calls. CallKit alone activates audio.
final class ModemDeckCallAudioSession: NSObject {
    typealias ConnectionCompletion = (Result<Void, Error>) -> Void
    static let activationChanged = Notification.Name("ModemDeckCallAudioActivation")
    private static var audioActivated = false
    let callID: String
    var onRemoteEnded: (() -> Void)?
    var onFailed: ((Error) -> Void)?
    var onBecameActive: (() -> Void)?
    var onTestPhaseChanged: ((String) -> Void)?
    var onTestAudioChanged: ((ModemDeckTestAudioStatus) -> Void)?
    var onMicrophoneLevelChanged: ((Int?) -> Void)?
    var onMediaStateChanged: ((String) -> Void)?
    var onLocalAudioCountsChanged: ((Int, Int) -> Void)?
    private let credential: ModemDeckCredential
    private let testCall: Bool
    private let ownerToken = UUID().uuidString.lowercased()
    private let queue: DispatchQueue
    private let urlSession: URLSession
    private var socket: URLSessionWebSocketTask?
    private var ready = false
    private var activated = false
    private var activationObserver: NSObjectProtocol?
    private var configurationObserver: NSObjectProtocol?
    private var muted = false
    private var stopped = false
    private var connecting = false
    private var reportedActive = false
    private var mediaClaimed = false
    private var connectCompletion: ConnectionCompletion?
    private var connectDeadline = Date.distantPast
    private var connectTimeout: DispatchWorkItem?
    private var reconnectDeadline = Date.distantPast
    private var reconnectAttempts = 0
    private var reconnectTimeout: DispatchWorkItem?
    private var recoveryHealth = ModemDeckMediaRecoveryHealth()
    private var connectionStartedAt = ProcessInfo.processInfo.systemUptime
    private var connectionStage = "idle"
    private var stageStartedAt = ProcessInfo.processInfo.systemUptime
    private var activePollAttempt = 0
    private var leaseTimer: DispatchSourceTimer?
    private var leaseRequestInFlight = false
    private var testPhaseTimer: DispatchSourceTimer?
    private var testPhaseRequestInFlight = false
    private var telemetryTimer: DispatchSourceTimer?
    private var pingInFlight = false
    private var pingStarted = 0.0
    private var lastStatisticsLog = 0.0
    private var latestTestAudio: ModemDeckTestAudioStatus?
    private var testAudioAt = 0.0
    private var engine: AVAudioEngine?
    private var player: AVAudioPlayerNode?
    private var captureTapInstalled = false
    private var captureGeneration = 0
    private var encoder: OpaquePointer?
    private var decoder: OpaquePointer?
    private let format = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 16_000, channels: 1, interleaved: false)!
    private var captureSamples: [Float] = []
    private var sendQueue: [(Data, Double)] = []
    private var sending = false
    private var sendStarted = 0.0
    private var sequence: UInt32 = 0
    private var timestamp: UInt32 = 0
    private var receiveClock = ModemDeckAudioClock()
    private var serverReceivedPackets = 0
    private var serverAudioDropFields: [String: String] = [:]
    private var playbackPending = 0
    private var playbackUnderruns = 0
    // Source-clock generation changes and genuine unrendered queue overflow;
    // excludes ordinary teardown/reconfiguration and independently counted underruns.
    private var playbackResets = 0
    private var playbackGeneration = 0
    private var playbackEpochSourceSamples: Double?
    private var playbackEpochStartedAt = 0.0
    private var playbackQueuedUntil = -Double.infinity
    private var playbackFrameEnds: [AVAudioFramePosition] = []
    private var playbackLastSampleEnd: AVAudioFramePosition = 0
    private var capturedFrames = 0
    private var sentPackets = 0
    private var receivedPackets = 0
    private var renderedPackets = 0
    private var lastSentAudioAt = -Double.infinity
    private var lastRenderedAudioAt = -Double.infinity
    private var droppedFrames = 0
    private var dropCounts = ModemDeckAudioDropCounts()
    private var microphoneLevel: Double = -96
    private let captureLock = NSLock()
    private var capturePendingSamples = 0
    // Tap-assigned ranges survive queue delays; an older task never consumes a
    // later dropped batch. Stream generation also isolates reconnect resets.
    private var captureStreamGeneration = 0
    private var captureSourceCursor: UInt64 = 0
    private var captureSourceFrameStart: UInt64 = 0
    private var captureHardwareNext: AVAudioFramePosition?
    private var captureDroppedSamples: UInt64 = 0
    private var captureReportedDropFrames: UInt64 = 0

    init(callID: String, credential: ModemDeckCredential, testCall: Bool = false) {
        self.callID = callID; self.credential = credential; self.testCall = testCall
        queue = DispatchQueue(label: "modemdeck.call-audio.\(callID)")
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 12
        // A WebSocket is a long-lived stream; a short resource timeout closes healthy calls.
        config.timeoutIntervalForResource = 24 * 60 * 60
        urlSession = ModemDeckDiagnostics.urlSession(configuration: config)
        super.init()
        activated = Self.audioActivated
        activationObserver = NotificationCenter.default.addObserver(forName: Self.activationChanged, object: nil, queue: .main) { [weak self] event in
            let active = event.userInfo?["active"] as? Bool ?? false
            self?.queue.async { [weak self] in
                guard let self, !self.stopped else { return }
                self.activated = active
                if active { self.startAudioIfReady() } else { self.stopAudio() }
            }
        }
        configurationObserver = NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: nil, queue: nil) { [weak self] event in
            let changedEngine = event.object as? AVAudioEngine
            self?.queue.async { [weak self] in
                self?.audioConfigurationChanged(changedEngine)
            }
        }
    }
    deinit {
        if let activationObserver { NotificationCenter.default.removeObserver(activationObserver) }
        if let configurationObserver { NotificationCenter.default.removeObserver(configurationObserver) }
        if let encoder { opus_encoder_destroy(encoder) }
        if let decoder { opus_decoder_destroy(decoder) }
    }
    static func prepareAudioSession() {
        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.allowBluetoothHFP])
            try session.setPreferredSampleRate(48_000)
            try session.setPreferredIOBufferDuration(0.02)
        } catch { ModemDeckDiagnostics.shared.record(.audio, "session_prepare_failed", error: error) }
    }
    static func didActivate(_ audioSession: AVAudioSession) {
        dispatchPrecondition(condition: .onQueue(.main))
        audioActivated = true
        ModemDeckAudioRoute.shared.setActive(true)
        NotificationCenter.default.post(name: activationChanged, object: nil, userInfo: ["active": true])
        ModemDeckDiagnostics.shared.record(.audio, "session_activated")
    }
    static func didDeactivate(_ audioSession: AVAudioSession) {
        dispatchPrecondition(condition: .onQueue(.main))
        audioActivated = false
        ModemDeckAudioRoute.shared.setActive(false)
        NotificationCenter.default.post(name: activationChanged, object: nil, userInfo: ["active": false])
        ModemDeckDiagnostics.shared.record(.audio, "session_deactivated")
    }
    func applyRuntimeState(phase: String) {
        queue.async { [weak self] in
            guard let self, !self.stopped, phase == "active", !self.reportedActive else { return }
            self.reportedActive = true
            DispatchQueue.main.async { [weak self] in self?.onBecameActive?() }
        }
    }
    func connect(completion: @escaping ConnectionCompletion) {
        queue.async { [weak self] in
            guard let self, !self.stopped else {
                DispatchQueue.main.async { completion(.failure(ModemDeckCallAudioError.callEnded)) }; return
            }
            guard !self.connecting else {
                DispatchQueue.main.async { completion(.failure(ModemDeckCallAudioError.negotiationFailed)) }; return
            }
            self.connecting = true; self.connectCompletion = completion
            self.connectDeadline = Date().addingTimeInterval(20)
            self.connectionStartedAt = ProcessInfo.processInfo.systemUptime
            self.setConnectionStage("waiting_for_active_call")
            let timeout = DispatchWorkItem { [weak self] in self?.finishConnection(.failure(ModemDeckCallAudioError.timedOut)) }
            self.connectTimeout = timeout
            self.queue.asyncAfter(deadline: .now() + 20, execute: timeout)
            self.startTelemetry(); self.waitForActiveCall()
        }
    }
    func setMuted(_ muted: Bool) {
        queue.async { [weak self] in
            guard let self, !self.stopped else { return }
            self.muted = muted
        }
    }
    func stop() {
        // Retain the session until queued cleanup, even after its CallKit owner is removed.
        queue.async { self.stopLocked(notifyRemoteEnd: false) }
    }
    func startControlHeartbeat() {
        queue.async { [weak self] in
            guard let self, !self.stopped else { return }
            self.startLeaseHeartbeat()
        }
    }
    private func openSocket() {
        guard !stopped else { return }
        setConnectionStage(reconnectAttempts == 0 ? "connecting_wss" : "reconnecting_wss")
        do {
            var request = try authorizedRequest(credential: credential, path: callPath("media/ws"), method: "GET", timeout: 12)
            guard let url = request.url, var components = URLComponents(url: url, resolvingAgainstBaseURL: false), components.scheme == "https" else {
                throw ModemDeckCallAudioError.invalidResponse
            }
            components.scheme = "wss"
            guard let wsURL = components.url else { throw ModemDeckCallAudioError.invalidResponse }
            request.url = wsURL
            let task = urlSession.webSocketTask(with: request)
            socket = task; ready = false; mediaClaimed = true; task.resume()
            let start: [String: Any] = ["type": "start", "version": 1, "codec": "opus", "sample_rate": 16_000,
                                        "channels": 1, "frame_ms": 20, "owner_token": ownerToken]
            let data = try JSONSerialization.data(withJSONObject: start)
            task.send(.string(String(decoding: data, as: UTF8.self))) { [weak self, weak task] error in
                if let error { self?.queue.async { [weak self] in self?.transportFailed(task: task, error: error) } }
            }
            receive(task)
            let opening = DispatchWorkItem { [weak self, weak task] in
                guard let self, self.socket === task, !self.ready else { return }
                self.transportFailed(task: task, error: ModemDeckCallAudioError.timedOut)
            }
            queue.asyncAfter(deadline: .now() + 6, execute: opening)
        } catch { failMedia(error) }
    }
    private func receive(_ task: URLSessionWebSocketTask) {
        task.receive { [weak self, weak task] result in
            self?.queue.async { [weak self] in
                guard let self, let task, !self.stopped, self.socket === task else { return }
                switch result {
                case .failure(let error): self.transportFailed(task: task, error: error)
                case .success(let message):
                    switch message {
                    case .string(let text): self.receiveControl(text, task: task)
                    case .data(let data): if self.ready { self.receiveAudio(data) }
                    @unknown default: break
                    }
                    if self.socket === task { self.receive(task) }
                }
            }
        }
    }
    private func receiveControl(_ text: String, task: URLSessionWebSocketTask) {
        guard let data = text.data(using: .utf8), let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            transportFailed(task: task, error: ModemDeckCallAudioError.invalidResponse); return
        }
        switch json["type"] as? String {
        case "ready":
            guard json["version"] as? Int == 1, json["codec"] as? String == "opus", json["sample_rate"] as? Int == 16_000,
                  json["channels"] as? Int == 1, json["frame_ms"] as? Int == 20 else {
                failMedia(ModemDeckCallAudioError.invalidResponse); return
            }
            markSocketReady(now: ProcessInfo.processInfo.systemUptime)
            sendQueue.removeAll(); resetCaptureStream(); receiveClock = ModemDeckAudioClock()
            sequence = 0; timestamp = 0
            if let encoder { opus_encoder_destroy(encoder) }; encoder = nil
            if let decoder { opus_decoder_destroy(decoder) }; decoder = nil
            var codecError: Int32 = 0
            encoder = opus_encoder_create(16_000, 1, OPUS_APPLICATION_VOIP, &codecError)
            decoder = opus_decoder_create(16_000, 1, &codecError)
            guard encoder != nil, decoder != nil, codecError == OPUS_OK else {
                failMedia(ModemDeckCallAudioError.negotiationFailed); return
            }
            setConnectionStage("connected"); publishMediaState(activated && engine?.isRunning == true ? "active" : "waiting_for_audio")
            startLeaseHeartbeat(); startTestPhaseUpdates()
            finishConnection(.success(())); startAudioIfReady()
        case "stats":
            if let audio = json["audio"] as? [String: Any] { updateServerAudioStatistics(audio) }
        case "ended": stopLocked(notifyRemoteEnd: true)
        case "error":
            handleServerError(code: json["code"] as? String ?? "", task: task)
        default: break
        }
    }
    private func updateServerAudioStatistics(_ audio: [String: Any]) {
        if let received = audio["received_packets"] as? Int, received >= 0 { serverReceivedPackets = received }
        let names = ["dropped_packets", "dropped_source_early_packets", "dropped_source_late_packets",
                     "dropped_queue_overflow_packets", "dropped_reanchor_packets", "dropped_playout_packets",
                     "dropped_rebuffer_packets", "clock_reanchors", "playout_underruns",
                     "playout_silence_frames", "playout_missed_ticks"]
        for name in names {
            if let value = audio[name] as? Int, value >= 0 { serverAudioDropFields["server_" + name] = String(value) }
        }
        recordConnectionEvent("server_audio_statistics", fields: serverAudioDropFields.merging(
            ["server_received_packets": String(serverReceivedPackets)]) { _, new in new })
    }
    private func recordDroppedFrames(_ count: Int, reason: ModemDeckAudioDropReason) {
        guard count > 0 else { return }
        dropCounts.add(count, reason: reason); droppedFrames += count
    }
    private func markSocketReady(now: Double) {
        ready = true
        recoveryHealth.markReady(now: now, sent: sentPackets, rendered: renderedPackets)
    }
    private func clearRecoveryAfterStableMedia(now: Double) {
        guard reconnectDeadline != .distantPast, ready,
              recoveryHealth.isStable(now: now, sent: sentPackets, rendered: renderedPackets,
                                      lastSent: lastSentAudioAt, lastRendered: lastRenderedAudioAt) else { return }
        reconnectAttempts = 0; reconnectDeadline = .distantPast
        reconnectTimeout?.cancel(); reconnectTimeout = nil
        recordConnectionEvent("wss_recovered")
    }
    private func handleServerError(code: String, task: URLSessionWebSocketTask) {
        recordConnectionEvent("server_media_error")
        if ["transport_timeout", "transport_closed", "backpressure"].contains(code) {
            transportFailed(task: task, error: ModemDeckCallAudioError.timedOut)
        } else { failMedia(ModemDeckCallAudioError.mediaUnavailable) }
    }
    private func transportFailed(task: URLSessionWebSocketTask?, error: Error) {
        guard !stopped, let task, socket === task else { return }
        recordConnectionEvent("wss_disconnected", error: error)
        socket = nil; ready = false; sending = false; pingInFlight = false
        task.cancel(with: .goingAway, reason: nil)
        recordDroppedFrames(sendQueue.count, reason: .send)
        sendQueue.removeAll(); captureSamples.removeAll(); flushPlayback()
        publishMediaState("reconnecting")
        if reconnectDeadline == .distantPast {
            reconnectDeadline = Date().addingTimeInterval(8)
            let timeout = DispatchWorkItem { [weak self] in
                guard let self, !self.stopped, self.reconnectDeadline != .distantPast else { return }
                self.failMedia(ModemDeckCallAudioError.timedOut)
            }
            reconnectTimeout = timeout
            queue.asyncAfter(deadline: .now() + 8, execute: timeout)
        }
        let deadline = connectCompletion == nil ? reconnectDeadline : min(connectDeadline, reconnectDeadline)
        let delay = min(2.0, 0.3 * pow(2, Double(reconnectAttempts)))
        guard reconnectAttempts < 4, Date().addingTimeInterval(delay + 1) < deadline else {
            failMedia(error)
            return
        }
        reconnectAttempts += 1
        queue.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, !self.stopped else { return }
            self.releaseForReconnect(deadline: deadline)
        }
    }
    private func releaseForReconnect(deadline: Date) {
        guard !stopped else { return }
        guard Date().addingTimeInterval(0.5) < deadline else { failMedia(ModemDeckCallAudioError.timedOut); return }
        // The server's successful DELETE waits for the exact old owner to close.
        // Failed cleanup must not race a fresh upgrade or become a call hangup.
        releaseMedia { [weak self] released in
            self?.queue.async { [weak self] in
                guard let self, !self.stopped else { return }
                if released { self.probeForReconnect(deadline: deadline) }
                else {
                    self.queue.asyncAfter(deadline: .now() + 0.3) { [weak self] in self?.releaseForReconnect(deadline: deadline) }
                }
            }
        }
    }
    private func probeForReconnect(deadline: Date) {
        guard !stopped else { return }
        guard Date().addingTimeInterval(0.5) < deadline else { failMedia(ModemDeckCallAudioError.timedOut); return }
        request(path: activeCallsPath, method: "GET") { [weak self] result in
            guard let self, !self.stopped else { return }
            switch result {
            case .success(let data):
                guard let state = try? JSONDecoder().decode(ModemDeckActiveCallsResponse.self, from: data),
                      let call = state.calls.first(where: { $0.id == self.callID }),
                      call.phase == "active", call.controlState == "owned", call.mediaAvailable else {
                    self.failMedia(ModemDeckCallAudioError.callEnded); return
                }
                guard Date() < deadline else { self.failMedia(ModemDeckCallAudioError.timedOut); return }
                self.openSocket()
            case .failure:
                self.queue.asyncAfter(deadline: .now() + 0.3) { [weak self] in self?.probeForReconnect(deadline: deadline) }
            }
        }
    }
    private func startAudioIfReady() {
        guard !stopped, activated, ready, engine == nil else { return }
        do {
            var status: Int32 = 0
            if encoder == nil { encoder = opus_encoder_create(16_000, 1, OPUS_APPLICATION_VOIP, &status) }
            guard encoder != nil, status == OPUS_OK else { throw ModemDeckCallAudioError.negotiationFailed }
            if decoder == nil { decoder = opus_decoder_create(16_000, 1, &status) }
            guard decoder != nil, status == OPUS_OK else { throw ModemDeckCallAudioError.negotiationFailed }
            let engine = AVAudioEngine(), player = AVAudioPlayerNode()
            try engine.inputNode.setVoiceProcessingEnabled(true)
            engine.inputNode.isVoiceProcessingAGCEnabled = true
            engine.attach(player)
            self.engine = engine; self.player = player
            try configureVoiceGraph(engine, player: player)
            try startVoiceGraph(engine, player: player)
            recordConnectionEvent("voice_processing_started")
        } catch {
            recordConnectionEvent("audio_engine_failed", error: error)
            failMedia(error)
        }
    }
    private func matchesVoiceFormat(_ candidate: AVAudioFormat) -> Bool {
        candidate.sampleRate == format.sampleRate && candidate.channelCount == format.channelCount &&
            candidate.commonFormat == format.commonFormat && candidate.isInterleaved == format.isInterleaved
    }
    private func voiceGraphMatches(_ engine: AVAudioEngine) -> Bool {
        matchesVoiceFormat(engine.inputNode.outputFormat(forBus: 0)) &&
            matchesVoiceFormat(engine.outputNode.inputFormat(forBus: 0))
    }
    private func configureVoiceGraph(_ engine: AVAudioEngine, player: AVAudioPlayerNode) throws {
        // VoiceProcessingIO requires the same client format in both directions.
        // Hardware/echo-reference channels are not separate telephone channels.
        engine.connect(player, to: engine.mainMixerNode, format: format)
        engine.connect(engine.mainMixerNode, to: engine.outputNode, format: format)
        resetCaptureStream(replacingGraph: true)
        let generation = captureGeneration
        // Taps request 100 ms (the SDK minimum); capture() splits it into 20 ms Opus frames.
        engine.inputNode.installTap(onBus: 0, bufferSize: 1600, format: format) { [weak self, weak engine] buffer, when in
            guard let self, buffer.frameLength > 0 else { return }
            guard self.matchesVoiceFormat(buffer.format), let samples = buffer.floatChannelData?[0] else {
                self.queue.async { [weak self, weak engine] in
                    guard let self, !self.stopped, self.engine === engine,
                          self.captureGeneration == generation else { return }
                    self.recordConnectionEvent("capture_format_invalid", error: ModemDeckCallAudioError.audioFormatUnavailable)
                    self.failMedia(ModemDeckCallAudioError.audioFormatUnavailable)
                }
                return
            }
            let now = ProcessInfo.processInfo.systemUptime
            let total = Int(buffer.frameLength), sampleCount = min(1600, total), trimmed = total - sampleCount
            // Input hostTime describes the first sample. Retain its true end
            // time through dispatch/encoding; fallback to callback end-time.
            let hostNow = AVAudioTime.seconds(forHostTime: mach_absolute_time())
            let sampleEnd = when.isHostTimeValid
                ? min(now, now + AVAudioTime.seconds(forHostTime: when.hostTime) - hostNow + Double(total) / 16_000)
                : now
            self.captureLock.lock()
            guard self.captureGeneration == generation else { self.captureLock.unlock(); return }
            let streamGeneration = self.captureStreamGeneration
            var sourceStart = self.captureSourceCursor
            if when.isSampleTimeValid, when.sampleRate == 16_000 {
                if let expected = self.captureHardwareNext, when.sampleTime > expected {
                    let missing = UInt64(when.sampleTime - expected)
                    sourceStart += missing; self.captureDroppedSamples += missing
                }
                self.captureHardwareNext = when.sampleTime + AVAudioFramePosition(total)
            } else { self.captureHardwareNext = nil }
            self.captureSourceCursor = sourceStart + UInt64(total)
            self.captureDroppedSamples += UInt64(trimmed)
            guard self.capturePendingSamples + sampleCount <= 1600 else {
                self.captureDroppedSamples += UInt64(sampleCount)
                self.captureLock.unlock(); return
            }
            self.capturePendingSamples += sampleCount; self.captureLock.unlock()
            let retainedStart = sourceStart + UInt64(trimmed)
            let values = Array(UnsafeBufferPointer(start: samples.advanced(by: trimmed), count: sampleCount))
            self.queue.async { [weak self, weak engine] in
                guard let self else { return }
                defer {
                    self.captureLock.lock()
                    if self.captureGeneration == generation && self.captureStreamGeneration == streamGeneration {
                        self.capturePendingSamples -= sampleCount
                    }
                    self.captureLock.unlock()
                }
                guard !self.stopped, self.engine === engine, self.captureGeneration == generation,
                      self.captureStreamGeneration == streamGeneration else { return }
                self.captureBatch(values, sourceStart: retainedStart, sampleEnd: sampleEnd)
            }
        }
        captureTapInstalled = true
        guard voiceGraphMatches(engine) else {
            let input = engine.inputNode.outputFormat(forBus: 0), output = engine.outputNode.inputFormat(forBus: 0)
            recordConnectionEvent("voice_input_format", fields: ["sample_rate": String(Int(input.sampleRate)), "channels": String(input.channelCount)])
            recordConnectionEvent("voice_output_format", fields: ["sample_rate": String(Int(output.sampleRate)), "channels": String(output.channelCount)])
            throw ModemDeckCallAudioError.audioFormatUnavailable
        }
    }
    private func startVoiceGraph(_ engine: AVAudioEngine, player: AVAudioPlayerNode) throws {
        engine.prepare(); try engine.start()
        let input = engine.inputNode.outputFormat(forBus: 0), output = engine.outputNode.inputFormat(forBus: 0)
        recordConnectionEvent("voice_input_format", fields: ["sample_rate": String(Int(input.sampleRate)), "channels": String(input.channelCount)])
        recordConnectionEvent("voice_output_format", fields: ["sample_rate": String(Int(output.sampleRate)), "channels": String(output.channelCount)])
        guard voiceGraphMatches(engine) else { throw ModemDeckCallAudioError.audioFormatUnavailable }
        // Receive starts the player on the first source slot plus 40 ms.
        publishMediaState(ready ? "active" : "reconnecting")
    }
    private func audioConfigurationChanged(_ changedEngine: AVAudioEngine?) {
        guard !stopped, activated, let engine, let player, changedEngine === engine else { return }
        // Startup can queue a notification that is delivered after the graph is
        // already running. Recreating VoiceProcessingIO here changes its format again.
        guard !engine.isRunning || !voiceGraphMatches(engine) else { return }
        do {
            engine.stop(); flushPlayback(); captureSamples.removeAll()
            if captureTapInstalled { engine.inputNode.removeTap(onBus: 0); captureTapInstalled = false }
            try configureVoiceGraph(engine, player: player)
            try startVoiceGraph(engine, player: player)
            recordConnectionEvent("voice_processing_reconfigured")
        } catch {
            recordConnectionEvent("audio_engine_failed", error: error)
            failMedia(error)
        }
    }
    private func resetCaptureStream(replacingGraph: Bool = false) {
        captureLock.lock()
        if replacingGraph { captureGeneration += 1 }
        captureStreamGeneration += 1; captureSourceCursor = 0
        captureHardwareNext = nil; capturePendingSamples = 0
        captureLock.unlock()
        captureSourceFrameStart = 0; captureSamples.removeAll()
        updateCaptureDropCounts()
    }
    private func updateCaptureDropCounts() {
        captureLock.lock(); let total = (captureDroppedSamples + 319) / 320; captureLock.unlock()
        let newlyDropped = total - captureReportedDropFrames
        captureReportedDropFrames = total
        recordDroppedFrames(Int(newlyDropped), reason: .capture)
    }
    private func captureBatch(_ values: [Float], sourceStart: UInt64, sampleEnd: Double) {
        var skip = 0
        if sourceStart > captureSourceFrameStart + UInt64(captureSamples.count) {
            let aligned = ((sourceStart + 319) / 320) * 320
            let missingFrames = (aligned - captureSourceFrameStart) / 320
            sequence &+= UInt32(truncatingIfNeeded: missingFrames)
            timestamp &+= UInt32(truncatingIfNeeded: missingFrames * 320)
            skip = min(values.count, Int(aligned - sourceStart))
            captureLock.lock(); captureDroppedSamples += UInt64(captureSamples.count + skip); captureLock.unlock()
            captureSourceFrameStart = aligned; captureSamples.removeAll()
        }
        let remainingSkip = min(values.count, Int(captureSourceFrameStart > sourceStart ? captureSourceFrameStart - sourceStart : 0))
        if remainingSkip > skip {
            captureLock.lock(); captureDroppedSamples += UInt64(remainingSkip - skip); captureLock.unlock()
            skip = remainingSkip
        }
        updateCaptureDropCounts()
        capture(Array(values.dropFirst(skip)), sampleEnd: sampleEnd)
    }
    private func capture(_ values: [Float], sampleEnd: Double) {
        captureSamples.append(contentsOf: values)
        while captureSamples.count >= 320 {
            let frameEnd = sampleEnd - Double(captureSamples.count - 320) / 16_000
            var frame = Array(captureSamples.prefix(320)); captureSamples.removeFirst(320)
            captureSourceFrameStart += 320; capturedFrames += 1
            let rms = sqrt(frame.reduce(0.0) { $0 + Double($1 * $1) } / 320)
            let db = muted ? -96 : max(-96, min(0, 20 * log10(max(rms, 0.00001585))))
            microphoneLevel += (db - microphoneLevel) * (db > microphoneLevel ? 0.55 : 0.12)
            if muted { frame = Array(repeating: 0, count: 320) }
            let frameSequence = sequence, frameTimestamp = timestamp
            sequence &+= 1; timestamp &+= 320
            guard md_audio_frame_expired(frameEnd, ProcessInfo.processInfo.systemUptime) == 0 else {
                recordDroppedFrames(1, reason: .capture); continue
            }
            guard ready, let encoder else { recordDroppedFrames(1, reason: .send); continue }
            var payload = [UInt8](repeating: 0, count: 1275)
            let count = opus_encode_float(encoder, &frame, 320, &payload, 1275)
            guard count > 0 else { recordDroppedFrames(1, reason: .send); continue }
            let packet = ModemDeckAudioPacket.encode(sequence: frameSequence, timestamp: frameTimestamp, payload: Data(payload.prefix(Int(count))))
            if sendQueue.count >= Int(md_audio_send_queue_capacity()) { sendQueue.removeFirst(); recordDroppedFrames(1, reason: .send) }
            sendQueue.append((packet, frameEnd)); sendNext()
        }
    }
    private func sendNext() {
        guard !sending, ready, let task = socket else { return }
        let now = ProcessInfo.processInfo.systemUptime
        while let first = sendQueue.first, md_audio_frame_expired(first.1, now) != 0 { sendQueue.removeFirst(); recordDroppedFrames(1, reason: .send) }
        guard !sendQueue.isEmpty else { return }
        let packet = sendQueue.removeFirst().0; sending = true; sendStarted = now
        task.send(.data(packet)) { [weak self, weak task] error in
            self?.queue.async { [weak self] in
                guard let self, let task, !self.stopped, self.socket === task else { return }
                self.sending = false
                if let error { self.transportFailed(task: task, error: error) }
                else { self.sentPackets += 1; self.lastSentAudioAt = ProcessInfo.processInfo.systemUptime; self.sendNext() }
            }
        }
    }
    private func receiveAudio(_ data: Data) {
        guard let packet = ModemDeckAudioPacket.decode(data), let decoder else { recordDroppedFrames(1, reason: .receiveInvalid); return }
        let now = ProcessInfo.processInfo.systemUptime
        let clockGeneration = receiveClock.generation
        guard let current = receiveClock.accept(sequence: packet.sequence, timestamp: packet.timestamp, now: now) else {
            recordDroppedFrames(1, reason: .receiveInvalid); recordConnectionEvent("invalid_audio_packet"); return
        }
        if clockGeneration != 0, receiveClock.generation != clockGeneration { resetPlaybackForMedia(underrun: false) }
        receivedPackets += 1
        var output = [Float](repeating: 0, count: 320)
        let count = packet.payload.withUnsafeBytes { bytes in
            opus_decode_float(decoder, bytes.bindMemory(to: UInt8.self).baseAddress, Int32(bytes.count), &output, 320, 0)
        }
        // Decode valid stale packets to retain codec continuity, but never render them.
        guard current else { recordDroppedFrames(1, reason: .receiveStale); return }
        guard count == 320 else { recordDroppedFrames(1, reason: .receiveInvalid); return }
        guard activated, let player, engine?.isRunning == true,
              let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: 320), let samples = buffer.floatChannelData?[0] else {
            recordDroppedFrames(1, reason: .playback); return
        }
        // An exhausted player gets a new playback epoch, never a new source-age
        // anchor. Old TCP audio must pass the unchanged clock freshness gate.
        let renderedThrough = retireRenderedPlayback()
        if playbackEpochSourceSamples != nil, now > playbackQueuedUntil + 0.0000001,
           renderedThrough.map({ $0 >= playbackLastSampleEnd }) ?? (playbackPending == 0) {
            resetPlaybackForMedia(underrun: true)
        }
        // Five frames per legitimate 100 ms batch plus two prebuffer frames.
        if playbackPending >= Int(md_audio_queue_capacity()) { resetPlaybackForMedia(underrun: false) }
        if playbackEpochSourceSamples == nil {
            let start = md_audio_playback_start(receiveClock.playAt, now)
            guard md_audio_frame_expired(receiveClock.playAt, start) == 0 else { recordDroppedFrames(1, reason: .playback); return }
            playbackEpochSourceSamples = receiveClock.sourceSamples
            playbackEpochStartedAt = start
        }
        let sampleTime = AVAudioFramePosition(receiveClock.sourceSamples - playbackEpochSourceSamples!)
        buffer.frameLength = 320
        output.withUnsafeBufferPointer { source in samples.update(from: source.baseAddress!, count: 320) }
        playbackFrameEnds.append(sampleTime + 320)
        playbackLastSampleEnd = sampleTime + 320
        playbackPending = playbackFrameEnds.count; renderedPackets += 1
        lastRenderedAudioAt = now
        playbackQueuedUntil = max(playbackQueuedUntil, md_audio_source_slot(playbackEpochStartedAt, Double(sampleTime + 320)))
        let generation = playbackGeneration
        // Explicit source sample slots preserve missing-frame gaps. Completion
        // means rendered by the player; downstream device latency is not backlog.
        player.scheduleBuffer(buffer, at: AVAudioTime(sampleTime: sampleTime, atRate: 16_000), options: [],
                              completionCallbackType: .dataRendered) { [weak self] _ in
            self?.queue.async { [weak self] in
                guard let self, self.playbackGeneration == generation else { return }
                self.playbackFrameEnds.removeAll { $0 == sampleTime + 320 }
                self.playbackPending = self.playbackFrameEnds.count
            }
        }
        if !player.isPlaying {
            let delay = max(0, playbackEpochStartedAt - now)
            player.play(at: AVAudioTime(hostTime: mach_absolute_time() + AVAudioTime.hostTime(forSeconds: delay)))
        }
    }
    private func retireRenderedPlayback() -> AVAudioFramePosition? {
        guard let player, let renderTime = player.lastRenderTime,
              let playerTime = player.playerTime(forNodeTime: renderTime) else { return nil }
        // Completion delivery can lag a packet burst on this serial queue.
        // Inspect the actual player cursor before treating its count as backlog.
        playbackFrameEnds.removeAll { $0 <= playerTime.sampleTime }
        playbackPending = playbackFrameEnds.count
        return playerTime.sampleTime
    }
    private func resetPlaybackForMedia(underrun: Bool) {
        _ = retireRenderedPlayback()
        if underrun { playbackUnderruns += 1 } else { playbackResets += 1 }
        recordDroppedFrames(playbackPending, reason: .playback)
        flushPlayback()
    }
    private func flushPlayback() {
        playbackGeneration += 1; playbackPending = 0; player?.stop()
        playbackEpochSourceSamples = nil; playbackEpochStartedAt = 0; playbackQueuedUntil = -Double.infinity
        playbackFrameEnds.removeAll(); playbackLastSampleEnd = 0
    }
    private func stopAudio() {
        guard let engine else { return }
        resetCaptureStream(replacingGraph: true)
        flushPlayback(); engine.stop()
        if captureTapInstalled { engine.inputNode.removeTap(onBus: 0); captureTapInstalled = false }
        self.engine = nil; player = nil; captureSamples.removeAll()
        publishMediaState(ready ? "waiting_for_audio" : "reconnecting")
    }
    private func startTelemetry() {
        guard telemetryTimer == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now(), repeating: 0.1, leeway: .milliseconds(20))
        timer.setEventHandler { [weak self] in
            guard let self, !self.stopped else { return }
            let now = ProcessInfo.processInfo.systemUptime
            self.clearRecoveryAfterStableMedia(now: now)
            let level: Int? = self.engine?.isRunning == true ? Int(self.microphoneLevel) : nil
            let captured = self.capturedFrames, sent = self.sentPackets
            let audio = self.interpolatedTestAudio(now: now)
            DispatchQueue.main.async { [weak self] in
                self?.onMicrophoneLevelChanged?(level); self?.onLocalAudioCountsChanged?(captured, sent)
                if let audio { self?.onTestAudioChanged?(audio) }
            }
            if now - self.lastStatisticsLog >= 5 {
                self.updateCaptureDropCounts()
                self.lastStatisticsLog = now
                self.recordConnectionEvent("audio_statistics", fields: self.localAudioStatisticsFields())
                if let task = self.socket, self.ready, !self.pingInFlight {
                    self.pingInFlight = true; self.pingStarted = now
                    task.sendPing { [weak self, weak task] error in
                        self?.queue.async { [weak self] in
                            guard let self, self.socket === task else { return }
                            self.pingInFlight = false
                            if let error { self.transportFailed(task: task, error: error) }
                        }
                    }
                }
            }
            if self.sending, now - self.sendStarted > 1 { self.transportFailed(task: self.socket, error: ModemDeckCallAudioError.timedOut) }
            if self.pingInFlight, now - self.pingStarted > 10 { self.transportFailed(task: self.socket, error: ModemDeckCallAudioError.timedOut) }
        }
        telemetryTimer = timer; timer.resume()
    }
    private func localAudioStatisticsFields() -> [String: String] {
        ModemDeckAudioRoute.fields().merging([
            "microphone_dbfs": String(Int(microphoneLevel)), "captured_frames": String(capturedFrames),
            "sent_packets": String(sentPackets), "server_received_packets": String(serverReceivedPackets),
            "received_packets": String(receivedPackets), "dropped_frames": String(droppedFrames),
            "playback_pending": String(playbackPending), "playback_underruns": String(playbackUnderruns),
            "playback_resets": String(playbackResets), "audio_enabled": String(activated),
            "microphone_track_enabled": String(!muted)]) { _, new in new }
            .merging(dropCounts.fields) { _, new in new }
    }
    private func interpolatedTestAudio(now: Double) -> ModemDeckTestAudioStatus? {
        guard let audio = latestTestAudio else { return nil }
        let remaining = max(0, audio.remainingMS - Int((now - testAudioAt) * 1000))
        return ModemDeckTestAudioStatus(phase: audio.phase, remainingMS: remaining,
            capturedFrames: audio.capturedFrames, capturedDBFS: audio.capturedDBFS, capturedPeakDBFS: audio.capturedPeakDBFS,
            receivedPackets: audio.receivedPackets, inputDBFS: audio.inputDBFS, outputDBFS: audio.outputDBFS)
    }
    private func publishMediaState(_ state: String) {
        DispatchQueue.main.async { [weak self] in self?.onMediaStateChanged?(state) }
    }
    private func failMedia(_ error: Error) {
        guard !stopped else { return }
        if connectCompletion != nil { finishConnection(.failure(error)) }
        else {
            recordConnectionEvent("media_failed", error: error); stopLocked(notifyRemoteEnd: false)
            DispatchQueue.main.async { [weak self] in self?.onFailed?(error) }
        }
    }
    private func finishConnection(_ result: Result<Void, Error>) {
        guard let completion = connectCompletion else { return }
        connectCompletion = nil; connecting = false; connectTimeout?.cancel(); connectTimeout = nil
        DispatchQueue.main.async { completion(result) }
        if case .failure(let error) = result {
            recordConnectionEvent("connection_failed", error: error); stopLocked(notifyRemoteEnd: false)
        }
    }
    private func stopLocked(notifyRemoteEnd: Bool) {
        guard !stopped else { return }
        recordConnectionEvent("stopped"); stopped = true
        socket?.cancel(with: .normalClosure, reason: nil); socket = nil; ready = false
        stopAudio(); sendQueue.removeAll()
        leaseTimer?.cancel(); leaseTimer = nil
        testPhaseTimer?.cancel(); testPhaseTimer = nil
        telemetryTimer?.cancel(); telemetryTimer = nil
        connectTimeout?.cancel(); connectTimeout = nil
        reconnectTimeout?.cancel(); reconnectTimeout = nil
        urlSession.invalidateAndCancel()
        if mediaClaimed { releaseMedia() }
        publishMediaState("ended")
        if let completion = connectCompletion {
            connectCompletion = nil
            DispatchQueue.main.async { completion(.failure(ModemDeckCallAudioError.callEnded)) }
        }
        if notifyRemoteEnd { DispatchQueue.main.async { [weak self] in self?.onRemoteEnded?() } }
    }
    private func setConnectionStage(_ stage: String) {
        connectionStage = stage; stageStartedAt = ProcessInfo.processInfo.systemUptime
        recordConnectionEvent("stage_changed")
    }
    private func recordConnectionEvent(_ event: String, fields: [String: String] = [:], error: Error? = nil) {
        ModemDeckDiagnostics.shared.record(.audio, event, callID: callID, fields: [
            "elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - connectionStartedAt) * 1000)),
            "stage_elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - stageStartedAt) * 1000)),
            "stage": connectionStage, "test_call": String(testCall)].merging(fields) { _, new in new },
            error: error, scope: credential.callControlScope)
    }

    private func waitForActiveCall() {
        guard !stopped else { return }
        if Date() >= connectDeadline {
            finishConnection(.failure(ModemDeckCallAudioError.timedOut))
            return
        }
        request(path: activeCallsPath, method: "GET") { [weak self] result in
            guard let self, !self.stopped else { return }
            switch result {
            case .failure(let error):
                self.retryActiveCall(after: self.activePollDelay(), lastError: error)
            case .success(let data):
                guard let response = try? JSONDecoder().decode(
                    ModemDeckActiveCallsResponse.self,
                    from: data
                ) else {
                    self.finishConnection(.failure(ModemDeckCallAudioError.invalidResponse))
                    return
                }
                guard let call = response.calls.first(where: { $0.id == self.callID }) else {
                    self.retryActiveCall(
                        after: self.activePollDelay(),
                        lastError: ModemDeckCallAudioError.callNotActive
                    )
                    return
                }
                switch call.phase {
                case "active":
                    guard call.controlState == "owned" else {
                        self.finishConnection(.failure(ModemDeckCallAudioError.callNotActive))
                        return
                    }
                    guard call.mediaAvailable else {
                        self.finishConnection(.failure(ModemDeckCallAudioError.mediaUnavailable))
                        return
                    }
                    self.openSocket()
                case "ended", "failed":
                    self.finishConnection(.failure(ModemDeckCallAudioError.callEnded))
                default:
                    self.retryActiveCall(
                        after: self.activePollDelay(),
                        lastError: ModemDeckCallAudioError.callNotActive
                    )
                }
            }
        }
    }

    private func activePollDelay() -> TimeInterval {
        defer { activePollAttempt += 1 }
        return min(1, 0.25 * pow(2, Double(min(activePollAttempt, 2))))
    }

    private func retryActiveCall(after delay: TimeInterval, lastError: Error) {
        guard Date().addingTimeInterval(delay) < connectDeadline else {
            finishConnection(.failure(lastError))
            return
        }
        queue.asyncAfter(deadline: .now() + delay) { [weak self] in
            self?.waitForActiveCall()
        }
    }

    private func startLeaseHeartbeat() {
        guard leaseTimer == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now() + 5, repeating: 5, leeway: .milliseconds(250))
        timer.setEventHandler { [weak self] in
            guard let self, !self.stopped, !self.leaseRequestInFlight else { return }
            self.leaseRequestInFlight = true
            self.request(
                path: self.callPath("lease"),
                method: "PUT",
                json: [:]
            ) { [weak self] result in
                guard let self, !self.stopped else { return }
                self.leaseRequestInFlight = false
                if case .failure(let error) = result {
                    ModemDeckDiagnostics.shared.record(.audio, "lease_renewal_failed", error: error)
                }
            }
        }
        leaseTimer = timer
        timer.resume()
    }

    private func startTestPhaseUpdates() {
        guard testCall, testPhaseTimer == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now(), repeating: 1, leeway: .milliseconds(100))
        timer.setEventHandler { [weak self] in
            guard let self, !self.stopped, !self.testPhaseRequestInFlight else { return }
            self.testPhaseRequestInFlight = true
            self.request(path: self.activeCallsPath, method: "GET") { [weak self] result in
                guard let self, !self.stopped else { return }
                self.testPhaseRequestInFlight = false
                guard case .success(let data) = result,
                      let state = try? JSONDecoder().decode(ModemDeckActiveCallsResponse.self, from: data) else { return }
                guard state.calls.contains(where: { $0.id == self.callID }) else {
                    self.stopLocked(notifyRemoteEnd: true)
                    return
                }
                if let phase = state.testPhase {
                    DispatchQueue.main.async { [weak self] in self?.onTestPhaseChanged?(phase) }
                }
                if let audio = state.testAudio {
                    let now = ProcessInfo.processInfo.systemUptime
                    var snapshot = audio
                    if self.latestTestAudio?.phase == audio.phase, let previous = self.interpolatedTestAudio(now: now) {
                        snapshot = ModemDeckTestAudioStatus(phase: audio.phase, remainingMS: min(audio.remainingMS, previous.remainingMS),
                            capturedFrames: audio.capturedFrames, capturedDBFS: audio.capturedDBFS, capturedPeakDBFS: audio.capturedPeakDBFS,
                            receivedPackets: audio.receivedPackets, inputDBFS: audio.inputDBFS, outputDBFS: audio.outputDBFS)
                    }
                    self.latestTestAudio = snapshot; self.testAudioAt = now
                    let published = snapshot
                    DispatchQueue.main.async { [weak self] in self?.onTestAudioChanged?(published) }
                }
            }
        }
        testPhaseTimer = timer
        timer.resume()
    }

    private func releaseMedia(completion: @escaping (Bool) -> Void = { _ in }) {
        guard let body = try? JSONSerialization.data(
            withJSONObject: ["owner_token": ownerToken]
        ), let request = try? authorizedRequest(
            credential: credential,
            path: callPath("media"),
            method: "DELETE",
            headers: [
                "Accept": "application/json",
                "Content-Type": "application/json"
            ],
            body: body,
            timeout: 8
        ) else {
            completion(false); return
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 8
        configuration.timeoutIntervalForResource = 10
        let releaseSession = ModemDeckDiagnostics.urlSession(configuration: configuration)
        releaseSession.diagnosticDataTask(with: request) { _, response, error in
            releaseSession.finishTasksAndInvalidate()
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            completion(error == nil && (200..<300).contains(status))
        }.resume()
    }

    private func request(
        path: String,
        method: String,
        json: [String: Any]? = nil,
        completion: @escaping (Result<Data, Error>) -> Void
    ) {
        let body: Data?
        do {
            body = try json.map {
                try JSONSerialization.data(withJSONObject: $0)
            }
        } catch {
            completion(.failure(error))
            return
        }
        let request: URLRequest
        do {
            request = try authorizedRequest(
                credential: credential,
                path: path,
                method: method,
                headers: [
                    "Accept": "application/json",
                    "Content-Type": "application/json"
                ],
                body: body,
                timeout: 12
            )
        } catch {
            completion(.failure(error))
            return
        }
        urlSession.diagnosticDataTask(with: request) { [weak self] data, response, error in
            self?.queue.async {
                if let error {
                    completion(.failure(error))
                    return
                }
                guard let response = response as? HTTPURLResponse else {
                    completion(.failure(ModemDeckCallAudioError.invalidResponse))
                    return
                }
                guard (200..<300).contains(response.statusCode) else {
                    if let data,
                       let error = try? JSONDecoder().decode(ModemDeckServerError.self, from: data),
                       let message = error.message, !message.isEmpty {
                        completion(.failure(ModemDeckAPIError.server(
                            status: response.statusCode, code: error.code ?? "", message: message
                        )))
                        return
                    }
                    completion(.failure(ModemDeckHTTPError(status: response.statusCode)))
                    return
                }
                completion(.success(data ?? Data()))
            }
        }.resume()
    }

    private var activeCallsPath: String {
        testCall ? callPath("active") : "/api/v1/calls/active"
    }

    private func callPath(_ suffix: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(
            CharacterSet(charactersIn: "-._~")
        )
        let encoded = callID.addingPercentEncoding(withAllowedCharacters: allowed) ?? ""
        let prefix = testCall ? "/api/v1/mobile/call-tests" : "/api/v1/calls"
        return "\(prefix)/\(encoded)/\(suffix)"
    }
}

final class ModemDeckTestCallTone {
    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    private var started = false
    private var muted = false
    private var forcedSpeaker = false
    private weak var audioSession: AVAudioSession?

    func start(audioSession: AVAudioSession) {
        guard !started else { return }
        self.audioSession = audioSession
        if audioSession.currentRoute.outputs.contains(where: {
            $0.portType == .builtInReceiver
        }) {
            do {
                try audioSession.overrideOutputAudioPort(.speaker)
                forcedSpeaker = true
            } catch {
                ModemDeckDiagnostics.shared.record(.audio, "speaker_route_failed", error: error)
            }
        }

        let outputFormat = engine.outputNode.outputFormat(forBus: 0)
        let sampleRate = outputFormat.sampleRate > 0
            ? outputFormat.sampleRate
            : max(audioSession.sampleRate, 48_000)
        let channelCount = max(AVAudioChannelCount(1), outputFormat.channelCount)
        guard let format = AVAudioFormat(
                commonFormat: .pcmFormatFloat32,
                sampleRate: sampleRate,
                channels: channelCount,
                interleaved: false
              ),
              let buffer = AVAudioPCMBuffer(
                pcmFormat: format,
                frameCapacity: AVAudioFrameCount(sampleRate)
              ),
              let channels = buffer.floatChannelData else {
            restoreOutputRoute()
            return
        }
        buffer.frameLength = buffer.frameCapacity
        for channel in 0..<Int(format.channelCount) {
            let samples = channels[channel]
            for frame in 0..<Int(buffer.frameLength) {
                let time = Double(frame) / format.sampleRate
                let audible = time < 0.18 || (time >= 0.30 && time < 0.48)
                samples[frame] = audible
                    ? Float(sin(2 * Double.pi * 440 * time) * 0.35)
                    : 0
            }
        }
        if !engine.attachedNodes.contains(player) {
            engine.attach(player)
        }
        engine.disconnectNodeOutput(player)
        engine.connect(player, to: engine.mainMixerNode, format: format)
        engine.mainMixerNode.outputVolume = 1
        player.volume = muted ? 0 : 1
        player.scheduleBuffer(buffer, at: nil, options: .loops)
        engine.prepare()
        do {
            try engine.start()
            player.play()
            started = true
            ModemDeckDiagnostics.shared.record(.audio, "test_tone_started", fields: ["sample_rate": String(Int(sampleRate)), "channels": String(channelCount)])
        } catch {
            ModemDeckDiagnostics.shared.record(.audio, "test_tone_failed", error: error)
            restoreOutputRoute()
        }
    }

    func setMuted(_ muted: Bool) {
        self.muted = muted
        player.volume = muted ? 0 : 1
    }

    func stop() {
        if started {
            player.stop()
            engine.stop()
            engine.reset()
        }
        started = false
        restoreOutputRoute()
    }

    private func restoreOutputRoute() {
        defer {
            forcedSpeaker = false
            audioSession = nil
        }
        guard forcedSpeaker, let audioSession else { return }
        do {
            try audioSession.overrideOutputAudioPort(.none)
        } catch {
            ModemDeckDiagnostics.shared.record(.audio, "route_restore_failed", error: error)
        }
    }
}
