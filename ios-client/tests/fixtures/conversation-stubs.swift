import Foundation
import Combine
struct ModemDeckMessage { let id: Int64; let timestamp: String; let incoming: Bool }
struct ModemDeckMessageThread { let id: String; let lineId: String; let peer: String; let firstUnreadMessageId: Int64? }
struct Meta { let hasMore: Bool; let nextCursor: String }
typealias ModemDeckPageMeta = Meta
struct ModemDeckMessagesResponse { let messages: [ModemDeckMessage]; let meta: Meta }
struct Summary {}
enum Action { case read }
enum ModemDeckAPIError: Error { case invalidResponse }
enum ModemDeckPhoneIdentity { static func sameThreadPeer(_ left: String, _ right: String) -> Bool { left == right } }
enum ModemDeckDateText { static func date(_ value: String) -> Date? { ISO8601DateFormatter().date(from: value) } }
@MainActor final class ModemDeckAPIClient {
 var cache: [ModemDeckMessage] = []
 var response = ModemDeckMessagesResponse(messages: [], meta: Meta(hasMore: false, nextCursor: ""))
 func cachedMessages(lineID: String, peer: String) -> [ModemDeckMessage] { cache }
 var older: ModemDeckMessagesResponse?
 func messagePage(lineID: String, peer: String, cursor: String = "") async throws -> ModemDeckMessagesResponse { cursor.isEmpty ? response : (older ?? response) }
 func cacheMessages(_ messages: [ModemDeckMessage], lineID: String, peer: String) { cache = messages }
 func markThreadRead(lineID: String, peer: String, throughMessageID: Int64) async throws -> Summary { Summary() }
 var sendFails = false
 var refused = false
 var sentRequests: [String] = []
 func sendMessage(lineID: String, to: String, content: String, requestID: String) async throws -> ModemDeckMessage {
  sentRequests.append(requestID)
  if sendFails { throw ModemDeckAPIError.invalidResponse }
  return ModemDeckMessage(id: 100, timestamp: "2026-09-05T02:00:00Z", incoming: false)
 }
 func sendOutcomeUnknown(_ error: Error) -> Bool { !refused }
 // Stands in for the pairing's stored data: it outlives every store, as a file outlives the app.
 var storedOutbox = Data("[]".utf8)
 func cachedOutbox() -> [ModemDeckOutgoingMessage] { (try? JSONDecoder().decode([ModemDeckOutgoingMessage].self, from: storedOutbox)) ?? [] }
 func cacheOutbox(_ outbox: [ModemDeckOutgoingMessage]) { storedOutbox = try! JSONEncoder().encode(outbox) }
}
@MainActor final class ModemDeckMessagesStore {
 let pendingSends: ModemDeckMessageOutbox
 init(api: ModemDeckAPIClient) { pendingSends = ModemDeckMessageOutbox(api: api) }
 private var drafts: [String: ModemDeckMessageDraft] = [:]
 func draft(for threadID: String) -> ModemDeckMessageDraft {
  if let existing = drafts[threadID] { return existing }
  let draft = ModemDeckMessageDraft()
  drafts[threadID] = draft
  return draft
 }
 func apply(_ action: Action, to ids: Set<String>) {}
 func apply(_ summary: Summary) {}
 func load() async {}
}

// INSERT_PRODUCT_METHODS

@main struct Verification {
 @MainActor static func main() async {
  let api = ModemDeckAPIClient()
  func message(_ id: Int64) -> ModemDeckMessage { .init(id: id, timestamp: "2026-09-05T01:00:00Z", incoming: true) }
  api.cache = [message(1)]
  api.response = .init(messages: [message(3)], meta: .init(hasMore: false, nextCursor: ""))
  let thread = ModemDeckMessageThread(id: "line:peer", lineId: "line", peer: "peer", firstUnreadMessageId: nil)
  let messagesStore = ModemDeckMessagesStore(api: api)
  let store = ModemDeckConversationStore(api: api, thread: thread, messagesStore: messagesStore)
  await store.load()
  precondition(store.messages.map(\.id) == [3] && api.cache.map(\.id) == [3], "deleted cached messages returned")
  api.response = .init(messages: [message(4)], meta: .init(hasMore: true, nextCursor: "older"))
  api.older = .init(messages: [message(3)], meta: .init(hasMore: false, nextCursor: ""))
  await store.load()
  precondition(store.messages.map(\.id) == [3, 4], "refresh truncated the previously displayed window")
  api.response = .init(messages: [message(4)], meta: .init(hasMore: false, nextCursor: ""))
  await store.load()
  precondition(store.messages.map(\.id) == [4] && api.cache.map(\.id) == [4], "remote deletion not reconciled")
  let draft = ModemDeckMessageDraft()
  draft.text = "message A"
  let sentRevision = draft.revision
  draft.text = "message B"
  draft.clear(ifRevision: sentRevision)
  precondition(draft.text == "message B")
  draft.text = "message A"
  draft.clear(ifRevision: sentRevision)
  precondition(draft.text == "message A", "a retyped draft must survive an earlier send")
  draft.clear(ifRevision: draft.revision)
  precondition(draft.text.isEmpty)
  draft.text = "ok"
  let firstAttempt = draft.sendAttempt
  draft.text = "ok"
  precondition(draft.sendAttempt == firstAttempt, "rewriting the same text must retry the same SMS")
  draft.text = "ok!"
  draft.text = "ok"
  precondition(draft.sendAttempt != firstAttempt, "the same words typed again are a new SMS")
  // A send whose outcome is unknown stays in the outbox; retry repeats the same request and lands it.
  api.sendFails = true
  let failed = await store.send("hello", attempt: "attempt-1")
  precondition(!failed && store.outbox.count == 1, "a failed send must remain as a bubble")
  if case .failed = store.outbox[0].state {} else { preconditionFailure("the bubble must show the failure") }
  precondition(store.errorMessage.isEmpty, "a failed send is reported on its bubble, not a banner")
  // Leaving the conversation drops its store; the failed SMS must still be there on return.
  let reopened = ModemDeckConversationStore(api: api, thread: thread, messagesStore: messagesStore)
  precondition(reopened.outbox.map(\.id) == ["attempt-1"], "a failed SMS must survive leaving the conversation")
  // Killing the app drops every store; the stored SMS comes back with the request it was sent under.
  let relaunched = ModemDeckConversationStore(api: api, thread: thread, messagesStore: ModemDeckMessagesStore(api: api))
  precondition(relaunched.outbox.map(\.requestID) == ["attempt-1"], "an unconfirmed SMS must survive a relaunch")
  let elsewhere = ModemDeckMessageThread(id: "line:other", lineId: "line", peer: "other", firstUnreadMessageId: nil)
  precondition(
   ModemDeckConversationStore(api: api, thread: elsewhere, messagesStore: messagesStore).outbox.isEmpty,
   "an unconfirmed SMS belongs to its own conversation only"
  )
  api.sendFails = false
  let retried = await store.retry(store.outbox[0])
  precondition(retried && store.outbox.isEmpty && store.messages.last?.id == 100, "retry must land the message")
  precondition(api.sentRequests == ["attempt-1", "attempt-1"], "retry must repeat the same request")
  precondition(api.cachedOutbox().isEmpty, "a confirmed SMS must leave the stored outbox")
  // The app is killed while a send is in flight: the request may have reached the server.
  api.cacheOutbox([ModemDeckOutgoingMessage(
   id: "attempt-k", requestID: "attempt-k", lineID: "line", peer: "peer", content: "in flight", state: .sending
  )])
  let afterKill = ModemDeckConversationStore(api: api, thread: thread, messagesStore: ModemDeckMessagesStore(api: api))
  precondition(afterKill.outbox.map(\.state) == [.unconfirmed], "an interrupted send must come back as unconfirmed")
  api.sentRequests = []
  let settled = await afterKill.retry(afterKill.outbox[0])
  precondition(settled && api.sentRequests == ["attempt-k"], "an interrupted send must be retried under its own request")
  // A definite refusal is final for that request, so the retry is a new request for the same bubble.
  api.sendFails = true
  api.refused = true
  api.sentRequests = []
  _ = await store.send("refused", attempt: "attempt-r")
  precondition(store.outbox.map(\.id) == ["attempt-r"] && store.outbox[0].requestID != "attempt-r", "a refused request must not be reused")
  _ = await store.retry(store.outbox[0])
  precondition(api.sentRequests.count == 2 && api.sentRequests[0] != api.sentRequests[1], "a refused SMS must be retried as a new request")
  store.discard(store.outbox[0])
  precondition(store.outbox.isEmpty && api.cachedOutbox().isEmpty, "a discarded failure must disappear for good")
  // A retry can confirm an SMS that a refresh already listed; it must not appear twice.
  api.sendFails = false
  api.refused = false
  _ = await store.send("listed", attempt: "attempt-l")
  precondition(store.messages.filter { $0.id == 100 }.count == 1, "a confirmed SMS already in the list must not be duplicated")
  print("authoritative cache reconciliation and draft revisions passed")
 }
}
