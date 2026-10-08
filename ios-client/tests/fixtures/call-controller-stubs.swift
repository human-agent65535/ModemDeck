import Foundation
struct Call { let callID: String }
enum Failure: Error { case ended }
@MainActor final class ModemDeckPushCoordinator {
    static let shared = ModemDeckPushCoordinator()
    var ended: [String] = []
    func endCall(callID: String, completion: (Result<Void, Error>) -> Void) { ended.append(callID); completion(.success(())) }
}
@MainActor final class Controller {
    var call: Call? = Call(callID: "old-call")
    var busy = false, ending = false
    var errorMessage = ""
    var localEndRequestedCallID: String?
    var answerCompletion: ((Result<Void, Error>) -> Void)?
    func answer() async { await perform { self.answerCompletion = $0 } }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests {
    @MainActor static func main() async {
        let c = Controller()
        let answer = Task { await c.answer() }
        while c.answerCompletion == nil { await Task.yield() }
        await c.end()
        precondition(ModemDeckPushCoordinator.shared.ended == ["old-call"], "answer must not block hangup")
        c.call = Call(callID: "new-call"); c.busy = true
        c.answerCompletion?(.failure(Failure.ended))
        await answer.value
        precondition(c.errorMessage.isEmpty && c.busy, "old answer must not mutate the new call")
    }
}
