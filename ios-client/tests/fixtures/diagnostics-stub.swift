import Foundation

// Diagnostic side effects are exercised separately against the real collector.
// The extracted call/UI behavior fixtures only need an inert logging sink.
final class ModemDeckDiagnostics {
    enum Category { case app, network, api, pairing, push, callkit, audio, storage, permissions }
    static let shared = ModemDeckDiagnostics()
    func record(_ category: Category, _ name: String, callID: String? = nil,
                fields: [String: String] = [:], error: Error? = nil, scope: String? = nil) {}
    func recordRequest(_ request: URLRequest?, response: URLResponse?, error: Error?) {}
    static func urlSession(configuration: URLSessionConfiguration) -> URLSession { URLSession(configuration: configuration) }
}
