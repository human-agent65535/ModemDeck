import Foundation
import CryptoKit

// Generate the HTTP contract fixture with the production Swift sanitizer and
// Codable models, rather than a duplicate client implementation. From repo root:
// DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer xcrun swiftc \
//   -swift-version 5 -parse-as-library \
//   ios-client/ios/App/App/ModemDeckDiagnostics.swift \
//   internal/httpapi/testdata/mobile_diagnostics_wss.swift \
//   -o /private/tmp/modemdeck-diagnostics-fixture
// /private/tmp/modemdeck-diagnostics-fixture \
//   ios-client/ios/App/App/ModemDeckDiagnostics.swift \
//   internal/httpapi/testdata/mobile_diagnostics_wss.json
@main enum GenerateWSSDiagnosticsFixture {
    static func main() throws {
        precondition(CommandLine.arguments.count == 3)
        let inputs: [(String, [String: String])] = [
            ("stage_changed", ["stage": "connecting_wss", "stage_elapsed_ms": "0", "test_call": "true"]),
            ("audio_statistics", ["stage": "connected", "captured_frames": "250", "dropped_frames": "7",
                "server_received_packets": "240", "sent_packets": "240", "received_packets": "239",
                "microphone_dbfs": "-37", "input_route": "microphone", "output_route": "speaker",
                "sample_rate": "16000", "channels": "1", "test_call": "true",
                "authorization": "synthetic-private-value", "audio_samples": "synthetic-private-value"]),
            ("wss_disconnected", ["stage": "reconnecting_wss", "stage_elapsed_ms": "25", "attempt": "2",
                "error_domain": "url", "error_code": "-1001", "test_call": "true"]),
            ("server_audio_statistics", ["stage": "connected", "server_received_packets": "1000000000"])
        ]
        let events = inputs.enumerated().map { index, input in
            ModemDeckDiagnostics.Event(
                id: String(format: "00000000-0000-4000-8000-%012d", index + 1),
                timeMS: 1_780_000_000_000 + Int64(index), category: .audio,
                name: input.0, network: "wifi",
                callID: ModemDeckDiagnostics.safeCallID("test-00000000-0000-4000-8000-000000000001"),
                fields: ModemDeckDiagnostics.sanitize(input.1))
        }
        precondition(events[0].fields["stage"] == "connecting_wss")
        precondition(events[1].fields["captured_frames"] == "250" && events[1].fields["dropped_frames"] == "7")
        precondition(events[1].fields["server_received_packets"] == "240")
        precondition(events[1].fields["authorization"] == nil && events[1].fields["audio_samples"] == nil)
        precondition(events[2].fields["stage"] == "reconnecting_wss")
        let batch = ModemDeckDiagnostics.Batch(appVersion: "0.1.0", appBuild: "26", osVersion: "18.0", dropped: 2, events: events)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        let output = URL(fileURLWithPath: CommandLine.arguments[2])
        try encoder.encode(batch).write(to: output)
        let source = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
        let hash = SHA256.hash(data: source).map { String(format: "%02x", $0) }.joined()
        try (hash + "\n").write(to: output.deletingPathExtension().appendingPathExtension("source.sha256"), atomically: true, encoding: .utf8)
    }
}
