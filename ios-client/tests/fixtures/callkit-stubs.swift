import Foundation

class CXAction { let uuid = UUID() }
class CXCallAction: CXAction { let callUUID: UUID; init(_ id: UUID) { callUUID = id } }
final class CXEndCallAction: CXCallAction {}
final class CXAnswerCallAction: CXCallAction {}
final class CXStartCallAction: CXCallAction {}
final class CXPlayDTMFCallAction: CXCallAction {}
enum CXCallEndedReason { case failed }
final class CXProvider {
    var ended: [UUID] = []
    func reportCall(with uuid: UUID, endedAt: Date, reason: CXCallEndedReason) { ended.append(uuid) }
}
struct PendingCall { let completion: (Result<Void, Error>) -> Void }
final class Coordinator {
    var callIDsByUUID: [UUID: String] = [:]
    var testCallUUIDs: Set<UUID> = []
    var audioTestCallUUIDs: Set<UUID> = []
    var outgoingCallUUIDs: Set<UUID> = []
    var answeredCallUUIDs: Set<UUID> = []
    var answerRequestedCallUUIDs: Set<UUID> = []
    var providerCallActions: [UUID: CXCallAction] = [:]
    var pendingOutgoingCalls: [UUID: PendingCall] = [:]
    let callProvider = CXProvider()
    var commands: [String] = []
    var fulfilled: [UUID] = []
    var cleaned: [UUID] = []
    var events: [String] = []
    func configureCallHistory(enabled: Bool) {}
    func trackProviderCallAction(_ action: CXCallAction) { providerCallActions[action.uuid] = action }
    func finishProviderCallAction(_ action: CXCallAction, result: Result<Void, Error>) {
        if case .success = result { fulfilled.append(action.uuid) }
        providerCallActions.removeValue(forKey: action.uuid)
        events.append("fulfilled")
    }
    func finishRequestedCallAction(_ action: CXAction, result: Result<Void, Error>) {}
    func sendEndCallAction(verb: String, callUUID: UUID) {
        precondition(callIDsByUUID[callUUID] != nil, "must snapshot identity before cleanup")
        commands.append(verb)
        events.append("dispatched")
        // Deliberately never completes: no HTTP response is needed for local teardown.
    }
    func cleanupCall(_ uuid: UUID) {
        callIDsByUUID.removeValue(forKey: uuid)
        cleaned.append(uuid)
        events.append("cleaned")
    }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        for mode in ["incoming", "outgoing", "answered", "answering", "test", "audio-test"] {
            let c = Coordinator(), id = UUID()
            c.callIDsByUUID[id] = "call"
            if mode == "outgoing" { c.outgoingCallUUIDs.insert(id) }
            if mode == "answered" { c.answeredCallUUIDs.insert(id) }
            if mode == "answering" { c.answerRequestedCallUUIDs.insert(id) }
            if mode == "test" { c.testCallUUIDs.insert(id) }
            if mode == "audio-test" {
                c.testCallUUIDs.insert(id); c.audioTestCallUUIDs.insert(id)
            }
            let end = CXEndCallAction(id)
            c.provider(c.callProvider, perform: end)
            precondition(c.fulfilled == [end.uuid] && c.cleaned == [id])
            precondition(c.callIDsByUUID.isEmpty)
            if mode == "test" { precondition(c.commands.isEmpty) }
            else {
                precondition(c.commands == [mode == "incoming" ? "reject" : "hangup"])
                precondition(c.events == ["dispatched", "fulfilled", "cleaned"])
            }
            c.provider(c.callProvider, perform: CXEndCallAction(id))
            precondition(c.commands.count == (mode == "test" ? 0 : 1), "duplicate end must not resend")
        }
        let c = Coordinator(), id = UUID()
        c.callIDsByUUID[id] = "call"
        c.provider(c.callProvider, timedOutPerforming: CXPlayDTMFCallAction(id))
        precondition(c.cleaned.isEmpty && c.commands.isEmpty && c.callProvider.ended.isEmpty)
        c.answerRequestedCallUUIDs.insert(id)
        c.provider(c.callProvider, timedOutPerforming: CXAnswerCallAction(id))
        precondition(c.commands == ["hangup"] && c.cleaned == [id] && c.callProvider.ended == [id])
        for action in [CXAnswerCallAction(UUID()), CXEndCallAction(UUID())] {
            let synthetic = Coordinator()
            synthetic.callIDsByUUID[action.callUUID] = "test-call"
            synthetic.testCallUUIDs.insert(action.callUUID)
            synthetic.provider(synthetic.callProvider, timedOutPerforming: action)
            precondition(synthetic.commands.isEmpty, "synthetic call timeouts stay local")
            precondition(synthetic.cleaned == [action.callUUID] && synthetic.callProvider.ended == [action.callUUID])
            let audio = Coordinator()
            audio.callIDsByUUID[action.callUUID] = "test-audio"
            audio.testCallUUIDs.insert(action.callUUID); audio.audioTestCallUUIDs.insert(action.callUUID)
            audio.answeredCallUUIDs.insert(action.callUUID)
            audio.provider(audio.callProvider, timedOutPerforming: action)
            precondition(audio.commands == ["hangup"], "server audio tests must close their remote session")
        }
        print("CallKit lifecycle regressions passed")
    }
}
