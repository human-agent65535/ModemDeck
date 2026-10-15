import Foundation
struct Call { let callID: String; var state = "active", direction = "outgoing"; var testCall = false }
typealias ModemDeckPresentedCall = Call
struct ModemDeckCallRecordingState { let callId: String; let enabled: Bool; let status: String }
enum ModemDeckAPIError: Error { case invalidResponse }
enum Failure: Error { case late }
@MainActor final class ModemDeckPushCoordinator {
 static let shared = ModemDeckPushCoordinator()
 var mute: [String: (Result<Void, Error>) -> Void] = [:]
 var dtmf: [String: (Result<Void, Error>) -> Void] = [:]
 func setCurrentCallMuted(_ value: Bool, callID: String, completion: @escaping (Result<Void, Error>) -> Void) { mute[callID] = completion }
 func playDTMF(callID: String, digits: String, completion: @escaping (Result<Void, Error>) -> Void) { dtmf[callID] = completion }
 func setPreferredCallRecording(callID: String, enabled: Bool) {}
}
@MainActor final class RecordingAPI {
 var pending: [String: CheckedContinuation<ModemDeckCallRecordingState, Error>] = [:]
 func setCallRecording(callID: String, enabled: Bool) async throws -> ModemDeckCallRecordingState {
  try await withCheckedThrowingContinuation { pending[callID] = $0 }
 }
}
// INSERT_PRODUCT_METHODS
@main struct Tests {
 @MainActor static func settle(_ condition: () -> Bool) async {
  for _ in 0..<2000 { if condition() { return }; await Task.yield() }; precondition(condition())
 }
 @MainActor static func main() async {
  let c = Controller(), native = ModemDeckPushCoordinator.shared
  let oldMute = Task { await c.setMuted(true) }
  await settle { native.mute["old"] != nil }
  c.call = Call(callID: "new"); c.prepareForNewCall(c.call!)
  let newMute = Task { await c.setMuted(false) }
  await settle { native.mute["new"] != nil }
  native.mute["old"]?(.failure(Failure.late)); await oldMute.value
  precondition(c.muteBusy && c.errorMessage.isEmpty, "old mute completion must not unlock or fail the new call")
  native.mute["new"]?(.success(())); await newMute.value
  precondition(!c.muteBusy)

  c.call = Call(callID: "old"); c.prepareForNewCall(c.call!)
  precondition(c.enqueueDTMF("1"))
  c.call = Call(callID: "new"); c.prepareForNewCall(c.call!)
  precondition(c.enqueueDTMF("2"))
  native.dtmf["old"]?(.failure(Failure.late))
  for _ in 0..<100 { await Task.yield() }
  precondition(c.dtmfBusy && c.dtmfQueue.first?.callID == "new" && c.errorMessage.isEmpty)
  native.dtmf["new"]?(.success(()))
  await settle { !c.dtmfBusy }
  precondition(c.dtmfQueue.isEmpty)

  for failed in [false, true] {
   c.call = Call(callID: "old"); c.prepareForNewCall(c.call!)
   let old = Task { await c.toggleRecording() }
   await settle { c.api.pending["old"] != nil }
   c.call = Call(callID: "new"); c.prepareForNewCall(c.call!)
   let next = Task { await c.toggleRecording() }
   await settle { c.api.pending["new"] != nil }
   if failed { c.api.pending.removeValue(forKey: "old")!.resume(throwing: Failure.late) }
   else { c.api.pending.removeValue(forKey: "old")!.resume(returning: ModemDeckCallRecordingState(callId: "old", enabled: true, status: "recording")) }
   await old.value
   precondition(c.recordingBusy && c.errorMessage.isEmpty && c.recordingReads == 0, "old recording result must not mutate or refresh the new call")
   c.api.pending.removeValue(forKey: "new")!.resume(returning: ModemDeckCallRecordingState(callId: "new", enabled: false, status: "off"))
   await next.value
   precondition(!c.recordingBusy && !c.recordingEnabled)
  }
 }
}
