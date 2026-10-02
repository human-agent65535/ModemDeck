import Foundation
class CXCallAction {
    let uuid = UUID()
    let callUUID: UUID
    var fulfilled = 0, failed = 0
    init(_ id: UUID) { callUUID = id }
    func fulfill() { fulfilled += 1 }
    func fail() { failed += 1 }
}
final class CXAnswerCallAction: CXCallAction {}
final class CXProvider {}
enum ModemDeckNativeError: Error { case noActiveCall, microphoneDenied }
final class ModemDeckCallAudioSession { static func prepareAudioSession() {} }
struct PresentedCall { var activeAt: Date? }
final class Tone { func start(audioSession: Int) {} }
final class Coordinator {
    var providerCallActions: [UUID: CXCallAction] = [:]
    var callIDsByUUID: [UUID: String] = [:]
    var presentedCalls: [UUID: PresentedCall] = [:]
    var testCallUUIDs: Set<UUID> = []
    var audioTestCallUUIDs: Set<UUID> = []
    var answeredCallUUIDs: Set<UUID> = []
    var activeAudioSession: Int?
    let testCallTone = Tone()
    var permissionRequests = 0, answers = 0, completions = 0
    var permission: ((Bool) -> Void)?
    func configureCallHistory(enabled: Bool) {}
    func requestMicrophoneAccess(completion: @escaping (Bool) -> Void) {
        permissionRequests += 1
        permission = completion
    }
    func performAnswer(_ action: CXAnswerCallAction, uuid: UUID) { answers += 1 }
    func markCallActive(_ uuid: UUID) {}
    func trackProviderCallAction(_ action: CXCallAction) { providerCallActions[action.uuid] = action }
    func finishRequestedCallAction(_ action: CXCallAction, result: Result<Void, Error>) { completions += 1 }
    func settle(_ action: CXCallAction, granted: Bool) {
        finishProviderCallAction(action, result: granted ? .success(()) : .failure(ModemDeckNativeError.microphoneDenied))
    }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    static func main() {
        for (audioTest, granted) in [(false, true), (false, false), (true, true), (true, false)] {
            let c = Coordinator(), id = UUID(), provider = CXProvider()
            c.callIDsByUUID[id] = "call"
            if audioTest { c.testCallUUIDs.insert(id); c.audioTestCallUUIDs.insert(id) }
            let first = CXAnswerCallAction(id), second = CXAnswerCallAction(id)
            c.provider(provider, perform: first)
            c.provider(provider, perform: second)
            precondition(c.permissionRequests == 1)
            c.permission?(granted)
            precondition(c.answers == (granted ? 1 : 0))
            if granted { c.settle(first, granted: true) }
            precondition(first.fulfilled == (granted ? 1 : 0) && second.fulfilled == first.fulfilled)
            precondition(first.failed == (granted ? 0 : 1) && second.failed == first.failed)
            precondition(c.providerCallActions.isEmpty && c.completions == 2)
            c.settle(first, granted: true)
            precondition(c.completions == 2, "late result must not settle twice")
        }
        let c = Coordinator(), id = UUID()
        c.callIDsByUUID[id] = "call"
        c.presentedCalls[id] = PresentedCall(activeAt: Date())
        let action = CXAnswerCallAction(id)
        c.provider(CXProvider(), perform: action)
        precondition(action.fulfilled == 1 && c.permissionRequests == 0 && c.answers == 0)
        print("CallKit repeated answer regressions passed")
    }
}
