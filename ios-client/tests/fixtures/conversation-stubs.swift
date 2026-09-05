import Foundation
import Combine
struct ModemDeckMessage { let id: Int64; let timestamp: String; let incoming: Bool }
struct ModemDeckMessageThread { let id: String; let lineId: String; let peer: String; let firstUnreadMessageId: Int64? }
struct Meta { let hasMore: Bool; let nextCursor: String }
struct ModemDeckMessagesResponse { let messages: [ModemDeckMessage]; let meta: Meta }
struct Summary {}
enum Action { case read }
enum ModemDeckAPIError: Error { case invalidResponse }
enum ModemDeckDateText { static func date(_ value: String) -> Date? { ISO8601DateFormatter().date(from: value) } }
@MainActor final class ModemDeckAPIClient {
 var cache: [ModemDeckMessage] = []
 var response = ModemDeckMessagesResponse(messages: [], meta: Meta(hasMore: false, nextCursor: ""))
 func cachedMessages(lineID: String, peer: String) -> [ModemDeckMessage] { cache }
 var older: ModemDeckMessagesResponse?
 func messagePage(lineID: String, peer: String, cursor: String = "") async throws -> ModemDeckMessagesResponse { cursor.isEmpty ? response : (older ?? response) }
 func cacheMessages(_ messages: [ModemDeckMessage], lineID: String, peer: String) { cache = messages }
 func markThreadRead(lineID: String, peer: String, throughMessageID: Int64) async throws -> Summary { Summary() }
 func sendMessage(lineID: String, to: String, content: String) async throws -> ModemDeckMessage { fatalError() }
}
@MainActor final class ModemDeckMessagesStore {
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
  let store = ModemDeckConversationStore(api: api, thread: thread, messagesStore: ModemDeckMessagesStore())
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
  print("authoritative cache reconciliation and draft revisions passed")
 }
}
