import Foundation
struct ModemDeckCredential { let serverURL: String; let token: String }
struct ModemDeckMobileSession: Decodable { let authenticated: Bool }
struct ModemDeckServerError: Decodable { let code: String; let message: String? }
enum ModemDeckAPIError: Error { case notPaired, invalidResponse; case server(status: Int, code: String, message: String) }
final class CredentialStore {
    var current = ModemDeckCredential(serverURL: "https://example.invalid", token: "old")
    func load() throws -> ModemDeckCredential? { current }
}
extension Notification.Name {
    static let modemDeckAuthenticationFailed = Notification.Name("auth")
    static let modemDeckRequestConnectivity = Notification.Name("connectivity")
}
func authorizedRequest(credential: ModemDeckCredential, path: String, method: String, headers: [String: String], body: Data?, timeout: Double) throws -> URLRequest {
    var request = URLRequest(url: URL(string: credential.serverURL + path)!)
    request.setValue("Bearer " + credential.token, forHTTPHeaderField: "Authorization")
    return request
}
final class Session {
    var status = 200
    var requests: [URLRequest] = []
    var onRequest: (() -> Void)?
    func diagnosticData(for request: URLRequest) async throws -> (Data, URLResponse) {
        requests.append(request)
        onRequest?()
        let payload = status == 200 ? #"{"authenticated":true}"# : #"{"code":"authentication_required","message":"Denied"}"#
        return (Data(payload.utf8), HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!)
    }
}
final class API {
    let credentialStore = CredentialStore()
    let decoder = JSONDecoder()
    let session = Session()
// INSERT_PRODUCT_METHODS
}
final class Counts { var auth = 0; var connectivity = 0 }
@main struct Tests {
    static func main() async throws {
        let counts = Counts()
        let auth = NotificationCenter.default.addObserver(forName: .modemDeckAuthenticationFailed, object: nil, queue: nil) { _ in counts.auth += 1 }
        let connectivity = NotificationCenter.default.addObserver(forName: .modemDeckRequestConnectivity, object: nil, queue: nil) { _ in counts.connectivity += 1 }
        defer { NotificationCenter.default.removeObserver(auth); NotificationCenter.default.removeObserver(connectivity) }
        let current = API()
        _ = try await current.data(path: "/calls", credential: current.credentialStore.current)
        precondition(counts.connectivity == 1 && counts.auth == 0)
        let replaced = API()
        let original = replaced.credentialStore.current
        replaced.session.status = 401
        replaced.session.onRequest = { replaced.credentialStore.current = ModemDeckCredential(serverURL: "https://example.invalid", token: "new") }
        do { _ = try await replaced.data(path: "/calls", credential: original); preconditionFailure("late old 401 accepted") }
        catch is CancellationError {}
        for _ in 0..<20 { await Task.yield() }
        precondition(counts.auth == 0 && counts.connectivity == 1, "old response must not revoke/connect a replacement pairing")
        do { _ = try await replaced.data(path: "/calls", credential: original); preconditionFailure("stale request started") }
        catch is CancellationError {}
        precondition(replaced.session.requests.count == 1, "already stale credentials must not start another request")
        let verifier = API()
        let candidate = ModemDeckCredential(serverURL: "https://example.invalid", token: "candidate")
        let verified = try await verifier.verify(candidate)
        precondition(verified.authenticated && verifier.session.requests[0].value(forHTTPHeaderField: "Authorization") == "Bearer candidate")
        verifier.session.status = 401
        do { _ = try await verifier.verify(candidate); preconditionFailure("invalid pairing verified") }
        catch ModemDeckAPIError.server {}
        for _ in 0..<20 { await Task.yield() }
        precondition(counts.auth == 0 && counts.connectivity == 1, "pair verification must not alter the current account")
        let revoked = API()
        revoked.session.status = 401
        do { _ = try await revoked.data(path: "/calls", credential: revoked.credentialStore.current); preconditionFailure("current 401 accepted") }
        catch ModemDeckAPIError.server {}
        for _ in 0..<2000 { if counts.auth == 1 { break }; await Task.yield() }
        precondition(counts.auth == 1, "normal pinned pagination must still handle current-session revocation")
    }
}
