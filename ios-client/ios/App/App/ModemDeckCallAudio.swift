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

    private let credential: ModemDeckCredential
    private let testCall: Bool
    private let ownerToken = UUID().uuidString.lowercased()
    private let queue: DispatchQueue
    private let urlSession: URLSession
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
    private var diagnosticEntries: [String] = []
    private var mediaClaimed = false
    private var stopped = false
    private var connecting = false
    private var reportedActive = false
    private var leaseTimer: DispatchSourceTimer?
    private var leaseRequestInFlight = false
    private var connectTimeout: DispatchWorkItem?
    private var disconnectTimeout: DispatchWorkItem?

    init(callID: String, credential: ModemDeckCredential, testCall: Bool = false) {
        self.callID = callID
        self.credential = credential
        self.testCall = testCall
        queue = DispatchQueue(label: "modemdeck.call-audio.\(callID)")
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 12
        configuration.timeoutIntervalForResource = 18
        urlSession = URLSession(configuration: configuration)
        super.init()
    }

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
            NSLog("ModemDeck could not prepare the CallKit audio session: %@", error.localizedDescription)
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
        NSLog("ModemDeck CallKit audio activated")
        let rtcSession = RTCAudioSession.sharedInstance()
        rtcSession.audioSessionDidActivate(audioSession)
        rtcSession.isAudioEnabled = true
    }

    static func didDeactivate(_ audioSession: AVAudioSession) {
        dispatchPrecondition(condition: .onQueue(.main))
        NSLog("ModemDeck CallKit audio deactivated")
        let rtcSession = RTCAudioSession.sharedInstance()
        rtcSession.isAudioEnabled = false
        rtcSession.audioSessionDidDeactivate(audioSession)
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
        guard !testCall else { return }
        queue.async { [weak self] in
            guard let self, !self.stopped else { return }
            self.startLeaseHeartbeat()
        }
    }

    private func waitForActiveCall() {
        guard !stopped else { return }
        if testCall {
            request(path: callPath("status"), method: "GET") { [weak self] result in
                guard let self, !self.stopped else { return }
                switch result {
                case .failure(let error): self.finishConnection(.failure(error))
                case .success(let data):
                    guard let status = try? JSONDecoder().decode(ModemDeckAudioTestStatus.self, from: data),
                          status.phase == "active" else {
                        self.finishConnection(.failure(ModemDeckCallAudioError.callEnded))
                        return
                    }
                    self.fetchICEConfiguration()
                }
            }
            return
        }
        if Date() >= connectDeadline {
            finishConnection(.failure(ModemDeckCallAudioError.timedOut))
            return
        }
        request(path: "/api/v1/calls/active", method: "GET") { [weak self] result in
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
        request(
            path: callPath("media"),
            method: "POST",
            json: [
                "owner_token": ownerToken,
                "offer_sdp": offer
            ]
        ) { [weak self] result in
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

    private func startLeaseHeartbeat() {
        guard leaseTimer == nil else { return }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        let interval: TimeInterval = testCall ? 1 : 5
        timer.schedule(deadline: .now() + interval, repeating: interval, leeway: .milliseconds(250))
        timer.setEventHandler { [weak self] in
            guard let self, !self.stopped, !self.leaseRequestInFlight else { return }
            self.leaseRequestInFlight = true
            self.request(
                path: self.callPath(self.testCall ? "status" : "lease"),
                method: self.testCall ? "GET" : "PUT",
                json: self.testCall ? nil : [:]
            ) { [weak self] result in
                guard let self, !self.stopped else { return }
                self.leaseRequestInFlight = false
                if case .failure(let error) = result {
                    NSLog("ModemDeck call lease renewal failed: %@", error.localizedDescription)
                } else if self.testCall, case .success(let data) = result,
                          let status = try? JSONDecoder().decode(ModemDeckAudioTestStatus.self, from: data) {
                    if status.phase == "ended" || status.phase == "failed" {
                        self.stopLocked(notifyRemoteEnd: true)
                    } else {
                        DispatchQueue.main.async { [weak self] in
                            self?.onTestPhaseChanged?(status.testPhase)
                        }
                    }
                }
            }
        }
        leaseTimer = timer
        timer.resume()
    }

    private func finishConnection(_ result: Result<Void, Error>) {
        guard let completion = connectCompletion else { return }
        switch result {
        case .success:
            setConnectionStage("connected")
        case .failure(let error):
            let failure = error as NSError
            recordConnectionEvent("failed domain=\(failure.domain) code=\(failure.code)")
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
        waitingForICE = false
        gatheringTimeout?.cancel()
        gatheringTimeout = nil
        offerSubmission?.cancel()
        offerSubmission = nil
        leaseTimer?.cancel()
        leaseTimer = nil
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
        recordConnectionEvent("stage_changed")
    }

    private func recordConnectionEvent(_ event: String) {
        let elapsed = Int((ProcessInfo.processInfo.systemUptime - connectionStartedAt) * 1_000)
        // Never record SDP, candidate addresses, phone numbers, or TURN credentials.
        let entry = "call_id=\(callID) elapsed_ms=\(elapsed) stage=\(connectionStage) " +
            "candidates=\(gatheredCandidateCount) relay_candidates=\(relayCandidateCount) event=\(event)"
        NSLog("ModemDeck call connection %@", entry)
        diagnosticEntries.append(entry)
        if diagnosticEntries.count > 64 { diagnosticEntries.removeFirst() }
        UserDefaults.standard.set(diagnosticEntries, forKey: "modemdeck.last-call-connection-diagnostics")
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
        let releaseSession = URLSession(configuration: configuration)
        releaseSession.dataTask(with: request) { _, _, _ in
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
        urlSession.dataTask(with: request) { [weak self] data, response, error in
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
                    if self?.testCall == true, let data,
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

    private func callPath(_ suffix: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(
            CharacterSet(charactersIn: "-._~")
        )
        let encoded = callID.addingPercentEncoding(withAllowedCharacters: allowed) ?? ""
        let prefix = testCall ? "/api/v1/mobile/call-tests" : "/api/v1/calls"
        return "\(prefix)/\(encoded)/\(suffix)"
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
            self.recordConnectionEvent("ice_state=\(newState.rawValue)")
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
            self.recordConnectionEvent("gathering_state=\(newState.rawValue)")
            if newState == .complete { self.finishGathering() }
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
            self.recordConnectionEvent("candidate_gathered")
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
                NSLog(
                    "ModemDeck could not route the test call to the speaker: %@",
                    error.localizedDescription
                )
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
            NSLog(
                "ModemDeck test call tone started at %.0f Hz on %u channel(s)",
                sampleRate,
                channelCount
            )
        } catch {
            NSLog("ModemDeck test call tone failed: %@", error.localizedDescription)
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
            NSLog(
                "ModemDeck could not restore the test call audio route: %@",
                error.localizedDescription
            )
        }
    }
}
