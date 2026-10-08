import Foundation
import Network

/// One device-wide diagnostic sink. Only enumerated metadata crosses this boundary;
/// request/response bodies, headers, addresses and localized errors never enter it.
final class ModemDeckDiagnostics: @unchecked Sendable {
    static let uploadPreference = "modemdeck.diagnostics-upload-enabled"
    static let shared = ModemDeckDiagnostics()

    enum Category: String, Codable, CaseIterable { case app, network, api, pairing, push, callkit, audio, storage, permissions }
    struct Event: Codable {
        let id: String
        let timeMS: Int64
        let category: Category
        let name: String
        let network: String
        let callID: String?
        let fields: [String: String]
        enum CodingKeys: String, CodingKey {
            case id, category, name, network, fields
            case timeMS = "time_ms", callID = "call_id"
        }
    }
    struct Batch: Codable {
        let schema = 1
        let appVersion: String
        let appBuild: String
        let osVersion: String
        let dropped: Int
        let events: [Event]
        enum CodingKeys: String, CodingKey {
            case schema, dropped, events
            case appVersion = "app_version", appBuild = "app_build", osVersion = "os_version"
        }
    }
    struct Status {
        let enabled: Bool
        let pending: Int
        let dropped: Int
        let lastUpload: Date?
    }
    private struct Journal: Codable {
        var scope: String?
        var pending: [Event] = []
        var local: [Event] = []
        var dropped = 0
        var lastUpload: Date?
    }
    // Transport is separate from a call's URLSession, which is canceled on hangup.
    typealias Transport = (Data, @escaping (Int) -> Void) -> (() -> Void)
    private let queue = DispatchQueue(label: "modemdeck.diagnostics", qos: .utility)
    private let defaults: UserDefaults
    private let storageURL: URL?
    private let monitor: NWPathMonitor?
    private let uploadDelay: TimeInterval
    private let retryDelay: TimeInterval
    private var journal: Journal
    private var enabled: Bool
    private var network = "unknown"
    private var networkFields: [String: String] = [:]
    private var transport: Transport?
    private var authorization: String?
    private var cancelUpload: (() -> Void)?
    private var uploadScheduled: DispatchWorkItem?
    private var saveScheduled: DispatchWorkItem?
    private var generation = 0
    private var sending = false
    private var retryAttempt = 0
    private var blocked = false
    private var persistenceFailureReported = false
    private let capacity = 512

    init(defaults: UserDefaults = .standard, storageURL: URL? = ModemDeckDiagnostics.defaultStorageURL,
         monitorNetwork: Bool = true, uploadDelay: TimeInterval = 2, retryDelay: TimeInterval = 5) {
        self.defaults = defaults
        self.storageURL = storageURL
        self.uploadDelay = uploadDelay
        self.retryDelay = retryDelay
        enabled = defaults.bool(forKey: Self.uploadPreference)
        journal = storageURL.flatMap { try? Data(contentsOf: $0) }
            .flatMap { try? JSONDecoder().decode(Journal.self, from: $0) } ?? Journal()
        journal.local = Array(journal.local.suffix(128))
        if !enabled { journal.pending = []; journal.dropped = 0 }
        if journal.pending.count > capacity {
            journal.dropped += journal.pending.count - capacity
            journal.pending = Array(journal.pending.suffix(capacity))
        }
        monitor = monitorNetwork ? NWPathMonitor() : nil
        monitor?.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            let kind = path.status != .satisfied ? "offline" :
                (path.usesInterfaceType(.cellular) ? "cellular" :
                    (path.usesInterfaceType(.wifi) ? "wifi" :
                        (path.usesInterfaceType(.wiredEthernet) ? "wired" : "other")))
            self.network = kind
            self.networkFields = ["expensive": String(path.isExpensive), "constrained": String(path.isConstrained),
                                  "ipv4": String(path.supportsIPv4), "ipv6": String(path.supportsIPv6),
                                  "dns": String(path.supportsDNS)]
            self.append(.network, "path_changed", fields: self.networkFields)
            if kind != "offline" { self.scheduleUpload() }
        }
        monitor?.start(queue: queue)
    }

    deinit { monitor?.cancel(); cancelUpload?(); uploadScheduled?.cancel(); saveScheduled?.cancel() }

    static var defaultStorageURL: URL? {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first?
            .appendingPathComponent("ModemDeck/diagnostics.json")
    }

    func configure(scope: String?, authorization: String? = nil, transport: Transport? = nil) {
        queue.sync {
            if journal.scope != scope {
                cancelWork()
                journal = Journal(scope: scope)
            }
            journal.scope = scope
            self.authorization = authorization
            self.transport = transport
            blocked = false
            persist()
            scheduleUpload()
        }
    }

    func setUploadEnabled(_ value: Bool) {
        queue.sync {
            guard enabled != value else { return }
            enabled = value
            defaults.set(value, forKey: Self.uploadPreference)
            cancelWork()
            // Consent applies to subsequent events, never historical local logs.
            journal.pending = []
            journal.dropped = 0
            journal.lastUpload = nil
            append(.app, value ? "diagnostics_enabled" : "diagnostics_disabled")
            persist()
        }
    }

    func status() -> Status {
        queue.sync { Status(enabled: enabled, pending: journal.pending.count,
                            dropped: journal.dropped, lastUpload: journal.lastUpload) }
    }

    func record(_ category: Category, _ name: String, callID: String? = nil,
                fields: [String: String] = [:], error: Error? = nil, scope: String? = nil) {
        let safeFields = Self.sanitize(fields.merging(Self.errorFields(error)) { _, new in new })
        queue.async {
            guard scope == nil || scope == self.journal.scope else { return }
            self.append(category, name, callID: callID, fields: safeFields)
        }
    }

    func flush() {
        queue.async {
            self.persist()
            self.uploadScheduled?.cancel()
            self.uploadScheduled = nil
            self.scheduleUpload(after: 0)
        }
    }

    private func append(_ category: Category, _ name: String, callID: String? = nil, fields: [String: String] = [:]) {
        guard name.range(of: "^[a-z][a-z0-9_]{0,63}$", options: .regularExpression) != nil else { return }
        let event = Event(id: UUID().uuidString.lowercased(), timeMS: Int64(Date().timeIntervalSince1970 * 1_000),
                          category: category, name: name, network: network, callID: Self.safeCallID(callID),
                          fields: Self.sanitize(networkFields.merging(fields) { _, new in new }))
        journal.local.append(event)
        if journal.local.count > 128 { journal.local.removeFirst(journal.local.count - 128) }
        if enabled, journal.scope != nil {
            journal.pending.append(event)
            if journal.pending.count > capacity {
                journal.pending.removeFirst()
                journal.dropped += 1
            }
            scheduleUpload()
        }
        // The local sink uses exactly the same safe data as the upload sink.
        NSLog("ModemDeck diagnostic %@ %@ %@", category.rawValue, name, String(describing: event.fields))
        if saveScheduled == nil {
            let work = DispatchWorkItem { [weak self] in self?.saveScheduled = nil; self?.persist() }
            saveScheduled = work
            queue.asyncAfter(deadline: .now() + 0.25, execute: work)
        }
    }

    private func scheduleUpload(after delay: TimeInterval? = nil) {
        guard enabled, !blocked, !sending, uploadScheduled == nil, transport != nil,
              journal.scope != nil, !journal.pending.isEmpty, network != "offline" else { return }
        let work = DispatchWorkItem { [weak self] in self?.uploadScheduled = nil; self?.upload() }
        uploadScheduled = work
        queue.asyncAfter(deadline: .now() + (delay ?? uploadDelay), execute: work)
    }

    private func upload() {
        guard enabled, !blocked, !sending, let transport, journal.scope != nil,
              !journal.pending.isEmpty, network != "offline" else { return }
        let events = Array(journal.pending.prefix(24))
        let dropped = journal.dropped
        let os = ProcessInfo.processInfo.operatingSystemVersion
        let batch = Batch(appVersion: Self.version(Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String),
                          appBuild: Self.version(Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String),
                          osVersion: "\(os.majorVersion).\(os.minorVersion).\(os.patchVersion)", dropped: dropped, events: events)
        guard let data = try? JSONEncoder().encode(batch), data.count <= 32_768 else { return }
        sending = true
        let current = generation
        cancelUpload = transport(data) { [weak self] status in
            self?.queue.async {
                guard let self, current == self.generation else { return }
                self.sending = false
                self.cancelUpload = nil
                if (200..<300).contains(status) {
                    let ids = Set(events.map(\.id))
                    self.journal.pending.removeAll { ids.contains($0.id) }
                    self.journal.dropped = max(0, self.journal.dropped - dropped)
                    self.journal.lastUpload = Date()
                    self.retryAttempt = 0
                    self.persist()
                    self.scheduleUpload()
                } else if [400, 401, 403, 404, 405, 413, 415, 422].contains(status) {
                    // Keep evidence but stop retrying an incompatible/revoked endpoint.
                    // A subsequent foreground configure can try again after an upgrade.
                    self.blocked = true
                } else {
                    self.retryAttempt = min(self.retryAttempt + 1, 6)
                    self.scheduleUpload(after: min(300, self.retryDelay * pow(2, Double(self.retryAttempt - 1))))
                }
            }
        }
    }

    private func cancelWork() {
        generation &+= 1
        uploadScheduled?.cancel(); uploadScheduled = nil
        cancelUpload?(); cancelUpload = nil
        sending = false
        retryAttempt = 0
        blocked = false
    }

    private func persist() {
        guard let storageURL, let data = try? JSONEncoder().encode(journal) else { return }
        do {
            try FileManager.default.createDirectory(at: storageURL.deletingLastPathComponent(), withIntermediateDirectories: true)
            #if os(iOS)
            try data.write(to: storageURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            #else
            try data.write(to: storageURL, options: .atomic)
            #endif
            var url = storageURL
            var resource = URLResourceValues()
            resource.isExcludedFromBackup = true
            try url.setResourceValues(resource)
            persistenceFailureReported = false
        } catch {
            // Report once until disk writes recover; repeated failures must not
            // recursively produce an unbounded stream of diagnostic writes.
            if !persistenceFailureReported {
                persistenceFailureReported = true
                append(.storage, "diagnostics_persist_failed", fields: Self.errorFields(error))
            }
        }
    }

    static func errorFields(_ error: Error?) -> [String: String] {
        guard let error else { return [:] }
        let failure = error as NSError
        let domains = [NSURLErrorDomain: "url", NSCocoaErrorDomain: "cocoa", NSOSStatusErrorDomain: "osstatus",
                       "com.apple.CallKit.error.requesttransaction": "callkit",
                       "com.apple.CallKit.error.incomingcall": "callkit"]
        let domain = domains[failure.domain] ?? (failure.domain.hasSuffix("ModemDeckCallAudioError") ? "call_audio" :
            (failure.domain.hasSuffix("ModemDeckAPIError") ? "api" : "other"))
        return ["error_domain": domain, "error_code": String(failure.code)]
    }

    static func safeCallID(_ raw: String?) -> String? {
        guard let raw, raw.range(of: "^(call_[0-9a-f]{32}|call_[A-Za-z0-9_-]{24}|(test-|call-)?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$", options: .regularExpression) != nil else { return nil }
        return raw.hasPrefix("call_") ? raw : raw.lowercased()
    }

    static func version(_ raw: String?) -> String {
        guard let raw, raw.count <= 32, raw.range(of: "^[0-9]+(\\.[0-9]+)*$", options: .regularExpression) != nil else { return "0" }
        return raw
    }

    static func route(_ path: String) -> String {
        // Replace ALL non-schema components, even if a contact name resembles a URL.
        let known: Set<String> = ["api", "v1", "mobile", "session", "bootstrap", "pairing", "push", "call-tests", "test-call",
            "calls", "active", "media", "lease", "answer", "hangup", "reject", "hold", "resume", "mute", "dtmf", "recording",
            "recordings", "audio", "segments", "contacts", "batch", "messages", "threads", "state", "read", "read-all", "unread-summary",
            "events", "runtime", "settings", "system", "lines", "devices", "users", "account", "sessions", "password", "profile",
            "telegram", "units", "diagnostics", "logs", "health", "external-access", "status", "favorite", "contact"]
        let pathOnly = path.split(separator: "?", maxSplits: 1).first ?? ""
        return "/" + pathOnly.split(separator: "/").prefix(12).map { known.contains(String($0)) ? String($0) : ":id" }.joined(separator: "/")
    }

    static func sanitize(_ fields: [String: String]) -> [String: String] {
        let numbers: Set<String> = ["elapsed_ms", "stage_elapsed_ms", "dns_ms", "connect_ms", "tls_ms", "relay_candidates",
            "error_code", "http_status", "attempt", "delay_ms", "sample_rate", "channels", "input_gain_percent", "output_volume_percent", "microphone_dbfs", "captured_frames", "dropped_frames", "server_received_packets", "sent_packets", "sent_bytes", "received_packets", "received_bytes", "lost_packets", "playback_pending", "playback_underruns", "playback_resets", "capture_dropped_frames", "send_dropped_frames", "receive_stale_frames", "receive_invalid_frames", "playback_dropped_frames", "server_dropped_packets", "server_dropped_source_early_packets", "server_dropped_source_late_packets", "server_dropped_queue_overflow_packets", "server_dropped_reanchor_packets", "server_dropped_playout_packets", "server_dropped_rebuffer_packets", "server_clock_reanchors", "server_playout_underruns", "server_playout_silence_frames", "server_playout_missed_ticks"]
        let flags: Set<String> = ["expensive", "constrained", "ipv4", "ipv6", "dns", "enabled", "test_call", "input_available", "input_gain_settable", "audio_enabled", "reused_connection"]
        let enums: [String: Set<String>] = [
            "input_route": ["none", "microphone", "receiver", "speaker", "bluetooth", "headphones", "external"],
            "output_route": ["none", "microphone", "receiver", "speaker", "bluetooth", "headphones", "external"],
            "http_protocol": ["h2", "h3", "http/1.1", "other"],
            "stage": ["idle", "waiting_for_active_call", "connecting_wss", "reconnecting_wss", "connected"],
            "method": ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"],
            "error_domain": ["url", "cocoa", "osstatus", "callkit", "call_audio", "api", "other"],
            "reason": ["dns", "tls", "timeout", "authentication", "address_family", "unreachable", "other"],
            "status": ["online", "offline", "checking", "paired", "unpaired", "launching", "loading", "unavailable", "authorized", "denied", "notDetermined", "not_determined", "provisional", "ephemeral", "restricted", "unknown", "granted", "undetermined"]]
        return fields.reduce(into: [:]) { result, entry in
            let (key, value) = entry
            if numbers.contains(key), let number = Int64(value), abs(number == Int64.min ? Int64.max : number) <= 1_000_000_000 {
                result[key] = String(number)
            } else if flags.contains(key), ["true", "false"].contains(value) { result[key] = value
            } else if enums[key]?.contains(value) == true { result[key] = value
            } else if key == "route" { result[key] = route(value) }
        }
    }

    func recordRequest(_ request: URLRequest?, response: URLResponse?, error: Error?, fields: [String: String] = [:], name: String = "request_completed") {
        guard let request, let url = request.url, url.path.hasPrefix("/api/v1/"),
              url.path != "/api/v1/mobile/diagnostics" else { return }
        let bearer = request.value(forHTTPHeaderField: "Authorization")
        var values = fields.merging(Self.errorFields(error)) { _, new in new }
        values["route"] = Self.route(url.path)
        values["method"] = request.httpMethod ?? "GET"
        values["http_status"] = String((response as? HTTPURLResponse)?.statusCode ?? 0)
        let safe = Self.sanitize(values)
        queue.async {
            guard let bearer, bearer == self.authorization else { return }
            self.append(.api, name, fields: safe)
        }
    }

    static func urlSession(configuration: URLSessionConfiguration) -> URLSession {
        URLSession(configuration: configuration, delegate: ModemDeckDiagnosticTaskDelegate(), delegateQueue: nil)
    }
}

private final class ModemDeckDiagnosticTaskDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, didFinishCollecting metrics: URLSessionTaskMetrics) {
        var fields = ["elapsed_ms": String(Int(metrics.taskInterval.duration * 1_000))]
        for (key, pair) in ["dns_ms": (metrics.transactionMetrics.first?.domainLookupStartDate, metrics.transactionMetrics.first?.domainLookupEndDate),
                            "connect_ms": (metrics.transactionMetrics.first?.connectStartDate, metrics.transactionMetrics.first?.connectEndDate),
                            "tls_ms": (metrics.transactionMetrics.first?.secureConnectionStartDate, metrics.transactionMetrics.first?.secureConnectionEndDate)] {
            if let start = pair.0, let end = pair.1 { fields[key] = String(max(0, Int(end.timeIntervalSince(start) * 1_000))) }
        }
        if let transaction = metrics.transactionMetrics.last {
            let proto = transaction.networkProtocolName ?? "other"
            fields["http_protocol"] = ["h2", "h3", "http/1.1"].contains(proto) ? proto : "other"
            fields["reused_connection"] = String(transaction.isReusedConnection)
        }
        ModemDeckDiagnostics.shared.recordRequest(task.originalRequest, response: task.response, error: task.error,
                                                 fields: fields, name: "request_metrics")
    }
}

extension URLSession {
    // Completion-handler tasks do not call didCompleteWithError on the delegate.
    func diagnosticDataTask(with request: URLRequest,
                            completionHandler: @escaping (Data?, URLResponse?, Error?) -> Void) -> URLSessionDataTask {
        let started = ProcessInfo.processInfo.systemUptime
        return dataTask(with: request) { data, response, error in
            ModemDeckDiagnostics.shared.recordRequest(request, response: response, error: error,
                fields: ["elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - started) * 1_000))])
            completionHandler(data, response, error)
        }
    }

    func diagnosticData(for request: URLRequest) async throws -> (Data, URLResponse) {
        let started = ProcessInfo.processInfo.systemUptime
        var response: URLResponse?
        var failure: Error?
        defer {
            ModemDeckDiagnostics.shared.recordRequest(request, response: response, error: failure,
                fields: ["elapsed_ms": String(Int((ProcessInfo.processInfo.systemUptime - started) * 1_000))])
        }
        do {
            let result = try await data(for: request)
            response = result.1
            return result
        } catch { failure = error; throw error }
    }
}
