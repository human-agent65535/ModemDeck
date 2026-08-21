import Foundation
import AVFoundation
import CallKit
import Contacts
import Darwin
import PushKit
import Security
import UIKit
import UserNotifications
import WebKit
import Capacitor

struct ModemDeckCredential: Codable {
    let serverURL: String
    let token: String
}

private enum ModemDeckNativeError: LocalizedError {
    case notConfigured
    case invalidServerURL
    case invalidToken
    case invalidPath
    case invalidMethod
    case invalidResponse
    case noActiveCall
    case microphoneDenied
    case keychain(OSStatus)

    var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "This iOS device is not paired with ModemDeck."
        case .invalidServerURL:
            return "The pairing server address is invalid."
        case .invalidToken:
            return "The iOS pairing token is invalid."
        case .invalidPath:
            return "The requested ModemDeck API path is invalid."
        case .invalidMethod:
            return "The requested HTTP method is invalid."
        case .invalidResponse:
            return "ModemDeck returned an invalid response."
        case .noActiveCall:
            return "There is no active iOS call."
        case .microphoneDenied:
            return "Microphone access is required for calls."
        case .keychain(let status):
            return "Keychain operation failed (\(status))."
        }
    }
}

final class ModemDeckCredentialStore {
    private let service: String
    private let account = "active-ios-pairing"
    #if DEBUG
    private var uatCredential: ModemDeckCredential?
    #endif

    init(bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "modemdeck") {
        service = "\(bundleIdentifier).ios-pairing"
    }

    func load() throws -> ModemDeckCredential? {
        #if DEBUG
        if let uatCredential {
            return uatCredential
        }
        #endif
        var result: CFTypeRef?
        var query = baseQuery()
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound {
            return nil
        }
        guard status == errSecSuccess, let data = result as? Data else {
            throw ModemDeckNativeError.keychain(status)
        }
        do {
            return try JSONDecoder().decode(ModemDeckCredential.self, from: data)
        } catch {
            try? clear()
            throw ModemDeckNativeError.invalidResponse
        }
    }

    func save(_ credential: ModemDeckCredential) throws {
        let data = try JSONEncoder().encode(credential)
        let query = baseQuery()
        let update: [String: Any] = [kSecValueData as String: data]
        let updateStatus = SecItemUpdate(query as CFDictionary, update as CFDictionary)
        if updateStatus == errSecSuccess {
            return
        }
        guard updateStatus == errSecItemNotFound else {
            throw ModemDeckNativeError.keychain(updateStatus)
        }
        var addQuery = query
        addQuery[kSecValueData as String] = data
        addQuery[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let addStatus = SecItemAdd(addQuery as CFDictionary, nil)
        guard addStatus == errSecSuccess else {
            throw ModemDeckNativeError.keychain(addStatus)
        }
    }

    func clear() throws {
        #if DEBUG
        if uatCredential != nil {
            uatCredential = nil
            return
        }
        #endif
        let status = SecItemDelete(baseQuery() as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw ModemDeckNativeError.keychain(status)
        }
    }

    private func baseQuery() -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
    }

    #if DEBUG
    func installUATCredential(_ credential: ModemDeckCredential) {
        uatCredential = credential
    }
    #endif
}

private struct ModemDeckPushRegistration: Encodable {
    let apnsToken: String
    let voipToken: String
    let environment: String
    let bundleID: String

    enum CodingKeys: String, CodingKey {
        case apnsToken = "apns_token"
        case voipToken = "voip_token"
        case environment
        case bundleID = "bundle_id"
    }
}

struct ModemDeckNotificationRoute: Equatable {
    let messageID: String
    let lineID: String
    let threadKey: String
}

private struct ModemDeckRuntimeCallState: Decodable {
    let id: String
    let revision: Int64
    let lineID: String
    let direction: String
    let remoteNumber: String
    let displayName: String?
    let phase: String
    let active: Bool
    let ended: Bool
    let wasAnswered: Bool
    let endReason: String?
    let failureCode: String?
    let controlState: String?
    let mediaAvailable: Bool
}

private final class ModemDeckRuntimeCallStream: NSObject, URLSessionDataDelegate {
    typealias StateHandler = (ModemDeckRuntimeCallState) -> Void
    typealias CompletionHandler = (_ status: Int, _ error: Error?) -> Void

    private let request: URLRequest
    private let onState: StateHandler
    private let onCompletion: CompletionHandler
    private var session: URLSession?
    private var task: URLSessionDataTask?
    private var buffer = Data()
    private var eventName = "message"
    private var dataLines: [String] = []
    private var statusCode = 0
    private var accepted = false
    private var cancelled = false
    private var completed = false

    init(
        request: URLRequest,
        onState: @escaping StateHandler,
        onCompletion: @escaping CompletionHandler
    ) {
        self.request = request
        self.onState = onState
        self.onCompletion = onCompletion
        super.init()
    }

    func start() {
        dispatchPrecondition(condition: .onQueue(.main))
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 90
        configuration.timeoutIntervalForResource = 7 * 24 * 60 * 60
        let session = URLSession(
            configuration: configuration,
            delegate: self,
            delegateQueue: .main
        )
        self.session = session
        let task = session.dataTask(with: request)
        self.task = task
        task.resume()
    }

    func cancel() {
        dispatchPrecondition(condition: .onQueue(.main))
        cancelled = true
        task?.cancel()
        session?.invalidateAndCancel()
        task = nil
        session = nil
    }

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        guard let response = response as? HTTPURLResponse else {
            completionHandler(.cancel)
            finish(status: 0, error: ModemDeckNativeError.invalidResponse)
            return
        }
        statusCode = response.statusCode
        let contentType = response.value(forHTTPHeaderField: "Content-Type")?
            .lowercased() ?? ""
        accepted = response.statusCode == 200 && contentType.contains("text/event-stream")
        completionHandler(accepted ? .allow : .cancel)
        if !accepted {
            finish(status: response.statusCode, error: nil)
        }
    }

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive data: Data
    ) {
        guard accepted else { return }
        buffer.append(data)
        consumeLines()
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        guard !cancelled else { return }
        finish(status: statusCode, error: error)
    }

    private func consumeLines() {
        while let newline = buffer.firstIndex(of: 0x0A) {
            var lineData = buffer[..<newline]
            buffer.removeSubrange(...newline)
            if lineData.last == 0x0D {
                lineData = lineData.dropLast()
            }
            guard let line = String(data: lineData, encoding: .utf8) else {
                finish(status: statusCode, error: ModemDeckNativeError.invalidResponse)
                return
            }
            consume(line: line)
        }
    }

    private func consume(line: String) {
        if line.isEmpty {
            dispatchEvent()
            return
        }
        guard !line.hasPrefix(":") else { return }
        let parts = line.split(
            separator: ":",
            maxSplits: 1,
            omittingEmptySubsequences: false
        )
        let field = String(parts[0])
        var value = parts.count == 2 ? String(parts[1]) : ""
        if value.hasPrefix(" ") {
            value.removeFirst()
        }
        switch field {
        case "event":
            eventName = value.isEmpty ? "message" : value
        case "data":
            dataLines.append(value)
        default:
            break
        }
    }

    private func dispatchEvent() {
        defer {
            eventName = "message"
            dataLines.removeAll(keepingCapacity: true)
        }
        guard eventName == "call_state", !dataLines.isEmpty,
              let data = dataLines.joined(separator: "\n").data(using: .utf8) else {
            return
        }
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        guard let state = try? decoder.decode(ModemDeckRuntimeCallState.self, from: data) else {
            return
        }
        onState(state)
    }

    private func finish(status: Int, error: Error?) {
        guard !completed else { return }
        completed = true
        task = nil
        session?.finishTasksAndInvalidate()
        session = nil
        onCompletion(status, error)
    }
}

protocol ModemDeckCallStateObserver: AnyObject {
    func callStateDidChange(_ state: [String: Any])
}

final class ModemDeckPushCoordinator: NSObject, PKPushRegistryDelegate, CXProviderDelegate {
    static let shared = ModemDeckPushCoordinator()

    private struct RequestedCallAction {
        let callUUID: UUID
        let completion: (Result<Void, Error>) -> Void
    }

    private struct PendingOutgoingCall {
        let callID: String
        let lineID: String
        let remoteNumber: String
        let displayName: String
        let completion: (Result<UUID, Error>) -> Void
    }

    private struct PresentedCall {
        let callID: String
        let lineID: String
        let remoteNumber: String
        let displayName: String
        let direction: String
        let testCall: Bool
        let createdAt: Date
        var activeAt: Date?
    }

    private static let timestampFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let maximumRememberedEndedCalls = 64

    private let callProvider: CXProvider
    private let callController = CXCallController()
    private var pushRegistry: PKPushRegistry?
    private var credentialStore: ModemDeckCredentialStore?
    private var apnsToken: String?
    private var voipToken: String?
    private var tokenSyncTask: URLSessionDataTask?
    private var tokenSyncRetryWorkItem: DispatchWorkItem?
    private var tokenSyncRetryAttempt = 0
    private var tokenSyncGeneration = 0
    private var apnsRegistrationRetryWorkItem: DispatchWorkItem?
    private var apnsRegistrationRetryAttempt = 0
    private var pendingNotificationRoute: ModemDeckNotificationRoute?
    private var runtimeCallStream: ModemDeckRuntimeCallStream?
    private var runtimeCallReconnectWorkItem: DispatchWorkItem?
    private var runtimeCallRetryAttempt = 0
    private var runtimeCallGeneration = 0
    private var runtimeCallTargetID: String?
    private var runtimeCallRevision: Int64 = -1
    private var callIDsByUUID: [UUID: String] = [:]
    private var answeredCallUUIDs: Set<UUID> = []
    private var answerRequestedCallUUIDs: Set<UUID> = []
    private var testCallUUIDs: Set<UUID> = []
    private var outgoingCallUUIDs: Set<UUID> = []
    private var mutedCallUUIDs: Set<UUID> = []
    private var preferredRecordingByUUID: [UUID: Bool] = [:]
    private var requestedCallActions: [UUID: RequestedCallAction] = [:]
    private var providerCallActions: [UUID: CXCallAction] = [:]
    private var presentedCalls: [UUID: PresentedCall] = [:]
    private var callAudioSessions: [UUID: ModemDeckCallAudioSession] = [:]
    private var pendingOutgoingCalls: [UUID: PendingOutgoingCall] = [:]
    private var testCallTimeouts: [UUID: DispatchWorkItem] = [:]
    private var endedCallUUIDs: Set<UUID> = []
    private var endedCallOrder: [UUID] = []
    private lazy var testCallTone = ModemDeckTestCallTone()
    private var activeAudioSession: AVAudioSession?
    private weak var callStateObserver: ModemDeckCallStateObserver?
    private var started = false

    private override init() {
        let configuration = CXProviderConfiguration()
        configuration.supportsVideo = false
        configuration.includesCallsInRecents = true
        configuration.maximumCallsPerCallGroup = 1
        configuration.maximumCallGroups = 1
        configuration.supportedHandleTypes = [.phoneNumber, .generic]
        callProvider = CXProvider(configuration: configuration)
        super.init()
        callProvider.setDelegate(self, queue: .main)
    }

    func start() {
        dispatchPrecondition(condition: .onQueue(.main))
        guard !started else { return }
        started = true

        let registry = PKPushRegistry(queue: .main)
        registry.delegate = self
        registry.desiredPushTypes = [.voIP]
        pushRegistry = registry
        UIApplication.shared.registerForRemoteNotifications()
    }

    func configure(store: ModemDeckCredentialStore) {
        let configure = { [weak self] in
            guard let self else { return }
            self.credentialStore = store
            self.start()
            self.syncTokens()
            self.startRuntimeCallStream(forceRestart: true)
        }
        if Thread.isMainThread {
            configure()
        } else {
            DispatchQueue.main.async(execute: configure)
        }
    }

    func clearConfiguration() {
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.credentialStore = nil
            self.apnsRegistrationRetryWorkItem?.cancel()
            self.apnsRegistrationRetryWorkItem = nil
            self.apnsRegistrationRetryAttempt = 0
            self.tokenSyncRetryWorkItem?.cancel()
            self.tokenSyncRetryWorkItem = nil
            self.tokenSyncRetryAttempt = 0
            self.tokenSyncGeneration &+= 1
            self.tokenSyncTask?.cancel()
            self.tokenSyncTask = nil
            self.stopRuntimeCallStream()
            self.endAllCalls(reason: .failed)
        }
    }

    func applicationDidBecomeActive() {
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            UIApplication.shared.registerForRemoteNotifications()
            self.syncTokens()
            self.startRuntimeCallStream()
        }
    }

    private func startRuntimeCallStream(forceRestart: Bool = false) {
        dispatchPrecondition(condition: .onQueue(.main))
        guard credentialStore != nil else { return }
        guard let target = callIDsByUUID.first(where: {
            !testCallUUIDs.contains($0.key)
        }) else {
            if runtimeCallStream != nil || runtimeCallReconnectWorkItem != nil {
                stopRuntimeCallStream()
            }
            return
        }
        let callID = target.value
        if runtimeCallStream != nil,
           runtimeCallTargetID == callID,
           !forceRestart {
            return
        }
        runtimeCallGeneration &+= 1
        runtimeCallReconnectWorkItem?.cancel()
        runtimeCallReconnectWorkItem = nil
        runtimeCallStream?.cancel()
        runtimeCallStream = nil
        runtimeCallTargetID = callID
        runtimeCallRevision = -1

        var queryAllowed = CharacterSet.urlQueryAllowed
        queryAllowed.remove(charactersIn: "&=?+#")
        guard let encodedCallID = callID.addingPercentEncoding(
            withAllowedCharacters: queryAllowed
        ),
              let credential = loadCredential(),
              let request = try? authorizedRequest(
                  credential: credential,
                  path: "/api/v1/runtime/events?call_id=\(encodedCallID)",
                  method: "GET",
                  headers: ["Accept": "text/event-stream"],
                  timeout: 90
              ) else {
            scheduleRuntimeCallReconnect()
            return
        }
        let generation = runtimeCallGeneration
        let stream = ModemDeckRuntimeCallStream(
            request: request,
            onState: { [weak self] state in
                guard let self, generation == self.runtimeCallGeneration else { return }
                guard state.id == callID,
                      state.revision > self.runtimeCallRevision else {
                    return
                }
                self.runtimeCallRetryAttempt = 0
                self.runtimeCallRevision = state.revision
                self.reconcileRuntimeCall(state)
            },
            onCompletion: { [weak self] status, error in
                guard let self, generation == self.runtimeCallGeneration else { return }
                self.runtimeCallStream = nil
                if status == 401 {
                    NotificationCenter.default.post(
                        name: .modemDeckAuthenticationFailed,
                        object: nil
                    )
                    return
                }
                if status == 403 || status == 404 {
                    if let uuid = self.callIDsByUUID.first(where: {
                        $0.value == callID
                    })?.key {
                        self.callProvider.reportCall(
                            with: uuid,
                            endedAt: Date(),
                            reason: .failed
                        )
                        self.cleanupCall(uuid)
                    }
                    return
                }
                if let error, (error as NSError).code != NSURLErrorCancelled {
                    NSLog(
                        "ModemDeck runtime call stream closed: %@",
                        error.localizedDescription
                    )
                }
                self.scheduleRuntimeCallReconnect()
            }
        )
        runtimeCallStream = stream
        stream.start()
    }

    private func scheduleRuntimeCallReconnect() {
        dispatchPrecondition(condition: .onQueue(.main))
        guard credentialStore != nil,
              let callID = runtimeCallTargetID,
              callIDsByUUID.contains(where: {
                  $0.value == callID && !testCallUUIDs.contains($0.key)
              }) else {
            return
        }
        runtimeCallReconnectWorkItem?.cancel()
        let delays: [TimeInterval] = [1, 2, 5, 10, 30]
        let delay = delays[min(runtimeCallRetryAttempt, delays.count - 1)]
        runtimeCallRetryAttempt += 1
        let workItem = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.runtimeCallReconnectWorkItem = nil
            self.startRuntimeCallStream()
        }
        runtimeCallReconnectWorkItem = workItem
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: workItem)
    }

    private func stopRuntimeCallStream() {
        dispatchPrecondition(condition: .onQueue(.main))
        runtimeCallGeneration &+= 1
        runtimeCallReconnectWorkItem?.cancel()
        runtimeCallReconnectWorkItem = nil
        runtimeCallRetryAttempt = 0
        runtimeCallStream?.cancel()
        runtimeCallStream = nil
        runtimeCallTargetID = nil
        runtimeCallRevision = -1
    }

    private func reconcileRuntimeCall(_ runtimeCall: ModemDeckRuntimeCallState) {
        dispatchPrecondition(condition: .onQueue(.main))
        guard let uuid = callIDsByUUID.first(where: {
            $0.value == runtimeCall.id && !testCallUUIDs.contains($0.key)
        })?.key else {
            return
        }
        let phase = runtimeCall.phase.lowercased()
        if runtimeCall.ended {
            callProvider.reportCall(
                with: uuid,
                endedAt: Date(),
                reason: callEndedReason(from: runtimeCall, uuid: uuid)
            )
            cleanupCall(uuid)
            return
        }
        if runtimeCall.controlState?.lowercased() == "occupied" {
            callProvider.reportCall(
                with: uuid,
                endedAt: Date(),
                reason: .answeredElsewhere
            )
            cleanupCall(uuid)
            return
        }
        callAudioSessions[uuid]?.applyRuntimeState(phase: phase)
    }

    func observeCallState(_ observer: ModemDeckCallStateObserver) {
        DispatchQueue.main.async { [weak self, weak observer] in
            guard let self, let observer else { return }
            self.callStateObserver = observer
            observer.callStateDidChange(self.currentCallStatePayload())
        }
    }

    func currentCallState(
        completion: @escaping ([String: Any]) -> Void
    ) {
        DispatchQueue.main.async { [weak self] in
            completion(self?.currentCallStatePayload() ?? ["state": "idle"])
        }
    }

    func setCurrentCallMuted(
        _ muted: Bool,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        DispatchQueue.main.async { [weak self] in
            guard let self,
                  let uuid = self.answeredCallUUIDs.first else {
                completion(.failure(ModemDeckNativeError.noActiveCall))
                return
            }
            let action = CXSetMutedCallAction(call: uuid, muted: muted)
            self.requestedCallActions[action.uuid] = RequestedCallAction(
                callUUID: uuid,
                completion: completion
            )
            self.callController.request(CXTransaction(action: action)) { error in
                DispatchQueue.main.async {
                    if let error {
                        self.finishRequestedCallAction(action, result: .failure(error))
                    }
                }
            }
        }
    }

    func setPreferredCallRecording(callID: String, enabled: Bool?) {
        let update = { [weak self] in
            guard let self,
                  let uuid = self.callIDsByUUID.first(where: { $0.value == callID })?.key else {
                return
            }
            if let enabled {
                self.preferredRecordingByUUID[uuid] = enabled
            } else {
                self.preferredRecordingByUUID.removeValue(forKey: uuid)
            }
        }
        if Thread.isMainThread {
            update()
        } else {
            DispatchQueue.main.async(execute: update)
        }
    }

    func startOutgoingCall(
        callID: String,
        lineID: String,
        remoteNumber: String,
        displayName: String,
        completion: @escaping (Result<UUID, Error>) -> Void
    ) {
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            if let existing = self.callIDsByUUID.first(where: {
                $0.value == callID
            })?.key {
                completion(.success(existing))
                return
            }
            guard self.callIDsByUUID.isEmpty,
                  self.pendingOutgoingCalls.isEmpty else {
                completion(.failure(NSError(
                    domain: "ModemDeckCallKit",
                    code: 409,
                    userInfo: [
                        NSLocalizedDescriptionKey: "Another iOS call is already active."
                    ]
                )))
                return
            }
            ModemDeckCallAudioSession.prepareAudioSession()
            let uuid = UUID()
            self.pendingOutgoingCalls[uuid] = PendingOutgoingCall(
                callID: callID,
                lineID: lineID,
                remoteNumber: remoteNumber,
                displayName: displayName,
                completion: completion
            )
            let handle = CXHandle(type: .phoneNumber, value: remoteNumber)
            let action = CXStartCallAction(call: uuid, handle: handle)
            action.isVideo = false
            self.callController.request(CXTransaction(action: action)) { [weak self] error in
                guard let error else { return }
                DispatchQueue.main.async {
                    guard let pending = self?.pendingOutgoingCalls.removeValue(
                        forKey: uuid
                    ) else {
                        return
                    }
                    pending.completion(.failure(error))
                }
            }
        }
    }

    func answerCall(
        callID: String,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        requestCallKitAction(callID: callID, completion: completion) { uuid in
            CXAnswerCallAction(call: uuid)
        }
    }

    func requestMicrophoneAccess(
        completion: @escaping (Bool) -> Void
    ) {
        DispatchQueue.main.async {
            if #available(iOS 17.0, *) {
                switch AVAudioApplication.shared.recordPermission {
                case .granted:
                    completion(true)
                case .denied:
                    completion(false)
                case .undetermined:
                    AVAudioApplication.requestRecordPermission { granted in
                        DispatchQueue.main.async { completion(granted) }
                    }
                @unknown default:
                    completion(false)
                }
            } else {
                let session = AVAudioSession.sharedInstance()
                switch session.recordPermission {
                case .granted:
                    completion(true)
                case .denied:
                    completion(false)
                case .undetermined:
                    session.requestRecordPermission { granted in
                        DispatchQueue.main.async { completion(granted) }
                    }
                @unknown default:
                    completion(false)
                }
            }
        }
    }

    func endCall(
        callID: String,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        requestCallKitAction(callID: callID, completion: completion) { uuid in
            CXEndCallAction(call: uuid)
        }
    }

    func playDTMF(
        callID: String,
        digits: String,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        requestCallKitAction(callID: callID, completion: completion) { uuid in
            CXPlayDTMFCallAction(call: uuid, digits: digits, type: .singleTone)
        }
    }

    func didRegisterForRemoteNotifications(deviceToken: Data) {
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.apnsRegistrationRetryWorkItem?.cancel()
            self.apnsRegistrationRetryWorkItem = nil
            self.apnsRegistrationRetryAttempt = 0
            self.apnsToken = deviceToken.hexString
            self.syncTokens()
        }
    }

    func didFailToRegisterForRemoteNotifications(error: Error) {
        DispatchQueue.main.async { [weak self] in
            NSLog("ModemDeck APNs registration failed: %@", error.localizedDescription)
            self?.scheduleAPNSRegistrationRetry()
        }
    }

    func handleRemoteNotification(_ userInfo: [AnyHashable: Any]) {
        DispatchQueue.main.async {
            NotificationCenter.default.post(
                name: .modemDeckRemoteNotification,
                object: nil,
                userInfo: userInfo
            )
        }
    }

    func handleNotificationResponse(_ userInfo: [AnyHashable: Any]) {
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            if let route = self.notificationRoute(from: userInfo) {
                self.pendingNotificationRoute = route
                NotificationCenter.default.post(
                    name: .modemDeckNotificationResponse,
                    object: nil,
                    userInfo: userInfo
                )
            }
            NotificationCenter.default.post(
                name: .modemDeckRemoteNotification,
                object: nil,
                userInfo: userInfo
            )
        }
    }

    func consumePendingNotificationRoute() -> ModemDeckNotificationRoute? {
        dispatchPrecondition(condition: .onQueue(.main))
        defer { pendingNotificationRoute = nil }
        return pendingNotificationRoute
    }

    func pushRegistry(
        _ registry: PKPushRegistry,
        didUpdate pushCredentials: PKPushCredentials,
        for type: PKPushType
    ) {
        guard type == .voIP else { return }
        voipToken = pushCredentials.token.hexString
        syncTokens()
    }

    func pushRegistry(
        _ registry: PKPushRegistry,
        didInvalidatePushTokenFor type: PKPushType
    ) {
        guard type == .voIP else { return }
        voipToken = nil
        syncTokens(forceEmpty: true)
    }

    func pushRegistry(
        _ registry: PKPushRegistry,
        didReceiveIncomingPushWith payload: PKPushPayload,
        for type: PKPushType,
        completion: @escaping () -> Void
    ) {
        guard type == .voIP else {
            completion()
            return
        }
        handleVoIPPush(
            payload.dictionaryPayload,
            mustReport: true,
            completion: completion
        )
    }

    @available(iOS 26.4, *)
    func pushRegistry(
        _ registry: PKPushRegistry,
        didReceiveIncomingVoIPPushWith payload: PKPushPayload,
        metadata: PKVoIPPushMetadata,
        withCompletionHandler completion: @escaping () -> Void
    ) {
        handleVoIPPush(
            payload.dictionaryPayload,
            mustReport: metadata.mustReport,
            completion: completion
        )
    }

    private func syncTokens(forceEmpty: Bool = false) {
        dispatchPrecondition(condition: .onQueue(.main))
        let hasAPNSToken = !(apnsToken ?? "").isEmpty
        let hasVoIPToken = !(voipToken ?? "").isEmpty
        guard let credentialStore,
              forceEmpty || hasAPNSToken || hasVoIPToken else {
            return
        }

        let credential: ModemDeckCredential
        do {
            guard let loaded = try credentialStore.load() else { return }
            credential = loaded
        } catch {
            return
        }

        guard let bundleID = Bundle.main.bundleIdentifier,
              !bundleID.isEmpty else {
            return
        }
        let configuredEnvironment = Bundle.main.object(
            forInfoDictionaryKey: "ModemDeckAPNSEnvironment"
        ) as? String
        let environment = configuredEnvironment == "production"
            ? "production"
            : "development"
        let registration = ModemDeckPushRegistration(
            apnsToken: apnsToken ?? "",
            voipToken: voipToken ?? "",
            environment: environment,
            bundleID: bundleID
        )
        guard let body = try? JSONEncoder().encode(registration),
              let request = try? authorizedRequest(
                  credential: credential,
                  path: "/api/v1/mobile/push",
                  method: "PUT",
                  headers: [
                      "Accept": "application/json",
                      "Content-Type": "application/json"
                  ],
                  body: body,
                  timeout: 15
              ) else {
            return
        }

        tokenSyncRetryWorkItem?.cancel()
        tokenSyncRetryWorkItem = nil
        tokenSyncGeneration &+= 1
        let generation = tokenSyncGeneration
        tokenSyncTask?.cancel()
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 20
        tokenSyncTask = URLSession(configuration: configuration).dataTask(with: request) { [weak self] _, response, error in
            DispatchQueue.main.async {
                guard let self, generation == self.tokenSyncGeneration else { return }
                self.tokenSyncTask = nil
                if let error {
                    guard (error as NSError).code != NSURLErrorCancelled else { return }
                    NSLog("ModemDeck push token sync failed: %@", error.localizedDescription)
                    self.scheduleTokenSyncRetry(forceEmpty: forceEmpty)
                    return
                }
                let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                guard (200..<300).contains(status) else {
                    NSLog("ModemDeck push token sync returned HTTP %ld", status)
                    if status == 401 {
                        NotificationCenter.default.post(
                            name: .modemDeckAuthenticationFailed,
                            object: nil
                        )
                    } else if status == 408 || status == 429 || status >= 500 {
                        self.scheduleTokenSyncRetry(forceEmpty: forceEmpty)
                    }
                    return
                }
                self.tokenSyncRetryAttempt = 0
            }
        }
        tokenSyncTask?.resume()
    }

    private func scheduleAPNSRegistrationRetry() {
        dispatchPrecondition(condition: .onQueue(.main))
        apnsRegistrationRetryWorkItem?.cancel()
        let delays: [TimeInterval] = [5, 15, 30, 60]
        let delay = delays[min(apnsRegistrationRetryAttempt, delays.count - 1)]
        apnsRegistrationRetryAttempt += 1
        let workItem = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.apnsRegistrationRetryWorkItem = nil
            UIApplication.shared.registerForRemoteNotifications()
        }
        apnsRegistrationRetryWorkItem = workItem
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: workItem)
    }

    private func scheduleTokenSyncRetry(forceEmpty: Bool) {
        dispatchPrecondition(condition: .onQueue(.main))
        tokenSyncRetryWorkItem?.cancel()
        let delays: [TimeInterval] = [5, 15, 30, 60]
        let delay = delays[min(tokenSyncRetryAttempt, delays.count - 1)]
        tokenSyncRetryAttempt += 1
        let workItem = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.tokenSyncRetryWorkItem = nil
            self.syncTokens(forceEmpty: forceEmpty)
        }
        tokenSyncRetryWorkItem = workItem
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: workItem)
    }

    private func notificationRoute(
        from userInfo: [AnyHashable: Any]
    ) -> ModemDeckNotificationRoute? {
        let rawMessage = userInfo["modemdeck_message"]
        let message: [AnyHashable: Any]
        if let value = rawMessage as? [AnyHashable: Any] {
            message = value
        } else if let value = rawMessage as? [String: Any] {
            message = Dictionary(uniqueKeysWithValues: value.map { (AnyHashable($0.key), $0.value) })
        } else {
            return nil
        }
        let threadKey = (message["thread_key"] as? String)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !threadKey.isEmpty else { return nil }
        return ModemDeckNotificationRoute(
            messageID: (message["message_id"] as? String)?
                .trimmingCharacters(in: .whitespacesAndNewlines) ?? "",
            lineID: (message["line_id"] as? String)?
                .trimmingCharacters(in: .whitespacesAndNewlines) ?? "",
            threadKey: threadKey
        )
    }

    private func handleVoIPPush(
        _ payload: [AnyHashable: Any],
        mustReport: Bool,
        completion: @escaping () -> Void
    ) {
        if let callEnd = payload["modemdeck_call_end"] as? [AnyHashable: Any] {
            reportTerminalCall(
                from: callEnd,
                mustReport: mustReport,
                completion: completion
            )
            return
        }
        let call = (payload["modemdeck_call"] as? [AnyHashable: Any]) ??
            (payload["call"] as? [AnyHashable: Any]) ?? payload
        let event = stringValue(call["event"])?.lowercased() ?? "incoming"
        if event == "ended" || event == "terminal" || event == "cancelled" {
            reportTerminalCall(
                from: call,
                mustReport: mustReport,
                completion: completion
            )
            return
        }
        reportIncomingCall(from: call, completion: completion)
    }

    private func reportIncomingCall(
        from call: [AnyHashable: Any],
        completion: @escaping () -> Void
    ) {
        let callID = stringValue(call["call_id"]) ?? stringValue(call["id"])
        guard let callID, !callID.isEmpty,
              let uuidText = stringValue(call["uuid"]),
              let uuid = UUID(uuidString: uuidText) else {
            NSLog("ModemDeck ignored an incoming VoIP push without a stable call identity.")
            completion()
            return
        }

        let number = stringValue(call["remote_number"]) ??
            stringValue(call["number"]) ??
            "Unknown caller"
        let lineID = stringValue(call["line_id"]) ?? ""
        let displayName = stringValue(call["display_name"]) ?? number
        let isTestCall = booleanValue(call["test_call"])
        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(
            type: isTestCall ? .generic : .phoneNumber,
            value: number
        )
        update.localizedCallerName = displayName
        update.hasVideo = false
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        update.supportsDTMF = !isTestCall

        if endedCallUUIDs.contains(uuid) {
            completion()
            return
        }
        if callIDsByUUID[uuid] != nil {
            completion()
            return
        }

        callIDsByUUID[uuid] = callID
        presentedCalls[uuid] = PresentedCall(
            callID: callID,
            lineID: lineID,
            remoteNumber: number,
            displayName: displayName,
            direction: "incoming",
            testCall: isTestCall,
            createdAt: Date(),
            activeAt: nil
        )
        answeredCallUUIDs.remove(uuid)
        answerRequestedCallUUIDs.remove(uuid)
        mutedCallUUIDs.remove(uuid)
        if isTestCall {
            testCallUUIDs.insert(uuid)
        } else {
            testCallUUIDs.remove(uuid)
        }
        callProvider.reportNewIncomingCall(with: uuid, update: update) { [weak self] error in
            DispatchQueue.main.async {
                guard let self else {
                    completion()
                    return
                }
                guard self.callIDsByUUID[uuid] == callID,
                      !self.endedCallUUIDs.contains(uuid) else {
                    completion()
                    return
                }
                if let error {
                    NSLog("ModemDeck could not report the incoming call: %@", error.localizedDescription)
                    self.cleanupCall(uuid)
                } else if isTestCall {
                    self.scheduleTestCallTimeout(uuid)
                    self.publishCallState()
                } else if let credential = self.loadCredential() {
                    _ = self.installAudioSession(
                        uuid: uuid,
                        callID: callID,
                        credential: credential
                    )
                    self.startRuntimeCallStream(forceRestart: true)
                    self.publishCallState()
                } else {
                    self.callProvider.reportCall(
                        with: uuid,
                        endedAt: Date(),
                        reason: .failed
                    )
                    self.cleanupCall(uuid)
                }
                completion()
            }
        }
    }

    private func reportTerminalCall(
        from call: [AnyHashable: Any],
        mustReport: Bool,
        completion: @escaping () -> Void
    ) {
        guard let uuidText = stringValue(call["uuid"]),
              let uuid = UUID(uuidString: uuidText) else {
            NSLog("ModemDeck ignored a terminal VoIP push without a stable CallKit UUID.")
            completion()
            return
        }
        let callExists = callIDsByUUID[uuid] != nil ||
            callController.callObserver.calls.contains(where: { $0.uuid == uuid })
        if endedCallUUIDs.contains(uuid) && !callExists {
            completion()
            return
        }

        let reason = callEndedReason(from: call, uuid: uuid)
        if callExists {
            callProvider.reportCall(with: uuid, endedAt: Date(), reason: reason)
            cleanupCall(uuid)
            completion()
            return
        }

        rememberEndedCall(uuid)
        guard mustReport else {
            completion()
            return
        }

        let update = callUpdate(from: call)
        callProvider.reportNewIncomingCall(with: uuid, update: update) { [weak self] error in
            DispatchQueue.main.async {
                guard let self else {
                    completion()
                    return
                }
                if let error {
                    NSLog(
                        "ModemDeck could not reconcile an unknown terminal call: %@",
                        error.localizedDescription
                    )
                } else {
                    self.callProvider.reportCall(
                        with: uuid,
                        endedAt: Date(),
                        reason: reason
                    )
                }
                completion()
            }
        }
    }

    private func callUpdate(from call: [AnyHashable: Any]) -> CXCallUpdate {
        let number = stringValue(call["remote_number"]) ??
            stringValue(call["number"]) ??
            "Unknown caller"
        let displayName = stringValue(call["display_name"]) ?? number
        let isTestCall = booleanValue(call["test_call"])
        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(
            type: isTestCall ? .generic : .phoneNumber,
            value: number
        )
        update.localizedCallerName = displayName
        update.hasVideo = false
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        update.supportsDTMF = !isTestCall
        return update
    }

    private func callEndedReason(
        from call: [AnyHashable: Any],
        uuid: UUID
    ) -> CXCallEndedReason {
        let observedCall = callController.callObserver.calls.first(where: { $0.uuid == uuid })
        if answeredCallUUIDs.contains(uuid) || answerRequestedCallUUIDs.contains(uuid) ||
            observedCall?.hasConnected == true {
            return .remoteEnded
        }
        switch stringValue(call["reason"])?.lowercased() {
        case "answered_elsewhere":
            return .answeredElsewhere
        case "declined_elsewhere":
            return .declinedElsewhere
        case "unanswered", "timeout", "timed_out", "no_answer":
            return .unanswered
        case "failed":
            return .failed
        default:
            break
        }
        let endReason = stringValue(call["end_reason"])?.lowercased()
        let failureCode = stringValue(call["failure_code"])?.lowercased()
        if endReason == "rejected" || failureCode == "rejected" {
            return .declinedElsewhere
        }
        if booleanValue(call["was_answered"]) {
            return .answeredElsewhere
        }
        if stringValue(call["phase"])?.lowercased() == "failed" || failureCode != nil {
            return .failed
        }
        return .remoteEnded
    }

    private func callEndedReason(
        from call: ModemDeckRuntimeCallState,
        uuid: UUID
    ) -> CXCallEndedReason {
        let observedCall = callController.callObserver.calls.first(where: { $0.uuid == uuid })
        if answeredCallUUIDs.contains(uuid) || answerRequestedCallUUIDs.contains(uuid) ||
            observedCall?.hasConnected == true {
            return .remoteEnded
        }
        let endReason = call.endReason?.lowercased() ?? ""
        let failureCode = call.failureCode?.lowercased() ?? ""
        if endReason.contains("answered_elsewhere") ||
            failureCode.contains("answered_elsewhere") {
            return .answeredElsewhere
        }
        if endReason.contains("reject") || endReason.contains("declin") ||
            failureCode.contains("reject") || failureCode.contains("declin") {
            return .declinedElsewhere
        }
        if call.wasAnswered {
            return .answeredElsewhere
        }
        if call.phase.lowercased() == "failed" || !failureCode.isEmpty {
            return .failed
        }
        if presentedCalls[uuid]?.direction == "incoming" {
            return .unanswered
        }
        return .remoteEnded
    }

    private func sendCallAction(
        verb: String,
        callUUID: UUID,
        digits: String = "",
        recordingEnabled: Bool? = nil,
        operationID: String? = nil,
        completion: @escaping (Result<Void, Error>) -> Void
    ) {
        guard let callID = callIDsByUUID[callUUID] else {
            completion(.failure(ModemDeckNativeError.noActiveCall))
            return
        }
        guard let credential = loadCredential() else {
            completion(.failure(ModemDeckNativeError.notConfigured))
            return
        }

        let encodedCallID = callID.addingPercentEncoding(
            withAllowedCharacters: CharacterSet.alphanumerics.union(
                CharacterSet(charactersIn: "-._~")
            )
        ) ?? ""
        let requestID = operationID ?? [
            "ios-callkit",
            verb,
            callUUID.uuidString.lowercased()
        ].joined(separator: "-")
        var payload: [String: Any] = ["request_id": requestID]
        if !digits.isEmpty {
            payload["digits"] = digits
        }
        if verb == "answer", let recordingEnabled {
            payload["recording_enabled"] = recordingEnabled
        }
        guard !encodedCallID.isEmpty,
              let body = try? JSONSerialization.data(withJSONObject: payload),
              let request = try? authorizedRequest(
                  credential: credential,
                  path: "/api/v1/calls/\(encodedCallID)/\(verb)",
                  method: "POST",
                  headers: [
                      "Accept": "application/json",
                      "Content-Type": "application/json",
                      "Idempotency-Key": requestID
                  ],
                  body: body,
                  timeout: 15
              ) else {
            completion(.failure(ModemDeckNativeError.invalidResponse))
            return
        }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 20
        let session = URLSession(configuration: configuration)
        session.dataTask(with: request) { _, response, error in
            session.finishTasksAndInvalidate()
            DispatchQueue.main.async {
                if let error {
                    completion(.failure(error))
                    return
                }
                guard let status = (response as? HTTPURLResponse)?.statusCode,
                      (200..<300).contains(status) else {
                    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                    completion(.failure(NSError(
                        domain: "ModemDeckCallKit",
                        code: status,
                        userInfo: [
                            NSLocalizedDescriptionKey: "ModemDeck call action returned HTTP \(status)."
                        ]
                    )))
                    return
                }
                completion(.success(()))
            }
        }.resume()
    }

    func providerDidReset(_ provider: CXProvider) {
        failRequestedCallActions(ModemDeckCallAudioError.callEnded)
        failPendingOutgoingCalls(ModemDeckCallAudioError.callEnded)
        for uuid in Array(callIDsByUUID.keys) {
            cleanupCall(uuid)
        }
        activeAudioSession = nil
        testCallTone.stop()
    }

    func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) {
        let uuid = action.callUUID
        trackProviderCallAction(action)
        guard callIDsByUUID[uuid] != nil else {
            finishProviderCallAction(
                action,
                result: .failure(ModemDeckNativeError.noActiveCall)
            )
            return
        }
        if testCallUUIDs.contains(uuid) {
            answeredCallUUIDs.insert(uuid)
            finishProviderCallAction(action, result: .success(()))
            markCallActive(uuid)
            if let activeAudioSession {
                testCallTone.start(audioSession: activeAudioSession)
            }
            return
        }
        requestMicrophoneAccess { [weak self] granted in
            guard let self else {
                action.fail()
                return
            }
            guard self.providerCallActions[action.uuid] != nil else { return }
            guard granted else {
                self.finishProviderCallAction(
                    action,
                    result: .failure(ModemDeckNativeError.microphoneDenied)
                )
                return
            }
            ModemDeckCallAudioSession.prepareAudioSession()
            self.performAnswer(action, uuid: uuid)
        }
    }

    private func performAnswer(_ action: CXAnswerCallAction, uuid: UUID) {
        guard providerCallActions[action.uuid] != nil else { return }
        answerRequestedCallUUIDs.insert(uuid)
        publishCallState()
        sendCallAction(
            verb: "answer",
            callUUID: uuid,
            recordingEnabled: preferredRecordingByUUID[uuid]
        ) { [weak self] result in
            guard let self else {
                action.fail()
                return
            }
            guard self.providerCallActions[action.uuid] != nil else { return }
            switch result {
            case .failure(let error):
                NSLog("ModemDeck CallKit answer failed: %@", error.localizedDescription)
                self.answerRequestedCallUUIDs.remove(uuid)
                self.finishProviderCallAction(action, result: .failure(error))
                self.callProvider.reportCall(with: uuid, endedAt: Date(), reason: .failed)
                self.cleanupCall(uuid)
            case .success:
                guard self.callIDsByUUID[uuid] != nil,
                      let audioSession = self.callAudioSessions[uuid] else {
                    self.finishProviderCallAction(
                        action,
                        result: .failure(ModemDeckNativeError.noActiveCall)
                    )
                    self.callProvider.reportCall(with: uuid, endedAt: Date(), reason: .failed)
                    self.cleanupCall(uuid)
                    return
                }
                self.answeredCallUUIDs.insert(uuid)
                audioSession.connect { [weak self, weak audioSession] mediaResult in
                    guard let self,
                          self.callAudioSessions[uuid] === audioSession,
                          self.providerCallActions[action.uuid] != nil else { return }
                    self.answerRequestedCallUUIDs.remove(uuid)
                    switch mediaResult {
                    case .success:
                        self.finishProviderCallAction(action, result: .success(()))
                        self.markCallActive(uuid)
                    case .failure(let error):
                        NSLog("ModemDeck CallKit audio connection failed: %@", error.localizedDescription)
                        self.finishProviderCallAction(action, result: .failure(error))
                        self.sendCallAction(verb: "hangup", callUUID: uuid) { _ in }
                        self.callProvider.reportCall(
                            with: uuid,
                            endedAt: Date(),
                            reason: .failed
                        )
                        self.cleanupCall(uuid)
                    }
                }
            }
        }
    }

    func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        let uuid = action.callUUID
        trackProviderCallAction(action)
        guard callIDsByUUID[uuid] != nil else {
            finishProviderCallAction(action, result: .success(()))
            return
        }
        if testCallUUIDs.contains(uuid) {
            finishProviderCallAction(action, result: .success(()))
            cleanupCall(uuid)
            return
        }
        let verb = outgoingCallUUIDs.contains(uuid) ||
            answeredCallUUIDs.contains(uuid) ||
            answerRequestedCallUUIDs.contains(uuid)
            ? "hangup"
            : "reject"
        sendCallAction(verb: verb, callUUID: uuid) { [weak self] result in
            guard let self,
                  self.providerCallActions[action.uuid] != nil else { return }
            switch result {
            case .success:
                self.finishProviderCallAction(action, result: .success(()))
                self.cleanupCall(uuid)
            case .failure(let error):
                NSLog("ModemDeck CallKit end failed: %@", error.localizedDescription)
                self.finishProviderCallAction(action, result: .failure(error))
            }
        }
    }

    func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        let uuid = action.callUUID
        guard let pending = pendingOutgoingCalls.removeValue(forKey: uuid) else {
            action.fail()
            return
        }
        guard let credential = loadCredential() else {
            action.fail()
            pending.completion(.failure(ModemDeckNativeError.notConfigured))
            return
        }
        callIDsByUUID[uuid] = pending.callID
        presentedCalls[uuid] = PresentedCall(
            callID: pending.callID,
            lineID: pending.lineID,
            remoteNumber: pending.remoteNumber,
            displayName: pending.displayName,
            direction: "outgoing",
            testCall: false,
            createdAt: Date(),
            activeAt: nil
        )
        outgoingCallUUIDs.insert(uuid)
        answeredCallUUIDs.remove(uuid)
        answerRequestedCallUUIDs.remove(uuid)
        testCallUUIDs.remove(uuid)
        mutedCallUUIDs.remove(uuid)

        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(type: .phoneNumber, value: pending.remoteNumber)
        update.localizedCallerName = pending.displayName
        update.hasVideo = false
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        update.supportsDTMF = true
        callProvider.reportCall(with: uuid, updated: update)

        let audioSession = installAudioSession(
            uuid: uuid,
            callID: pending.callID,
            credential: credential
        )
        audioSession.onBecameActive = { [weak self, weak audioSession] in
            guard let self,
                  let audioSession,
                  self.callAudioSessions[uuid] === audioSession else {
                return
            }
            audioSession.onBecameActive = nil
            audioSession.connect { [weak self, weak audioSession] result in
                guard let self,
                      self.callAudioSessions[uuid] === audioSession else {
                    return
                }
                switch result {
                case .success:
                    self.answeredCallUUIDs.insert(uuid)
                    self.callProvider.reportOutgoingCall(with: uuid, connectedAt: Date())
                    self.markCallActive(uuid)
                case .failure(let error):
                    NSLog(
                        "ModemDeck outgoing CallKit audio failed: %@",
                        error.localizedDescription
                    )
                    self.sendCallAction(verb: "hangup", callUUID: uuid) { _ in }
                    self.callProvider.reportCall(
                        with: uuid,
                        endedAt: Date(),
                        reason: .failed
                    )
                    self.cleanupCall(uuid)
                }
            }
        }
        startRuntimeCallStream(forceRestart: true)
        callProvider.reportOutgoingCall(with: uuid, startedConnectingAt: Date())
        action.fulfill()
        publishCallState()
        pending.completion(.success(uuid))
    }

    func provider(_ provider: CXProvider, perform action: CXSetHeldCallAction) {
        action.fail()
    }

    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        trackProviderCallAction(action)
        guard callIDsByUUID[action.callUUID] != nil else {
            finishProviderCallAction(
                action,
                result: .failure(ModemDeckNativeError.noActiveCall)
            )
            return
        }
        if action.isMuted {
            mutedCallUUIDs.insert(action.callUUID)
        } else {
            mutedCallUUIDs.remove(action.callUUID)
        }
        if testCallUUIDs.contains(action.callUUID) {
            testCallTone.setMuted(action.isMuted)
        } else {
            callAudioSessions[action.callUUID]?.setMuted(action.isMuted)
        }
        finishProviderCallAction(action, result: .success(()))
        publishCallState()
    }

    func provider(_ provider: CXProvider, perform action: CXSetGroupCallAction) {
        action.fail()
    }

    func provider(_ provider: CXProvider, perform action: CXPlayDTMFCallAction) {
        let uuid = action.callUUID
        trackProviderCallAction(action)
        guard answeredCallUUIDs.contains(uuid),
              !testCallUUIDs.contains(uuid) else {
            finishProviderCallAction(
                action,
                result: .failure(ModemDeckNativeError.noActiveCall)
            )
            return
        }
        sendCallAction(
            verb: "dtmf",
            callUUID: uuid,
            digits: action.digits,
            operationID: "ios-callkit-dtmf-\(action.uuid.uuidString.lowercased())"
        ) { [weak self] result in
            guard let self,
                  self.providerCallActions[action.uuid] != nil else { return }
            switch result {
            case .success:
                self.finishProviderCallAction(action, result: .success(()))
            case .failure(let error):
                NSLog("ModemDeck CallKit DTMF failed: %@", error.localizedDescription)
                self.finishProviderCallAction(action, result: .failure(error))
            }
        }
    }

    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        activeAudioSession = audioSession
        for session in callAudioSessions.values {
            session.didActivate(audioSession)
        }
        if answeredCallUUIDs.contains(where: { testCallUUIDs.contains($0) }) {
            testCallTone.start(audioSession: audioSession)
        }
    }

    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        for session in callAudioSessions.values {
            session.didDeactivate(audioSession)
        }
        activeAudioSession = nil
        testCallTone.stop()
    }

    func provider(_ provider: CXProvider, timedOutPerforming action: CXAction) {
        guard let callAction = action as? CXCallAction else { return }
        let uuid = callAction.callUUID
        let timeoutError = NSError(
            domain: "ModemDeckCallKit",
            code: NSURLErrorTimedOut,
            userInfo: [NSLocalizedDescriptionKey: "CallKit action timed out."]
        )
        providerCallActions.removeValue(forKey: action.uuid)
        finishRequestedCallAction(action, result: .failure(timeoutError))
        if let pending = pendingOutgoingCalls.removeValue(forKey: uuid) {
            pending.completion(.failure(timeoutError))
        }
        if callIDsByUUID[uuid] != nil {
            sendCallAction(verb: "hangup", callUUID: uuid) { _ in }
            cleanupCall(uuid)
        }
    }

    private func currentCallStatePayload() -> [String: Any] {
        guard let uuid = callIDsByUUID.keys.first,
              let call = presentedCalls[uuid] else {
            return ["state": "idle"]
        }
        let state: String
        if answerRequestedCallUUIDs.contains(uuid) {
            state = "connecting"
        } else if answeredCallUUIDs.contains(uuid) {
            state = "active"
        } else if outgoingCallUUIDs.contains(uuid) {
            state = "connecting"
        } else {
            state = "ringing"
        }
        return callStatePayload(uuid: uuid, call: call, state: state)
    }

    private func callStatePayload(
        uuid: UUID,
        call: PresentedCall,
        state: String
    ) -> [String: Any] {
        var payload: [String: Any] = [
            "callID": call.callID,
            "lineID": call.lineID,
            "remoteNumber": call.remoteNumber,
            "displayName": call.displayName,
            "direction": call.direction,
            "state": state,
            "testCall": call.testCall,
            "muted": mutedCallUUIDs.contains(uuid),
            "createdAt": Self.timestampFormatter.string(from: call.createdAt)
        ]
        if let activeAt = call.activeAt {
            payload["activeAt"] = Self.timestampFormatter.string(from: activeAt)
        }
        return payload
    }

    private func markCallActive(_ uuid: UUID) {
        guard var call = presentedCalls[uuid] else { return }
        if call.activeAt == nil {
            call.activeAt = Date()
            presentedCalls[uuid] = call
        }
        publishCallState()
    }

    private func publishCallState() {
        dispatchPrecondition(condition: .onQueue(.main))
        callStateObserver?.callStateDidChange(currentCallStatePayload())
    }

    private func loadCredential() -> ModemDeckCredential? {
        do {
            guard let credentialStore else { return nil }
            return try credentialStore.load()
        } catch {
            NSLog("ModemDeck could not load the iOS credential: %@", error.localizedDescription)
            return nil
        }
    }

    private func requestCallKitAction(
        callID: String,
        completion: @escaping (Result<Void, Error>) -> Void,
        action: @escaping (UUID) -> CXAction
    ) {
        DispatchQueue.main.async { [weak self] in
            guard let self,
                  let uuid = self.callIDsByUUID.first(where: {
                      $0.value == callID
                  })?.key else {
                completion(.failure(ModemDeckNativeError.noActiveCall))
                return
            }
            let requestedAction = action(uuid)
            self.requestedCallActions[requestedAction.uuid] = RequestedCallAction(
                callUUID: uuid,
                completion: completion
            )
            self.callController.request(
                CXTransaction(action: requestedAction)
            ) { error in
                DispatchQueue.main.async {
                    if let error {
                        self.finishRequestedCallAction(
                            requestedAction,
                            result: .failure(error)
                        )
                    }
                }
            }
        }
    }

    private func finishRequestedCallAction(
        _ action: CXAction,
        result: Result<Void, Error>
    ) {
        requestedCallActions.removeValue(forKey: action.uuid)?.completion(result)
    }

    private func trackProviderCallAction(_ action: CXCallAction) {
        providerCallActions[action.uuid] = action
    }

    @discardableResult
    private func finishProviderCallAction(
        _ action: CXCallAction,
        result: Result<Void, Error>
    ) -> Bool {
        guard providerCallActions.removeValue(forKey: action.uuid) != nil else {
            return false
        }
        switch result {
        case .success:
            action.fulfill()
        case .failure:
            action.fail()
        }
        finishRequestedCallAction(action, result: result)
        return true
    }

    private func settleProviderCallActions(for callUUID: UUID) {
        let actions = providerCallActions.values.filter { $0.callUUID == callUUID }
        for action in actions {
            if action is CXEndCallAction {
                finishProviderCallAction(action, result: .success(()))
            } else {
                finishProviderCallAction(
                    action,
                    result: .failure(ModemDeckCallAudioError.callEnded)
                )
            }
        }
    }

    private func failRequestedCallActions(
        _ error: Error,
        for callUUID: UUID? = nil
    ) {
        let actionIDs = requestedCallActions.compactMap { actionID, request in
            callUUID == nil || request.callUUID == callUUID ? actionID : nil
        }
        for actionID in actionIDs {
            requestedCallActions.removeValue(forKey: actionID)?.completion(.failure(error))
        }
    }

    private func installAudioSession(
        uuid: UUID,
        callID: String,
        credential: ModemDeckCredential
    ) -> ModemDeckCallAudioSession {
        if let existing = callAudioSessions[uuid] {
            return existing
        }
        let audioSession = ModemDeckCallAudioSession(
            callID: callID,
            credential: credential
        )
        audioSession.onRemoteEnded = { [weak self, weak audioSession] in
            guard let self,
                  self.callAudioSessions[uuid] === audioSession else {
                return
            }
            self.callProvider.reportCall(
                with: uuid,
                endedAt: Date(),
                reason: .remoteEnded
            )
            self.cleanupCall(uuid)
        }
        callAudioSessions[uuid] = audioSession
        if let activeAudioSession {
            audioSession.didActivate(activeAudioSession)
        }
        return audioSession
    }

    private func scheduleTestCallTimeout(_ uuid: UUID) {
        let timeout = DispatchWorkItem { [weak self] in
            guard let self, self.testCallUUIDs.contains(uuid) else { return }
            let reason: CXCallEndedReason = self.answeredCallUUIDs.contains(uuid)
                ? .remoteEnded
                : .unanswered
            self.callProvider.reportCall(with: uuid, endedAt: Date(), reason: reason)
            self.cleanupCall(uuid)
        }
        testCallTimeouts[uuid]?.cancel()
        testCallTimeouts[uuid] = timeout
        DispatchQueue.main.asyncAfter(deadline: .now() + 30, execute: timeout)
    }

    private func rememberEndedCall(_ uuid: UUID) {
        guard endedCallUUIDs.insert(uuid).inserted else { return }
        endedCallOrder.append(uuid)
        while endedCallOrder.count > Self.maximumRememberedEndedCalls {
            let oldest = endedCallOrder.removeFirst()
            endedCallUUIDs.remove(oldest)
        }
    }

    private func cleanupCall(_ uuid: UUID) {
        settleProviderCallActions(for: uuid)
        failRequestedCallActions(ModemDeckCallAudioError.callEnded, for: uuid)
        rememberEndedCall(uuid)
        callAudioSessions.removeValue(forKey: uuid)?.stop()
        testCallTimeouts.removeValue(forKey: uuid)?.cancel()
        callIDsByUUID.removeValue(forKey: uuid)
        presentedCalls.removeValue(forKey: uuid)
        answeredCallUUIDs.remove(uuid)
        answerRequestedCallUUIDs.remove(uuid)
        outgoingCallUUIDs.remove(uuid)
        mutedCallUUIDs.remove(uuid)
        preferredRecordingByUUID.removeValue(forKey: uuid)
        let wasTestCall = testCallUUIDs.remove(uuid) != nil
        if wasTestCall && !answeredCallUUIDs.contains(where: { testCallUUIDs.contains($0) }) {
            testCallTone.stop()
        }
        if !callIDsByUUID.contains(where: { !testCallUUIDs.contains($0.key) }) {
            stopRuntimeCallStream()
        }
        publishCallState()
    }

    private func endAllCalls(reason: CXCallEndedReason) {
        failPendingOutgoingCalls(ModemDeckCallAudioError.callEnded)
        for uuid in Array(callIDsByUUID.keys) {
            callProvider.reportCall(with: uuid, endedAt: Date(), reason: reason)
            cleanupCall(uuid)
        }
    }

    private func failPendingOutgoingCalls(_ error: Error) {
        let pending = Array(pendingOutgoingCalls.values)
        pendingOutgoingCalls.removeAll()
        for call in pending {
            call.completion(.failure(error))
        }
    }

    private func stringValue(_ value: Any?) -> String? {
        guard let string = value as? String else { return nil }
        let trimmed = string.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    private func booleanValue(_ value: Any?) -> Bool {
        if let bool = value as? Bool {
            return bool
        }
        if let number = value as? NSNumber {
            return number.boolValue
        }
        return false
    }
}

private extension Data {
    var hexString: String {
        map { String(format: "%02x", $0) }.joined()
    }
}

func validatedCredential(serverURL rawServerURL: String, token rawToken: String) throws -> ModemDeckCredential {
    let token = rawToken.trimmingCharacters(in: .whitespacesAndNewlines)
    let prefix = "md_ios_"
    let encoded = String(token.dropFirst(prefix.count))
    let tokenCharacters = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-")
    guard token.hasPrefix(prefix), encoded.count == 43,
          encoded.unicodeScalars.allSatisfy({ tokenCharacters.contains($0) }) else {
        throw ModemDeckNativeError.invalidToken
    }

    let candidate = rawServerURL.trimmingCharacters(in: .whitespacesAndNewlines)
    guard var components = URLComponents(string: candidate),
          components.scheme?.lowercased() == "https",
          components.host?.isEmpty == false,
          components.user == nil,
          components.password == nil,
          components.query == nil,
          components.fragment == nil,
          components.path.isEmpty || components.path == "/" else {
        throw ModemDeckNativeError.invalidServerURL
    }
    components.scheme = "https"
    components.path = ""
    guard let normalizedURL = components.url?.absoluteString else {
        throw ModemDeckNativeError.invalidServerURL
    }
    return ModemDeckCredential(
        serverURL: normalizedURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")),
        token: token
    )
}

#if DEBUG
func installModemDeckUATCredentialFromEnvironment(in store: ModemDeckCredentialStore) {
    let environment = ProcessInfo.processInfo.environment
    guard environment["MODEMDECK_UAT_MODE"] == "1",
          let serverURL = environment["MODEMDECK_UAT_SERVER_URL"],
          let token = environment["MODEMDECK_UAT_TOKEN"] else {
        return
    }
    do {
        let credential = try validatedCredential(serverURL: serverURL, token: token)
        store.installUATCredential(credential)
    } catch {
        assertionFailure("Unable to install the simulator UAT credential: \(error.localizedDescription)")
    }
}
#endif

private func endpointURL(for path: String, credential: ModemDeckCredential) throws -> URL {
    guard path.hasPrefix("/api/v1/"),
          !path.contains("\\"),
          !path.contains("\u{0000}"),
          let base = URL(string: credential.serverURL),
          let endpoint = URL(string: path, relativeTo: base)?.absoluteURL,
          endpoint.scheme?.lowercased() == "https",
          endpoint.host?.lowercased() == base.host?.lowercased(),
          endpoint.port == base.port,
          endpoint.path.hasPrefix("/api/v1/") else {
        throw ModemDeckNativeError.invalidPath
    }
    return endpoint
}

func authorizedRequest(
    credential: ModemDeckCredential,
    path: String,
    method: String,
    headers: [String: String] = [:],
    body: Data? = nil,
    timeout: TimeInterval = 60
) throws -> URLRequest {
    let normalizedMethod = method.uppercased()
    guard ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"].contains(normalizedMethod) else {
        throw ModemDeckNativeError.invalidMethod
    }
    var request = URLRequest(url: try endpointURL(for: path, credential: credential))
    request.httpMethod = normalizedMethod
    request.timeoutInterval = max(1, min(timeout, 180))
    request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
    request.httpBody = body
    for (name, value) in headers {
        let normalizedName = name.lowercased()
        if ["authorization", "cookie", "host", "content-length", "connection"].contains(normalizedName) {
            continue
        }
        request.setValue(value, forHTTPHeaderField: name)
    }
    if path.split(separator: "?", maxSplits: 1).first == "/api/v1/mobile/session" {
        for (name, value) in deviceInformationHeaders() {
            request.setValue(value, forHTTPHeaderField: name)
        }
    }
    request.setValue("Bearer \(credential.token)", forHTTPHeaderField: "Authorization")
    request.setValue("no-store", forHTTPHeaderField: "Cache-Control")
    return request
}

private func deviceInformationHeaders() -> [String: String] {
    let device = UIDevice.current
    let bundle = Bundle.main
    return [
        "X-ModemDeck-Device-Name": encodedDeviceHeader(device.name, maximumCharacters: 128),
        "X-ModemDeck-Device-Model": encodedDeviceHeader(device.model, maximumCharacters: 64),
        "X-ModemDeck-Device-Model-Identifier": encodedDeviceHeader(
            deviceModelIdentifier(),
            maximumCharacters: 64
        ),
        "X-ModemDeck-OS-Name": encodedDeviceHeader(device.systemName, maximumCharacters: 64),
        "X-ModemDeck-OS-Version": encodedDeviceHeader(device.systemVersion, maximumCharacters: 64),
        "X-ModemDeck-App-Version": encodedDeviceHeader(
            bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "",
            maximumCharacters: 64
        ),
        "X-ModemDeck-App-Build": encodedDeviceHeader(
            bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "",
            maximumCharacters: 64
        )
    ].filter { !$0.value.isEmpty }
}

private func encodedDeviceHeader(_ rawValue: String, maximumCharacters: Int) -> String {
    let sanitized = rawValue
        .unicodeScalars
        .filter { !CharacterSet.controlCharacters.contains($0) }
        .prefix(maximumCharacters)
        .map(String.init)
        .joined()
        .trimmingCharacters(in: .whitespacesAndNewlines)
    guard !sanitized.isEmpty, let data = sanitized.data(using: .utf8) else {
        return ""
    }
    return "b64:\(data.base64EncodedString())"
}

private func deviceModelIdentifier() -> String {
    var systemInformation = utsname()
    guard uname(&systemInformation) == 0 else { return "" }
    let capacity = MemoryLayout.size(ofValue: systemInformation.machine)
    return withUnsafePointer(to: &systemInformation.machine) { pointer in
        pointer.withMemoryRebound(to: CChar.self, capacity: capacity) {
            String(cString: $0)
        }
    }
}

private func notificationStatusPayload(_ settings: UNNotificationSettings) -> [String: Any] {
    let authorization: String
    let enabled: Bool
    switch settings.authorizationStatus {
    case .notDetermined:
        authorization = "notDetermined"
        enabled = false
    case .denied:
        authorization = "denied"
        enabled = false
    case .authorized:
        authorization = "authorized"
        enabled = true
    case .provisional:
        authorization = "provisional"
        enabled = true
    case .ephemeral:
        authorization = "ephemeral"
        enabled = true
    @unknown default:
        authorization = "unknown"
        enabled = false
    }
    return [
        "authorization": authorization,
        "enabled": enabled,
        "canRequest": settings.authorizationStatus == .notDetermined
    ]
}

private func responseHeaders(_ response: HTTPURLResponse) -> [String: String] {
    var headers: [String: String] = [:]
    for (key, value) in response.allHeaderFields {
        headers[String(describing: key).lowercased()] = String(describing: value)
    }
    return headers
}

private func serverErrorMessage(data: Data, fallback: String) -> String {
    guard let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
          let message = value["message"] as? String,
          !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
        return fallback
    }
    return message
}

private enum ModemDeckQRScannerSetupError: LocalizedError {
    case cameraUnavailable
    case inputUnavailable
    case outputUnavailable

    var errorDescription: String? {
        switch self {
        case .cameraUnavailable:
            return "No camera is available on this device."
        case .inputUnavailable:
            return "The camera could not be opened."
        case .outputUnavailable:
            return "The QR code scanner could not be started."
        }
    }
}

enum ModemDeckQRScanResult {
    case value(String)
    case cancelled
    case failed(Error)
}

final class ModemDeckQRScannerViewController: UIViewController,
    AVCaptureMetadataOutputObjectsDelegate {
    var completion: ((ModemDeckQRScanResult) -> Void)?

    private let captureSession = AVCaptureSession()
    private let captureQueue = DispatchQueue(label: "modemdeck.qr-scanner")
    private lazy var previewLayer = AVCaptureVideoPreviewLayer(session: captureSession)
    private let dimmingLayer = CAShapeLayer()
    private let scanFrameView = UIView()
    private var completed = false

    override var preferredStatusBarStyle: UIStatusBarStyle {
        .lightContent
    }

    override var supportedInterfaceOrientations: UIInterfaceOrientationMask {
        .allButUpsideDown
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black

        previewLayer.videoGravity = .resizeAspectFill
        view.layer.addSublayer(previewLayer)

        dimmingLayer.fillColor = UIColor.black.withAlphaComponent(0.42).cgColor
        dimmingLayer.fillRule = .evenOdd
        view.layer.addSublayer(dimmingLayer)

        scanFrameView.translatesAutoresizingMaskIntoConstraints = false
        scanFrameView.isUserInteractionEnabled = false
        scanFrameView.layer.borderColor = UIColor.white.cgColor
        scanFrameView.layer.borderWidth = 2.5
        scanFrameView.layer.cornerRadius = 22
        view.addSubview(scanFrameView)

        let closeButton = UIButton(type: .system)
        closeButton.translatesAutoresizingMaskIntoConstraints = false
        closeButton.tintColor = .white
        closeButton.backgroundColor = UIColor.black.withAlphaComponent(0.5)
        closeButton.layer.cornerRadius = 22
        closeButton.setImage(UIImage(systemName: "xmark"), for: .normal)
        closeButton.accessibilityLabel = "关闭扫码"
        closeButton.addTarget(self, action: #selector(cancelScan), for: .touchUpInside)
        view.addSubview(closeButton)

        let titleLabel = UILabel()
        titleLabel.translatesAutoresizingMaskIntoConstraints = false
        titleLabel.text = "扫描配对二维码"
        titleLabel.textColor = .white
        titleLabel.textAlignment = .center
        titleLabel.font = .systemFont(ofSize: 24, weight: .bold)
        view.addSubview(titleLabel)

        let instructionLabel = UILabel()
        instructionLabel.translatesAutoresizingMaskIntoConstraints = false
        instructionLabel.text = "将 Web 设置中的二维码放入框内"
        instructionLabel.textColor = UIColor.white.withAlphaComponent(0.82)
        instructionLabel.textAlignment = .center
        instructionLabel.font = .systemFont(ofSize: 15, weight: .regular)
        instructionLabel.numberOfLines = 0
        view.addSubview(instructionLabel)

        let privacyLabel = UILabel()
        privacyLabel.translatesAutoresizingMaskIntoConstraints = false
        privacyLabel.text = "二维码仅在此设备上读取"
        privacyLabel.textColor = UIColor.white.withAlphaComponent(0.72)
        privacyLabel.textAlignment = .center
        privacyLabel.font = .systemFont(ofSize: 13, weight: .medium)
        privacyLabel.numberOfLines = 0
        view.addSubview(privacyLabel)

        let proportionalWidth = scanFrameView.widthAnchor.constraint(
            equalTo: view.widthAnchor,
            multiplier: 0.72
        )
        proportionalWidth.priority = .defaultHigh
        NSLayoutConstraint.activate([
            closeButton.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 12),
            closeButton.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -18),
            closeButton.widthAnchor.constraint(equalToConstant: 44),
            closeButton.heightAnchor.constraint(equalToConstant: 44),

            titleLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 70),
            titleLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 28),
            titleLabel.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -28),
            instructionLabel.topAnchor.constraint(equalTo: titleLabel.bottomAnchor, constant: 8),
            instructionLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 32),
            instructionLabel.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -32),

            scanFrameView.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            scanFrameView.centerYAnchor.constraint(equalTo: view.centerYAnchor, constant: -12),
            proportionalWidth,
            scanFrameView.widthAnchor.constraint(lessThanOrEqualToConstant: 300),
            scanFrameView.heightAnchor.constraint(equalTo: scanFrameView.widthAnchor),

            privacyLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 28),
            privacyLabel.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -28),
            privacyLabel.bottomAnchor.constraint(
                equalTo: view.safeAreaLayoutGuide.bottomAnchor,
                constant: -28
            )
        ])
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        captureQueue.async { [weak self] in
            self?.configureAndStartCapture()
        }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        stopCapture()
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer.frame = view.bounds
        if let connection = previewLayer.connection {
            let orientation = view.window?.windowScene?.interfaceOrientation ?? .portrait
            if #available(iOS 17.0, *) {
                let angle: CGFloat
                switch orientation {
                case .landscapeLeft:
                    angle = 0
                case .landscapeRight:
                    angle = 180
                case .portraitUpsideDown:
                    angle = 270
                default:
                    angle = 90
                }
                if connection.isVideoRotationAngleSupported(angle) {
                    connection.videoRotationAngle = angle
                }
            } else if connection.isVideoOrientationSupported {
                switch orientation {
                case .landscapeLeft:
                    connection.videoOrientation = .landscapeRight
                case .landscapeRight:
                    connection.videoOrientation = .landscapeLeft
                case .portraitUpsideDown:
                    connection.videoOrientation = .portraitUpsideDown
                default:
                    connection.videoOrientation = .portrait
                }
            }
        }
        let path = UIBezierPath(rect: view.bounds)
        path.append(
            UIBezierPath(
                roundedRect: scanFrameView.frame,
                cornerRadius: scanFrameView.layer.cornerRadius
            )
        )
        dimmingLayer.frame = view.bounds
        dimmingLayer.path = path.cgPath
    }

    func metadataOutput(
        _ output: AVCaptureMetadataOutput,
        didOutput metadataObjects: [AVMetadataObject],
        from connection: AVCaptureConnection
    ) {
        guard let code = metadataObjects
            .compactMap({ $0 as? AVMetadataMachineReadableCodeObject })
            .first(where: { $0.type == .qr })?
            .stringValue?
            .trimmingCharacters(in: .whitespacesAndNewlines),
              !code.isEmpty else {
            return
        }
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        finish(with: .value(code))
    }

    @objc private func cancelScan() {
        finish(with: .cancelled)
    }

    private func configureAndStartCapture() {
        do {
            guard let device = AVCaptureDevice.default(for: .video) else {
                throw ModemDeckQRScannerSetupError.cameraUnavailable
            }
            let input = try AVCaptureDeviceInput(device: device)
            let output = AVCaptureMetadataOutput()

            captureSession.beginConfiguration()
            captureSession.sessionPreset = .high
            guard captureSession.canAddInput(input) else {
                captureSession.commitConfiguration()
                throw ModemDeckQRScannerSetupError.inputUnavailable
            }
            captureSession.addInput(input)
            guard captureSession.canAddOutput(output) else {
                captureSession.removeInput(input)
                captureSession.commitConfiguration()
                throw ModemDeckQRScannerSetupError.outputUnavailable
            }
            captureSession.addOutput(output)
            output.setMetadataObjectsDelegate(self, queue: .main)
            output.metadataObjectTypes = [.qr]
            captureSession.commitConfiguration()
            captureSession.startRunning()
        } catch {
            DispatchQueue.main.async { [weak self] in
                self?.finish(with: .failed(error))
            }
        }
    }

    private func stopCapture() {
        captureQueue.async { [weak self] in
            guard let self, self.captureSession.isRunning else {
                return
            }
            self.captureSession.stopRunning()
        }
    }

    private func finish(with result: ModemDeckQRScanResult) {
        guard !completed else {
            return
        }
        completed = true
        stopCapture()
        let completion = completion
        dismiss(animated: true) {
            completion?(result)
        }
    }
}

@objc(ModemDeckNativePlugin)
final class ModemDeckNativePlugin: CAPPlugin, CAPBridgedPlugin, ModemDeckCallStateObserver {
    let identifier = "ModemDeckNativePlugin"
    let jsName = "ModemDeckNative"
    let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "status", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "appInfo", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "notificationStatus", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "requestNotificationPermission", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "openNotificationSettings", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "showNotification", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "currentCallState", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "startOutgoingCall", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "answerCall", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "endCall", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "playDTMF", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setCallMuted", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "scanPairingCode", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "readContacts", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "configure", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "clearConfiguration", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "disconnect", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "request", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "cancelRequest", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "startEventStream", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "stopEventStream", returnType: CAPPluginReturnPromise)
    ]

    private let store: ModemDeckCredentialStore
    private let lock = NSLock()
    private var requests: [String: URLSessionDataTask] = [:]
    private var streams: [String: ModemDeckEventStream] = [:]
    private var scanner: ModemDeckQRScannerViewController?
    private var scanRequestPending = false

    init(store: ModemDeckCredentialStore) {
        self.store = store
        super.init()
        ModemDeckPushCoordinator.shared.observeCallState(self)
    }

    func callStateDidChange(_ state: [String: Any]) {
        notifyListeners("callState", data: state, retainUntilConsumed: true)
    }

    @objc func status(_ call: CAPPluginCall) {
        do {
            guard let credential = try store.load() else {
                call.resolve(["configured": false])
                return
            }
            let validated = try validatedCredential(
                serverURL: credential.serverURL,
                token: credential.token
            )
            call.resolve([
                "configured": true,
                "serverURL": validated.serverURL
            ])
        } catch {
            try? store.clear()
            call.resolve(["configured": false])
        }
    }

    @objc func appInfo(_ call: CAPPluginCall) {
        let bundle = Bundle.main
        call.resolve([
            "version": bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "",
            "build": bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? ""
        ])
    }

    @objc func currentCallState(_ call: CAPPluginCall) {
        ModemDeckPushCoordinator.shared.currentCallState { state in
            call.resolve(state)
        }
    }

    @objc func setCallMuted(_ call: CAPPluginCall) {
        guard let muted = call.getBool("muted") else {
            call.reject("muted is required", "INVALID_ARGUMENT")
            return
        }
        ModemDeckPushCoordinator.shared.setCurrentCallMuted(muted) { result in
            switch result {
            case .success:
                call.resolve(["muted": muted])
            case .failure(let error):
                call.reject(error.localizedDescription, "CALLKIT_MUTE_FAILED", error)
            }
        }
    }

    @objc func startOutgoingCall(_ call: CAPPluginCall) {
        let callID = (call.getString("callID") ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let remoteNumber = (call.getString("remoteNumber") ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let displayName = (call.getString("displayName") ?? remoteNumber)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !callID.isEmpty, !remoteNumber.isEmpty else {
            call.reject("callID and remoteNumber are required", "INVALID_ARGUMENT")
            return
        }
        ModemDeckPushCoordinator.shared.startOutgoingCall(
            callID: callID,
            lineID: call.getString("lineID") ?? "",
            remoteNumber: remoteNumber,
            displayName: displayName.isEmpty ? remoteNumber : displayName
        ) { result in
            switch result {
            case .success(let uuid):
                call.resolve(["uuid": uuid.uuidString.lowercased()])
            case .failure(let error):
                call.reject(error.localizedDescription, "CALLKIT_START_FAILED", error)
            }
        }
    }

    @objc func answerCall(_ call: CAPPluginCall) {
        performCallKitAction(call, code: "CALLKIT_ANSWER_FAILED") { callID, completion in
            ModemDeckPushCoordinator.shared.answerCall(
                callID: callID,
                completion: completion
            )
        }
    }

    @objc func endCall(_ call: CAPPluginCall) {
        performCallKitAction(call, code: "CALLKIT_END_FAILED") { callID, completion in
            ModemDeckPushCoordinator.shared.endCall(
                callID: callID,
                completion: completion
            )
        }
    }

    @objc func playDTMF(_ call: CAPPluginCall) {
        let digits = (call.getString("digits") ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !digits.isEmpty else {
            call.reject("digits is required", "INVALID_ARGUMENT")
            return
        }
        performCallKitAction(call, code: "CALLKIT_DTMF_FAILED") { callID, completion in
            ModemDeckPushCoordinator.shared.playDTMF(
                callID: callID,
                digits: digits,
                completion: completion
            )
        }
    }

    private func performCallKitAction(
        _ call: CAPPluginCall,
        code: String,
        operation: (
            String,
            @escaping (Result<Void, Error>) -> Void
        ) -> Void
    ) {
        let callID = (call.getString("callID") ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard !callID.isEmpty else {
            call.reject("callID is required", "INVALID_ARGUMENT")
            return
        }
        operation(callID) { result in
            switch result {
            case .success:
                call.resolve()
            case .failure(let error):
                call.reject(error.localizedDescription, code, error)
            }
        }
    }

    @objc func notificationStatus(_ call: CAPPluginCall) {
        UNUserNotificationCenter.current().getNotificationSettings { settings in
            call.resolve(notificationStatusPayload(settings))
        }
    }

    @objc func requestNotificationPermission(_ call: CAPPluginCall) {
        let center = UNUserNotificationCenter.current()
        center.requestAuthorization(options: [.alert, .sound, .badge]) { _, error in
            if let error {
                call.reject(
                    "Unable to request iOS notification permission.",
                    "NOTIFICATION_PERMISSION_FAILED",
                    error
                )
                return
            }
            center.getNotificationSettings { settings in
                call.resolve(notificationStatusPayload(settings))
            }
        }
    }

    @objc func openNotificationSettings(_ call: CAPPluginCall) {
        DispatchQueue.main.async {
            guard let settingsURL = URL(string: UIApplication.openNotificationSettingsURLString) else {
                call.reject(
                    "The iOS notification settings URL is unavailable.",
                    "NOTIFICATION_SETTINGS_UNAVAILABLE"
                )
                return
            }
            UIApplication.shared.open(settingsURL, options: [:]) { opened in
                call.resolve(["opened": opened])
            }
        }
    }

    @objc func showNotification(_ call: CAPPluginCall) {
        let title = call.getString("title")?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let body = call.getString("body")?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let requestedIdentifier = call.getString("identifier")?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !title.isEmpty else {
            call.reject("A notification title is required.", "INVALID_NOTIFICATION")
            return
        }

        let center = UNUserNotificationCenter.current()
        center.getNotificationSettings { settings in
            guard notificationStatusPayload(settings)["enabled"] as? Bool == true else {
                call.resolve(["scheduled": false])
                return
            }
            let content = UNMutableNotificationContent()
            content.title = String(title.prefix(160))
            content.body = String(body.prefix(512))
            content.sound = .default
            content.threadIdentifier = "modemdeck-communications"
            let identifier = requestedIdentifier.isEmpty
                ? UUID().uuidString
                : String(requestedIdentifier.prefix(160))
            let trigger = UNTimeIntervalNotificationTrigger(
                timeInterval: 1,
                repeats: false
            )
            center.add(
                UNNotificationRequest(identifier: identifier, content: content, trigger: trigger)
            ) { error in
                if let error {
                    call.reject(
                        "Unable to deliver the iOS notification.",
                        "NOTIFICATION_DELIVERY_FAILED",
                        error
                    )
                } else {
                    call.resolve(["scheduled": true])
                }
            }
        }
    }

    @objc func scanPairingCode(_ call: CAPPluginCall) {
        DispatchQueue.main.async { [weak self] in
            guard let self else {
                call.reject("The QR code scanner is unavailable.", "SCANNER_UNAVAILABLE")
                return
            }
            self.beginPairingCodeScan(call)
        }
    }

    @objc func readContacts(_ call: CAPPluginCall) {
        let contactStore = CNContactStore()
        let authorizationStatus = CNContactStore.authorizationStatus(for: .contacts)
        if #available(iOS 18.0, *), authorizationStatus == .limited {
            resolveContacts(call, from: contactStore)
            return
        }
        switch authorizationStatus {
        case .authorized:
            resolveContacts(call, from: contactStore)
        case .notDetermined:
            contactStore.requestAccess(for: .contacts) { [weak self] granted, error in
                guard let self else {
                    call.reject("The device address book is unavailable.", "CONTACTS_UNAVAILABLE")
                    return
                }
                if let error {
                    call.reject(error.localizedDescription, "CONTACTS_PERMISSION_FAILED", error)
                } else if granted {
                    self.resolveContacts(call, from: contactStore)
                } else {
                    call.reject(
                        "Contacts access is required to import the device address book.",
                        "CONTACTS_PERMISSION_DENIED"
                    )
                }
            }
        case .denied, .restricted:
            call.reject(
                "Contacts access is required to import the device address book.",
                "CONTACTS_PERMISSION_DENIED"
            )
        default:
            call.reject("The contacts authorization state is unavailable.", "CONTACTS_UNAVAILABLE")
        }
    }

    @objc func configure(_ call: CAPPluginCall) {
        let credential: ModemDeckCredential
        do {
            credential = try validatedCredential(
                serverURL: call.getString("serverURL") ?? "",
                token: call.getString("token") ?? ""
            )
        } catch {
            call.reject(error.localizedDescription, "INVALID_PAIRING", error)
            return
        }

        let request: URLRequest
        do {
            request = try authorizedRequest(
                credential: credential,
                path: "/api/v1/mobile/session",
                method: "GET",
                headers: ["Accept": "application/json"],
                timeout: 15
            )
        } catch {
            call.reject(error.localizedDescription, "INVALID_PAIRING", error)
            return
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 20
        URLSession(configuration: configuration).dataTask(with: request) { [weak self] data, response, error in
            if let error {
                call.reject("Unable to reach the paired ModemDeck server.", "PAIRING_CONNECTION_FAILED", error)
                return
            }
            guard let self,
                  let response = response as? HTTPURLResponse,
                  let data else {
                call.reject(ModemDeckNativeError.invalidResponse.localizedDescription, "PAIRING_INVALID_RESPONSE")
                return
            }
            guard response.statusCode == 200,
                  let payload = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  payload["authenticated"] as? Bool == true else {
                call.reject(
                    serverErrorMessage(data: data, fallback: "The pairing code was rejected by ModemDeck."),
                    response.statusCode == 401 ? "PAIRING_REJECTED" : "PAIRING_INVALID_RESPONSE"
                )
                return
            }
            do {
                try self.store.save(credential)
                ModemDeckPushCoordinator.shared.configure(store: self.store)
                call.resolve(["serverURL": credential.serverURL])
            } catch {
                call.reject(error.localizedDescription, "CREDENTIAL_SAVE_FAILED", error)
            }
        }.resume()
    }

    @objc func clearConfiguration(_ call: CAPPluginCall) {
        cancelAllStreams()
        cancelAllRequests()
        ModemDeckPushCoordinator.shared.clearConfiguration()
        do {
            try store.clear()
            call.resolve()
        } catch {
            call.reject(error.localizedDescription, "CREDENTIAL_CLEAR_FAILED", error)
        }
    }

    @objc func disconnect(_ call: CAPPluginCall) {
        let credential: ModemDeckCredential
        do {
            guard let loaded = try store.load() else {
                call.resolve(["revoked": false])
                return
            }
            credential = loaded
        } catch {
            try? store.clear()
            call.reject(error.localizedDescription, "CREDENTIAL_READ_FAILED", error)
            return
        }
        cancelAllStreams()
        cancelAllRequests()
        ModemDeckPushCoordinator.shared.clearConfiguration()
        let request = try? authorizedRequest(
            credential: credential,
            path: "/api/v1/mobile/pairing",
            method: "DELETE",
            headers: ["Accept": "application/json"],
            timeout: 10
        )
        guard let request else {
            try? store.clear()
            call.resolve(["revoked": false])
            return
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 10
        configuration.timeoutIntervalForResource = 12
        URLSession(configuration: configuration).dataTask(with: request) { [weak self] _, response, _ in
            let status = (response as? HTTPURLResponse)?.statusCode
            try? self?.store.clear()
            call.resolve(["revoked": status == 204 || status == 401])
        }.resume()
    }

    @objc func request(_ call: CAPPluginCall) {
        let requestID = call.getString("requestID")?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !requestID.isEmpty, requestID.count <= 128 else {
            call.reject("A valid request ID is required.", "INVALID_REQUEST")
            return
        }
        let credential: ModemDeckCredential
        let request: URLRequest
        do {
            guard let loaded = try store.load() else {
                throw ModemDeckNativeError.notConfigured
            }
            credential = loaded
            var headers: [String: String] = [:]
            for (name, value) in call.getObject("headers") ?? [:] {
                if let value = value as? String {
                    headers[name] = value
                }
            }
            let body = call.getString("body")?.data(using: .utf8)
            let timeoutMilliseconds = call.getInt("timeoutMilliseconds") ?? 60_000
            request = try authorizedRequest(
                credential: credential,
                path: call.getString("path") ?? "",
                method: call.getString("method") ?? "GET",
                headers: headers,
                body: body,
                timeout: TimeInterval(timeoutMilliseconds) / 1000
            )
        } catch {
            call.reject(error.localizedDescription, "INVALID_REQUEST", error)
            return
        }

        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = request.timeoutInterval
        configuration.timeoutIntervalForResource = min(190, request.timeoutInterval + 10)
        let task = URLSession(configuration: configuration).dataTask(with: request) { [weak self] data, response, error in
            self?.removeRequest(requestID)
            if let error {
                let code = (error as NSError).code == NSURLErrorCancelled ? "REQUEST_CANCELLED" : "REQUEST_FAILED"
                call.reject(error.localizedDescription, code, error)
                return
            }
            guard let response = response as? HTTPURLResponse else {
                call.reject(ModemDeckNativeError.invalidResponse.localizedDescription, "INVALID_RESPONSE")
                return
            }
            let data = data ?? Data()
            if let body = String(data: data, encoding: .utf8) {
                call.resolve([
                    "status": response.statusCode,
                    "headers": responseHeaders(response),
                    "body": body,
                    "base64Encoded": false
                ])
            } else {
                call.resolve([
                    "status": response.statusCode,
                    "headers": responseHeaders(response),
                    "body": data.base64EncodedString(),
                    "base64Encoded": true
                ])
            }
        }
        lock.lock()
        if requests[requestID] != nil {
            lock.unlock()
            task.cancel()
            call.reject("The request ID is already active.", "DUPLICATE_REQUEST")
            return
        }
        requests[requestID] = task
        lock.unlock()
        task.resume()
    }

    @objc func cancelRequest(_ call: CAPPluginCall) {
        let requestID = call.getString("requestID") ?? ""
        let task: URLSessionDataTask?
        lock.lock()
        task = requests.removeValue(forKey: requestID)
        lock.unlock()
        task?.cancel()
        call.resolve()
    }

    @objc func startEventStream(_ call: CAPPluginCall) {
        let streamID = call.getString("streamID")?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !streamID.isEmpty, streamID.count <= 128 else {
            call.reject("A valid event stream ID is required.", "INVALID_STREAM")
            return
        }
        let request: URLRequest
        do {
            guard let credential = try store.load() else {
                throw ModemDeckNativeError.notConfigured
            }
            request = try authorizedRequest(
                credential: credential,
                path: call.getString("path") ?? "",
                method: "GET",
                headers: ["Accept": "text/event-stream"],
                timeout: 90
            )
        } catch {
            call.reject(error.localizedDescription, "INVALID_STREAM", error)
            return
        }
        let stream = ModemDeckEventStream(streamID: streamID, request: request, plugin: self)
        lock.lock()
        if streams[streamID] != nil {
            lock.unlock()
            call.reject("The event stream ID is already active.", "DUPLICATE_STREAM")
            return
        }
        streams[streamID] = stream
        lock.unlock()
        stream.start()
        call.resolve()
    }

    @objc func stopEventStream(_ call: CAPPluginCall) {
        stopStream(call.getString("streamID") ?? "")
        call.resolve()
    }

    fileprivate func emitStreamEvent(_ payload: [String: Any]) {
        DispatchQueue.main.async { [weak self] in
            self?.notifyListeners("streamEvent", data: payload)
        }
    }

    fileprivate func streamFinished(_ streamID: String) {
        lock.lock()
        streams.removeValue(forKey: streamID)
        lock.unlock()
    }

    private func stopStream(_ streamID: String) {
        let stream: ModemDeckEventStream?
        lock.lock()
        stream = streams.removeValue(forKey: streamID)
        lock.unlock()
        stream?.cancel()
    }

    private func cancelAllStreams() {
        let active: [ModemDeckEventStream]
        lock.lock()
        active = Array(streams.values)
        streams.removeAll()
        lock.unlock()
        active.forEach { $0.cancel() }
    }

    private func removeRequest(_ requestID: String) {
        lock.lock()
        requests.removeValue(forKey: requestID)
        lock.unlock()
    }

    private func cancelAllRequests() {
        let active: [URLSessionDataTask]
        lock.lock()
        active = Array(requests.values)
        requests.removeAll()
        lock.unlock()
        active.forEach { $0.cancel() }
    }

    private func resolveContacts(_ call: CAPPluginCall, from contactStore: CNContactStore) {
        DispatchQueue.global(qos: .userInitiated).async {
            let nameKeys = CNContactFormatter.descriptorForRequiredKeys(for: .fullName)
            let fetchRequest = CNContactFetchRequest(keysToFetch: [
                CNContactIdentifierKey as CNKeyDescriptor,
                CNContactOrganizationNameKey as CNKeyDescriptor,
                CNContactPhoneNumbersKey as CNKeyDescriptor,
                nameKeys
            ])
            fetchRequest.sortOrder = .userDefault
            fetchRequest.unifyResults = true
            let localeRegion = Locale.current.region?.identifier ?? ""
            var result: [[String: Any]] = []
            do {
                try contactStore.enumerateContacts(with: fetchRequest) { contact, _ in
                    let phones: [[String: String]] = contact.phoneNumbers.compactMap { value in
                        let number = value.value.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                        guard !number.isEmpty else { return nil }
                        let label = value.label.map {
                            CNLabeledValue<NSString>.localizedString(forLabel: $0)
                        } ?? "phone"
                        return [
                            "label": label,
                            "number": number,
                            "region": localeRegion
                        ]
                    }
                    guard !phones.isEmpty else { return }
                    let formattedName = CNContactFormatter.string(
                        from: contact,
                        style: .fullName
                    )?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                    let organization = contact.organizationName.trimmingCharacters(in: .whitespacesAndNewlines)
                    let displayName = !formattedName.isEmpty
                        ? formattedName
                        : (!organization.isEmpty ? organization : phones[0]["number"] ?? "")
                    result.append([
                        "identifier": contact.identifier,
                        "displayName": displayName,
                        "phones": phones
                    ])
                }
                call.resolve(["contacts": result])
            } catch {
                call.reject(error.localizedDescription, "CONTACTS_READ_FAILED", error)
            }
        }
    }

    private func beginPairingCodeScan(_ call: CAPPluginCall) {
        guard !scanRequestPending, scanner == nil else {
            call.reject("A QR code scan is already in progress.", "SCANNER_BUSY")
            return
        }
        guard AVCaptureDevice.default(for: .video) != nil else {
            call.reject("No camera is available on this device.", "CAMERA_UNAVAILABLE")
            return
        }

        scanRequestPending = true
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            scanRequestPending = false
            presentPairingCodeScanner(call)
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { [weak self] granted in
                DispatchQueue.main.async {
                    guard let self else {
                        call.reject("The QR code scanner is unavailable.", "SCANNER_UNAVAILABLE")
                        return
                    }
                    self.scanRequestPending = false
                    if granted {
                        self.presentPairingCodeScanner(call)
                    } else {
                        call.reject(
                            "Camera access is required to scan the pairing QR code.",
                            "CAMERA_PERMISSION_DENIED"
                        )
                    }
                }
            }
        case .denied, .restricted:
            scanRequestPending = false
            call.reject(
                "Camera access is required to scan the pairing QR code.",
                "CAMERA_PERMISSION_DENIED"
            )
        @unknown default:
            scanRequestPending = false
            call.reject("The camera authorization state is unavailable.", "SCANNER_UNAVAILABLE")
        }
    }

    private func presentPairingCodeScanner(_ call: CAPPluginCall) {
        guard var presenter = bridge?.viewController,
              presenter.viewIfLoaded?.window != nil else {
            call.reject("The QR code scanner cannot be presented.", "SCANNER_UNAVAILABLE")
            return
        }
        while let presented = presenter.presentedViewController,
              !presented.isBeingDismissed {
            presenter = presented
        }

        let scanner = ModemDeckQRScannerViewController()
        scanner.modalPresentationStyle = .fullScreen
        scanner.completion = { [weak self, weak scanner] result in
            if let scanner, self?.scanner === scanner {
                self?.scanner = nil
            }
            switch result {
            case .value(let value):
                call.resolve(["value": value, "cancelled": false])
            case .cancelled:
                call.resolve(["cancelled": true])
            case .failed(let error):
                call.reject(error.localizedDescription, "SCANNER_FAILED", error)
            }
        }
        self.scanner = scanner
        presenter.present(scanner, animated: true)
    }
}

private final class ModemDeckEventStream: NSObject, URLSessionDataDelegate {
    private let streamID: String
    private let request: URLRequest
    private weak var plugin: ModemDeckNativePlugin?
    private var session: URLSession?
    private var task: URLSessionDataTask?
    private var buffer = Data()
    private var errorBody = Data()
    private var eventName = "message"
    private var dataLines: [String] = []
    private var statusCode = 0
    private var accepted = false
    private var cancelled = false
    private var completed = false

    init(streamID: String, request: URLRequest, plugin: ModemDeckNativePlugin) {
        self.streamID = streamID
        self.request = request
        self.plugin = plugin
        super.init()
    }

    func start() {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 90
        configuration.timeoutIntervalForResource = 7 * 24 * 60 * 60
        let queue = OperationQueue()
        queue.maxConcurrentOperationCount = 1
        let session = URLSession(configuration: configuration, delegate: self, delegateQueue: queue)
        self.session = session
        let task = session.dataTask(with: request)
        self.task = task
        task.resume()
    }

    func cancel() {
        cancelled = true
        task?.cancel()
        session?.invalidateAndCancel()
        task = nil
        session = nil
    }

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        guard let response = response as? HTTPURLResponse else {
            completionHandler(.cancel)
            finishWithError(message: "ModemDeck returned an invalid event stream response.")
            return
        }
        statusCode = response.statusCode
        let contentType = response.value(forHTTPHeaderField: "Content-Type")?.lowercased() ?? ""
        accepted = response.statusCode == 200 && contentType.contains("text/event-stream")
        if accepted {
            plugin?.emitStreamEvent([
                "streamID": streamID,
                "kind": "open"
            ])
        }
        completionHandler(.allow)
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        if !accepted {
            if errorBody.count < 64 * 1024 {
                errorBody.append(data.prefix(64 * 1024 - errorBody.count))
            }
            return
        }
        buffer.append(data)
        consumeLines()
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        if cancelled {
            plugin?.streamFinished(streamID)
            return
        }
        if !accepted {
            let fallback = statusCode > 0
                ? "ModemDeck event stream returned HTTP \(statusCode)."
                : "Unable to connect to the ModemDeck event stream."
            finishWithError(
                message: serverErrorMessage(data: errorBody, fallback: error?.localizedDescription ?? fallback)
            )
            return
        }
        finishWithError(message: error?.localizedDescription ?? "The ModemDeck event stream closed.")
    }

    private func consumeLines() {
        while let newline = buffer.firstIndex(of: 0x0A) {
            var lineData = buffer[..<newline]
            buffer.removeSubrange(...newline)
            if lineData.last == 0x0D {
                lineData = lineData.dropLast()
            }
            guard let line = String(data: lineData, encoding: .utf8) else {
                finishWithError(message: "ModemDeck sent invalid event stream text.")
                return
            }
            consume(line: line)
        }
    }

    private func consume(line: String) {
        if line.isEmpty {
            dispatchEvent()
            return
        }
        if line.hasPrefix(":") {
            return
        }
        let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let field = String(parts[0])
        var value = parts.count == 2 ? String(parts[1]) : ""
        if value.hasPrefix(" ") {
            value.removeFirst()
        }
        switch field {
        case "event":
            eventName = value.isEmpty ? "message" : value
        case "data":
            dataLines.append(value)
        default:
            break
        }
    }

    private func dispatchEvent() {
        guard !dataLines.isEmpty else {
            eventName = "message"
            return
        }
        plugin?.emitStreamEvent([
            "streamID": streamID,
            "kind": "event",
            "event": eventName,
            "data": dataLines.joined(separator: "\n")
        ])
        eventName = "message"
        dataLines.removeAll(keepingCapacity: true)
    }

    private func finishWithError(message: String) {
        guard !completed else {
            return
        }
        completed = true
        plugin?.emitStreamEvent([
            "streamID": streamID,
            "kind": "error",
            "status": statusCode,
            "message": message
        ])
        plugin?.streamFinished(streamID)
        session?.finishTasksAndInvalidate()
        session = nil
        task = nil
    }
}

final class ModemDeckAPIURLSchemeHandler: NSObject, WKURLSchemeHandler {
    private let store: ModemDeckCredentialStore
    private let lock = NSLock()
    private var tasks: [ObjectIdentifier: URLSessionDataTask] = [:]

    init(store: ModemDeckCredentialStore) {
        self.store = store
        super.init()
    }

    func webView(_ webView: WKWebView, start urlSchemeTask: WKURLSchemeTask) {
        let identifier = ObjectIdentifier(urlSchemeTask as AnyObject)
        do {
            guard let credential = try store.load(),
                  let sourceURL = urlSchemeTask.request.url,
                  sourceURL.scheme == "modemdeck-api" else {
                throw ModemDeckNativeError.notConfigured
            }
            let path = sourceURL.path + (sourceURL.query.map { "?\($0)" } ?? "")
            var headers: [String: String] = [:]
            for name in ["Accept", "Range", "If-None-Match", "If-Modified-Since"] {
                if let value = urlSchemeTask.request.value(forHTTPHeaderField: name) {
                    headers[name] = value
                }
            }
            let request = try authorizedRequest(
                credential: credential,
                path: path,
                method: urlSchemeTask.request.httpMethod ?? "GET",
                headers: headers,
                timeout: 60
            )
            let configuration = URLSessionConfiguration.ephemeral
            configuration.timeoutIntervalForRequest = 60
            configuration.timeoutIntervalForResource = 180
            let task = URLSession(configuration: configuration).dataTask(with: request) { [weak self] data, response, error in
                DispatchQueue.main.async {
                    guard let self, self.takeTask(identifier) else {
                        return
                    }
                    if let error {
                        urlSchemeTask.didFailWithError(error)
                        return
                    }
                    guard let response = response as? HTTPURLResponse,
                          let sourceURL = urlSchemeTask.request.url,
                          let mappedResponse = HTTPURLResponse(
                            url: sourceURL,
                            statusCode: response.statusCode,
                            httpVersion: "HTTP/1.1",
                            headerFields: responseHeaders(response)
                          ) else {
                        urlSchemeTask.didFailWithError(ModemDeckNativeError.invalidResponse)
                        return
                    }
                    urlSchemeTask.didReceive(mappedResponse)
                    if let data, !data.isEmpty {
                        urlSchemeTask.didReceive(data)
                    }
                    urlSchemeTask.didFinish()
                }
            }
            lock.lock()
            tasks[identifier] = task
            lock.unlock()
            task.resume()
        } catch {
            urlSchemeTask.didFailWithError(error)
        }
    }

    func webView(_ webView: WKWebView, stop urlSchemeTask: WKURLSchemeTask) {
        let identifier = ObjectIdentifier(urlSchemeTask as AnyObject)
        let task: URLSessionDataTask?
        lock.lock()
        task = tasks.removeValue(forKey: identifier)
        lock.unlock()
        task?.cancel()
    }

    private func takeTask(_ identifier: ObjectIdentifier) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return tasks.removeValue(forKey: identifier) != nil
    }
}
