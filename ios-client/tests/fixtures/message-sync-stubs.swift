import Foundation

// INSERT_PRODUCT_METHODS

@main struct Verification {
 static func main() {
  // Drive the product resync state the way ModemDeckSessionController does:
  // onOpen asks streamEstablished(), onCompletion (non-auth) calls streamDropped(),
  // a live "sms" event always refreshes, heartbeats never do.
  var resync = ModemDeckMessageStreamResync()
  var refreshes = 0
  func opened() { if resync.streamEstablished() { refreshes += 1 } }
  func event(_ name: String) { if name == "sms" { refreshes += 1 } }

  opened()
  event("heartbeat")
  precondition(refreshes == 0, "first connection must not add a refresh; foreground refresh owns it")

  resync.streamDropped()
  opened()
  precondition(refreshes == 1, "a reconnected stream must catch up on missed SMS")
  event("heartbeat"); event("heartbeat")
  precondition(refreshes == 1, "heartbeats on a healthy stream must not poll")
  event("sms")
  precondition(refreshes == 2, "live SMS still refresh")

  resync.streamDropped(); resync.streamDropped()
  opened(); opened()
  precondition(refreshes == 3, "several drops before one reconnect resync once")

  resync.streamDropped()
  resync.reset()
  opened()
  precondition(refreshes == 3, "a deliberate stop does not resync on the next start")

  // Text typed while the direct-message editor waited for the conversation.
  precondition(modemDeckHandOffDirectDraft(typed: "", conversationDraft: "kept") == "kept")
  precondition(modemDeckHandOffDirectDraft(typed: "  ", conversationDraft: "") == "")
  precondition(modemDeckHandOffDirectDraft(typed: "next one", conversationDraft: "") == "next one")
  precondition(modemDeckHandOffDirectDraft(typed: "next one", conversationDraft: "next one") == "next one")
  precondition(modemDeckHandOffDirectDraft(typed: "next one", conversationDraft: "older") == "older\nnext one")
  print("ok")
 }
}
