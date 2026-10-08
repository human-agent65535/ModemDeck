import Foundation
final class Socket {
    enum Close { case goingAway }
    var cancelled = false
    func cancel(with: Close, reason: Data?) { cancelled = true }
}
typealias URLSessionWebSocketTask = Socket
enum ModemDeckCallAudioError: Error { case timedOut }
final class Queue {
    var jobs: [() -> Void] = []
    func asyncAfter(deadline: DispatchTime, execute: DispatchWorkItem) { jobs.append { execute.perform() } }
    func asyncAfter(deadline: DispatchTime, execute: @escaping () -> Void) { jobs.append(execute) }
}
final class Owner {
    enum Reason { case send }
    var stopped = false, ready = true, sending = true, pingInFlight = true
    var socket: Socket? = Socket()
    var sendQueue = [Data([1]), Data([2])], captureSamples: [Float] = [1]
    var reconnectDeadline = Date.distantPast, connectDeadline = Date().addingTimeInterval(20)
    var reconnectAttempts = 0, reconnectTimeout: DispatchWorkItem?
    var connectCompletion: (() -> Void)?
    var failures = 0, remoteEnds = 0, dropped = 0, flushes = 0, releases = 0
    let queue = Queue()
    func recordConnectionEvent(_ event: String, error: Error) {}
    func recordDroppedFrames(_ count: Int, reason: Reason) { dropped += count }
    func stopAudio() { flushes += 1 }
    func publishMediaState(_ state: String) { precondition(state == "reconnecting") }
    func failMedia(_ error: Error) { failures += 1; stopped = true }
    func stopLocked(notifyRemoteEnd: Bool) { if notifyRemoteEnd { remoteEnds += 1 } }
    func releaseForReconnect(deadline: Date) { releases += 1 }
    func fail(_ task: Socket?) { transportFailed(task: task, error: ModemDeckCallAudioError.timedOut) }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        let owner = Owner(), old = owner.socket!
        owner.fail(old)
        precondition(owner.socket == nil && old.cancelled && !owner.ready && !owner.sending)
        precondition(owner.dropped == 2 && owner.sendQueue.isEmpty && owner.flushes == 1)
        precondition(owner.reconnectAttempts == 1 && owner.failures == 0 && owner.remoteEnds == 0)
        owner.fail(old)
        precondition(owner.reconnectAttempts == 1 && owner.dropped == 2, "The subsequent old close is inert")
        let exhausted = Owner(); exhausted.reconnectAttempts = 4
        exhausted.fail(exhausted.socket)
        precondition(exhausted.failures == 1 && exhausted.remoteEnds == 0 && exhausted.stopped,
                     "An exhausted local media budget is an error, not an authoritative remote hangup")
    }
}
