import AVFoundation
import Foundation
import WebRTC

enum ModemDeckCallAudioError: LocalizedError {
    case callEnded
    case callNotActive
    case mediaUnavailable
    case invalidResponse
    case negotiationFailed
    case relayUnavailable
    case timedOut

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
        case .relayUnavailable:
            return "The call audio relay could not be reached. Check your network and try again."
        case .timedOut:
            return "Call audio took too long to connect."
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

private struct ModemDeckICEConfiguration: Decodable {
    struct Server: Decodable {
        let urls: [String]
        let username: String?
        let credential: String?
    }

    let iceServers: [Server]
    let transportPolicy: String

    enum CodingKeys: String, CodingKey {
        case iceServers = "ice_servers"
        case transportPolicy = "ice_transport_policy"
    }
}

private struct ModemDeckMediaAnswer: Decodable {
    let answerSDP: String

    enum CodingKeys: String, CodingKey {
        case answerSDP = "answer_sdp"
    }
}

private struct ModemDeckHTTPError: LocalizedError {
    let status: Int

    var errorDescription: String? {
        "ModemDeck returned HTTP \(status)."
    }
}

final class ModemDeckCallAudioSession: NSObject {
    typealias ConnectionCompletion = (Result<Void, Error>) -> Void

    private static let factory: RTCPeerConnectionFactory = {
        RTCInitializeSSL()
        return RTCPeerConnectionFactory(
            encoderFactory: RTCDefaultVideoEncoderFactory(),
            decoderFactory: RTCDefaultVideoDecoderFactory()
        )
    }()

    let callID: String
    var onRemoteEnded: (() -> Void)?
    var onBecameActive: (() -> Void)?
    var onTestPhaseChanged: ((String) -> Void)?
    var onTestAudioChanged: ((ModemDeckTestAudioStatus) -> Void)?
    var onMicrophoneLevelChanged: ((Int?) -> Void)?

    private let credential: ModemDeckCredential
    private let testCall: Bool
    private let ownerToken = UUID().uuidString.lowercased()
    private let queue: DispatchQueue
    private let urlSession: URLSession
    private var mediaRequest: ModemDeckMediaRequest?
    private var peerConnection: RTCPeerConnection?
    private var localAudioTrack: RTCAudioTrack?
    private var muted = false
    private var connectCompletion: ConnectionCompletion?
    private var connectDeadline = Date.distantPast
    private var activePollAttempt = 0
    private var waitingForICE = false
    private var localDescriptionReady = false
    private var relayRequired = false
    private var gatheredCandidateCount = 0
    private var relayCandidateCount = 0
    private var gatheringTimeout: DispatchWorkItem?
    private var offerSubmission: DispatchWorkItem?
    private var connectionStartedAt = ProcessInfo.processInfo.systemUptime
    private var connectionStage = "idle"
    private var stageStartedAt = ProcessInfo.processInfo.systemUptime
    private var gatheringStartedAt = ProcessInfo.processInfo.systemUptime
    private var mediaClaimed = false
    private var stopped = false
    private var connecting = false
    private var reportedActive = false
    private var leaseTimer: DispatchSourceTimer?
    private var leaseRequestInFlight = false
    private var testPhaseTimer: DispatchSourceTimer?
    private var testPhaseRequestInFlight = false
    private var connectTimeout: DispatchWorkItem?
    private var disconnectTimeout: DispatchWorkItem?
    private var statisticsTimer: DispatchSourceTimer?
    private var statisticsInFlight = false
    private var previousAudioEnergy: (energy: Double, duration: Double)?
    private var lastStatisticsLog = 0.0

    init(callID: String, credential: ModemDeckCredential, testCall: Bool = false) {
        self.callID = callID
        self.credential = credential
        self.testCall = testCall
        queue = DispatchQueue(label: "modemdeck.call-audio.\(callID)")
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 12
        configuration.timeoutIntervalForResource = 18
        urlSession = ModemDeckDiagnostics.urlSession(configuration: configuration)
        super.init()
        RTCAudioSession.sharedInstance().add(self)
    }

    deinit { RTCAudioSession.sharedInstance().remove(self) }

    static func prepareAudioSession() {
        let session = RTCAudioSession.sharedInstance()
        session.useManualAudio = true
        session.isAudioEnabled = false
        let configuration = RTCAudioSessionConfiguration.webRTC()
        configuration.category = AVAudioSession.Category.playAndRecord.rawValue
        configuration.mode = AVAudioSession.Mode.voiceChat.rawValue
        configuration.categoryOptions = [.allowBluetoothHFP]
        RTCAudioSessionConfiguration.setWebRTC(configuration)
        session.lockForConfiguration()
        defer { session.unlockForConfiguration() }
        do {
            try session.setConfiguration(configuration)
        } catch {
            ModemDeckDiagnostics.shared.record(.audio, "session_prepare_failed", error: error)
        }
    }

    func applyRuntimeState(phase: String) {
        queue.async { [weak self] in
            guard let self, !self.stopped,
                  phase == "active", !self.reportedActive else { return }
            self.reportedActive = true
            DispatchQueue.main.async { [weak self] in
                self?.onBecameActive?()
            }
        }
    }

    func connect(completion: @escaping ConnectionCompletion) {
        queue.async { [weak self] in
            guard let self, !self.stopped else {
                DispatchQueue.main.async {
                    completion(.failure(ModemDeckCallAudioError.callEnded))
                }
                return
            }
            guard !self.connecting else {
                DispatchQueue.main.async {
                    completion(.failure(ModemDeckCallAudioError.negotiationFailed))
                }
                return
            }
            self.connecting = true
            self.connectCompletion = completion
            self.connectionStartedAt = ProcessInfo.processInfo.systemUptime
            self.setConnectionStage("waiting_for_active_call")
            self.connectDeadline = Date().addingTimeInterval(20)
            self.activePollAttempt = 0
            let timeout = DispatchWorkItem { [weak self] in
                guard let self, self.connectCompletion != nil else { return }
                self.recordConnectionEvent("connection_deadline")
                self.finishConnection(.failure(ModemDeckCallAudioError.timedOut))
            }
            self.connectTimeout = timeout
            self.queue.asyncAfter(deadline: .now() + 20, execute: timeout)
            self.waitForActiveCall()
        }
    }

    static func didActivate(_ audioSession: AVAudioSession) {
        dispatchPrecondition(condition: .onQueue(.main))
        ModemDeckDiagnostics.shared.record(.audio, "session_activated")
        let rtcSession = RTCAudioSession.sharedInstance()
        rtcSession.audioSessionDidActivate(audioSession)
        rtcSession.isAudioEnabled = true
        ModemDeckAudioRoute.shared.setActive(true)
    }

    static func didDeactivate(_ audioSession: AVAudioSession) {
        dispatchPrecondition(condition: .onQueue(.main))
        ModemDeckDiagnostics.shared.record(.audio, "session_deactivated")
        let rtcSession = RTCAudioSession.sharedInstance()
        rtcSession.isAudioEnabled = false
        rtcSession.audioSessionDidDeactivate(audioSession)
        ModemDeckAudioRoute.shared.setActive(false)
    }

    func setMuted(_ muted: Bool) {
        queue.async { [weak self] in
            guard let self, !self.stopped else { return }
            self.muted = muted
            self.localAudioTrack?.isEnabled = !muted
        }
    }

    func stop() {
        // cleanupCall removes the last owner before this work can execute.
        queue.async {
            self.stopLocked(notifyRemoteEnd: false)
        }
    }

    func startControlHeartbeat() {
        queue.async { [weak self] in
            guard let self, !self.stopped else { return }
            self.startLeaseHeartbeat()
        }
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
                    self.fetchICEConfiguration()
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

    private func fetchICEConfiguration() {
        setConnectionStage("fetching_turn_configuration")
        request(
            path: callPath("media/ice"),
            method: "POST",
            json: [:]
        ) { [weak self] result in
            guard let self, !self.stopped else { return }
            switch result {
            case .failure(let error):
                self.finishConnection(.failure(error))
            case .success(let data):
                guard let configuration = try? JSONDecoder().decode(
                    ModemDeckICEConfiguration.self,
                    from: data
                ) else {
                    self.finishConnection(.failure(ModemDeckCallAudioError.invalidResponse))
                    return
                }
                self.createOffer(configuration: configuration)
            }
        }
    }

    private func createOffer(configuration: ModemDeckICEConfiguration) {
        setConnectionStage("creating_offer")
        for (index, url) in configuration.iceServers.flatMap(\.urls).enumerated() {
            recordConnectionEvent("turn_endpoint", fields: ModemDeckDiagnostics.turnFields(url).merging(
                ["turn_index": String(index)]) { _, new in new })
        }
        relayRequired = configuration.transportPolicy == "relay"
        let rtcConfiguration = RTCConfiguration()
        rtcConfiguration.sdpSemantics = .unifiedPlan
        rtcConfiguration.continualGatheringPolicy = .gatherOnce
        rtcConfiguration.iceTransportPolicy = relayRequired
            ? .relay
            : .all
        rtcConfiguration.iceServers = configuration.iceServers.map { server in
            RTCIceServer(
                urlStrings: server.urls,
                username: server.username,
                credential: server.credential
            )
        }
        let peerConstraints = RTCMediaConstraints(
            mandatoryConstraints: nil,
            optionalConstraints: ["DtlsSrtpKeyAgreement": "true"]
        )
        guard let peer = Self.factory.peerConnection(
            with: rtcConfiguration,
            constraints: peerConstraints,
            delegate: self
        ) else {
            finishConnection(.failure(ModemDeckCallAudioError.negotiationFailed))
            return
        }
        peerConnection = peer
        let source = Self.factory.audioSource(
            with: RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        )
        let track = Self.factory.audioTrack(with: source, trackId: "modemdeck-audio")
        installLocalAudioTrack(track)
        peer.add(track, streamIds: ["modemdeck-call"])

        let offerConstraints = RTCMediaConstraints(
            mandatoryConstraints: [
                kRTCMediaConstraintsOfferToReceiveAudio: kRTCMediaConstraintsValueTrue,
                kRTCMediaConstraintsOfferToReceiveVideo: kRTCMediaConstraintsValueFalse
            ],
            optionalConstraints: nil
        )
        peer.offer(for: offerConstraints) { [weak self] description, error in
            self?.queue.async {
                guard let self, !self.stopped else { return }
                guard let description, error == nil else {
                    self.finishConnection(
                        .failure(error ?? ModemDeckCallAudioError.negotiationFailed)
                    )
                    return
                }
                self.waitingForICE = true
                self.setConnectionStage("setting_local_description")
                peer.setLocalDescription(description) { [weak self] error in
                    self?.queue.async {
                        guard let self, !self.stopped else { return }
                        if let error {
                            self.finishConnection(.failure(error))
                        } else {
                            self.localDescriptionReady = true
                            self.setConnectionStage("gathering_candidates")
                            self.startGatheringDeadline()
                            self.scheduleGatheredOffer()
                            if peer.iceGatheringState == .complete {
                                self.finishGathering()
                            }
                        }
                    }
                }
            }
        }
    }

    private func startGatheringDeadline() {
        gatheringStartedAt = ProcessInfo.processInfo.systemUptime
        let timeout = DispatchWorkItem { [weak self] in
            guard let self, !self.stopped, self.waitingForICE else { return }
            self.recordConnectionEvent("gathering_deadline")
            self.finishGathering()
        }
        gatheringTimeout = timeout
        queue.asyncAfter(deadline: .now() + 8, execute: timeout)
    }

    private func hasUsableCandidate(in sdp: String) -> Bool {
        sdp.components(separatedBy: .newlines).contains { line in
            guard line.hasPrefix("a=candidate:") else { return false }
            return !relayRequired || line.contains(" typ relay")
        }
    }

    private func scheduleGatheredOffer() {
        guard !stopped, waitingForICE, localDescriptionReady,
              offerSubmission == nil,
              let sdp = peerConnection?.localDescription?.sdp,
              hasUsableCandidate(in: sdp) else { return }
        // The API accepts one SDP snapshot. Briefly collect adjacent candidates,
        // then negotiate without waiting for every interface/TURN URL to finish.
        let submission = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.offerSubmission = nil
            self.exchangeGatheredOffer()
        }
        offerSubmission = submission
        queue.asyncAfter(deadline: .now() + .milliseconds(200), execute: submission)
    }

    private func finishGathering() {
        guard !stopped, waitingForICE, localDescriptionReady else { return }
        guard let sdp = peerConnection?.localDescription?.sdp,
              hasUsableCandidate(in: sdp) else {
            finishConnection(.failure(relayRequired
                ? ModemDeckCallAudioError.relayUnavailable
                : ModemDeckCallAudioError.negotiationFailed))
            return
        }
        exchangeGatheredOffer()
    }

    private func exchangeGatheredOffer() {
        guard !stopped, waitingForICE, localDescriptionReady,
              let offer = peerConnection?.localDescription?.sdp,
              hasUsableCandidate(in: offer) else { return }
        waitingForICE = false
        gatheringTimeout?.cancel()
        gatheringTimeout = nil
        offerSubmission?.cancel()
        offerSubmission = nil
        setConnectionStage("exchanging_offer")
        mediaClaimed = true
        exchangeMedia(offer: offer) { [weak self] result in
            guard let self, !self.stopped else { return }
            switch result {
            case .failure(let error):
                self.finishConnection(.failure(error))
            case .success(let data):
                guard let answer = try? JSONDecoder().decode(
                    ModemDeckMediaAnswer.self,
                    from: data
                ), !answer.answerSDP.isEmpty,
                      let peer = self.peerConnection else {
                    self.finishConnection(.failure(ModemDeckCallAudioError.invalidResponse))
                    return
                }
                self.setConnectionStage("applying_answer")
                peer.setRemoteDescription(
                    RTCSessionDescription(type: .answer, sdp: answer.answerSDP)
                ) { [weak self] error in
                    self?.queue.async {
                        guard let self, !self.stopped else { return }
                        if let error {
                            self.finishConnection(.failure(error))
                            return
                        }
                        self.setConnectionStage("connecting_ice")
                        self.startLeaseHeartbeat()
                        self.startTestPhaseUpdates()
                        self.startAudioStatistics()
                        if peer.iceConnectionState == .connected ||
                            peer.iceConnectionState == .completed {
                            self.finishConnection(.success(()))
                        }
                    }
                }
            }
        }
    }

    private func installLocalAudioTrack(_ track: RTCAudioTrack) {
        track.isEnabled = !muted
        localAudioTrack = track
    }

    private func exchangeMedia(offer: String, completion: @escaping (Result<Data, Error>) -> Void) {
        do {
            let body = try JSONSerialization.data(withJSONObject: ["owner_token": ownerToken, "offer_sdp": offer])
            let request = try authorizedRequest(credential: credential, path: callPath("media"), method: "POST",
                headers: ["Accept": "application/json", "Content-Type": "application/json"], body: body, timeout: 10)
            let operation = ModemDeckMediaRequest(queue: queue)
            operation.onRetry = { [weak self] attempt, error in
                self?.recordConnectionEvent("media_request_retry", fields: ["attempt": String(attempt)], error: error)
            }
            mediaRequest = operation
            operation.start(request, deadline: connectDeadline) { [weak self] data, response, error in
                guard let self, !self.stopped else { return }
                self.mediaRequest = nil
                if let error { completion(.failure(error)); return }
                guard let response = response as? HTTPURLResponse else {
                    completion(.failure(ModemDeckCallAudioError.invalidResponse)); return
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
                    completion(.failure(ModemDeckHTTPError(status: response.statusCode))); return
                }
                completion(.success(data ?? Data()))
            }
        } catch { completion(.failure(error)) }
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

    // Presentation polling is separate from ownership: both call types use the
    // same five-second PUT lease heartbeat and server-side media liveness.
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
                    DispatchQueue.main.async { [weak self] in self?.onTestAudioChanged?(audio) }
                }
            }
        }
        testPhaseTimer = timer
        timer.resume()
    }

    private func finishConnection(_ result: Result<Void, Error>) {
        guard let completion = connectCompletion else { return }
        switch result {
        case .success:
            setConnectionStage("connected")
        case .failure(let error):
            recordConnectionEvent("connection_failed", error: error)
            ModemDeckDiagnostics.shared.flush()
        }
        connectCompletion = nil
        connecting = false
        connectTimeout?.cancel()
        connectTimeout = nil
        // Let CallKit/UI finish even if WebRTC teardown takes time.
        DispatchQueue.main.async {
            completion(result)
        }
        if case .failure = result {
            stopLocked(notifyRemoteEnd: false)
        }
    }

    private func stopLocked(notifyRemoteEnd: Bool) {
        guard !stopped else { return }
        recordConnectionEvent("stopped")
        stopped = true
        mediaRequest?.cancel()
        mediaRequest = nil
        waitingForICE = false
        gatheringTimeout?.cancel()
        gatheringTimeout = nil
        offerSubmission?.cancel()
        offerSubmission = nil
        leaseTimer?.cancel()
        leaseTimer = nil
        testPhaseTimer?.cancel()
        testPhaseTimer = nil
        statisticsTimer?.cancel()
        statisticsTimer = nil
        connectTimeout?.cancel()
        connectTimeout = nil
        disconnectTimeout?.cancel()
        disconnectTimeout = nil
        localAudioTrack?.isEnabled = false
        peerConnection?.close()
        peerConnection = nil
        localAudioTrack = nil
        // CallKit's activation callbacks exclusively own the shared audio unit.
        // An older session's queued stop must never disable a newer call.
        urlSession.invalidateAndCancel()
        if mediaClaimed {
            releaseMedia()
        }
        if let completion = connectCompletion {
            connectCompletion = nil
            DispatchQueue.main.async {
                completion(.failure(ModemDeckCallAudioError.callEnded))
            }
        }
        if notifyRemoteEnd {
            DispatchQueue.main.async { [weak self] in
                self?.onRemoteEnded?()
            }
        }
    }

    private func setConnectionStage(_ stage: String) {
        connectionStage = stage
        stageStartedAt = ProcessInfo.processInfo.systemUptime
        recordConnectionEvent("stage_changed")
    }

    private func startAudioStatistics() {
        guard statisticsTimer == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now(), repeating: testCall ? 1 : 5)
        timer.setEventHandler { [weak self] in
            guard let self, !self.stopped, !self.statisticsInFlight, let peer = self.peerConnection else { return }
            self.statisticsInFlight = true
            peer.statistics { [weak self] report in
                self?.queue.async { [weak self] in
                    guard let self, !self.stopped, self.peerConnection === peer else { return }
                    self.statisticsInFlight = false
                    var fields = ModemDeckAudioRoute.fields()
                    fields["audio_enabled"] = String(RTCAudioSession.sharedInstance().isAudioEnabled)
                    fields["microphone_track_enabled"] = String(self.localAudioTrack?.isEnabled == true)
                    var microphoneDBFS: Int?
                    for stats in report.statistics.values {
                        let values = stats.values
                        guard (values["kind"] as? String ?? values["mediaType"] as? String) == "audio" else { continue }
                        if stats.type == "media-source" {
                            var level = (values["audioLevel"] as? NSNumber)?.doubleValue
                            if let energy = (values["totalAudioEnergy"] as? NSNumber)?.doubleValue,
                               let duration = (values["totalSamplesDuration"] as? NSNumber)?.doubleValue {
                                if let previous = self.previousAudioEnergy, duration > previous.duration, energy >= previous.energy {
                                    level = sqrt((energy - previous.energy) / (duration - previous.duration))
                                }
                                self.previousAudioEnergy = (energy, duration)
                            }
                            if let level, level.isFinite {
                                microphoneDBFS = level > 0 ? Int(max(-96, min(0, 20 * log10(level)))) : -96
                                fields["microphone_dbfs"] = String(microphoneDBFS!)
                            }
                        }
                        let counters = stats.type == "outbound-rtp" ? ["packetsSent": "sent_packets", "bytesSent": "sent_bytes"] :
                            (stats.type == "inbound-rtp" ? ["packetsReceived": "received_packets", "bytesReceived": "received_bytes", "packetsLost": "lost_packets"] : [:])
                        for (key, field) in counters {
                            if let value = values[key] as? NSNumber { fields[field] = String(max(-1_000_000_000, min(1_000_000_000, value.int64Value))) }
                        }
                    }
                    if self.testCall {
                        DispatchQueue.main.async { [weak self] in self?.onMicrophoneLevelChanged?(microphoneDBFS) }
                    }
                    let now = ProcessInfo.processInfo.systemUptime
                    if now - self.lastStatisticsLog >= 5 {
                        self.lastStatisticsLog = now
                        self.recordConnectionEvent("audio_statistics", fields: fields)
                    }
                }
            }
        }
        statisticsTimer = timer
        timer.resume()
    }

    private func recordConnectionEvent(_ event: String, fields: [String: String] = [:], error: Error? = nil) {
        let elapsed = Int((ProcessInfo.processInfo.systemUptime - connectionStartedAt) * 1_000)
        let values = ["elapsed_ms": String(elapsed), "stage": connectionStage,
                      "stage_elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - stageStartedAt) * 1_000)),
                      "candidates": String(gatheredCandidateCount), "relay_candidates": String(relayCandidateCount),
                      "test_call": String(testCall)].merging(fields) { _, new in new }
        ModemDeckDiagnostics.shared.record(.audio, event, callID: callID, fields: values,
                                           error: error, scope: credential.callControlScope)
    }

    private func releaseMedia() {
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
            return
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 8
        configuration.timeoutIntervalForResource = 10
        let releaseSession = ModemDeckDiagnostics.urlSession(configuration: configuration)
        releaseSession.diagnosticDataTask(with: request) { _, _, _ in
            releaseSession.finishTasksAndInvalidate()
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

extension ModemDeckCallAudioSession: RTCAudioSessionDelegate {
    func audioSessionDidStartPlayOrRecord(_ session: RTCAudioSession) {
        queue.async { [weak self] in self?.recordConnectionEvent("audio_unit_started") }
    }
    func audioSessionDidStopPlayOrRecord(_ session: RTCAudioSession) {
        queue.async { [weak self] in self?.recordConnectionEvent("audio_unit_stopped") }
    }
    func audioSession(_ audioSession: RTCAudioSession, audioUnitStartFailedWithError error: Error) {
        queue.async { [weak self] in self?.recordConnectionEvent("audio_unit_failed", error: error) }
    }
}

extension ModemDeckCallAudioSession: RTCPeerConnectionDelegate {
    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didChange stateChanged: RTCSignalingState
    ) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}

    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}

    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didChange newState: RTCIceConnectionState
    ) {
        queue.async { [weak self] in
            guard let self, !self.stopped,
                  self.peerConnection === peerConnection else { return }
            self.recordConnectionEvent("ice_state_changed", fields: ["ice_state": String(newState.rawValue)])
            switch newState {
            case .connected, .completed:
                self.disconnectTimeout?.cancel()
                self.disconnectTimeout = nil
                if self.connectCompletion != nil {
                    self.finishConnection(.success(()))
                }
            case .disconnected:
                guard self.disconnectTimeout == nil else { return }
                let timeout = DispatchWorkItem {
                    [weak self, weak peerConnection = peerConnection] in
                    guard let self, !self.stopped, let peerConnection else { return }
                    guard peerConnection.iceConnectionState == .disconnected ||
                            peerConnection.iceConnectionState == .failed ||
                            peerConnection.iceConnectionState == .closed else {
                        return
                    }
                    if self.connectCompletion != nil {
                        self.finishConnection(
                            .failure(ModemDeckCallAudioError.negotiationFailed)
                        )
                    } else {
                        self.stopLocked(notifyRemoteEnd: true)
                    }
                }
                self.disconnectTimeout = timeout
                self.queue.asyncAfter(deadline: .now() + 10, execute: timeout)
            case .failed, .closed:
                if self.connectCompletion != nil {
                    self.finishConnection(.failure(ModemDeckCallAudioError.negotiationFailed))
                } else {
                    self.stopLocked(notifyRemoteEnd: true)
                }
            default:
                break
            }
        }
    }

    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didChange newState: RTCIceGatheringState
    ) {
        queue.async { [weak self] in
            guard let self, !self.stopped,
                  self.peerConnection === peerConnection else { return }
            self.recordConnectionEvent("gathering_state_changed", fields: ["gathering_state": String(newState.rawValue)])
            if newState == .complete { self.finishGathering() }
        }
    }

    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didFailToGatherIceCandidate event: RTCIceCandidateErrorEvent
    ) {
        // The raw error can contain local addresses; retain only its code and a
        // fixed reason category. The TURN URL is reduced to provider/port/transport.
        var fields = ModemDeckDiagnostics.turnFields(event.url)
        fields["error_code"] = String(event.errorCode)
        fields["reason"] = ModemDeckDiagnostics.turnFailureReason(event.errorText)
        queue.async { [weak self] in
            guard let self, !self.stopped, self.peerConnection === peerConnection else { return }
            fields["stage_elapsed_ms"] = String(max(0, Int((ProcessInfo.processInfo.systemUptime - self.gatheringStartedAt) * 1_000)))
            self.recordConnectionEvent("turn_gather_failed", fields: fields)
        }
    }

    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didGenerate candidate: RTCIceCandidate
    ) {
        queue.async { [weak self] in
            guard let self, !self.stopped,
                  self.peerConnection === peerConnection else { return }
            self.gatheredCandidateCount += 1
            if candidate.sdp.contains(" typ relay") { self.relayCandidateCount += 1 }
            self.recordConnectionEvent("candidate_gathered", fields: ModemDeckDiagnostics.turnFields(candidate.serverUrl ?? ""))
            self.scheduleGatheredOffer()
        }
    }

    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didRemove candidates: [RTCIceCandidate]
    ) {}

    func peerConnection(
        _ peerConnection: RTCPeerConnection,
        didOpen dataChannel: RTCDataChannel
    ) {}
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
