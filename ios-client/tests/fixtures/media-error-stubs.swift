import Foundation
final class Socket {}
typealias URLSessionWebSocketTask = Socket
enum ModemDeckCallAudioError: Error { case timedOut, mediaUnavailable }
final class Owner {
    var retries = 0, hangups = 0
    var current: Socket?
    func recordConnectionEvent(_ event: String) {}
    func transportFailed(task: Socket?, error: Error) {
        guard task === current else { return }
        current = nil; retries += 1
    }
    func failMedia(_ error: Error) { hangups += 1 }
    func error(_ code: String, _ task: Socket) { handleServerError(code: code, task: task) }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        for code in ["transport_timeout", "transport_closed", "backpressure"] {
            let owner = Owner(), task = Socket(); owner.current = task
            owner.error(code, task)
            precondition(owner.retries == 1 && owner.hangups == 0)
            // The subsequent old socket close must not repeat recovery or end the call.
            owner.transportFailed(task: task, error: ModemDeckCallAudioError.timedOut)
            precondition(owner.retries == 1 && owner.hangups == 0)
        }
        for code in ["invalid_audio", "unsupported_codec", "ownership_lost", "authentication", ""] {
            let owner = Owner(), task = Socket(); owner.current = task
            owner.error(code, task)
            precondition(owner.retries == 0 && owner.hangups == 1)
        }
    }
}
