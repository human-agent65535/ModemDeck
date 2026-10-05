import Foundation
final class Owner {
    var ready = false
    var reconnectAttempts = 0
    var reconnectDeadline = Date(timeIntervalSince1970: 100)
    var reconnectTimeout: DispatchWorkItem? = DispatchWorkItem {}
    var recoveryHealth = ModemDeckMediaRecoveryHealth()
    var sentPackets = 0, renderedPackets = 0
    var lastSentAudioAt = -Double.infinity, lastRenderedAudioAt = -Double.infinity
    func recordConnectionEvent(_ event: String) {}
    func serverReady(_ now: Double) { markSocketReady(now: now) }
    func checkHealth(_ now: Double) { clearRecoveryAfterStableMedia(now: now) }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        let owner = Owner()
        let deadline = owner.reconnectDeadline
        let watchdog = owner.reconnectTimeout!
        for index in 0..<4 {
            owner.reconnectAttempts += 1
            owner.serverReady(Double(index))
            owner.sentPackets += 2; owner.renderedPackets += 2
            owner.checkHealth(Double(index) + 0.2)
            precondition(owner.reconnectAttempts == index + 1 && owner.reconnectDeadline == deadline)
            precondition(!watchdog.isCancelled, "A ready response must not cancel the eight-second watchdog")
            owner.ready = false // transient server error, followed by the next ready
        }
        owner.serverReady(4)
        owner.sentPackets += 50; owner.renderedPackets += 50
        owner.checkHealth(4.2)
        precondition(owner.reconnectAttempts == 4 && !watchdog.isCancelled)
                owner.checkHealth(5)
        precondition(owner.reconnectAttempts == 4, "Old packet counts without fresh duplex audio must not reset recovery")
        owner.lastSentAudioAt = 5; owner.lastRenderedAudioAt = 5
        owner.checkHealth(5)
        precondition(owner.reconnectAttempts == 0 && owner.reconnectDeadline == .distantPast && watchdog.isCancelled)
        let unidirectional = Owner(); unidirectional.serverReady(10); unidirectional.sentPackets = 100
        unidirectional.checkHealth(12)
        precondition(unidirectional.reconnectDeadline != .distantPast)
        var health = Owner.ModemDeckMediaRecoveryHealth()
        health.markReady(now: 20, sent: 100, rendered: 100)
        precondition(!health.isStable(now: 22, sent: 100, rendered: 100, lastSent: 22, lastRendered: 22), "Old-session counters cannot prove new media health")
    }
}
