import CryptoKit
import Foundation

/// The UI language for error text, set by the session controller (errors are described off the main actor).
enum ModemDeckErrorLanguage {
    private static let lock = NSLock()
    private static var chinese = Locale.preferredLanguages.first?.lowercased().hasPrefix("zh") ?? false

    static var usesChinese: Bool {
        lock.lock(); defer { lock.unlock() }
        return chinese
    }

    static func setChinese(_ value: Bool) {
        lock.lock(); chinese = value; lock.unlock()
    }

    static func text(_ chinese: String, _ english: String) -> String {
        usesChinese ? chinese : english
    }
}

enum ModemDeckAPIError: LocalizedError {
    case notPaired
    case invalidResponse
    case server(status: Int, code: String, message: String)

    var errorDescription: String? {
        let text = ModemDeckErrorLanguage.text
        switch self {
        case .notPaired:
            return text("此设备尚未与 ModemDeck 配对。", "This device is not paired with ModemDeck.")
        case .invalidResponse:
            return text("ModemDeck 返回了无法识别的响应，请稍后重试。", "ModemDeck returned an invalid response. Try again shortly.")
        case .server(let status, let code, let message):
            // Server text is written for developers and never shown: known codes and status
            // classes get a translated, actionable message, anything else a plain one.
            if let known = Self.knownMessage(code: code) { return known }
            switch status {
            case 401: return text("登录已过期，请重新配对或登录。", "Your session has expired. Pair or sign in again.")
            case 403: return text("你没有执行此操作的权限。", "You don't have permission to do this.")
            case 500...: return text("ModemDeck 出现问题，请稍后重试。", "ModemDeck ran into a problem. Try again in a moment.")
            // Anything else is developer text in one language; say plainly that it failed.
            default: return text("请求未能完成，请稍后重试。", "The request couldn't be completed. Try again.")
            }
        }
    }

    private static func knownMessage(code: String) -> String? {
        let text = ModemDeckErrorLanguage.text
        switch code {
        case "microphone_permission_denied":
            return text("拨打电话前需要允许麦克风访问。", "Microphone access is required before placing a call.")
        case "not_found", "contact_not_found", "message_thread_not_found", "call_not_found",
             "recording_not_found", "user_not_found", "line_not_found":
            return text("该项目已不存在。", "This item no longer exists.")
        case "revision_conflict", "conflict":
            return text("内容已在其他地方被修改，请刷新后重试。", "This was changed somewhere else. Refresh and try again.")
        case "phone_conflict":
            return text("该号码已属于其他联系人。", "This number already belongs to another contact.")
        case "communications_unavailable":
            return text("模组服务暂时不可用，请稍后重试。", "The modem service is unavailable right now. Try again shortly.")
        case "media_in_use":
            return text("通话音频正在其他设备上使用。", "Call audio is in use on another device.")
        case "call_lease_unavailable":
            return text("该通话正由其他设备处理。", "This call is being handled on another device.")
        case "recording_unavailable":
            return text("录音暂时不可用。", "Recording isn't available right now.")
        case "invalid_line":
            return text("请选择可用线路。", "Choose an available line.")
        case "network_rejected":
            return text("移动网络拒绝了此请求。", "The mobile network rejected this request.")
        case "not_supported":
            return text("此模组不支持该操作。", "This modem doesn't support that.")
        case "invalid_current_password":
            return text("当前密码不正确。", "The current password is incorrect.")
        case "password_too_short":
            return text("密码太短。", "The password is too short.")
        case "password_unchanged":
            return text("新密码不能与当前密码相同。", "Choose a password different from the current one.")
        default:
            return nil
        }
    }

    /// The text for a failed password change. The password screen names the new password and
    /// its length, so its words differ from `knownMessage` for the same codes, and it also
    /// names `password_too_long` and `password_invalid`. `chinese` is the app's language.
    func passwordChangeDescription(chinese: Bool) -> String {
        let text = { (chineseText: String, englishText: String) in chinese ? chineseText : englishText }
        switch code {
        case "invalid_current_password":
            return text("当前密码不正确。", "The current password is incorrect.")
        case "password_too_short":
            return text("新密码至少需要 8 个字符。", "The new password needs at least 8 characters.")
        case "password_too_long", "password_invalid":
            return text("新密码包含不支持的内容。", "The new password contains unsupported content.")
        case "password_unchanged":
            return text("新密码不能与当前密码相同。", "The new password must differ from the current password.")
        default:
            return localizedDescription
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

/// One URL path segment built from an identifier. Only the unreserved characters
/// (letters, digits, `-`, `.`, `_`, `~`) pass through, so a `/`, `?` or `#` in an
/// identifier can never address another endpoint, and an identifier made only of
/// dots cannot be collapsed as a relative path segment.
func modemDeckPathSegment(_ identifier: String) -> String {
    if !identifier.isEmpty, identifier.allSatisfy({ $0 == "." }) {
        return String(repeating: "%2E", count: identifier.count)
    }
    let unreserved = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
    )
    return identifier.addingPercentEncoding(withAllowedCharacters: unreserved) ?? ""
}

private final class ModemDeckOfflineCache {
    private let credentialStore: ModemDeckCredentialStore
    private let fileManager: FileManager
    private let queue = DispatchQueue(label: "modemdeck.offline-cache", qos: .utility)
    private let rootURL: URL?

    init(
        credentialStore: ModemDeckCredentialStore,
        fileManager: FileManager = .default
    ) {
        self.credentialStore = credentialStore
        self.fileManager = fileManager
        do {
            rootURL = try fileManager.url(
                for: .applicationSupportDirectory,
                in: .userDomainMask,
                appropriateFor: nil,
                create: true
            )
            .appendingPathComponent("ModemDeck", isDirectory: true)
            .appendingPathComponent("OfflineCache", isDirectory: true)
            .appendingPathComponent("v1", isDirectory: true)
        } catch {
            // Without it nothing is cached and every write is skipped.
            rootURL = nil
            ModemDeckDiagnostics.shared.record(.storage, "cache_unavailable", error: error)
        }
    }

    func read<T: Decodable>(_ type: T.Type, key: String) -> T? {
        guard let fileURL = scopedFileURL(key: key) else { return nil }
        return queue.sync {
            guard let data = try? Data(contentsOf: fileURL) else { return nil }
            return try? JSONDecoder().decode(type, from: data)
        }
    }

    /// For state that must not be lost to a crash right after it changes.
    func writeNow<T: Encodable>(_ value: T, key: String) {
        guard let fileURL = scopedFileURL(key: key) else { return }
        queue.sync {
            guard let data = try? JSONEncoder().encode(value) else { return }
            write(data, to: fileURL)
        }
    }

    func write<T: Encodable>(_ value: T, key: String, credential: ModemDeckCredential? = nil) {
        // Capture the pairing scope before queueing: a pending write must never
        // land in a replacement account's cache. The serial queue preserves order.
        guard let fileURL = scopedFileURL(key: key, credential: credential) else { return }
        queue.async { [self] in
            guard let data = try? JSONEncoder().encode(value) else { return }
            write(data, to: fileURL)
        }
    }

    private func write(_ data: Data, to fileURL: URL) {
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
            // Messages and contacts must not reach a backup: no exclusion, no file.
            try protectedDirectory.setResourceValues(directoryValues)
            try data.write(
                to: fileURL,
                options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication]
            )
        } catch {
            // A cache write must never block live API data.
            ModemDeckDiagnostics.shared.record(.storage, "cache_write_failed", error: error)
        }
    }

    func clearCurrentScope() {
        guard let directory = scopedDirectoryURL() else { return }
        queue.async { [self] in try? fileManager.removeItem(at: directory) }
    }

    private func scopedFileURL(key: String, credential: ModemDeckCredential? = nil) -> URL? {
        scopedDirectoryURL(credential: credential)?.appendingPathComponent(digest(key), isDirectory: false)
    }

    private func scopedDirectoryURL(credential: ModemDeckCredential? = nil) -> URL? {
        guard let rootURL,
              let credential = credential ?? (try? credentialStore.load()) else {
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
    let wssAudio: Bool
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

extension ModemDeckLine {
    /// Registered for ordinary service, by the server's own rule (a connected or registered
    /// modem, or a home or roaming registration), so every screen agrees about a line.
    var registered: Bool {
        let modem = (state ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if modem == "connected" || modem == "registered" { return true }
        let registration = registrationState.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return [
            "home", "home-sms-only", "home-csfb-not-preferred",
            "roaming", "roaming-sms-only", "roaming-csfb-not-preferred"
        ].contains(registration)
    }
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

extension ModemDeckBootstrap {
    /// A line from the catalog, which names every line the account can see; history rows
    /// and settings use it, so a line that is offline now still has its name.
    func catalogLine(id: String) -> ModemDeckLine? {
        lineCatalog.first { $0.id == id }
    }

    /// A line from the live lines. With `singleLineFallback`, an empty id on a device with
    /// exactly one line means that line (a call that has not reported its line yet).
    func liveLine(id: String, singleLineFallback: Bool) -> ModemDeckLine? {
        lines.first { $0.id == id } ??
            (singleLineFallback && id.isEmpty && lines.count == 1 ? lines.first : nil)
    }
}

struct ModemDeckPageMeta: Decodable, Equatable {
    let limit: Int
    let nextCursor: String
    let hasMore: Bool
}

/// Fetches a listing's first page and follows `next_cursor` until the server says there is no
/// more, or until `wantsMore` says to stop, and returns the cursor of the first page not fetched
/// ("" when the listing ended). A cursor the server already handed out is an invalid response,
/// so a server that repeats one cannot keep the client looping. A page that has more but no
/// cursor ends the listing when `emptyCursorEnds`, and is an invalid response otherwise.
/// It runs on the caller's actor, so `wantsMore` and `fetch` may read the caller's state.
func modemDeckFollowPages(
    isolation: isolated (any Actor)? = #isolation,
    emptyCursorEnds: Bool,
    wantsMore: () -> Bool,
    fetch: (_ cursor: String) async throws -> ModemDeckPageMeta
) async throws -> String {
    var cursor = ""
    var seenCursors = Set<String>()
    while true {
        let page = try await fetch(cursor)
        guard page.hasMore else { return "" }
        guard !page.nextCursor.isEmpty else {
            if emptyCursorEnds { return "" }
            throw ModemDeckAPIError.invalidResponse
        }
        guard wantsMore() else { return page.nextCursor }
        guard seenCursors.insert(page.nextCursor).inserted else { throw ModemDeckAPIError.invalidResponse }
        cursor = page.nextCursor
    }
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

        guard let identity = ModemDeckPhoneIdentity.canonical(number) else { return nil }
        let matches = filter { contact in
            contact.phones.contains { phone in
                if let canonical = ModemDeckPhoneIdentity.canonical(phone.canonicalE164 ?? "") {
                    return canonical == identity
                }
                return ModemDeckPhoneIdentity.canonical(phone.displayNumber) == identity
            }
        }
        return matches.count == 1 ? matches.first : nil
    }
}

extension Collection where Element == ModemDeckMessageThread {
    func modemDeckThread(lineID: String, number: String) -> ModemDeckMessageThread? {
        guard !ModemDeckPhoneIdentity.compact(number).isEmpty else { return nil }
        return first { thread in
            thread.lineId == lineID && ModemDeckPhoneIdentity.sameThreadPeer(number, thread.peer)
        }
    }
}

/// The client's rules for telling whether two written numbers name the same party. The server
/// is the authority (it parses with a number library and the line's home region); these rules
/// only match what the client already has. The rules differ by purpose, and each is kept as it
/// was: contacts match only on a full `+` number, conversations fall back to the digits as
/// written, avatars also read a `00` prefix as `+`, and contact import keeps `+` and digits.
enum ModemDeckPhoneIdentity {
    /// Separators people write inside a number.
    static let separators = " ().-/"

    /// The number with its separators removed.
    static func compact(_ value: String) -> String {
        value.filter { !separators.contains($0) }
    }

    /// An ASCII digit; other scripts' digits are not phone digits here.
    static func isASCIIDigit(_ character: Character) -> Bool {
        character.unicodeScalars.count == 1 &&
            character.unicodeScalars.first.map { (48...57).contains($0.value) } == true
    }

    /// A `+` number of 8 to 15 ASCII digits not starting with 0, without separators;
    /// nil for anything else, including a national number.
    static func canonical(_ value: String) -> String? {
        let source = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !source.isEmpty else { return nil }
        let written = compact(source)
        guard written.first == "+" else { return nil }
        let digits = written.dropFirst()
        guard (8...15).contains(digits.count),
              digits.first != "0",
              digits.allSatisfy(isASCIIDigit) else {
            return nil
        }
        return written
    }

    /// Whether a typed number and a conversation's peer are the same party: by their
    /// canonical form when both have one, otherwise by the numbers without separators.
    static func sameThreadPeer(_ number: String, _ peer: String) -> Bool {
        let compactNumber = compact(number)
        let compactPeer = compact(peer)
        if let identity = canonical(compactNumber), let peerIdentity = canonical(compactPeer) {
            return identity == peerIdentity
        }
        return compactPeer == compactNumber
    }

    /// The key an avatar compares a name and an address by. Text that is not a number is
    /// lower-cased with its whitespace collapsed; a number loses its separators and a
    /// leading `00` before a digit becomes `+`.
    static func avatarKey(_ value: String) -> String {
        let source = value.trimmingCharacters(in: .whitespacesAndNewlines)
            .precomposedStringWithCompatibilityMapping
            .lowercased()
        guard !source.isEmpty else { return "" }
        let allowed = CharacterSet(charactersIn: "+0123456789" + separators)
        guard source.unicodeScalars.allSatisfy({ allowed.contains($0) }) else {
            return source.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        }
        var written = compact(source)
        if written.hasPrefix("00"), written.dropFirst(2).first?.isNumber == true {
            written = "+" + written.dropFirst(2)
        }
        return written
    }

    /// The key contact import removes a contact's repeated numbers by: `+` and every
    /// character Swift counts as a number, in any script.
    static func importKey(_ value: String) -> String {
        value.filter { $0 == "+" || $0.isNumber }
    }
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
    /// True when do-not-disturb held this incoming call back on arrival. Absent
    /// from servers and cached records that predate the field.
    var doNotDisturb: Bool? = nil

    var displayName: String {
        let name = contactName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? remoteNumber : name
    }

    /// An incoming call nobody was rung for because do-not-disturb applied.
    var screened: Bool { doNotDisturb == true && direction == "incoming" && durationSeconds == 0 }

    /// The one description of how a call went, shared by every list and detail.
    var outcomeIcon: String {
        if screened { return "moon.fill" }
        if missed { return "phone.arrow.down.left" }
        return direction == "incoming" ? "arrow.down.left" : "arrow.up.right"
    }

    func outcomeText(_ text: (String, String) -> String) -> String {
        if screened { return text("免打扰已拦截", "Blocked by Do Not Disturb") }
        if missed { return text("未接来电", "Missed") }
        return direction == "incoming" ? text("呼入", "Incoming") : text("呼出", "Outgoing")
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

struct ModemDeckCallPreparation: Decodable {
    let preparationId: String
    let mediaSocketPath: String
    let recordingEnabled: Bool
    var clientSessionID: String?
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

/// A line's own incoming-call policy and what it resolves to right now.
struct ModemDeckLineIncomingCalls: Decodable, Equatable {
    struct Enforcement: Decodable, Equatable {
        /// False when the line cannot reject a call: do-not-disturb then only silences it.
        let available: Bool
    }

    /// `follow_global`, `receive` or `do_not_disturb`.
    let policy: String
    let revision: Int
    /// `receive` or `do_not_disturb`, after resolving `follow_global`.
    let effectivePolicy: String
    let globalReceiveCalls: Bool
    let enforcement: Enforcement
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
        return model.isEmpty ? ModemDeckErrorLanguage.text("Apple 设备", "Apple device") : model
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

struct ModemDeckAudioTestStatus: Decodable {
    let id: String
    let phase: String
    let testPhase: String
    let failureCode: String?

    enum CodingKeys: String, CodingKey {
        case id, phase
        case testPhase = "test_phase"
        case failureCode = "failure_code"
    }
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

// The local copy of an item after a state action the server has accepted. Every field the
// action does not change is carried over; `.delete` returns the item unchanged, since callers
// remove deleted items themselves.

extension ModemDeckMessageThread {
    func applying(_ action: ModemDeckMessageThreadAction) -> ModemDeckMessageThread {
        ModemDeckMessageThread(
            key: key,
            lineId: lineId,
            peer: peer,
            contactId: contactId,
            contactName: contactName,
            lastMessageId: lastMessageId,
            lastTimestamp: lastTimestamp,
            lastContent: lastContent,
            unreadCount: action == .read ? 0 : unreadCount,
            firstUnreadMessageId: action == .read ? nil : firstUnreadMessageId,
            markedUnread: action == .unread ? true : (action == .read ? false : markedUnread),
            favorite: action == .favorite ? true : (action == .unfavorite ? false : favorite)
        )
    }
}

extension ModemDeckCallRecord {
    func applying(_ action: ModemDeckCallBatchAction) -> ModemDeckCallRecord {
        ModemDeckCallRecord(
            id: id,
            lineId: lineId,
            direction: direction,
            remoteNumber: remoteNumber,
            contactName: contactName,
            contactId: contactId,
            startedAt: startedAt,
            endedAt: endedAt,
            durationSeconds: durationSeconds,
            missed: missed,
            read: action == .read ? true : (action == .unread ? false : read),
            favorite: action == .favorite ? true : (action == .unfavorite ? false : favorite),
            failureCode: failureCode,
            doNotDisturb: doNotDisturb
        )
    }
}

extension ModemDeckRecording {
    func applying(_ action: ModemDeckRecordingBatchAction) -> ModemDeckRecording {
        ModemDeckRecording(
            segment: segment,
            call: call,
            playable: playable,
            favorite: action == .favorite ? true : (action == .unfavorite ? false : favorite)
        )
    }
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

struct ModemDeckCallResponse: Decodable {
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

struct ModemDeckServerError: Decodable {
    let code: String?
    let message: String?
    let detail: String?

    /// The error body of a failed response; nil when there is no body or it is not one.
    /// Each caller still turns it into its own error and text.
    static func decode(_ data: Data?) -> ModemDeckServerError? {
        guard let data else { return nil }
        return try? JSONDecoder().decode(ModemDeckServerError.self, from: data)
    }
}

final class ModemDeckAPIClient {
    let credentialStore: ModemDeckCredentialStore

    private let session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder
    private let offlineCache: ModemDeckOfflineCache

    init(credentialStore: ModemDeckCredentialStore) {
        self.credentialStore = credentialStore
        offlineCache = ModemDeckOfflineCache(credentialStore: credentialStore)
        // Earlier builds kept SMS request IDs here; the stored outbox replaced it.
        UserDefaults.standard.removeObject(forKey: "modemdeck.pending-operation-identities.v1")
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 60
        session = ModemDeckDiagnostics.urlSession(configuration: configuration)
        decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        encoder = JSONEncoder()
        encoder.keyEncodingStrategy = .convertToSnakeCase
    }

    func verify(_ credential: ModemDeckCredential) async throws -> ModemDeckMobileSession {
        try await decode(
            ModemDeckMobileSession.self,
            path: "/api/v1/mobile/session",
            credential: credential,
            pairingVerification: true
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

    // Nonisolated async work runs off the main actor. Store initializers must
    // not read/decode the entire offline history before the first window opens.
    func cachedStartupData() async -> (
        session: ModemDeckMobileSession?, bootstrap: ModemDeckBootstrap?,
        contacts: [ModemDeckContact], threads: [ModemDeckMessageThread],
        unread: ModemDeckUnreadSummary, calls: [ModemDeckCallRecord], recordings: [ModemDeckRecording]
    ) {
        (cachedMobileSession(), cachedBootstrap(), cachedContacts(), cachedMessageThreads(),
         cachedUnreadSummary(), cachedCalls(), cachedRecordings())
    }

    func clearCachedData() {
        offlineCache.clearCurrentScope()
    }

    func contacts(query: String = "") async throws -> [ModemDeckContact] {
        guard let credential = try credentialStore.load() else { throw ModemDeckAPIError.notPaired }
        let contacts = try await allPages(
            ModemDeckContactsResponse.self,
            credential: credential,
            base: "/api/v1/contacts",
            queryItems: [URLQueryItem(name: "q", value: query)],
            items: \.contacts, meta: \.meta
        )
        guard !Task.isCancelled, isCurrentCredential(credential) else { throw CancellationError() }
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            offlineCache.write(contacts, key: "contacts", credential: credential)
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
        _ = try await modemDeckFollowPages(emptyCursorEnds: true, wantsMore: { true }) { cursor in
            let response = try await decode(
                ModemDeckThreadsResponse.self,
                path: listPath("/api/v1/messages/threads", query: query, cursor: cursor)
            )
            result.append(contentsOf: response.threads)
            return response.meta
        }
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

    func cachedOutbox() -> [ModemDeckOutgoingMessage] {
        offlineCache.read([ModemDeckOutgoingMessage].self, key: "message-outbox") ?? []
    }

    /// Written before this returns: an unconfirmed SMS must be on disk before its request leaves.
    func cacheOutbox(_ outbox: [ModemDeckOutgoingMessage]) {
        offlineCache.writeNow(outbox, key: "message-outbox")
    }

    /// `requestID` belongs to one unconfirmed SMS in the messages store's outbox, which keeps it
    /// across retries and relaunches for as long as the outcome is unknown.
    func sendMessage(lineID: String, to: String, content: String, requestID: String) async throws -> ModemDeckMessage {
        struct Payload: Encodable {
            let requestId: String
            let lineId: String
            let to: String
            let content: String
        }
        let payload = Payload(requestId: requestID, lineId: lineID, to: to, content: content)
        let response = try await decode(
            ModemDeckMessageResponse.self,
            path: "/api/v1/messages",
            method: "POST",
            headers: ["Idempotency-Key": requestID],
            body: try encoder.encode(payload)
        )
        return response.message
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
        // Each answer is the unread summary after that batch; the last one is current.
        var latest: ModemDeckUnreadSummary?
        try await inBatches(threads) { batch in
            latest = try await decode(
                ModemDeckUnreadSummary.self,
                path: "/api/v1/messages/threads/state",
                method: "PATCH",
                body: try encoder.encode(Payload(
                    action: action,
                    threads: batch.map {
                        Identity(
                            lineId: $0.lineId,
                            peer: $0.peer,
                            throughMessageId: action == .read ? $0.lastMessageId : 0
                        )
                    }
                ))
            )
        }
        // Nothing to change leaves the summary as it was.
        guard let summary = latest else { return cachedUnreadSummary() }
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

    func calls() async throws -> [ModemDeckCallRecord] {
        guard let credential = try credentialStore.load() else { throw ModemDeckAPIError.notPaired }
        let calls = try await allPages(
            ModemDeckCallsResponse.self,
            credential: credential,
            base: "/api/v1/calls",
            queryItems: [URLQueryItem(name: "kind", value: "all")],
            items: \.calls, meta: \.meta
        )
        guard !Task.isCancelled, isCurrentCredential(credential) else { throw CancellationError() }
        offlineCache.write(calls, key: "calls", credential: credential)
        return calls
    }

    func cachedCalls() -> [ModemDeckCallRecord] {
        offlineCache.read([ModemDeckCallRecord].self, key: "calls") ?? []
    }

    func cacheCalls(_ calls: [ModemDeckCallRecord]) {
        offlineCache.write(calls, key: "calls")
    }

    func recordings() async throws -> [ModemDeckRecording] {
        guard let credential = try credentialStore.load() else { throw ModemDeckAPIError.notPaired }
        let items = try await allPages(
            ModemDeckRecordingsResponse.self,
            credential: credential,
            base: "/api/v1/recordings", items: \.recordings, meta: \.meta
        )
        guard !Task.isCancelled, isCurrentCredential(credential) else { throw CancellationError() }
        let recordings = items.map {
            ModemDeckRecording(
                segment: $0.segment,
                call: $0.call,
                playable: $0.playable,
                favorite: $0.favorite
            )
        }
        offlineCache.write(recordings, key: "recordings", credential: credential)
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
        try await inBatches(ids) { batch in
            _ = try await data(
                path: "/api/v1/calls/batch",
                method: "PATCH",
                body: try encoder.encode(Payload(action: action, ids: batch))
            )
        }
    }

    /// The server accepts at most this many items in one batch request.
    static let batchLimit = 100

    /// Sends a selection of any size as requests the server accepts, in order. A failure
    /// stops there: the batches already sent stay applied on the server.
    private func inBatches<Item>(_ items: [Item], _ send: ([Item]) async throws -> Void) async throws {
        for start in stride(from: 0, to: items.count, by: Self.batchLimit) {
            try await send(Array(items[start..<min(start + Self.batchLimit, items.count)]))
        }
    }

    func deleteCall(id: String) async throws {
        let encodedID = modemDeckPathSegment(id)
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
        try await inBatches(recordings) { batch in
            _ = try await data(
                path: "/api/v1/recordings/batch",
                method: "PATCH",
                body: try encoder.encode(Payload(
                    action: action,
                    recordings: batch.map { Identity(callId: $0.call.id, id: $0.id) }
                ))
            )
        }
    }

    func deleteRecording(callID: String, recordingID: String) async throws {
        let encodedCallID = modemDeckPathSegment(callID)
        let encodedRecordingID = modemDeckPathSegment(recordingID)
        _ = try await data(
            path: "/api/v1/calls/\(encodedCallID)/recordings/\(encodedRecordingID)",
            method: "DELETE"
        )
    }

    func prepareCall(lineID: String, number: String, recordingEnabled: Bool?) async throws -> ModemDeckCallPreparation {
        struct Payload: Encodable {
            let requestId: String
            let lineId: String
            let number: String
            let recordingEnabled: Bool?
        }
        let sessionID = UUID().uuidString.lowercased()
        let started = ProcessInfo.processInfo.systemUptime
        ModemDeckDiagnostics.shared.record(.app, "outgoing_prepare_started", fields: ["session_id": sessionID])
        do {
            var preparation = try await decode(ModemDeckCallPreparation.self, path: "/api/v1/calls/prepare", method: "POST",
                body: try encoder.encode(Payload(requestId: sessionID, lineId: lineID,
                    number: number, recordingEnabled: recordingEnabled)))
            ModemDeckDiagnostics.shared.record(.app, "outgoing_prepare_completed", callID: preparation.preparationId,
                fields: ["session_id": sessionID, "preparation_id": preparation.preparationId,
                    "elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - started) * 1000))])
            preparation.clientSessionID = sessionID
            return preparation
        } catch {
            ModemDeckDiagnostics.shared.record(.app, "outgoing_prepare_failed", fields: ["session_id": sessionID,
                "elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - started) * 1000))], error: error)
            throw error
        }
    }

    func cancelCallPreparation(_ id: String) async throws {
        let escaped = modemDeckPathSegment(id)
        _ = try await data(path: "/api/v1/calls/preparations/\(escaped)", method: "DELETE")
    }

    func callRecording(callID: String) async throws -> ModemDeckCallRecordingState {
        let encodedID = modemDeckPathSegment(callID)
        let response = try await decode(
            ModemDeckCallRecordingsResponse.self,
            path: "/api/v1/calls/\(encodedID)/recordings"
        )
        return response.state
    }

    func callAction(callID: String, action: String) async throws {
        struct Payload: Encodable { let requestId: String }
        let encodedID = modemDeckPathSegment(callID)
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
        let encodedID = modemDeckPathSegment(callID)
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

    func lineIncomingCalls(lineID: String) async throws -> ModemDeckLineIncomingCalls {
        try await lineIncomingCalls(lineID: lineID, method: "GET", body: nil)
    }

    func updateLineIncomingCallPolicy(
        lineID: String,
        policy: String,
        revision: Int
    ) async throws -> ModemDeckLineIncomingCalls {
        struct Payload: Encodable {
            let operation = "set_incoming_call_policy"
            let incomingCallPolicy: String
            let expectedPolicyRevision: Int
        }
        return try await lineIncomingCalls(
            lineID: lineID,
            method: "PATCH",
            body: try encoder.encode(Payload(
                incomingCallPolicy: policy,
                expectedPolicyRevision: revision
            ))
        )
    }

    private func lineIncomingCalls(
        lineID: String,
        method: String,
        body: Data?
    ) async throws -> ModemDeckLineIncomingCalls {
        struct Response: Decodable { let incomingCalls: ModemDeckLineIncomingCalls? }
        let response = try await decode(
            Response.self,
            path: "/api/v1/devices/\(modemDeckPathSegment(lineID))/configuration",
            method: method,
            body: body
        )
        guard let incomingCalls = response.incomingCalls else { throw ModemDeckAPIError.invalidResponse }
        return incomingCalls
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
        let encodedID = modemDeckPathSegment(id)
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
        let encodedCallID = modemDeckPathSegment(callID)
        let encodedSegmentID = modemDeckPathSegment(segmentID)
        return try await data(
            path: "/api/v1/calls/\(encodedCallID)/recordings/\(encodedSegmentID)/download"
        )
    }

    func sendIOSTestCall(language: String = "en") async throws -> ModemDeckIOSTestCallResult {
        try await decode(
            ModemDeckIOSTestCallResult.self,
            path: "/api/v1/mobile/call-tests",
            method: "POST",
            headers: ["Accept-Language": language]
        )
    }

    func audioTestStatus(id: String) async throws -> ModemDeckAudioTestStatus {
        // API decoding converts snake_case automatically; the audio owner uses
        // a plain decoder, so decode this shared wire model explicitly.
        let encoded = modemDeckPathSegment(id)
        let response = try await data(path: "/api/v1/mobile/call-tests/\(encoded)/status")
        return try JSONDecoder().decode(ModemDeckAudioTestStatus.self, from: response)
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
        let encodedID = modemDeckPathSegment(id)
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
        let encodedID = modemDeckPathSegment(contact.id)
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
        try await inBatches(identities) { batch in
            _ = try await data(
                path: "/api/v1/contacts/batch",
                method: "PATCH",
                body: try encoder.encode(Payload(action: "delete", contacts: batch))
            )
        }
    }

    func revokePairing() async throws {
        _ = try await data(path: "/api/v1/mobile/pairing", method: "DELETE")
    }

    private func allPages<Response: Decodable, Item>(
        _ responseType: Response.Type,
        credential: ModemDeckCredential,
        base: String,
        queryItems: [URLQueryItem] = [],
        items: KeyPath<Response, [Item]>,
        meta: KeyPath<Response, ModemDeckPageMeta>
    ) async throws -> [Item] {
        var result: [Item] = []
        _ = try await modemDeckFollowPages(emptyCursorEnds: false, wantsMore: { true }) { cursor in
            guard !Task.isCancelled, isCurrentCredential(credential) else { throw CancellationError() }
            var query = queryItems + [URLQueryItem(name: "limit", value: "100")]
            if !cursor.isEmpty { query.append(URLQueryItem(name: "cursor", value: cursor)) }
            let response = try await decode(responseType, path: path(base, queryItems: query), credential: credential)
            guard !Task.isCancelled, isCurrentCredential(credential) else { throw CancellationError() }
            result.append(contentsOf: response[keyPath: items])
            return response[keyPath: meta]
        }
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
        // URLComponents leaves '+' literal, but Go's query parser reads it as a space.
        // Escape it after encoding so phone prefixes survive without double encoding.
        components.percentEncodedQuery = components.percentEncodedQuery?
            .replacingOccurrences(of: "+", with: "%2B")
        return components.string ?? base
    }

    private func decode<T: Decodable>(
        _ type: T.Type,
        path: String,
        method: String = "GET",
        headers: [String: String] = [:],
        body: Data? = nil,
        credential: ModemDeckCredential? = nil,
        pairingVerification: Bool = false
    ) async throws -> T {
        let payload = try await data(
            path: path,
            method: method,
            headers: headers,
            body: body,
            credential: credential,
            pairingVerification: pairingVerification
        )
        do {
            return try decoder.decode(type, from: payload)
        } catch {
            ModemDeckDiagnostics.shared.record(.api, "response_decode_failed", fields: ["route": path], error: error)
            throw ModemDeckAPIError.invalidResponse
        }
    }

    private func data(
        path: String,
        method: String = "GET",
        headers suppliedHeaders: [String: String] = [:],
        body: Data? = nil,
        credential suppliedCredential: ModemDeckCredential? = nil,
        pairingVerification: Bool = false
    ) async throws -> Data {
        let credential: ModemDeckCredential
        if let suppliedCredential {
            credential = suppliedCredential
        } else if let loaded = try credentialStore.load() {
            credential = loaded
        } else {
            throw ModemDeckAPIError.notPaired
        }
        if !pairingVerification, !isCurrentCredential(credential) { throw CancellationError() }
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
            (payload, response) = try await session.diagnosticData(for: request)
        } catch {
            if let error = error as? URLError, error.code != .cancelled {
                await reportConnectivity(false, credential: credential, verifying: pairingVerification)
            }
            throw error
        }
        // A late response from a revoked/replaced pairing must not populate the
        // new account's cache or change its connectivity state.
        if !pairingVerification, !isCurrentCredential(credential) {
            throw CancellationError()
        }
        guard let response = response as? HTTPURLResponse else {
            throw ModemDeckAPIError.invalidResponse
        }
        guard (200..<300).contains(response.statusCode) else {
            if [502, 503, 504].contains(response.statusCode) {
                await reportConnectivity(false, credential: credential, verifying: pairingVerification)
            }
            let serverError = ModemDeckServerError.decode(payload)
            let message = serverError?.message?.trimmingCharacters(in: .whitespacesAndNewlines)
            if response.statusCode == 401, !pairingVerification {
                await MainActor.run {
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
        await reportConnectivity(true, credential: credential, verifying: pairingVerification)
        if !pairingVerification, !isCurrentCredential(credential) { throw CancellationError() }
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

    /// True when the server may have accepted a write although its request failed: the
    /// retry must then carry the same request ID. A definite refusal gets a new one.
    func sendOutcomeUnknown(_ error: Error) -> Bool {
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
