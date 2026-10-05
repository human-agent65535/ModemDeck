import Foundation
enum ModemDeckCallAudioError: Error { case timedOut }
final class Owner {
    let queue = DispatchQueue(label: "media-release-test")
    let retried = DispatchSemaphore(value: 0)
    var stopped = false
    var releases = 0, probes = 0, failures = 0
    var completion: ((Bool) -> Void)?
    func releaseMedia(completion: @escaping (Bool) -> Void) {
        releases += 1; self.completion = completion
        if releases == 2 { retried.signal() }
    }
    func probeForReconnect(deadline: Date) { probes += 1 }
    func failMedia(_ error: Error) { failures += 1 }
    func reconnect(_ deadline: Date) { releaseForReconnect(deadline: deadline) }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        let owner = Owner()
        owner.reconnect(Date().addingTimeInterval(2))
        precondition(owner.releases == 1 && owner.probes == 0)
        owner.completion!(false); owner.queue.sync {}
        precondition(owner.probes == 0 && owner.failures == 0, "Transient DELETE failure must not upgrade or hang up")
        precondition(owner.retried.wait(timeout: .now() + 1) == .success)
        owner.queue.sync {}
        owner.completion!(true); owner.queue.sync {}
        precondition(owner.probes == 1 && owner.failures == 0, "Successful owner cleanup gates the next state probe")
        let expired = Owner(); expired.reconnect(.distantPast)
        precondition(expired.releases == 0 && expired.failures == 1)
        let stopped = Owner(); stopped.reconnect(Date().addingTimeInterval(2)); stopped.stopped = true
        stopped.completion!(true); stopped.queue.sync {}
        precondition(stopped.probes == 0)
    }
}
