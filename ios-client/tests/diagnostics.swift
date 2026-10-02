import Foundation

private final class Uploads {
    private let lock = NSLock()
    private var payloads: [Data] = []
    private var callbacks: [(Int) -> Void] = []
    private var canceled = 0
    var count: Int { lock.lock(); defer { lock.unlock() }; return payloads.count }
    var cancellations: Int { lock.lock(); defer { lock.unlock() }; return canceled }
    func send(_ data: Data, completion: @escaping (Int) -> Void) -> (() -> Void) {
        lock.lock(); payloads.append(data); callbacks.append(completion); lock.unlock()
        return { self.lock.lock(); self.canceled += 1; self.lock.unlock() }
    }
    func finish(_ index: Int, _ status: Int) {
        lock.lock(); let callback = callbacks[index]; lock.unlock(); callback(status)
    }
    func batch(_ index: Int) -> ModemDeckDiagnostics.Batch {
        lock.lock(); let data = payloads[index]; lock.unlock()
        return try! JSONDecoder().decode(ModemDeckDiagnostics.Batch.self, from: data)
    }
}

private func eventually(_ predicate: () -> Bool) {
    let deadline = Date().addingTimeInterval(3)
    while !predicate(), Date() < deadline { Thread.sleep(forTimeInterval: 0.01) }
    precondition(predicate(), "Timed out waiting for diagnostic delivery")
}

@main struct DiagnosticsTests {
    static func main() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let suite = "modemdeck.diagnostics-test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let journal = directory.appendingPathComponent("diagnostics.json")
        let uploads = Uploads()
        let diagnostics = ModemDeckDiagnostics(defaults: defaults, storageURL: journal,
            monitorNetwork: false, uploadDelay: 0.03, retryDelay: 0.03)
        diagnostics.configure(scope: "pair-a", authorization: "Bearer synthetic-a", transport: uploads.send)

        // Default-off does not queue or send even errors. Enabling does not replay them.
        diagnostics.record(.audio, "before_consent", fields: ["error_code": "701"])
        precondition(!diagnostics.status().enabled && diagnostics.status().pending == 0 && uploads.count == 0)
        diagnostics.setUploadEnabled(true)
        for category in ModemDeckDiagnostics.Category.allCases { diagnostics.record(category, "after_consent") }
        eventually { uploads.count == 1 }
        let first = uploads.batch(0)
        precondition(Set(first.events.map(\.category)) == Set(ModemDeckDiagnostics.Category.allCases))
        precondition(first.events.allSatisfy { $0.name != "before_consent" })
        uploads.finish(0, 204)
        eventually { diagnostics.status().pending == 0 && diagnostics.status().lastUpload != nil }

        // One failure retries the SAME events; successful acknowledgement drains them.
        diagnostics.record(.push, "retry_event")
        eventually { uploads.count == 2 }
        let retriedID = uploads.batch(1).events.first!.id
        uploads.finish(1, 503)
        eventually { uploads.count == 3 }
        precondition(uploads.batch(2).events.first!.id == retriedID)
        uploads.finish(2, 204)
        eventually { diagnostics.status().pending == 0 }

        // Turning off cancels in-flight I/O, drops pending events and ignores late completion.
        diagnostics.record(.callkit, "cancel_me")
        eventually { uploads.count == 4 }
        diagnostics.setUploadEnabled(false)
        precondition(uploads.cancellations == 1 && diagnostics.status().pending == 0)
        uploads.finish(3, 204)
        precondition(diagnostics.status().lastUpload == nil)
        diagnostics.record(.api, "while_disabled")
        precondition(diagnostics.status().pending == 0)

        // Pending evidence survives process restart with the SAME pairing only.
        diagnostics.setUploadEnabled(true)
        diagnostics.record(.audio, "persist_me")
        diagnostics.configure(scope: "pair-a", authorization: "Bearer synthetic-a", transport: nil)
        let restoredUploads = Uploads()
        let restored = ModemDeckDiagnostics(defaults: defaults, storageURL: journal,
            monitorNetwork: false, uploadDelay: 0.03, retryDelay: 0.03)
        restored.configure(scope: "pair-a", authorization: "Bearer synthetic-a", transport: restoredUploads.send)
        eventually { restoredUploads.count == 1 }
        precondition(restoredUploads.batch(0).events.contains { $0.name == "persist_me" })

        // Changing pairing cancels old I/O and cannot misattribute delayed events/HTTP responses.
        restored.configure(scope: "pair-b", authorization: "Bearer synthetic-b", transport: restoredUploads.send)
        restored.record(.audio, "old_owner", scope: "pair-a")
        var oldRequest = URLRequest(url: URL(string: "https://example.invalid/api/v1/calls")!)
        oldRequest.setValue("Bearer synthetic-a", forHTTPHeaderField: "Authorization")
        restored.recordRequest(oldRequest, response: nil, error: URLError(.timedOut))
        precondition(restored.status().pending == 0 && restoredUploads.cancellations == 1)
        restoredUploads.finish(0, 204)
        precondition(restored.status().lastUpload == nil)
        oldRequest.setValue("Bearer synthetic-b", forHTTPHeaderField: "Authorization")
        restored.recordRequest(oldRequest, response: nil, error: URLError(.timedOut))
        eventually { restoredUploads.count == 2 }
        precondition(restoredUploads.batch(1).events.first?.fields["error_code"] == "-1001")
        restoredUploads.finish(1, 204)
        eventually { restored.status().pending == 0 }

        // The upload endpoint is excluded, including errors, to prevent a feedback loop.
        var recursive = oldRequest
        recursive.url = URL(string: "https://example.invalid/api/v1/mobile/diagnostics")
        restored.recordRequest(recursive, response: nil, error: URLError(.timedOut))
        precondition(restored.status().pending == 0)

        // Strip payloads, free-form errors, addresses, credentials, query values and IDs.
        let safe = ModemDeckDiagnostics.sanitize([
            "authorization": "Bearer secret", "sdp": "secret-sdp", "error_text": "192.0.2.1 secret",
            "error_domain": "secret", "error_code": "701", "turn_host": "private.example",
            "route": "/api/v1/messages/+12025550101?token=secret", "elapsed_ms": "8000"])
        precondition(safe == ["error_code": "701", "route": "/api/v1/messages/:id", "elapsed_ms": "8000"])
        let turn = ModemDeckDiagnostics.turnFields("turns:user:secret@turn.cloudflare.com:443?transport=tcp")
        precondition(turn == ["turn_transport": "tls", "turn_port": "443", "turn_host": "cloudflare"])
        precondition(ModemDeckDiagnostics.turnFailureReason("DNS failed at 192.0.2.1 secret") == "dns")
        precondition(ModemDeckDiagnostics.safeCallID("+12025550101") == nil)
        precondition(ModemDeckDiagnostics.safeCallID("call_aBcDeFgHiJkLmNoPqRsTuVwX") == "call_aBcDeFgHiJkLmNoPqRsTuVwX")
        precondition(ModemDeckDiagnostics.errorFields(NSError(domain: "secret", code: 10,
            userInfo: [NSLocalizedDescriptionKey: "secret"])) == ["error_domain": "other", "error_code": "10"])

        // Bound disk/memory use during extended outages, expose loss count, and clear on opt-out.
        restored.configure(scope: "pair-b")
        for _ in 0..<600 { restored.record(.network, "offline_event") }
        precondition(restored.status().pending == 512 && restored.status().dropped == 88)
        restored.flush()
        _ = restored.status()
        let bytes = try Data(contentsOf: journal)
        precondition(bytes.count < 1_048_576)
        restored.setUploadEnabled(false)
        let disabled = ModemDeckDiagnostics(defaults: defaults, storageURL: journal, monitorNetwork: false)
        precondition(!disabled.status().enabled && disabled.status().pending == 0)
        print("8 diagnostics behavior tests passed")
    }
}
