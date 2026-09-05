import CryptoKit
import Foundation

enum ModemDeckAPIError: LocalizedError {
    case notPaired
    case invalidResponse
    case server(status: Int, code: String, message: String)

    var errorDescription: String? {
        switch self {
        case .notPaired:
            return "This device is not paired with ModemDeck."
        case .invalidResponse:
            return "ModemDeck returned an invalid response."
        case .server(_, _, let message):
            return message
        }
    }

    var isAuthenticationFailure: Bool {
        if case .server(let status, _, _) = self {
            return status == 401
        }
        return false
    }

    var code: String {
        if case .server(_, let code, _) = self {
            return code
        }
        return ""
    }
}

private final class ModemDeckOfflineCache {
    private let credentialStore: ModemDeckCredentialStore
    private let fileManager: FileManager
    private let lock = NSLock()
    private let rootURL: URL?

    init(
        credentialStore: ModemDeckCredentialStore,
        fileManager: FileManager = .default
    ) {
        self.credentialStore = credentialStore
        self.fileManager = fileManager
        rootURL = try? fileManager.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        .appendingPathComponent("ModemDeck", isDirectory: true)
        .appendingPathComponent("OfflineCache", isDirectory: true)
        .appendingPathComponent("v1", isDirectory: true)
    }

    func read<T: Decodable>(_ type: T.Type, key: String) -> T? {
        lock.lock()
        defer { lock.unlock() }
        guard let fileURL = scopedFileURL(key: key),
              let data = try? Data(contentsOf: fileURL) else {
            return nil
        }
        return try? JSONDecoder().decode(type, from: data)
    }

    func write<T: Encodable>(_ value: T, key: String) {
        lock.lock()
        defer { lock.unlock() }
        guard let fileURL = scopedFileURL(key: key),
              let data = try? JSONEncoder().encode(value) else {
            return
        }
        let directory = fileURL.deletingLastPathComponent()
        do {
            try fileManager.createDirectory(
                at: directory,
                withIntermediateDirectories: true,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication]
            )
            var directoryValues = URLResourceValues()
            directoryValues.isExcludedFromBackup = true
            var protectedDirectory = directory
            try? protectedDirectory.setResourceValues(directoryValues)
            try data.write(
                to: fileURL,
                options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication]
            )
        } catch {
            // A cache write must never block live API data.
        }
    }

    func clearCurrentScope() {
        lock.lock()
        defer { lock.unlock() }
        guard let directory = scopedDirectoryURL() else { return }
        try? fileManager.removeItem(at: directory)
    }

    private func scopedFileURL(key: String) -> URL? {
        scopedDirectoryURL()?.appendingPathComponent(digest(key), isDirectory: false)
    }

    private func scopedDirectoryURL() -> URL? {
        guard let rootURL,
              let credential = try? credentialStore.load() else {
            return nil
        }
        return rootURL.appendingPathComponent(
            digest("\(credential.serverURL)\u{0}\(credential.token)"),
            isDirectory: true
        )
    }

    private func digest(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8))
            .map { String(format: "%02x", $0) }
            .joined()
    }
}

struct ModemDeckPairingPayload: Decodable {
    let version: Int
    let type: String
    let serverURL: String
    let token: String

    private enum CodingKeys: String, CodingKey {
        case version
        case type
        case serverURL = "server_url"
        case token
    }
}

struct ModemDeckMobileSession: Codable, Equatable {
    let authenticated: Bool
    let setupRequired: Bool
    let userId: String?
    let username: String?
    let role: String?
    let profileContactId: String?
    let iosPairingEnabled: Bool
    let allowedLineIds: [String]?
    let language: String
}

struct ModemDeckCapabilities: Codable, Equatable {
    let agentConnected: Bool
    let dial: Bool
    let message: Bool
    let webrtcAudio: Bool
    let deviceControl: Bool
    let volteControl: Bool
    let vowifiControl: Bool
    let unavailableReasons: [String: String]?
}

struct ModemDeckLineCapabilities: Codable, Hashable {
    let modem: Bool?
    let sim: Bool?
    let voice: Bool?
    let messaging: Bool?
    let media: Bool?
    let dial: Bool?
    let answerCall: Bool?
    let rejectCall: Bool?
    let hangupCall: Bool?
    let sendDtmf: Bool?
    let sendMessage: Bool?

    var answer: Bool? { answerCall }
    var reject: Bool? { rejectCall }
    var hangup: Bool? { hangupCall }
    var dtmf: Bool? { sendDtmf }
    var message: Bool? { sendMessage }
}

struct ModemDeckLine: Codable, Identifiable, Hashable {
    let id: String
    let phoneNumber: String
    let operatorName: String
    let servingOperatorName: String
    let registrationState: String
    let roaming: Bool
    let deviceName: String
    let lineLabel: String
    let lineColor: String?
    let state: String?
    let signalQuality: Int?
    let capabilities: ModemDeckLineCapabilities?

    var displayName: String {
        let label = lineLabel.trimmingCharacters(in: .whitespacesAndNewlines)
        if !label.isEmpty {
            return label
        }
        let number = phoneNumber.trimmingCharacters(in: .whitespacesAndNewlines)
        return number.isEmpty ? id : number
    }

    var networkName: String {
        let serving = servingOperatorName.trimmingCharacters(in: .whitespacesAndNewlines)
        if !serving.isEmpty {
            return serving
        }
        return operatorName
    }

    private enum CodingKeys: String, CodingKey {
        case id
        case phoneNumber
        case operatorName = "operator"
        case servingOperatorName
        case registrationState
        case roaming
        case deviceName
        case lineLabel
        case lineColor
        case state
        case signalQuality
        case capabilities
    }
}

struct ModemDeckLineSettings: Codable, Equatable {
    let defaultLineId: String
    let revision: Int
}

struct ModemDeckSystemSettings: Codable, Equatable {
    let language: String
    let revision: Int
}

struct ModemDeckBootstrap: Codable, Equatable {
    let capabilities: ModemDeckCapabilities
    let lines: [ModemDeckLine]
    let lineCatalog: [ModemDeckLine]
    let lineSettings: ModemDeckLineSettings
    let systemSettings: ModemDeckSystemSettings
}

struct ModemDeckPageMeta: Decodable, Equatable {
    let limit: Int
    let nextCursor: String
    let hasMore: Bool
}

struct ModemDeckContactPhone: Codable, Hashable {
    let id: String?
    let label: String
    let originalNumber: String
    let canonicalE164: String?
    let region: String?
    let primary: Bool

    var displayNumber: String {
        originalNumber.isEmpty ? (canonicalE164 ?? "") : originalNumber
    }
}

struct ModemDeckContact: Codable, Identifiable, Hashable {
    let id: String
    let displayName: String
    let avatar: String?
    let phones: [ModemDeckContactPhone]
    let favorite: Bool
    let notes: String?
    let preferredLineId: String?
    let revision: Int?
    let createdAt: String?
    let updatedAt: String?

    var primaryPhone: ModemDeckContactPhone? {
        phones.first(where: \.primary) ?? phones.first
    }
}

extension Collection where Element == ModemDeckContact {
    func modemDeckContact(id: String?, number: String) -> ModemDeckContact? {
        let contactID = id?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !contactID.isEmpty,
           let directMatch = first(where: { $0.id == contactID }) {
            return directMatch
        }

        guard let identity = modemDeckCanonicalPhoneIdentity(number) else { return nil }
        let matches = filter { contact in
            contact.phones.contains { phone in
                if let canonical = modemDeckCanonicalPhoneIdentity(phone.canonicalE164 ?? "") {
                    return canonical == identity
                }
                return modemDeckCanonicalPhoneIdentity(phone.displayNumber) == identity
            }
        }
        return matches.count == 1 ? matches.first : nil
    }
}

private func modemDeckCanonicalPhoneIdentity(_ value: String) -> String? {
    let source = value.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !source.isEmpty else { return nil }
    let compact = source.filter { !" ().-/".contains($0) }
    guard compact.first == "+" else { return nil }
    let digits = compact.dropFirst()
    guard (8...15).contains(digits.count),
          digits.first != "0",
          digits.allSatisfy({ character in
              character.unicodeScalars.count == 1 &&
                  character.unicodeScalars.first.map { (48...57).contains($0.value) } == true
          }) else {
        return nil
    }
    return compact
}

struct ModemDeckMessageThread: Codable, Identifiable, Hashable {
    let key: String
    let lineId: String
    let peer: String
    let contactId: String?
    let contactName: String?
    let lastMessageId: Int64
    let lastTimestamp: String
    let lastContent: String?
    let unreadCount: Int
    let firstUnreadMessageId: Int64?
    let markedUnread: Bool
    let favorite: Bool

    var id: String { key }
    var displayName: String {
        let name = contactName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? peer : name
    }
}

struct ModemDeckMessage: Codable, Identifiable, Hashable {
    let id: Int64
    let lineId: String
    let peer: String
    let direction: String
    let content: String
    let timestamp: String
    let type: Int
    let status: Int
    let state: String?
    let deliveryStatus: String?
    let failureCode: String?

    var incoming: Bool { type == 1 }
}

struct ModemDeckUnreadSummary: Codable, Equatable {
    let badgeCount: Int
    let unreadMessageCount: Int
    let unreadThreadCount: Int

    static let empty = ModemDeckUnreadSummary(
        badgeCount: 0,
        unreadMessageCount: 0,
        unreadThreadCount: 0
    )
}

struct ModemDeckCallRecord: Codable, Identifiable, Hashable {
    let id: String
    let lineId: String
    let direction: String
    let remoteNumber: String
    let contactName: String?
    let contactId: String?
    let startedAt: String
    let endedAt: String?
    let durationSeconds: Int
    let missed: Bool
    let read: Bool
    let favorite: Bool
    let failureCode: String?

    var displayName: String {
        let name = contactName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? remoteNumber : name
    }
}

struct ModemDeckRecordingCall: Codable, Identifiable, Hashable {
    let id: String
    let lineId: String
    let direction: String
    let remoteNumber: String
    let contactName: String?
    let contactId: String?
    let startedAt: String
    let endedAt: String?
    let durationSeconds: Int
    let missed: Bool
    let failureCode: String?

    var displayName: String {
        let name = contactName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? remoteNumber : name
    }
}

struct ModemDeckCallSession: Decodable, Identifiable, Hashable {
    let id: String
    let lineId: String
    let direction: String
    let remoteNumber: String
    let displayName: String?
    let phase: String
    let controlState: String
    let mediaAvailable: Bool
    let createdAt: String
    let activeAt: String?
    let endedAt: String?
    let bearer: String?
    let failureReason: String?
}

struct ModemDeckRecordingSegment: Codable, Hashable {
    let id: String
    let callId: String
    let segmentIndex: Int
    let status: String
    let createdAt: String
    let startedAt: String?
    let endedAt: String?
    let durationMs: Int
    let sizeBytes: Int
    let failureCode: String?
}

struct ModemDeckRecording: Codable, Identifiable, Hashable {
    let segment: ModemDeckRecordingSegment
    let call: ModemDeckRecordingCall
    let playable: Bool
    let favorite: Bool

    var id: String { segment.id }
}

struct ModemDeckCallRecordingState: Decodable, Equatable {
    let callId: String
    let enabled: Bool
    let status: String
    let activeSegmentId: String?
    let lastErrorCode: String?
}

struct ModemDeckRecordingSettings: Decodable, Equatable {
    let defaultEnabled: Bool
    let revision: Int
}

struct ModemDeckGlobalCallSettings: Decodable, Equatable {
    let receiveCalls: Bool
    let revision: Int
}

struct ModemDeckAccountSessionDevice: Decodable, Equatable {
    let deviceName: String?
    let deviceModel: String?
    let deviceModelIdentifier: String?
    let osName: String?
    let osVersion: String?
    let appVersion: String?
    let appBuild: String?

    var displayName: String {
        let name = deviceName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !name.isEmpty { return name }
        let model = deviceModel?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return model.isEmpty ? "Apple device" : model
    }
}

struct ModemDeckAccountSession: Decodable, Identifiable, Equatable {
    let id: String
    let kind: String
    let createdAt: String
    let pairedAt: String?
    let lastSeenAt: String?
    let userAgent: String?
    let accessIp: String?
    let accessHost: String?
    let device: ModemDeckAccountSessionDevice?
    let current: Bool
    let paired: Bool?
}

struct ModemDeckManagedDevice: Decodable, Identifiable, Equatable {
    let imei: String
    let name: String
    let model: String
    let firmware: String
    let port: String
    let state: String?
    let currentIccid: String
    let signalQuality: Int?
    let lastSeen: String?
    let present: Bool

    var id: String { imei }
}

struct ModemDeckUserSummary: Decodable, Identifiable, Equatable {
    let id: String
    let username: String
    let role: String
    let enabled: Bool
    let iosPairingEnabled: Bool
    let iosPairingPaired: Bool
    let iosPairingDeviceCount: Int
    let iosPairingPending: Bool
    let profileName: String?
    let lineIds: [String]
}

struct ModemDeckTelegramUnitSummary: Decodable, Identifiable, Equatable {
    let id: String
    let displayName: String
    let enabled: Bool
    let assignedUsername: String?
    let allAssignedLines: Bool
    let effectiveEnabled: Bool
    let lineScopes: [String]
    let incomingSms: Bool
    let missedCalls: Bool
    let tokenConfigured: Bool
    let botUsername: String
    let verifiedAt: String
    let lastErrorClass: String
}

struct ModemDeckDiagnosticAvailability: Decodable, Equatable {
    let available: Bool
    let error: String?
}

struct ModemDeckDiagnosticAgent: Decodable, Equatable {
    let connected: Bool
    let provider: String
    let agentVersion: String
    let runtimeVersion: String
    let observedAt: String
    let lastError: String?
}

struct ModemDeckDiagnosticCall: Decodable, Identifiable, Equatable {
    let id: String
    let lineId: String
    let direction: String
    let phase: String
    let bearer: String
    let mediaAvailable: Bool
}

struct ModemDeckDiagnosticSnapshot: Decodable, Equatable {
    let status: String
    let observedAt: String
    let database: ModemDeckDiagnosticAvailability
    let hostAgent: ModemDeckDiagnosticAgent
    let callRuntime: ModemDeckDiagnosticAvailability
    let lines: [ModemDeckLine]
    let activeCalls: [ModemDeckDiagnosticCall]

    var calls: ModemDeckDiagnosticAvailability { callRuntime }
}

struct ModemDeckIOSTestCallResult: Decodable, Equatable {
    let id: String
    let acceptedAt: String
}

struct ModemDeckContactDraft: Encodable, Hashable {
    struct Phone: Encodable, Hashable {
        let id: String?
        let label: String
        let number: String
        let region: String?
        let primary: Bool

        init(
            id: String? = nil,
            label: String,
            number: String,
            region: String?,
            primary: Bool
        ) {
            self.id = id
            self.label = label
            self.number = number
            self.region = region
            self.primary = primary
        }
    }

    let displayName: String
    let avatar: String?
    let favorite: Bool
    let phones: [Phone]
    let notes: String?
    let preferredLineId: String?
    let revision: Int?

    init(
        displayName: String,
        avatar: String? = nil,
        favorite: Bool,
        phones: [Phone],
        notes: String? = nil,
        preferredLineId: String? = nil,
        revision: Int? = nil
    ) {
        self.displayName = displayName
        self.avatar = avatar
        self.favorite = favorite
        self.phones = phones
        self.notes = notes
        self.preferredLineId = preferredLineId
        self.revision = revision
    }
}

enum ModemDeckMessageThreadAction: String, Encodable {
    case read
    case unread
    case favorite
    case unfavorite
    case delete
}

enum ModemDeckCallBatchAction: String, Encodable {
    case read
    case unread
    case favorite
    case unfavorite
    case delete
}

enum ModemDeckRecordingBatchAction: String, Encodable {
    case favorite
    case unfavorite
    case delete
}

private struct ModemDeckContactsResponse: Decodable {
    let contacts: [ModemDeckContact]
    let meta: ModemDeckPageMeta
}

struct ModemDeckThreadsResponse: Decodable {
    let threads: [ModemDeckMessageThread]
    let meta: ModemDeckPageMeta
}

struct ModemDeckMessagesResponse: Decodable {
    let messages: [ModemDeckMessage]
    let meta: ModemDeckPageMeta
}

private struct ModemDeckCallsResponse: Decodable {
    let calls: [ModemDeckCallRecord]
    let meta: ModemDeckPageMeta
}

private struct ModemDeckRecordingResponseItem: Decodable {
    let segment: ModemDeckRecordingSegment
    let call: ModemDeckRecordingCall
    let playable: Bool
    let favorite: Bool
}

private struct ModemDeckRecordingsResponse: Decodable {
    let recordings: [ModemDeckRecordingResponseItem]
    let meta: ModemDeckPageMeta
}

private struct ModemDeckMessageResponse: Decodable {
    let message: ModemDeckMessage
}

private struct ModemDeckCallResponse: Decodable {
    let call: ModemDeckCallSession
}

private struct ModemDeckContactResponse: Decodable {
    let contact: ModemDeckContact
}

private struct ModemDeckCallRecordingResponse: Decodable {
    let state: ModemDeckCallRecordingState
}

private struct ModemDeckCallRecordingsResponse: Decodable {
    let state: ModemDeckCallRecordingState
    let segments: [ModemDeckRecordingSegment]
}

private struct ModemDeckRecordingSettingsResponse: Decodable {
    let settings: ModemDeckRecordingSettings
}

private struct ModemDeckLineSettingsResponse: Decodable {
    let settings: ModemDeckLineSettings
}

private struct ModemDeckSystemSettingsResponse: Decodable {
    let settings: ModemDeckSystemSettings
}

private struct ModemDeckAccountSessionsResponse: Decodable {
    let sessions: [ModemDeckAccountSession]
}

private struct ModemDeckManagedDevicesResponse: Decodable {
    let devices: [ModemDeckManagedDevice]
}

private struct ModemDeckUsersResponse: Decodable {
    let users: [ModemDeckUserSummary]
}

private struct ModemDeckTelegramUnitsResponse: Decodable {
    let units: [ModemDeckTelegramUnitSummary]
}

private struct ModemDeckServerError: Decodable {
    let code: String?
    let message: String?
    let detail: String?
}

private struct ModemDeckOperationIdentity {
    let key: String
    let requestID: String
}

private final class ModemDeckOperationJournal {
    private struct Entry: Codable {
        let requestID: String
        let updatedAt: TimeInterval
    }

    private let defaults: UserDefaults
    private let storageKey = "modemdeck.pending-operation-identities.v1"
    private let lock = NSLock()

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func identity(scope: String, kind: String, parts: [String]) -> ModemDeckOperationIdentity {
        let fingerprint = digest(([scope, kind] + parts).joined(separator: "\u{0}"))
        let now = Date().timeIntervalSince1970
        lock.lock()
        defer { lock.unlock() }
        // An unresolved operation identity must survive indefinitely. Expiring it
        // can turn an ambiguous timeout into a second SMS or modem call later.
        var entries = load()
        if let existing = entries[fingerprint] {
            entries[fingerprint] = Entry(requestID: existing.requestID, updatedAt: now)
            save(entries)
            return ModemDeckOperationIdentity(key: fingerprint, requestID: existing.requestID)
        }
        let requestID = "ios-\(UUID().uuidString.lowercased())"
        entries[fingerprint] = Entry(requestID: requestID, updatedAt: now)
        save(entries)
        return ModemDeckOperationIdentity(key: fingerprint, requestID: requestID)
    }

    func complete(_ identity: ModemDeckOperationIdentity) {
        lock.lock()
        defer { lock.unlock() }
        var entries = load()
        guard entries.removeValue(forKey: identity.key) != nil else { return }
        save(entries)
    }

    private func load() -> [String: Entry] {
        guard let data = defaults.data(forKey: storageKey),
              let entries = try? JSONDecoder().decode([String: Entry].self, from: data) else {
            return [:]
        }
        return entries
    }

    private func save(_ entries: [String: Entry]) {
        if entries.isEmpty {
            defaults.removeObject(forKey: storageKey)
            return
        }
        guard let data = try? JSONEncoder().encode(entries) else { return }
        defaults.set(data, forKey: storageKey)
    }

    private func digest(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}

final class ModemDeckAPIClient {
    let credentialStore: ModemDeckCredentialStore

    private let session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder
    private let operationJournal = ModemDeckOperationJournal()
    private let offlineCache: ModemDeckOfflineCache

    init(credentialStore: ModemDeckCredentialStore) {
        self.credentialStore = credentialStore
        offlineCache = ModemDeckOfflineCache(credentialStore: credentialStore)
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 60
        session = URLSession(configuration: configuration)
        decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        encoder = JSONEncoder()
        encoder.keyEncodingStrategy = .convertToSnakeCase
    }

    func verify(_ credential: ModemDeckCredential) async throws -> ModemDeckMobileSession {
        try await decode(
            ModemDeckMobileSession.self,
            path: "/api/v1/mobile/session",
            credential: credential
        )
    }

    func mobileSession() async throws -> ModemDeckMobileSession {
        let value = try await decode(ModemDeckMobileSession.self, path: "/api/v1/mobile/session")
        if value.authenticated {
            offlineCache.write(value, key: "mobile-session")
        }
        return value
    }

    func bootstrap() async throws -> ModemDeckBootstrap {
        let value = try await decode(ModemDeckBootstrap.self, path: "/api/v1/bootstrap")
        offlineCache.write(value, key: "bootstrap")
        return value
    }

    func cachedMobileSession() -> ModemDeckMobileSession? {
        offlineCache.read(ModemDeckMobileSession.self, key: "mobile-session")
    }

    func cachedBootstrap() -> ModemDeckBootstrap? {
        offlineCache.read(ModemDeckBootstrap.self, key: "bootstrap")
    }

    func clearCachedData() {
        offlineCache.clearCurrentScope()
    }

    func contacts(query: String = "") async throws -> [ModemDeckContact] {
        let contacts = try await allPages(
            ModemDeckContactsResponse.self,
            base: "/api/v1/contacts",
            queryItems: [URLQueryItem(name: "q", value: query)],
            items: \.contacts, meta: \.meta
        )
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            offlineCache.write(contacts, key: "contacts")
        }
        return contacts
    }

    func cachedContacts() -> [ModemDeckContact] {
        offlineCache.read([ModemDeckContact].self, key: "contacts") ?? []
    }

    func cacheContacts(_ contacts: [ModemDeckContact]) {
        offlineCache.write(contacts, key: "contacts")
    }

    func messageThreads(query: String = "") async throws -> [ModemDeckMessageThread] {
        var result: [ModemDeckMessageThread] = []
        var cursor = ""
        repeat {
            let response = try await decode(
                ModemDeckThreadsResponse.self,
                path: listPath("/api/v1/messages/threads", query: query, cursor: cursor)
            )
            result.append(contentsOf: response.threads)
            cursor = response.meta.hasMore ? response.meta.nextCursor : ""
        } while !cursor.isEmpty
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            offlineCache.write(result, key: "message-threads")
        }
        return result
    }

    func cachedMessageThreads() -> [ModemDeckMessageThread] {
        offlineCache.read([ModemDeckMessageThread].self, key: "message-threads") ?? []
    }

    func cacheMessageThreads(_ threads: [ModemDeckMessageThread]) {
        offlineCache.write(threads, key: "message-threads")
    }

    func messagePage(lineID: String, peer: String, cursor: String = "") async throws -> ModemDeckMessagesResponse {
        var queryItems = [
            URLQueryItem(name: "line_id", value: lineID),
            URLQueryItem(name: "peer", value: peer),
            URLQueryItem(name: "limit", value: "100")
        ]
        if !cursor.isEmpty {
            queryItems.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return try await decode(
            ModemDeckMessagesResponse.self,
            path: path(
            "/api/v1/messages",
                queryItems: queryItems
            )
        )
    }

    func messages(lineID: String, peer: String) async throws -> [ModemDeckMessage] {
        let response = try await messagePage(lineID: lineID, peer: peer)
        offlineCache.write(response.messages, key: messageCacheKey(lineID: lineID, peer: peer))
        return response.messages
    }

    func cachedMessages(lineID: String, peer: String) -> [ModemDeckMessage] {
        offlineCache.read(
            [ModemDeckMessage].self,
            key: messageCacheKey(lineID: lineID, peer: peer)
        ) ?? []
    }

    func cacheMessages(_ messages: [ModemDeckMessage], lineID: String, peer: String) {
        offlineCache.write(messages, key: messageCacheKey(lineID: lineID, peer: peer))
    }

    func sendMessage(lineID: String, to: String, content: String) async throws -> ModemDeckMessage {
        struct Payload: Encodable {
            let requestId: String
            let lineId: String
            let to: String
            let content: String
        }
        let operation = try operationIdentity(
            kind: "send-message",
            parts: [lineID, to, content]
        )
        let payload = Payload(
            requestId: operation.requestID,
            lineId: lineID,
            to: to,
            content: content
        )
        do {
            let response = try await decode(
                ModemDeckMessageResponse.self,
                path: "/api/v1/messages",
                method: "POST",
                headers: ["Idempotency-Key": operation.requestID],
                body: try encoder.encode(payload)
            )
            operationJournal.complete(operation)
            return response.message
        } catch {
            if !preservesOperationIdentity(error) {
                operationJournal.complete(operation)
            }
            throw error
        }
    }

    func unreadSummary() async throws -> ModemDeckUnreadSummary {
        let summary = try await decode(
            ModemDeckUnreadSummary.self,
            path: "/api/v1/messages/unread-summary"
        )
        offlineCache.write(summary, key: "message-unread-summary")
        return summary
    }

    func cachedUnreadSummary() -> ModemDeckUnreadSummary {
        offlineCache.read(ModemDeckUnreadSummary.self, key: "message-unread-summary") ?? .empty
    }

    func cacheUnreadSummary(_ summary: ModemDeckUnreadSummary) {
        offlineCache.write(summary, key: "message-unread-summary")
    }

    func markThreadRead(lineID: String, peer: String, throughMessageID: Int64) async throws -> ModemDeckUnreadSummary {
        struct Payload: Encodable {
            let lineId: String
            let peer: String
            let throughMessageId: Int64
        }
        let summary = try await decode(
            ModemDeckUnreadSummary.self,
            path: "/api/v1/messages/read",
            method: "PATCH",
            body: try encoder.encode(Payload(
                lineId: lineID,
                peer: peer,
                throughMessageId: throughMessageID
            ))
        )
        cacheUnreadSummary(summary)
        return summary
    }

    func updateMessageThreads(
        action: ModemDeckMessageThreadAction,
        threads: [ModemDeckMessageThread]
    ) async throws -> ModemDeckUnreadSummary {
        struct Identity: Encodable {
            let lineId: String
            let peer: String
            let throughMessageId: Int64
        }
        struct Payload: Encodable {
            let action: ModemDeckMessageThreadAction
            let threads: [Identity]
        }
        let summary = try await decode(
            ModemDeckUnreadSummary.self,
            path: "/api/v1/messages/threads/state",
            method: "PATCH",
            body: try encoder.encode(Payload(
                action: action,
                threads: threads.map {
                    Identity(
                        lineId: $0.lineId,
                        peer: $0.peer,
                        throughMessageId: action == .read ? $0.lastMessageId : 0
                    )
                }
            ))
        )
        cacheUnreadSummary(summary)
        return summary
    }

    func markAllMessageThreadsRead(lineID: String = "") async throws -> ModemDeckUnreadSummary {
        struct Payload: Encodable { let lineId: String }
        let summary = try await decode(
            ModemDeckUnreadSummary.self,
            path: "/api/v1/messages/read-all",
            method: "PATCH",
            body: try encoder.encode(Payload(lineId: lineID))
        )
        cacheUnreadSummary(summary)
        return summary
    }

    func deleteMessageThread(_ thread: ModemDeckMessageThread) async throws -> ModemDeckUnreadSummary {
        struct Payload: Encodable {
            let lineId: String
            let peer: String
        }
        let summary = try await decode(
            ModemDeckUnreadSummary.self,
            path: "/api/v1/messages/threads",
            method: "DELETE",
            body: try encoder.encode(Payload(lineId: thread.lineId, peer: thread.peer))
        )
        cacheUnreadSummary(summary)
        cacheMessages([], lineID: thread.lineId, peer: thread.peer)
        return summary
    }

    func calls() async throws -> [ModemDeckCallRecord] {
        let calls = try await allPages(
            ModemDeckCallsResponse.self,
            base: "/api/v1/calls",
            queryItems: [URLQueryItem(name: "kind", value: "all")],
            items: \.calls, meta: \.meta
        )
        offlineCache.write(calls, key: "calls")
        return calls
    }

    func cachedCalls() -> [ModemDeckCallRecord] {
        offlineCache.read([ModemDeckCallRecord].self, key: "calls") ?? []
    }

    func cacheCalls(_ calls: [ModemDeckCallRecord]) {
        offlineCache.write(calls, key: "calls")
    }

    func recordings() async throws -> [ModemDeckRecording] {
        let items = try await allPages(
            ModemDeckRecordingsResponse.self,
            base: "/api/v1/recordings", items: \.recordings, meta: \.meta
        )
        let recordings = items.map {
            ModemDeckRecording(
                segment: $0.segment,
                call: $0.call,
                playable: $0.playable,
                favorite: $0.favorite
            )
        }
        offlineCache.write(recordings, key: "recordings")
        return recordings
    }

    func cachedRecordings() -> [ModemDeckRecording] {
        offlineCache.read([ModemDeckRecording].self, key: "recordings") ?? []
    }

    func cacheRecordings(_ recordings: [ModemDeckRecording]) {
        offlineCache.write(recordings, key: "recordings")
    }

    func updateCalls(action: ModemDeckCallBatchAction, ids: [String]) async throws {
        struct Payload: Encodable {
            let action: ModemDeckCallBatchAction
            let ids: [String]
        }
        _ = try await data(
            path: "/api/v1/calls/batch",
            method: "PATCH",
            body: try encoder.encode(Payload(action: action, ids: ids))
        )
    }

    func deleteCall(id: String) async throws {
        let encodedID = id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id
        _ = try await data(path: "/api/v1/calls/\(encodedID)", method: "DELETE")
    }

    func updateRecordings(
        action: ModemDeckRecordingBatchAction,
        recordings: [ModemDeckRecording]
    ) async throws {
        struct Identity: Encodable {
            let callId: String
            let id: String
        }
        struct Payload: Encodable {
            let action: ModemDeckRecordingBatchAction
            let recordings: [Identity]
        }
        _ = try await data(
            path: "/api/v1/recordings/batch",
            method: "PATCH",
            body: try encoder.encode(Payload(
                action: action,
                recordings: recordings.map { Identity(callId: $0.call.id, id: $0.id) }
            ))
        )
    }

    func deleteRecording(callID: String, recordingID: String) async throws {
        let encodedCallID = callID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? callID
        let encodedRecordingID = recordingID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? recordingID
        _ = try await data(
            path: "/api/v1/calls/\(encodedCallID)/recordings/\(encodedRecordingID)",
            method: "DELETE"
        )
    }

    func startCall(
        lineID: String,
        number: String,
        recordingEnabled: Bool?
    ) async throws -> ModemDeckCallSession {
        struct Payload: Encodable {
            let requestId: String
            let lineId: String
            let number: String
            let recordingEnabled: Bool?
        }
        let operation = try operationIdentity(
            kind: "start-call",
            parts: [lineID, number, recordingEnabled.map(String.init) ?? "default"]
        )
        do {
            let response = try await decode(
                ModemDeckCallResponse.self,
                path: "/api/v1/calls",
                method: "POST",
                headers: ["Idempotency-Key": operation.requestID],
                body: try encoder.encode(Payload(
                    requestId: operation.requestID,
                    lineId: lineID,
                    number: number,
                    recordingEnabled: recordingEnabled
                ))
            )
            operationJournal.complete(operation)
            return response.call
        } catch {
            if !preservesOperationIdentity(error) {
                operationJournal.complete(operation)
            }
            throw error
        }
    }

    func callRecording(callID: String) async throws -> ModemDeckCallRecordingState {
        let encodedID = callID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? callID
        let response = try await decode(
            ModemDeckCallRecordingsResponse.self,
            path: "/api/v1/calls/\(encodedID)/recordings"
        )
        return response.state
    }

    func callAction(callID: String, action: String) async throws {
        struct Payload: Encodable { let requestId: String }
        let encodedID = callID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? callID
        _ = try await data(
            path: "/api/v1/calls/\(encodedID)/\(action)",
            method: "POST",
            body: try encoder.encode(Payload(
                requestId: "ios-\(UUID().uuidString.lowercased())"
            ))
        )
    }

    func setCallRecording(callID: String, enabled: Bool) async throws -> ModemDeckCallRecordingState {
        struct Payload: Encodable { let enabled: Bool }
        let encodedID = callID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? callID
        let response = try await decode(
            ModemDeckCallRecordingResponse.self,
            path: "/api/v1/calls/\(encodedID)/recording",
            method: "PUT",
            body: try encoder.encode(Payload(enabled: enabled))
        )
        return response.state
    }

    func recordingSettings() async throws -> ModemDeckRecordingSettings {
        let response = try await decode(
            ModemDeckRecordingSettingsResponse.self,
            path: "/api/v1/settings/recording"
        )
        return response.settings
    }

    func updateRecordingSettings(
        enabled: Bool,
        revision: Int
    ) async throws -> ModemDeckRecordingSettings {
        struct Payload: Encodable {
            let defaultEnabled: Bool
            let revision: Int
        }
        let response = try await decode(
            ModemDeckRecordingSettingsResponse.self,
            path: "/api/v1/settings/recording",
            method: "PUT",
            body: try encoder.encode(Payload(defaultEnabled: enabled, revision: revision))
        )
        return response.settings
    }

    func callSettings() async throws -> ModemDeckGlobalCallSettings {
        try await decode(
            ModemDeckGlobalCallSettings.self,
            path: "/api/v1/settings/calls"
        )
    }

    func updateCallSettings(
        receiveCalls: Bool,
        revision: Int
    ) async throws -> ModemDeckGlobalCallSettings {
        struct Payload: Encodable {
            let receiveCalls: Bool
            let expectedRevision: Int
        }
        return try await decode(
            ModemDeckGlobalCallSettings.self,
            path: "/api/v1/settings/calls",
            method: "PATCH",
            body: try encoder.encode(Payload(
                receiveCalls: receiveCalls,
                expectedRevision: revision
            ))
        )
    }

    func updateLineSettings(
        defaultLineID: String,
        revision: Int
    ) async throws -> ModemDeckLineSettings {
        struct Payload: Encodable {
            let defaultLineId: String
            let expectedRevision: Int
        }
        let response = try await decode(
            ModemDeckLineSettingsResponse.self,
            path: "/api/v1/settings/lines",
            method: "PATCH",
            body: try encoder.encode(Payload(
                defaultLineId: defaultLineID,
                expectedRevision: revision
            ))
        )
        return response.settings
    }

    func updateSystemSettings(
        language: String,
        revision: Int
    ) async throws -> ModemDeckSystemSettings {
        struct Payload: Encodable {
            let language: String
            let expectedRevision: Int
        }
        let response = try await decode(
            ModemDeckSystemSettingsResponse.self,
            path: "/api/v1/settings/system",
            method: "PATCH",
            body: try encoder.encode(Payload(
                language: language,
                expectedRevision: revision
            ))
        )
        return response.settings
    }

    func accountSessions() async throws -> [ModemDeckAccountSession] {
        try await decode(
            ModemDeckAccountSessionsResponse.self,
            path: "/api/v1/account/sessions"
        ).sessions
    }

    func setAccountContact(_ contactID: String) async throws {
        struct Payload: Encodable {
            let contactId: String
        }
        _ = try await data(
            path: "/api/v1/account/contact",
            method: "PUT",
            body: try encoder.encode(
                Payload(contactId: contactID.trimmingCharacters(in: .whitespacesAndNewlines))
            )
        )
    }

    func changePassword(currentPassword: String, newPassword: String) async throws {
        struct Payload: Encodable {
            let currentPassword: String
            let newPassword: String
        }
        _ = try await data(
            path: "/api/v1/account/password",
            method: "PUT",
            body: try encoder.encode(
                Payload(currentPassword: currentPassword, newPassword: newPassword)
            )
        )
    }

    func revokeAccountSession(id: String) async throws {
        let encodedID = id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id
        _ = try await data(
            path: "/api/v1/account/sessions/\(encodedID)",
            method: "DELETE"
        )
    }

    func managedDevices() async throws -> [ModemDeckManagedDevice] {
        try await decode(
            ModemDeckManagedDevicesResponse.self,
            path: "/api/v1/devices"
        ).devices
    }

    func users() async throws -> [ModemDeckUserSummary] {
        try await decode(
            ModemDeckUsersResponse.self,
            path: "/api/v1/users"
        ).users
    }

    func telegramUnits() async throws -> [ModemDeckTelegramUnitSummary] {
        try await decode(
            ModemDeckTelegramUnitsResponse.self,
            path: "/api/v1/settings/telegram"
        ).units
    }

    func diagnostics() async throws -> ModemDeckDiagnosticSnapshot {
        try await decode(
            ModemDeckDiagnosticSnapshot.self,
            path: "/api/v1/diagnostics"
        )
    }

    func recordingAudio(callID: String, segmentID: String) async throws -> Data {
        let encodedCallID = callID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? callID
        let encodedSegmentID = segmentID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? segmentID
        return try await data(
            path: "/api/v1/calls/\(encodedCallID)/recordings/\(encodedSegmentID)/download"
        )
    }

    func sendIOSTestCall() async throws -> ModemDeckIOSTestCallResult {
        try await decode(
            ModemDeckIOSTestCallResult.self,
            path: "/api/v1/mobile/push/test-call",
            method: "POST"
        )
    }

    func createContact(_ draft: ModemDeckContactDraft) async throws -> ModemDeckContact {
        let response = try await decode(
            ModemDeckContactResponse.self,
            path: "/api/v1/contacts",
            method: "POST",
            body: try encoder.encode(draft)
        )
        return response.contact
    }

    func updateContact(id: String, draft: ModemDeckContactDraft) async throws -> ModemDeckContact {
        let encodedID = id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id
        let response = try await decode(
            ModemDeckContactResponse.self,
            path: "/api/v1/contacts/\(encodedID)",
            method: "PUT",
            body: try encoder.encode(draft)
        )
        return response.contact
    }

    func deleteContact(_ contact: ModemDeckContact) async throws {
        guard let revision = contact.revision, revision > 0 else {
            throw ModemDeckAPIError.invalidResponse
        }
        let encodedID = contact.id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? contact.id
        _ = try await data(
            path: path(
                "/api/v1/contacts/\(encodedID)",
                queryItems: [URLQueryItem(name: "revision", value: String(revision))]
            ),
            method: "DELETE"
        )
    }

    func deleteContacts(_ contacts: [ModemDeckContact]) async throws {
        struct Identity: Encodable {
            let id: String
            let revision: Int
        }
        struct Payload: Encodable {
            let action: String
            let contacts: [Identity]
        }
        let identities = try contacts.map { contact -> Identity in
            guard let revision = contact.revision, revision > 0 else {
                throw ModemDeckAPIError.invalidResponse
            }
            return Identity(id: contact.id, revision: revision)
        }
        _ = try await data(
            path: "/api/v1/contacts/batch",
            method: "PATCH",
            body: try encoder.encode(Payload(action: "delete", contacts: identities))
        )
    }

    func revokePairing() async throws {
        _ = try await data(path: "/api/v1/mobile/pairing", method: "DELETE")
    }

    private func allPages<Response: Decodable, Item>(
        _ responseType: Response.Type,
        base: String,
        queryItems: [URLQueryItem] = [],
        items: KeyPath<Response, [Item]>,
        meta: KeyPath<Response, ModemDeckPageMeta>
    ) async throws -> [Item] {
        var result: [Item] = []
        var cursor = ""
        var seenCursors = Set<String>()
        repeat {
            var query = queryItems + [URLQueryItem(name: "limit", value: "100")]
            if !cursor.isEmpty { query.append(URLQueryItem(name: "cursor", value: cursor)) }
            let response = try await decode(responseType, path: path(base, queryItems: query))
            result.append(contentsOf: response[keyPath: items])
            let page = response[keyPath: meta]
            if !page.hasMore { return result }
            cursor = page.nextCursor
            guard !cursor.isEmpty, seenCursors.insert(cursor).inserted else {
                throw ModemDeckAPIError.invalidResponse
            }
        } while !cursor.isEmpty
        return result
    }

    private func listPath(_ base: String, query: String, cursor: String = "") -> String {
        var items = [URLQueryItem(name: "limit", value: "100")]
        let normalized = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !normalized.isEmpty {
            items.insert(URLQueryItem(name: "q", value: normalized), at: 0)
        }
        if !cursor.isEmpty {
            items.append(URLQueryItem(name: "cursor", value: cursor))
        }
        return path(base, queryItems: items)
    }

    private func messageCacheKey(lineID: String, peer: String) -> String {
        let identity = SHA256.hash(data: Data("\(lineID)\u{0}\(peer)".utf8))
            .map { String(format: "%02x", $0) }
            .joined()
        return "messages-\(identity)"
    }

    private func path(_ base: String, queryItems: [URLQueryItem]) -> String {
        var components = URLComponents()
        components.path = base
        components.queryItems = queryItems
        return components.string ?? base
    }

    private func decode<T: Decodable>(
        _ type: T.Type,
        path: String,
        method: String = "GET",
        headers: [String: String] = [:],
        body: Data? = nil,
        credential: ModemDeckCredential? = nil
    ) async throws -> T {
        let payload = try await data(
            path: path,
            method: method,
            headers: headers,
            body: body,
            credential: credential
        )
        do {
            return try decoder.decode(type, from: payload)
        } catch {
            throw ModemDeckAPIError.invalidResponse
        }
    }

    private func data(
        path: String,
        method: String = "GET",
        headers suppliedHeaders: [String: String] = [:],
        body: Data? = nil,
        credential suppliedCredential: ModemDeckCredential? = nil
    ) async throws -> Data {
        let credential: ModemDeckCredential
        if let suppliedCredential {
            credential = suppliedCredential
        } else if let loaded = try credentialStore.load() {
            credential = loaded
        } else {
            throw ModemDeckAPIError.notPaired
        }
        var headers = suppliedHeaders
        headers["Accept"] = headers["Accept"] ?? "application/json"
        if body != nil {
            headers["Content-Type"] = "application/json"
        }
        let request = try authorizedRequest(
            credential: credential,
            path: path,
            method: method,
            headers: headers,
            body: body,
            timeout: 30
        )
        let payload: Data
        let response: URLResponse
        do {
            (payload, response) = try await session.data(for: request)
        } catch {
            if let error = error as? URLError, error.code != .cancelled {
                await reportConnectivity(false, credential: credential, verifying: suppliedCredential != nil)
            }
            throw error
        }
        // A late response from a revoked/replaced pairing must not populate the
        // new account's cache or change its connectivity state.
        if suppliedCredential == nil, !isCurrentCredential(credential) {
            throw CancellationError()
        }
        guard let response = response as? HTTPURLResponse else {
            throw ModemDeckAPIError.invalidResponse
        }
        guard (200..<300).contains(response.statusCode) else {
            if [502, 503, 504].contains(response.statusCode) {
                await reportConnectivity(false, credential: credential, verifying: suppliedCredential != nil)
            }
            let serverError = try? decoder.decode(ModemDeckServerError.self, from: payload)
            let message = serverError?.message?.trimmingCharacters(in: .whitespacesAndNewlines)
            if response.statusCode == 401, suppliedCredential == nil {
                DispatchQueue.main.async {
                    guard self.isCurrentCredential(credential) else { return }
                    NotificationCenter.default.post(
                        name: .modemDeckAuthenticationFailed,
                        object: nil
                    )
                }
            }
            throw ModemDeckAPIError.server(
                status: response.statusCode,
                code: serverError?.code ?? "http_\(response.statusCode)",
                message: message?.isEmpty == false
                    ? message!
                    : "ModemDeck request failed (HTTP \(response.statusCode))."
            )
        }
        await reportConnectivity(true, credential: credential, verifying: suppliedCredential != nil)
        if suppliedCredential == nil, !isCurrentCredential(credential) { throw CancellationError() }
        return payload
    }

    private func isCurrentCredential(_ credential: ModemDeckCredential) -> Bool {
        guard let current = try? credentialStore.load() else { return false }
        return current.serverURL == credential.serverURL && current.token == credential.token
    }

    private func reportConnectivity(_ reachable: Bool, credential: ModemDeckCredential, verifying: Bool) async {
        guard !verifying else { return }
        await MainActor.run {
            guard self.isCurrentCredential(credential) else { return }
            NotificationCenter.default.post(
                name: .modemDeckRequestConnectivity,
                object: self,
                userInfo: ["reachable": reachable]
            )
        }
    }

    private func operationIdentity(
        kind: String,
        parts: [String]
    ) throws -> ModemDeckOperationIdentity {
        guard let credential = try credentialStore.load() else {
            throw ModemDeckAPIError.notPaired
        }
        let scope = SHA256.hash(
            data: Data("\(credential.serverURL)\u{0}\(credential.token)".utf8)
        ).map { String(format: "%02x", $0) }.joined()
        return operationJournal.identity(scope: scope, kind: kind, parts: parts)
    }

    private func preservesOperationIdentity(_ error: Error) -> Bool {
        if let apiError = error as? ModemDeckAPIError {
            switch apiError {
            case .invalidResponse:
                return true
            case .server(let status, _, _):
                return status == 408 || status == 409 || status == 425 ||
                    status == 429 || status >= 500
            case .notPaired:
                return false
            }
        }
        if let urlError = error as? URLError {
            return urlError.code != .cancelled
        }
        return true
    }
}
