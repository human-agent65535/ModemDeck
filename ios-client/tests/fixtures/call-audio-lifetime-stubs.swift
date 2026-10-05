import Foundation
final class Track { var isEnabled = true }
typealias RTCAudioTrack = Track
final class Probe { var stopped = false }
final class Owner {
    let queue: DispatchQueue
    let probe: Probe
    var muted = false
    var stopped = false
    var localAudioTrack: Track?
    init(queue: DispatchQueue, probe: Probe) { self.queue = queue; self.probe = probe }
    func stopLocked(notifyRemoteEnd: Bool) { stopped = true; probe.stopped = true }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        let queue = DispatchQueue(label: "audio-test"), probe = Probe()
        var owner: Owner? = Owner(queue: queue, probe: probe)
        owner!.setMuted(true)
        queue.sync {}
        precondition(owner!.muted, "mute must be retained before an audio track exists")
        owner!.setMuted(false)
        queue.sync {}
        precondition(!owner!.muted)
        queue.suspend()
        weak var released = owner
        owner!.stop()
        owner = nil
        queue.resume()
        queue.sync {}
        precondition(probe.stopped, "queued cleanup must outlive the last owner")
        precondition(released == nil, "cleanup must not leak the session")
    }
}
