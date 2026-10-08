import Foundation
enum UIAccessibility { static var isVoiceOverRunning = false; static var isReduceMotionEnabled = false }
enum ModemDeckDateText {
 static func date(_ value: String) -> Date? { ISO8601DateFormatter().date(from: value) }
}
@MainActor final class ModemDeckPushCoordinator {
 static let shared = ModemDeckPushCoordinator()
 var completion: ((Result<Void, Error>) -> Void)?
 func endCall(callID: String, completion: @escaping (Result<Void, Error>) -> Void) { self.completion = completion }
}
// INSERT_PRODUCT_METHODS
@main struct Tests {
 @MainActor static func settle(_ condition: () -> Bool) async {
  for _ in 0..<2000 { if condition() { return }; await Task.yield() }
  precondition(condition(), "presentation did not settle")
 }
 @MainActor static func state(_ id: String, phase: String = "active", test: Bool = false, direction: String = "outgoing") -> [String: Any] {
  var result: [String: Any] = ["callID": id, "state": phase, "direction": direction, "testCall": test, "testAudio": test]
  if phase == "active" { result["activeAt"] = ISO8601DateFormatter().string(from: Date().addingTimeInterval(-12)) }
  return result
 }
 @MainActor static func main() async {
  let c = Controller()
  c.callStateDidChange(state("replaced"))
  await settle { c.call?.callID == "replaced" }
  c.busy = true; c.ending = true; c.muteBusy = true; c.recordingBusy = true; c.dtmfBusy = true
  c.dtmfQueue = ["old digit"]; c.errorMessage = "old error"
  c.callStateDidChange(state("replacement", phase: "ringing", direction: "incoming"))
  await settle { c.call?.callID == "replacement" }
  precondition(!c.busy && !c.ending && !c.muteBusy && !c.recordingBusy && !c.dtmfBusy,
               "direct replacement without idle must clear old call interaction state")
  precondition(c.dtmfQueue.isEmpty && c.errorMessage.isEmpty)
  c.callStateDidChange(state("old"))
  await settle { c.call?.callID == "old" }
  let end = Task { await c.end() }
  await settle { c.ending }
  c.callStateDidChange(["state": "idle", "endedCallID": "old", "terminationState": "pending"])
  await settle { c.endedCall?.status == "pending" }
  precondition(c.call == nil && c.visibleCall?.callID == "old", "media must be stopped while the independent result remains")
  precondition(c.endedCall!.message.english.contains("Confirming") && c.endedCallDismissWorkItem == nil)
  let elapsed = c.endedCall!.elapsedSeconds!
  precondition(elapsed >= 12)
  ModemDeckPushCoordinator.shared.completion?(.success(()))
  await end.value
  precondition(c.endedCall?.status == "pending", "CallKit completion is not server confirmation")
  c.applyCallTermination("old", status: "ended")
  precondition(c.endedCall!.elapsedSeconds == elapsed && c.endedCall!.message.english == "Call ended")
  let automatic = c.endedCallDismissWorkItem!
  c.applyCallTermination("old", status: "ended")
  precondition(c.endedCallDismissWorkItem === automatic, "duplicate confirmation must not restart the hold")
  automatic.perform()
  precondition(c.endedCallClosing && c.visibleCall != nil, "fade must retain the result so underlying content remains blocked")
  let fading = c.endedCallDismissWorkItem!
  c.callStateDidChange(state("incoming", direction: "incoming"))
  await settle { c.call?.callID == "incoming" }
  fading.perform()
  c.callStateDidChange(["state": "idle", "endedCallID": "old", "terminationState": "ended"])
  for _ in 0..<100 { await Task.yield() }
  c.applyCallTermination("old", status: "ended")
  precondition(c.visibleCall?.callID == "incoming" && c.endedCall == nil && !c.endedCallClosing)

  c.callStateDidChange(["state": "idle", "terminationState": "failed", "failureMessage": "Synthetic failure"])
  await settle { c.endedCall?.status == "failed" }
  precondition(c.endedCall!.requiresAcknowledgement && c.endedCallDismissWorkItem == nil)
  UIAccessibility.isReduceMotionEnabled = true
  c.closeEndedCall()
  precondition(c.endedCall == nil && !c.endedCallClosing, "Reduce Motion must close without a fade")
  UIAccessibility.isReduceMotionEnabled = false

  c.callStateDidChange(state("test", test: true))
  await settle { c.call?.callID == "test" }
  c.callStateDidChange(["state": "idle", "testResult": "connected"])
  await settle { c.endedCall != nil }
  precondition(c.endedCall!.requiresAcknowledgement && c.endedCallDismissWorkItem == nil)
  c.dismissEndedCall()

  c.callStateDidChange(state("cancel", phase: "connecting"))
  await settle { c.call?.callID == "cancel" }
  c.localEndRequestedCallID = "cancel"
  c.callStateDidChange(["state": "idle", "terminationState": "ended"])
  await settle { c.endedCall != nil }
  precondition(c.endedCall?.elapsedSeconds == nil && c.endedCall!.message.english == "Call cancelled")
  c.dismissEndedCall()
  for local in [false, true] {
   c.callStateDidChange(state("answered-connecting", phase: "connecting", direction: "incoming"))
   await settle { c.call?.callID == "answered-connecting" }
   c.localEndRequestedCallID = local ? "answered-connecting" : nil
   c.callStateDidChange(["state": "idle", "terminationState": "ended"])
   await settle { c.endedCall != nil }
   precondition(c.endedCall?.elapsedSeconds == nil)
   precondition(c.endedCall!.message.english == (local ? "Call cancelled" : "Call not connected"),
                "answered connecting incoming is not an unanswered declined/missed call")
   c.dismissEndedCall()
  }
  UIAccessibility.isVoiceOverRunning = true
  c.callStateDidChange(state("reader"))
  await settle { c.call?.callID == "reader" }
  c.callStateDidChange(["state": "idle"])
  await settle { c.endedCall != nil }
  precondition(c.endedCallDismissWorkItem == nil, "VoiceOver needs explicit completion instead of a one-second disappearance")
  c.dismissEndedCall()
 }
}
