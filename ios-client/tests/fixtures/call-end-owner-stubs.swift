import Foundation
struct ModemDeckCredential { let token: String; var callControlScope: String { token } }
enum ModemDeckNativeError: Error { case noActiveCall }
enum ModemDeckCallAudioError: Error { case callEnded }
struct PresentedCall { let activeAt: Date? }
final class Audio { var stops = 0; func stop() { stops += 1 } }
final class Observer {
 var states: [[String: Any]] = []
 var terminations: [(String, String)] = []
 func callStateDidChange(_ state: [String: Any]) { states.append(state) }
 func callTerminationDidChange(_ callID: String, status: String) { terminations.append((callID, status)) }
}
// INSERT_PRODUCT_METHODS
@main struct Tests {
 static func main() {
  let c = Coordinator(), uuid = UUID()
  let audio = Audio()
  c.callIDsByUUID[uuid] = "call"
  c.callAudioSessions[uuid] = audio
  c.presentedCalls[uuid] = PresentedCall(activeAt: Date())
  let intent = ModemDeckCallEndIntent(uuid: uuid, callID: "call", verb: "hangup", scope: "old")
  c.pendingEndIntents[uuid] = intent
  c.installPendingCallEnd(intent, credential: c.credential, reconcileFirst: false)
  c.cleanupCall(uuid)
  precondition(audio.stops == 1 && c.callIDsByUUID.isEmpty)
  precondition(c.callStateObserver!.states.last!["terminationState"] as? String == "pending")
  precondition(c.pendingEndIntents[uuid] != nil && c.pendingCallEnds[uuid] != nil, "closing native media must preserve remote end intent")
  c.readCompletion?(.success(.ended))
  precondition(c.pendingEndIntents.isEmpty && c.pendingCallEnds.isEmpty)
  precondition(c.callStateObserver!.terminations.last!.1 == "ended")

  let replaced = Coordinator(), second = UUID()
  let old = ModemDeckCallEndIntent(uuid: second, callID: "old", verb: "reject", scope: "old")
  replaced.pendingEndIntents[second] = old
  replaced.installPendingCallEnd(old, credential: replaced.credential, reconcileFirst: true)
  replaced.credential = ModemDeckCredential(token: "replacement")
  replaced.readCompletion?(.success(.ended))
  precondition(replaced.callStateObserver!.terminations.isEmpty, "late confirmation from the old pairing must not update the new presentation")
 }
}
