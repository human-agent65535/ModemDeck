import Foundation
private var pending: ((Result<Void, Error>) -> Void)?
final class Audio { func connect(completion: @escaping (Result<Void, Error>) -> Void) { pending = completion } }
enum EndReason { case failed }
final class Provider {
    var connected = 0, ended = 0
    func reportOutgoingCall(with: UUID, connectedAt: Date) { connected += 1 }
    func reportCall(with: UUID, endedAt: Date, reason: EndReason) { ended += 1 }
}
final class Coordinator {
    var callAudioSessions: [UUID: Audio] = [:]
    var answeredCallUUIDs: Set<UUID> = []
    let callProvider = Provider()
    var endCommands = 0
    func markCallActive(_ id: UUID) {}
    func cleanupCall(_ id: UUID, failureMessage: String? = nil) { callAudioSessions.removeValue(forKey: id) }
    func sendEndCallAction(verb: String, callUUID: UUID) { endCommands += 1 }
    func reportCallEnded(with uuid: UUID, endedAt: Date, reason: EndReason) {
        callProvider.reportCall(with: uuid, endedAt: endedAt, reason: reason)
    }
    func begin(_ uuid: UUID) {
        let audioSession = Audio()
        callAudioSessions[uuid] = audioSession
        // INSERT_PRODUCT_METHODS
    }
}
@main struct Tests {
    static func main() {
        let c = Coordinator(), id = UUID()
        c.begin(id); c.cleanupCall(id)
        pending?(.success(()))
        precondition(c.callProvider.connected == 0 && c.answeredCallUUIDs.isEmpty)
        c.begin(id); pending?(.success(()))
        precondition(c.callProvider.connected == 1 && c.answeredCallUUIDs.contains(id))
    }
}
