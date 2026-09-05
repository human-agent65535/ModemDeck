import Foundation
struct Item: Decodable { let id: String }
typealias ModemDeckContact = Item
typealias ModemDeckCallRecord = Item
struct ModemDeckRecording { let segment: Item; let call: Item; let playable: Bool; let favorite: Bool }
struct ModemDeckPageMeta: Decodable { let limit: Int; let nextCursor: String; let hasMore: Bool }
struct ModemDeckContactsResponse: Decodable { let contacts: [Item]; let meta: ModemDeckPageMeta }
struct ModemDeckCallsResponse: Decodable { let calls: [Item]; let meta: ModemDeckPageMeta }
struct RecordingItem: Decodable { let segment: Item; let call: Item; let playable: Bool; let favorite: Bool }
struct ModemDeckRecordingsResponse: Decodable { let recordings: [RecordingItem]; let meta: ModemDeckPageMeta }
enum ModemDeckAPIError: Error { case invalidResponse }
final class Cache { var writes = 0; func write<T>(_ value: T, key: String) { writes += 1 } }
final class API {
 let offlineCache = Cache()
 var paths: [String] = []
 var repeatCursor = false
 var failSecondPage = false
 func decode<T: Decodable>(_ type: T.Type, path: String) async throws -> T {
  paths.append(path)
  let paged = path.contains("cursor=")
  if paged && failSecondPage { throw ModemDeckAPIError.invalidResponse }
  let count = paged ? 1 : 100
  let items:[[String:Any]] = (0..<count).map { n in
   path.contains("recordings") ? ["segment":["id":"segment-\(n + (paged ? 100 : 0))"],"call":["id":"call-\(n)"],"playable":true,"favorite":false] : ["id":"item-\(n + (paged ? 100 : 0))"]
  }
  let name=path.contains("contacts") ? "contacts" : (path.contains("recordings") ? "recordings" : "calls")
  let data=try JSONSerialization.data(withJSONObject:[name:items,"meta":["limit":100,"nextCursor":(paged && !repeatCursor) ? "" : "page2","hasMore":(!paged || repeatCursor)]])
  let decoder=JSONDecoder()
  return try decoder.decode(type,from:data)
 }

// INSERT_PRODUCT_METHODS
}

@main struct Verification {
 static func main() async throws {
  let api = API()
  let contacts = try await api.contacts(query: "test")
  let calls = try await api.calls()
  let recordings = try await api.recordings()
  precondition(contacts.count == 101 && calls.count == 101 && recordings.count == 101)
  precondition(contacts.last?.id == "item-100" && recordings.last?.segment.id == "segment-100")
  precondition(api.paths.count == 6)
  precondition(api.paths.filter { $0.contains("q=test") }.count == 2)
  precondition(api.paths.filter { $0.contains("kind=all") }.count == 2)
  let repeated = API()
  repeated.repeatCursor = true
  do { _ = try await repeated.contacts(); preconditionFailure("repeated cursor accepted") }
  catch ModemDeckAPIError.invalidResponse {}
  precondition(repeated.offlineCache.writes == 0)
  let unavailable = API()
  unavailable.failSecondPage = true
  do { _ = try await unavailable.contacts(); preconditionFailure("partial result accepted") }
  catch ModemDeckAPIError.invalidResponse {}
  precondition(unavailable.offlineCache.writes == 0)
  print("pagination, query preservation, and incomplete-page rejection passed")
 }
}
