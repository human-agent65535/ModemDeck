import Foundation

struct ModemDeckMessagesResponse: Decodable {}

struct QueryCase: Encodable {
    let path: String
    let parameters: [String: String]
}

final class API {
    var paths: [String] = []

    func decode<T: Decodable>(_ type: T.Type, path: String) async throws -> T {
        paths.append(path)
        return try JSONDecoder().decode(type, from: Data("{}".utf8))
    }

    func searchPath(query: String, cursor: String) -> String {
        listPath("/api/v1/messages/threads", query: query, cursor: cursor)
    }

    // INSERT_PRODUCT_METHODS
}

@main struct Verification {
    static func main() async throws {
        let api = API()
        let lineID = "uat-line+a & b"
        let cursor = "page+2/=%&?#下一页"
        var cases: [QueryCase] = []
        for peer in ["+12025550101", "+1 202 555 0101", "10086", "UAT-SERVICE", "ACME+SMS", "A&B=%2B?#中文"] {
            for pageCursor in ["", cursor] {
                _ = try await api.messagePage(lineID: lineID, peer: peer, cursor: pageCursor)
                var parameters = ["line_id": lineID, "peer": peer, "limit": "100"]
                if !pageCursor.isEmpty { parameters["cursor"] = pageCursor }
                cases.append(QueryCase(path: api.paths.last!, parameters: parameters))
            }
        }
        let search = "+12025550101 & ACME+SMS / %2B 中文"
        cases.append(QueryCase(
            path: api.searchPath(query: " \(search) ", cursor: cursor),
            parameters: ["q": search, "cursor": cursor, "limit": "100"]
        ))
        let data = try JSONEncoder().encode(cases)
        print(String(decoding: data, as: UTF8.self))
    }
}
