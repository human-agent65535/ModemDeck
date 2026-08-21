import AVFoundation
import Combine
@preconcurrency import Contacts
import Foundation
import UIKit
import UserNotifications

extension Notification.Name {
    static let modemDeckRemoteNotification = Notification.Name("ModemDeckRemoteNotification")
    static let modemDeckNotificationResponse = Notification.Name("ModemDeckNotificationResponse")
    static let modemDeckAuthenticationFailed = Notification.Name("ModemDeckAuthenticationFailed")
}

enum ModemDeckSessionPhase: Equatable {
    case launching
    case unpaired
    case loading
    case paired
    case unavailable
}

enum ModemDeckConnectionState: Equatable {
    case checking
    case online
    case offline
}

@MainActor
final class ModemDeckSessionController: ObservableObject {
    @Published private(set) var phase: ModemDeckSessionPhase = .launching
    @Published private(set) var session: ModemDeckMobileSession?
    @Published private(set) var bootstrap: ModemDeckBootstrap?
    @Published private(set) var notificationStatus = "unknown"
    @Published private(set) var microphoneStatus = "unknown"
    @Published var errorMessage = ""
    @Published var pairingInProgress = false
    @Published var selectedSection: ModemDeckSection
    @Published private(set) var requestedMessageThreadKey: String?
    @Published private(set) var connectionState: ModemDeckConnectionState = .checking

    let credentialStore: ModemDeckCredentialStore
    let api: ModemDeckAPIClient
    let callController: ModemDeckCallController
    private var refreshGeneration = 0

    init(credentialStore: ModemDeckCredentialStore) {
        self.credentialStore = credentialStore
        let initialSection = ModemDeckSection.initialSection
        selectedSection = initialSection == .dial ? .home : initialSection
        let api = ModemDeckAPIClient(credentialStore: credentialStore)
        self.api = api
        callController = ModemDeckCallController(api: api)
    }

    var serverDisplayName: String {
        guard let credential = try? credentialStore.load(),
              let url = URL(string: credential.serverURL) else {
            return ""
        }
        return url.host ?? credential.serverURL
    }

    var isOnline: Bool {
        connectionState == .online
    }

    var voiceDialLines: [ModemDeckLine] {
        guard let bootstrap,
              bootstrap.capabilities.dial,
              bootstrap.capabilities.webrtcAudio else {
            return []
        }
        return bootstrap.lines.filter {
            $0.capabilities?.dial == true && $0.capabilities?.media == true
        }
    }

    func voiceDialLine(preferredID: String?) -> ModemDeckLine? {
        let preferredID = preferredID?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return voiceDialLines.first(where: { $0.id == preferredID }) ?? voiceDialLines.first
    }

    func start() async {
        do {
            guard try credentialStore.load() != nil else {
                phase = .unpaired
                connectionState = .offline
                await refreshNotificationStatus()
                return
            }
            ModemDeckPushCoordinator.shared.configure(store: credentialStore)
            session = api.cachedMobileSession()
            bootstrap = api.cachedBootstrap()
            phase = .paired
            connectionState = .checking
            await refresh()
        } catch {
            try? credentialStore.clear()
            phase = .unpaired
            connectionState = .offline
            errorMessage = error.localizedDescription
        }
        await refreshNotificationStatus()
        refreshMicrophoneStatus()
    }

    func refresh() async {
        refreshGeneration &+= 1
        let generation = refreshGeneration
        if connectionState == .offline {
            connectionState = .checking
        }
        do {
            async let nextSession = api.mobileSession()
            async let nextBootstrap = api.bootstrap()
            let values = try await (nextSession, nextBootstrap)
            guard generation == refreshGeneration else { return }
            guard values.0.authenticated else {
                await resetAfterRevocation()
                return
            }
            session = values.0
            bootstrap = values.1
            errorMessage = ""
            connectionState = .online
            phase = .paired
        } catch let error as ModemDeckAPIError where error.isAuthenticationFailure {
            guard generation == refreshGeneration else { return }
            await resetAfterRevocation()
        } catch {
            guard generation == refreshGeneration else { return }
            errorMessage = error.localizedDescription
            connectionState = .offline
            phase = .paired
        }
    }

    func pair(using payloadText: String) async {
        guard !pairingInProgress else { return }
        pairingInProgress = true
        errorMessage = ""
        defer { pairingInProgress = false }
        do {
            let payloadData = Data(payloadText.utf8)
            let decoder = JSONDecoder()
            let payload = try decoder.decode(ModemDeckPairingPayload.self, from: payloadData)
            guard payload.version == 1, payload.type == "modemdeck.ios.pairing" else {
                throw ModemDeckAPIError.server(
                    status: 400,
                    code: "invalid_pairing",
                    message: "This QR code is not a ModemDeck iOS pairing code."
                )
            }
            let credential = try validatedCredential(
                serverURL: payload.serverURL,
                token: payload.token
            )
            let verifiedSession = try await api.verify(credential)
            guard verifiedSession.authenticated else {
                throw ModemDeckAPIError.server(
                    status: 401,
                    code: "pairing_rejected",
                    message: "The pairing code was rejected by ModemDeck."
                )
            }
            api.clearCachedData()
            try credentialStore.save(credential)
            ModemDeckPushCoordinator.shared.configure(store: credentialStore)
            session = verifiedSession
            await refresh()
        } catch {
            errorMessage = error.localizedDescription
            connectionState = .offline
            phase = .unpaired
        }
    }

    func disconnect() async {
        do {
            try await api.revokePairing()
            await resetAfterRevocation()
        } catch {
            errorMessage = text(
                "服务器尚未确认撤销配对，请联网后重试。",
                "The server has not confirmed the disconnect. Reconnect and try again."
            )
            connectionState = .offline
            phase = .paired
        }
    }

    func openMessage(threadKey: String) {
        let normalized = threadKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !normalized.isEmpty else { return }
        requestedMessageThreadKey = normalized
        selectedSection = .messages
    }

    func acceptRequestedMessage(threadKey: String) {
        guard requestedMessageThreadKey == threadKey else { return }
        requestedMessageThreadKey = nil
    }

    func handleAuthenticationFailure() async {
        await resetAfterRevocation()
    }

    func requestNotificationPermission() async {
        do {
            _ = try await UNUserNotificationCenter.current().requestAuthorization(
                options: [.alert, .badge, .sound]
            )
            UIApplication.shared.registerForRemoteNotifications()
        } catch {
            errorMessage = error.localizedDescription
        }
        await refreshNotificationStatus()
    }

    func refreshNotificationStatus() async {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        switch settings.authorizationStatus {
        case .authorized:
            notificationStatus = "authorized"
        case .provisional:
            notificationStatus = "provisional"
        case .ephemeral:
            notificationStatus = "ephemeral"
        case .denied:
            notificationStatus = "denied"
        case .notDetermined:
            notificationStatus = "not_determined"
        @unknown default:
            notificationStatus = "unknown"
        }
    }

    func openNotificationSettings() {
        guard let url = URL(string: UIApplication.openNotificationSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }

    func scheduleLocalTestNotification() async -> Bool {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        switch settings.authorizationStatus {
        case .authorized, .provisional, .ephemeral:
            break
        case .denied, .notDetermined:
            errorMessage = text(
                "请先在系统设置中允许 ModemDeck 通知。",
                "Allow ModemDeck notifications in System Settings first."
            )
            return false
        @unknown default:
            errorMessage = text("无法确认通知权限。", "Notification permission is unavailable.")
            return false
        }
        let content = UNMutableNotificationContent()
        content.title = "ModemDeck"
        content.body = text("这是一条本机测试通知。", "This is a local test notification.")
        content.sound = .default
        let request = UNNotificationRequest(
            identifier: "modemdeck-local-test-\(UUID().uuidString)",
            content: content,
            trigger: UNTimeIntervalNotificationTrigger(timeInterval: 1, repeats: false)
        )
        do {
            try await UNUserNotificationCenter.current().add(request)
            errorMessage = ""
            return true
        } catch {
            errorMessage = error.localizedDescription
            return false
        }
    }

    func refreshMicrophoneStatus() {
        if #available(iOS 17.0, *) {
            switch AVAudioApplication.shared.recordPermission {
            case .granted: microphoneStatus = "authorized"
            case .denied: microphoneStatus = "denied"
            case .undetermined: microphoneStatus = "not_determined"
            @unknown default: microphoneStatus = "unknown"
            }
        } else {
            switch AVAudioSession.sharedInstance().recordPermission {
            case .granted: microphoneStatus = "authorized"
            case .denied: microphoneStatus = "denied"
            case .undetermined: microphoneStatus = "not_determined"
            @unknown default: microphoneStatus = "unknown"
            }
        }
    }

    func requestMicrophonePermission() async {
        _ = await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
            ModemDeckPushCoordinator.shared.requestMicrophoneAccess { granted in
                continuation.resume(returning: granted)
            }
        }
        refreshMicrophoneStatus()
    }

    private func resetAfterRevocation() async {
        refreshGeneration &+= 1
        ModemDeckPushCoordinator.shared.clearConfiguration()
        api.clearCachedData()
        try? credentialStore.clear()
        session = nil
        bootstrap = nil
        connectionState = .offline
        phase = .unpaired
    }
}

struct ModemDeckPresentedCall: Identifiable, Equatable {
    let callID: String
    let lineID: String
    let remoteNumber: String
    let displayName: String
    let direction: String
    let state: String
    let testCall: Bool
    let muted: Bool
    let createdAt: String
    let activeAt: String?

    var id: String { callID }
}

@MainActor
final class ModemDeckCallController: NSObject, ObservableObject, ModemDeckCallStateObserver {
    private struct DTMFRequest: Equatable {
        let callID: String
        let digits: String
    }

    @Published private(set) var call: ModemDeckPresentedCall?
    @Published private(set) var busy = false
    @Published private(set) var muteBusy = false
    @Published private(set) var recordingEnabled = false
    @Published private(set) var recordingStatus = "off"
    @Published private(set) var recordingReady = false
    @Published private(set) var recordingBusy = false
    @Published private(set) var dtmfBusy = false
    @Published var errorMessage = ""

    private let api: ModemDeckAPIClient
    private var recordingGeneration = 0
    private var recordingSnapshotCallID = ""
    private var dtmfQueue: [DTMFRequest] = []

    init(api: ModemDeckAPIClient) {
        self.api = api
        super.init()
        ModemDeckPushCoordinator.shared.observeCallState(self)
        #if DEBUG
        installUATCallStateFromEnvironment()
        #endif
    }

    nonisolated func callStateDidChange(_ state: [String: Any]) {
        Task { @MainActor [weak self] in
            guard let self else { return }
            guard (state["state"] as? String) != "idle",
                  let callID = state["callID"] as? String else {
                self.resetCallState()
                return
            }
            let previousCall = self.call
            let presentedCall = ModemDeckPresentedCall(
                callID: callID,
                lineID: state["lineID"] as? String ?? "",
                remoteNumber: state["remoteNumber"] as? String ?? "",
                displayName: state["displayName"] as? String ?? "",
                direction: state["direction"] as? String ?? "incoming",
                state: state["state"] as? String ?? "ringing",
                testCall: state["testCall"] as? Bool ?? false,
                muted: state["muted"] as? Bool ?? false,
                createdAt: state["createdAt"] as? String ?? "",
                activeAt: state["activeAt"] as? String
            )
            self.call = presentedCall
            if previousCall?.callID != callID {
                self.prepareForNewCall(presentedCall)
            } else if previousCall?.state != presentedCall.state {
                self.reconcileRecording(for: presentedCall)
            }
        }
    }

    func start(
        lineID: String,
        number: String,
        displayName: String,
        recording: Bool?
    ) async {
        guard !busy, call == nil else { return }
        busy = true
        errorMessage = ""
        defer { busy = false }
        var createdCallID: String?
        do {
            let microphoneGranted = await withCheckedContinuation {
                (continuation: CheckedContinuation<Bool, Never>) in
                ModemDeckPushCoordinator.shared.requestMicrophoneAccess { granted in
                    continuation.resume(returning: granted)
                }
            }
            guard microphoneGranted else {
                throw ModemDeckAPIError.server(
                    status: 403,
                    code: "microphone_permission_denied",
                    message: "Microphone access is required before placing a call."
                )
            }
            let callSession = try await api.startCall(
                lineID: lineID,
                number: number,
                recordingEnabled: recording
            )
            createdCallID = callSession.id
            try await withCheckedThrowingContinuation { continuation in
                ModemDeckPushCoordinator.shared.startOutgoingCall(
                    callID: callSession.id,
                    lineID: callSession.lineId,
                    remoteNumber: callSession.remoteNumber,
                    displayName: callSession.displayName ?? displayName
                ) { result in
                    continuation.resume(with: result.map { _ in () })
                }
            }
        } catch {
            if let createdCallID {
                try? await api.callAction(callID: createdCallID, action: "hangup")
            }
            errorMessage = error.localizedDescription
        }
    }

    func answer() async {
        guard let call else { return }
        await perform { completion in
            ModemDeckPushCoordinator.shared.answerCall(callID: call.callID, completion: completion)
        }
    }

    func refreshFromCallKit() {
        #if DEBUG
        let environment = ProcessInfo.processInfo.environment
        if environment["MODEMDECK_UAT_MODE"] == "1",
           environment["MODEMDECK_UAT_CALL_STATE"] != nil {
            return
        }
        #endif
        ModemDeckPushCoordinator.shared.currentCallState { [weak self] state in
            self?.callStateDidChange(state)
        }
    }

    func end() async {
        guard let call else { return }
        await perform { completion in
            ModemDeckPushCoordinator.shared.endCall(callID: call.callID, completion: completion)
        }
    }

    func setMuted(_ muted: Bool) async {
        guard !muteBusy else { return }
        muteBusy = true
        errorMessage = ""
        defer { muteBusy = false }
        do {
            try await withCheckedThrowingContinuation { continuation in
                ModemDeckPushCoordinator.shared.setCurrentCallMuted(muted) { result in
                    continuation.resume(with: result)
                }
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    @discardableResult
    func enqueueDTMF(_ digits: String) -> Bool {
        guard let call,
              call.state == "active",
              !call.testCall,
              !digits.isEmpty else {
            return false
        }
        dtmfQueue.append(DTMFRequest(callID: call.callID, digits: digits))
        drainDTMFQueue()
        return true
    }

    func toggleRecording() async {
        guard let call,
              !call.testCall,
              recordingReady,
              !recordingBusy else {
            return
        }
        if call.direction == "incoming", call.state == "ringing" {
            recordingEnabled.toggle()
            recordingStatus = recordingEnabled ? "pending" : "off"
            ModemDeckPushCoordinator.shared.setPreferredCallRecording(
                callID: call.callID,
                enabled: recordingEnabled
            )
            return
        }
        guard call.state == "active" || call.direction == "outgoing" else { return }
        recordingBusy = true
        errorMessage = ""
        defer { recordingBusy = false }
        do {
            let state = try await api.setCallRecording(
                callID: call.callID,
                enabled: !recordingEnabled
            )
            acceptRecordingState(state, for: call.callID)
        } catch {
            errorMessage = error.localizedDescription
            await refreshRecordingState(callID: call.callID)
        }
    }

    func refreshRecordingState() async {
        guard let call, !call.testCall else { return }
        await refreshRecordingState(callID: call.callID)
    }

    private func perform(
        _ operation: (@escaping (Result<Void, Error>) -> Void) -> Void
    ) async {
        guard !busy else { return }
        busy = true
        errorMessage = ""
        defer { busy = false }
        do {
            try await withCheckedThrowingContinuation { continuation in
                operation { result in continuation.resume(with: result) }
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func prepareForNewCall(_ call: ModemDeckPresentedCall) {
        recordingGeneration += 1
        recordingSnapshotCallID = ""
        recordingEnabled = false
        recordingStatus = "off"
        recordingReady = call.testCall
        recordingBusy = false
        dtmfQueue.removeAll()
        dtmfBusy = false
        errorMessage = ""
        reconcileRecording(for: call)
    }

    private func reconcileRecording(for call: ModemDeckPresentedCall) {
        guard !call.testCall else { return }
        if call.direction == "incoming", call.state == "ringing" {
            guard !recordingReady else { return }
            let generation = recordingGeneration
            Task { [weak self] in
                guard let self else { return }
                do {
                    let settings = try await self.api.recordingSettings()
                    guard generation == self.recordingGeneration,
                          self.call?.callID == call.callID,
                          self.call?.state == "ringing" else {
                        return
                    }
                    self.recordingEnabled = settings.defaultEnabled
                    self.recordingStatus = settings.defaultEnabled ? "pending" : "off"
                    self.recordingReady = true
                } catch {
                    guard generation == self.recordingGeneration,
                          self.call?.callID == call.callID else {
                        return
                    }
                    self.recordingReady = false
                    self.errorMessage = error.localizedDescription
                }
            }
            return
        }
        guard call.direction == "outgoing" || call.state == "active",
              recordingSnapshotCallID != call.callID else {
            return
        }
        recordingSnapshotCallID = call.callID
        Task { [weak self] in
            await self?.refreshRecordingState(callID: call.callID)
        }
    }

    private func refreshRecordingState(callID: String) async {
        let generation = recordingGeneration
        do {
            let state = try await api.callRecording(callID: callID)
            guard generation == recordingGeneration,
                  call?.callID == callID else {
                return
            }
            acceptRecordingState(state, for: callID)
        } catch {
            guard generation == recordingGeneration,
                  call?.callID == callID else {
                return
            }
            recordingReady = false
            errorMessage = error.localizedDescription
        }
    }

    private func acceptRecordingState(
        _ state: ModemDeckCallRecordingState,
        for callID: String
    ) {
        guard state.callId == callID, call?.callID == callID else {
            errorMessage = ModemDeckAPIError.invalidResponse.localizedDescription
            return
        }
        recordingEnabled = state.enabled
        recordingStatus = state.status
        recordingReady = true
    }

    private func drainDTMFQueue() {
        guard !dtmfBusy, let request = dtmfQueue.first else { return }
        guard call?.callID == request.callID, call?.state == "active" else {
            dtmfQueue.removeAll()
            return
        }
        dtmfBusy = true
        ModemDeckPushCoordinator.shared.playDTMF(
            callID: request.callID,
            digits: request.digits
        ) { [weak self] result in
            Task { @MainActor [weak self] in
                guard let self else { return }
                if self.dtmfQueue.first == request {
                    self.dtmfQueue.removeFirst()
                }
                self.dtmfBusy = false
                guard self.call?.callID == request.callID else {
                    self.dtmfQueue.removeAll()
                    return
                }
                if case .failure(let error) = result {
                    self.errorMessage = error.localizedDescription
                    self.dtmfQueue.removeAll()
                    return
                }
                self.drainDTMFQueue()
            }
        }
    }

    private func resetCallState() {
        recordingGeneration += 1
        call = nil
        busy = false
        muteBusy = false
        recordingEnabled = false
        recordingStatus = "off"
        recordingReady = false
        recordingBusy = false
        recordingSnapshotCallID = ""
        dtmfQueue.removeAll()
        dtmfBusy = false
        errorMessage = ""
    }

    #if DEBUG
    private func installUATCallStateFromEnvironment() {
        let environment = ProcessInfo.processInfo.environment
        guard environment["MODEMDECK_UAT_MODE"] == "1",
              let state = environment["MODEMDECK_UAT_CALL_STATE"],
              ["ringing", "connecting", "active"].contains(state) else {
            return
        }
        DispatchQueue.main.async { [weak self] in
            self?.callStateDidChange([
                "callID": "uat-call-surface",
                "lineID": "uat-line-a",
                "remoteNumber": "+1 202 555 0102",
                "displayName": "UAT Call",
                "direction": state == "ringing" ? "incoming" : "outgoing",
                "state": state,
                "testCall": false,
                "muted": false,
                "createdAt": ISO8601DateFormatter().string(from: Date()),
                "activeAt": ISO8601DateFormatter().string(from: Date())
            ])
        }
    }
    #endif
}

@MainActor
final class ModemDeckContactsStore: ObservableObject {
    @Published private(set) var contacts: [ModemDeckContact] = []
    @Published private(set) var loading = false
    @Published var errorMessage = ""

    private let api: ModemDeckAPIClient
    private var reloadRequested = false

    init(api: ModemDeckAPIClient) {
        self.api = api
        contacts = api.cachedContacts()
    }

    func load() async {
        if loading {
            reloadRequested = true
            return
        }
        loading = true
        defer { loading = false }
        repeat {
            reloadRequested = false
            do {
                contacts = try await api.contacts()
                errorMessage = ""
            } catch {
                errorMessage = contacts.isEmpty ? error.localizedDescription : ""
            }
        } while reloadRequested
    }

    func upsert(_ contact: ModemDeckContact) {
        if let index = contacts.firstIndex(where: { $0.id == contact.id }) {
            contacts[index] = contact
        } else {
            contacts.insert(contact, at: 0)
        }
        api.cacheContacts(contacts)
    }

    func remove<S: Sequence>(ids: S) where S.Element == String {
        let removed = Set(ids)
        contacts.removeAll { removed.contains($0.id) }
        api.cacheContacts(contacts)
    }
}

@MainActor
final class ModemDeckMessagesStore: ObservableObject {
    @Published private(set) var threads: [ModemDeckMessageThread] = []
    @Published private(set) var contacts: [ModemDeckContact] = []
    @Published private(set) var loading = false
    @Published var errorMessage = ""

    let api: ModemDeckAPIClient
    private var reloadRequested = false

    init(api: ModemDeckAPIClient) {
        self.api = api
        threads = api.cachedMessageThreads()
        contacts = api.cachedContacts()
    }

    func load() async {
        if loading {
            reloadRequested = true
            return
        }
        loading = true
        defer { loading = false }
        repeat {
            reloadRequested = false
            async let nextContacts: [ModemDeckContact]? = try? await api.contacts()
            do {
                threads = try await api.messageThreads()
                errorMessage = ""
            } catch {
                errorMessage = threads.isEmpty ? error.localizedDescription : ""
            }
            if let nextContacts = await nextContacts {
                contacts = nextContacts
            }
        } while reloadRequested
    }

    func apply(_ action: ModemDeckMessageThreadAction, to ids: Set<String>) {
        threads = threads.compactMap { thread in
            guard ids.contains(thread.id) else { return thread }
            if action == .delete { return nil }
            return ModemDeckMessageThread(
                key: thread.key,
                lineId: thread.lineId,
                peer: thread.peer,
                contactId: thread.contactId,
                contactName: thread.contactName,
                lastMessageId: thread.lastMessageId,
                lastTimestamp: thread.lastTimestamp,
                lastContent: thread.lastContent,
                unreadCount: action == .read ? 0 : thread.unreadCount,
                markedUnread: action == .unread
                    ? true
                    : (action == .read ? false : thread.markedUnread),
                favorite: action == .favorite
                    ? true
                    : (action == .unfavorite ? false : thread.favorite)
            )
        }
        api.cacheMessageThreads(threads)
    }
}

@MainActor
final class ModemDeckConversationStore: ObservableObject {
    @Published private(set) var messages: [ModemDeckMessage] = []
    @Published private(set) var loading = false
    @Published private(set) var sending = false
    @Published var errorMessage = ""

    let thread: ModemDeckMessageThread
    private let api: ModemDeckAPIClient
    private var reloadRequested = false

    init(api: ModemDeckAPIClient, thread: ModemDeckMessageThread) {
        self.api = api
        self.thread = thread
        messages = api.cachedMessages(lineID: thread.lineId, peer: thread.peer)
    }

    func load() async {
        if loading {
            reloadRequested = true
            return
        }
        loading = true
        defer { loading = false }
        repeat {
            reloadRequested = false
            do {
                messages = try await api.messages(lineID: thread.lineId, peer: thread.peer)
                errorMessage = ""
            } catch {
                errorMessage = messages.isEmpty ? error.localizedDescription : ""
            }
        } while reloadRequested
    }

    func markRead() async -> Bool {
        do {
            try await api.markThreadRead(lineID: thread.lineId, peer: thread.peer)
            errorMessage = ""
            return true
        } catch {
            errorMessage = error.localizedDescription
            return false
        }
    }

    func send(_ content: String) async -> Bool {
        let normalized = content.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !normalized.isEmpty, !sending else { return false }
        sending = true
        defer { sending = false }
        do {
            let message = try await api.sendMessage(
                lineID: thread.lineId,
                to: thread.peer,
                content: normalized
            )
            messages.append(message)
            api.cacheMessages(messages, lineID: thread.lineId, peer: thread.peer)
            errorMessage = ""
            return true
        } catch {
            errorMessage = error.localizedDescription
            return false
        }
    }
}

@MainActor
final class ModemDeckCallsStore: ObservableObject {
    @Published private(set) var calls: [ModemDeckCallRecord] = []
    @Published private(set) var recordings: [ModemDeckRecording] = []
    @Published private(set) var contacts: [ModemDeckContact] = []
    @Published private(set) var loading = false
    @Published var errorMessage = ""

    private let api: ModemDeckAPIClient
    private var reloadRequested = false

    init(api: ModemDeckAPIClient) {
        self.api = api
        calls = api.cachedCalls()
        recordings = api.cachedRecordings()
        contacts = api.cachedContacts()
    }

    func load() async {
        if loading {
            reloadRequested = true
            return
        }
        loading = true
        defer { loading = false }
        repeat {
            reloadRequested = false
            do {
                async let nextCalls = api.calls()
                async let nextRecordings = api.recordings()
                async let nextContacts: [ModemDeckContact]? = try? await api.contacts()
                let values = try await (nextCalls, nextRecordings)
                calls = values.0
                recordings = values.1
                if let nextContacts = await nextContacts {
                    contacts = nextContacts
                }
                errorMessage = ""
            } catch {
                errorMessage = calls.isEmpty && recordings.isEmpty
                    ? error.localizedDescription
                    : ""
            }
        } while reloadRequested
    }

    func apply(_ action: ModemDeckCallBatchAction, to ids: Set<String>) {
        calls = calls.compactMap { call in
            guard ids.contains(call.id) else { return call }
            if action == .delete { return nil }
            return ModemDeckCallRecord(
                id: call.id,
                lineId: call.lineId,
                direction: call.direction,
                remoteNumber: call.remoteNumber,
                contactName: call.contactName,
                contactId: call.contactId,
                startedAt: call.startedAt,
                endedAt: call.endedAt,
                durationSeconds: call.durationSeconds,
                missed: call.missed,
                read: action == .read ? true : (action == .unread ? false : call.read),
                favorite: action == .favorite
                    ? true
                    : (action == .unfavorite ? false : call.favorite),
                failureCode: call.failureCode
            )
        }
        api.cacheCalls(calls)
    }

    func apply(_ action: ModemDeckRecordingBatchAction, to ids: Set<String>) {
        recordings = recordings.compactMap { recording in
            guard ids.contains(recording.id) else { return recording }
            if action == .delete { return nil }
            return ModemDeckRecording(
                segment: recording.segment,
                call: recording.call,
                playable: recording.playable,
                favorite: action == .favorite
                    ? true
                    : (action == .unfavorite ? false : recording.favorite)
            )
        }
        api.cacheRecordings(recordings)
    }
}

@MainActor
final class ModemDeckContactImporter: ObservableObject {
    @Published private(set) var pendingCount = 0
    @Published private(set) var importing = false
    @Published var resultMessage = ""
    @Published var errorMessage = ""

    private var pendingContacts: [ModemDeckContactDraft] = []

    func prepare() async {
        guard !importing else { return }
        errorMessage = ""
        resultMessage = ""
        do {
            let store = CNContactStore()
            let granted = try await withCheckedThrowingContinuation {
                (continuation: CheckedContinuation<Bool, Error>) in
                store.requestAccess(for: .contacts) { granted, error in
                    if let error {
                        continuation.resume(throwing: error)
                    } else {
                        continuation.resume(returning: granted)
                    }
                }
            }
            guard granted else {
                throw ModemDeckAPIError.server(
                    status: 403,
                    code: "contacts_permission_denied",
                    message: "Contacts access is required before importing."
                )
            }
            pendingContacts = try await readContacts(from: store)
            pendingCount = pendingContacts.count
            if pendingContacts.isEmpty {
                resultMessage = "No contacts with phone numbers were found."
            }
        } catch {
            pendingContacts = []
            pendingCount = 0
            errorMessage = error.localizedDescription
        }
    }

    func cancelPreparedImport() {
        pendingContacts = []
        pendingCount = 0
    }

    func importPrepared(using api: ModemDeckAPIClient) async {
        guard !pendingContacts.isEmpty, !importing else { return }
        importing = true
        errorMessage = ""
        resultMessage = ""
        let contacts = pendingContacts
        pendingContacts = []
        pendingCount = 0
        defer { importing = false }

        var imported = 0
        var skipped = 0
        var failed = 0
        for contact in contacts {
            do {
                _ = try await api.createContact(contact)
                imported += 1
            } catch let error as ModemDeckAPIError where error.code == "phone_conflict" {
                skipped += 1
            } catch {
                failed += 1
            }
        }
        resultMessage = "Imported \(imported), skipped \(skipped), failed \(failed)."
        if failed > 0 {
            errorMessage = resultMessage
        }
    }

    private func readContacts(from store: CNContactStore) async throws -> [ModemDeckContactDraft] {
        try await withCheckedThrowingContinuation {
            (continuation: CheckedContinuation<[ModemDeckContactDraft], Error>) in
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    let nameKeys = CNContactFormatter.descriptorForRequiredKeys(for: .fullName)
                    let request = CNContactFetchRequest(keysToFetch: [
                        CNContactIdentifierKey as CNKeyDescriptor,
                        CNContactOrganizationNameKey as CNKeyDescriptor,
                        CNContactPhoneNumbersKey as CNKeyDescriptor,
                        nameKeys
                    ])
                    request.sortOrder = .userDefault
                    request.unifyResults = true
                    let region = Locale.current.region?.identifier ?? ""
                    var drafts: [ModemDeckContactDraft] = []
                    try store.enumerateContacts(with: request) { contact, _ in
                        var seen = Set<String>()
                        let phones = contact.phoneNumbers.compactMap { value -> ModemDeckContactDraft.Phone? in
                            let number = value.value.stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
                            let identity = number.filter { $0 == "+" || $0.isNumber }
                            guard !identity.isEmpty, seen.insert(identity).inserted else { return nil }
                            let label = value.label.map {
                                CNLabeledValue<NSString>.localizedString(forLabel: $0)
                            } ?? "phone"
                            return ModemDeckContactDraft.Phone(
                                label: String(label.prefix(64)),
                                number: number,
                                region: region.isEmpty ? nil : region,
                                primary: false
                            )
                        }
                        guard !phones.isEmpty else { return }
                        let formattedName = CNContactFormatter.string(
                            from: contact,
                            style: .fullName
                        )?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
                        let organization = contact.organizationName.trimmingCharacters(in: .whitespacesAndNewlines)
                        let name = !formattedName.isEmpty
                            ? formattedName
                            : (!organization.isEmpty ? organization : phones[0].number)
                        let normalizedPhones = phones.enumerated().map { index, phone in
                            ModemDeckContactDraft.Phone(
                                label: phone.label,
                                number: phone.number,
                                region: phone.region,
                                primary: index == 0
                            )
                        }
                        drafts.append(ModemDeckContactDraft(
                            displayName: String(name.prefix(128)),
                            favorite: false,
                            phones: normalizedPhones
                        ))
                    }
                    continuation.resume(returning: drafts)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}
