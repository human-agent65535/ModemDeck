import Foundation
final class AVAudioSession {
    enum Port { case builtInMic, builtInReceiver, builtInSpeaker, bluetoothHFP, bluetoothA2DP, bluetoothLE, headphones, headsetMic, other }
    struct Description { var portType: Port }
    struct Route { var inputs = [Description(portType: .builtInMic)], outputs = [Description(portType: .builtInReceiver)] }
    static func sharedInstance() -> AVAudioSession { AVAudioSession() }
    var currentRoute = Route(), isInputAvailable = true, isInputGainSettable = false
    var inputGain: Float = 1, outputVolume: Float = 0.5, sampleRate = 48_000.0, inputNumberOfChannels = 1
}
struct Credential { let callControlScope = "fixture" }
final class Owner {
    var droppedFrames = 0, serverReceivedPackets = 0
    private var dropCounts = ModemDeckAudioDropCounts()
    var serverAudioDropFields: [String: String] = [:]
    var recorded: [String: String] { ModemDeckDiagnostics.shared.lastRecordedFields }
    var microphoneLevel = -20.0, capturedFrames = 50, sentPackets = 50, receivedPackets = 50
    var playbackPending = 2, playbackUnderruns = 0, playbackResets = 0, activated = true, muted = false
    var connectionStartedAt = ProcessInfo.processInfo.systemUptime, stageStartedAt = ProcessInfo.processInfo.systemUptime
    var connectionStage = "connected", testCall = false, callID = "call_0123456789abcdef0123456789abcdef"
    let credential = Credential()
    func exercise() {
        for (index, reason) in [ModemDeckAudioDropReason.capture, .send, .receiveStale, .receiveInvalid, .playback].enumerated() {
            recordDroppedFrames(index + 1, reason: reason)
        }
        recordDroppedFrames(0, reason: .send); recordDroppedFrames(-1, reason: .send)
        precondition(droppedFrames == 15)
        precondition(dropCounts.fields == ["capture_dropped_frames": "1", "send_dropped_frames": "2",
            "receive_stale_frames": "3", "receive_invalid_frames": "4", "playback_dropped_frames": "5"])
        let names = ["dropped_packets", "dropped_source_early_packets", "dropped_source_late_packets",
                     "dropped_queue_overflow_packets", "dropped_reanchor_packets", "dropped_playout_packets",
                     "dropped_rebuffer_packets", "clock_reanchors", "playout_underruns", "playout_silence_frames", "playout_missed_ticks"]
        var input: [String: Any] = ["received_packets": 825, "pcm": "private-audio", "device_name": "private-name"]
        for (index, name) in names.enumerated() { input[name] = index + 1 }
        updateServerAudioStatistics(input)
        precondition(serverReceivedPackets == 825 && serverAudioDropFields.count == names.count)
        for (index, name) in names.enumerated() { precondition(recorded["server_" + name] == String(index + 1)) }
        precondition(recorded.count == names.count + 5 && recorded["pcm"] == nil && recorded["device_name"] == nil)
        updateServerAudioStatistics(["received_packets": -1, "dropped_packets": -3, "clock_reanchors": "arbitrary"])
        precondition(serverReceivedPackets == 825 && recorded["server_dropped_packets"] == "1")
        precondition(recorded.count <= 32)
        recordConnectionEvent("audio_statistics", fields: localAudioStatisticsFields())
        precondition(recorded.count == 28 && recorded.count <= 32)
        precondition(recorded["input_route"] == "microphone" && recorded["channels"] == "1")
        precondition(recorded["capture_dropped_frames"] == "1" && recorded["server_dropped_packets"] == nil,
                     "Local and server statistics must each remain within the API field-count contract")
    }
    // INSERT_PRODUCT_METHODS
}
@main struct Tests { static func main() { Owner().exercise() } }
