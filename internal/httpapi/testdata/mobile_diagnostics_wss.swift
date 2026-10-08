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
                "playback_pending": "3", "playback_underruns": "2", "playback_resets": "1",
                "sample_rate": "16000", "channels": "1", "test_call": "false",
                "capture_dropped_frames": "1", "send_dropped_frames": "2", "receive_stale_frames": "1",
                "receive_invalid_frames": "0", "playback_dropped_frames": "3",
                "authorization": "synthetic-private-value", "audio_samples": "synthetic-private-value"]),
            ("wss_disconnected", ["stage": "reconnecting_wss", "stage_elapsed_ms": "25", "attempt": "2",
                "error_domain": "url", "error_code": "-1001", "test_call": "true"]),
            ("server_audio_statistics", ["stage": "connected", "server_received_packets": "1000000000",
                "server_dropped_packets": "21", "server_dropped_source_early_packets": "1",
                "server_dropped_source_late_packets": "2", "server_dropped_queue_overflow_packets": "3",
                "server_dropped_reanchor_packets": "4", "server_dropped_playout_packets": "5",
                "server_dropped_rebuffer_packets": "6", "server_clock_reanchors": "2",
                "server_playout_underruns": "3", "server_playout_silence_frames": "4",
                "server_playout_missed_ticks": "1"])
        ]
        let events = inputs.enumerated().map { index, input in
            ModemDeckDiagnostics.Event(
                id: String(format: "00000000-0000-4000-8000-%012d", index + 1),
                timeMS: 1_780_000_000_000 + Int64(index), category: .audio,
                name: input.0, network: "wifi",
                callID: ModemDeckDiagnostics.safeCallID(index == 0
                    ? "test-00000000-0000-4000-8000-000000000001"
                    : "call_0123456789abcdef0123456789abcdef"),
                fields: ModemDeckDiagnostics.sanitize(input.1))
        }
        precondition(events[0].fields["stage"] == "connecting_wss")
        precondition(events[1].fields["captured_frames"] == "250" && events[1].fields["dropped_frames"] == "7")
        precondition(events[1].fields["server_received_packets"] == "240")
        precondition(events[1].fields["playback_pending"] == "3" && events[1].fields["playback_underruns"] == "2" && events[1].fields["playback_resets"] == "1")
        precondition(events[1].fields["authorization"] == nil && events[1].fields["audio_samples"] == nil)
        precondition(events[2].fields["stage"] == "reconnecting_wss")
        precondition(events[1].callID == "call_0123456789abcdef0123456789abcdef")
        precondition(events.allSatisfy { $0.fields.count <= 32 })
        precondition(events[1].fields["send_dropped_frames"] == "2")
        precondition(events[3].fields["server_dropped_rebuffer_packets"] == "6")
        let batch = ModemDeckDiagnostics.Batch(appVersion: "0.1.0", appBuild: "29", osVersion: "18.0", dropped: 2, events: events)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        let output = URL(fileURLWithPath: CommandLine.arguments[2])
        try encoder.encode(batch).write(to: output)
        let source = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
        let hash = SHA256.hash(data: source).map { String(format: "%02x", $0) }.joined()
        try (hash + "\n").write(to: output.deletingPathExtension().appendingPathExtension("source.sha256"), atomically: true, encoding: .utf8)
    }
}
