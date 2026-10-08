import Foundation

enum Phase { case paired, unpaired }
struct Credential {}
struct CredentialStore { func load() throws -> Credential? { Credential() } }
func authorizedRequest(credential: Credential, path: String, method: String, headers: [String: String], timeout: Double) throws -> URLRequest {
    precondition(path == "/api/v1/runtime/events", "history needs the global stream, not call_id snapshots")
    return URLRequest(url: URL(string: "https://example.invalid" + path)!)
}
extension Notification.Name { static let modemDeckRemoteNotification = Notification.Name("refresh") }
enum LoadFailure: Error { case failed }
@MainActor final class HistoryAPI {
    var loads = 0, fail = false, block = false
    var gates: [Int: CheckedContinuation<Void, Never>] = [:]
    func calls() async throws -> [Int] {
        loads += 1
        let snapshot = loads
        if block { await withCheckedContinuation { gates[snapshot] = $0 } }
        if fail { throw LoadFailure.failed }
        return [snapshot]
    }
    func recordings() async throws -> [Int] { [loads] }
    func cacheCalls(_ values: [Int]) {}
    func cacheRecordings(_ values: [Int]) {}
}
final class ModemDeckRuntimeCallStream {
    let onEvent: ((String, Data) -> Void)?
    let onCompletion: (Int, Error?) -> Void
    var cancelled = false
    init(request: URLRequest, onEvent: ((String, Data) -> Void)?, onCompletion: @escaping (Int, Error?) -> Void) {
        self.onEvent = onEvent; self.onCompletion = onCompletion
    }
    func start() {}
    func cancel() { cancelled = true }
    func heartbeat() { onEvent?("heartbeat", Data(#"{"at":"2026-01-01T00:00:00Z"}"#.utf8)) }
    func emit(epoch: String = "a", revision: Int, durable: Int) {
        onEvent?("state", Data("{\"epoch\":\"\(epoch)\",\"revision\":\(revision),\"data_revision\":\(durable),\"calls\":{\"calls\":[]},\"recordings\":[]}".utf8))
    }
}
// INSERT_PRODUCT_METHODS
@main struct Tests {
    @MainActor static func settle(_ predicate: () -> Bool) async {
        for _ in 0..<2000 { if predicate() { return }; await Task.yield() }
        precondition(predicate(), "async state did not settle")
    }
    @MainActor static func main() async {
        let c = Controller()
        c.startRuntimeEvents()
        let stream = c.runtimeEventStream!
        c.callsStore.block = true
        stream.emit(revision: 1, durable: 5)
        await settle { c.callsStore.loads == 1 && c.callsStore.continuation != nil }
        stream.emit(revision: 2, durable: 6) // call ended while REST is in flight
        stream.emit(revision: 3, durable: 7) // independent recording finalization
        stream.emit(revision: 3, durable: 7) // duplicate must not cause another fetch
        c.callsStore.release()
        await settle { c.runtimeCollectionsTask == nil }
        precondition(c.callsStore.calls == [2] && c.callsStore.lastLoadSucceeded)
        precondition(c.callsStore.loads == 2, "finalization invalidation must survive an in-flight end refresh")
        precondition(c.contactsStore.loads == 0 && c.messagesStore.loads == 0, "call invalidation must not expand unrelated collection behavior")
        stream.emit(revision: 4, durable: 7) // non-durable status updates
        stream.emit(revision: 2, durable: 8) // out-of-order revision
        stream.emit(revision: 5, durable: 6) // inconsistent durable rollback
        await Task.yield()
        precondition(c.callsStore.loads == 2)
        stream.emit(epoch: "b", revision: 1, durable: 0) // process restart, counters may be lower
        await settle { c.runtimeCollectionsTask == nil }
        precondition(c.callsStore.loads == 3)
        c.callsStore.fail = true
        stream.emit(epoch: "b", revision: 2, durable: 1)
        await settle { c.runtimeCollectionsTask == nil }
        precondition(c.callsStore.loads == 4 && c.runtimeReloadRequested, "a failed refresh must remain dirty")
        for _ in 0..<20 { await Task.yield() }
        precondition(c.callsStore.loads == 4, "failure must not introduce polling")
        c.callsStore.fail = false
        stream.heartbeat() // No new durable state: the actual server heartbeat must retry unresolved finalization
        await settle { c.runtimeCollectionsTask == nil }
        precondition(c.callsStore.loads == 5 && !c.runtimeReloadRequested)
        for _ in 0..<20 { stream.heartbeat(); await Task.yield() }
        precondition(c.callsStore.loads == 5, "healthy heartbeats must not poll history")
        stream.onCompletion(200, nil) // EOF uses SSE reconnect, never collection polling
        precondition(c.runtimeReconnectWorkItem != nil)
        c.runtimeForeground = false
        c.stopRuntimeEvents()
        precondition(c.runtimeReconnectWorkItem == nil)
        stream.emit(epoch: "b", revision: 2, durable: 1)
        stream.heartbeat()
        precondition(c.callsStore.loads == 5, "cancelled stream must not mutate background/revoked scope")
        c.runtimeForeground = true
        c.startRuntimeEvents()
        let resumed = c.runtimeEventStream!
        resumed.emit(epoch: "b", revision: 3, durable: 2)
        await settle { c.runtimeCollectionsTask == nil }
        precondition(c.callsStore.loads == 6, "foreground snapshot catches missed durable revisions")
        resumed.onCompletion(401, nil)
        await settle { c.revoked }
        precondition(c.runtimeEventStream == nil && c.runtimeReconnectWorkItem == nil)
        resumed.emit(epoch: "b", revision: 4, durable: 3)
        precondition(c.callsStore.loads == 6)

        // Actual CallsStore scope invalidation must protect a newly paired owner.
        let paired = Controller()
        paired.callsStore.block = true
        paired.startRuntimeEvents()
        let old = paired.runtimeEventStream!
        old.emit(revision: 1, durable: 1)
        await settle { paired.callsStore.api.gates[1] != nil }
        paired.stopRuntimeEvents(resetWatermark: true)
        paired.callsStore.clear()
        paired.startRuntimeEvents()
        paired.runtimeEventStream!.emit(epoch: "new", revision: 1, durable: 1)
        await settle { paired.callsStore.api.gates[2] != nil }
        paired.callsStore.api.gates.removeValue(forKey: 2)!.resume()
        await settle { paired.runtimeCollectionsTask == nil }
        precondition(paired.callsStore.calls == [2])
        paired.callsStore.api.gates.removeValue(forKey: 1)!.resume()
        for _ in 0..<100 { await Task.yield() }
        precondition(paired.callsStore.calls == [2], "old pairing REST result must not overwrite the new directory")
        old.emit(epoch: "old", revision: 20, durable: 20)
        precondition(paired.callsStore.loads == 2, "old pairing SSE must not invalidate the new scope")
        paired.stopRuntimeEvents(resetWatermark: true)
    }
}
