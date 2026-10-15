import Foundation
// INSERT_PRODUCT_METHODS
struct FakeOutgoingFlow {
    var gate = ModemDeckOutgoingPreparationGate()
    var ready = false, running = false, uplink = false
    var events = ["prepare", "callkit"]
    mutating func socketReady() { ready = true; events.append("wss"); tryCommit() }
    mutating func engineReady() { running = true; events.append("engine"); tryCommit() }
    mutating func serverUplink() { uplink = true; events.append("uplink"); tryCommit() }
    mutating func cancel() { gate.cancel(); events.append("cancel") }
    mutating func tryCommit() {
        if gate.beginCommit(socketReady: ready, audioRunning: running, uplinkReady: uplink) { events.append("dial") }
    }
}
@main struct Main {
    static func main() {
        var call = FakeOutgoingFlow()
        call.socketReady(); call.serverUplink()
        precondition(!call.events.contains("dial"), "server evidence cannot substitute for a running local engine")
        call.engineReady()
        precondition(call.events == ["prepare", "callkit", "wss", "uplink", "engine", "dial"])
        call.serverUplink(); call.engineReady()
        precondition(call.events.filter { $0 == "dial" }.count == 1, "duplicate readiness cannot dial twice")
        var cancelled = FakeOutgoingFlow()
        cancelled.socketReady(); cancelled.engineReady(); cancelled.cancel(); cancelled.serverUplink()
        precondition(!cancelled.events.contains("dial"), "late uplink after cancel must never commit")
        var failedEngine = FakeOutgoingFlow()
        failedEngine.socketReady(); failedEngine.serverUplink(); failedEngine.cancel()
        precondition(!failedEngine.events.contains("dial"), "engine failure must never dial")
        var reverse = FakeOutgoingFlow()
        reverse.engineReady(); reverse.serverUplink(); reverse.socketReady()
        precondition(reverse.events.last == "dial", "readiness must work regardless of callback order")
        print("outgoing-preparation-ok")
    }
}
