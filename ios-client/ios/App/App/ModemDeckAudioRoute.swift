import AVFoundation
import Combine
import UIKit

/// Controls the audio session already activated by CallKit. It never activates
/// another session or creates a player/capture unit.
final class ModemDeckAudioRoute: NSObject, ObservableObject {
    static let shared = ModemDeckAudioRoute()
    @Published private(set) var active = false
    @Published private(set) var output = "none"
    @Published private(set) var externalInputs: [AVAudioSessionPortDescription] = []
    @Published private(set) var errorMessage = ""
    private var routeObserver: NSObjectProtocol?
    var hasReceiver: Bool { UIDevice.current.userInterfaceIdiom == .phone }

    override private init() {
        super.init()
        routeObserver = NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification,
            object: nil, queue: .main) { [weak self] _ in self?.refresh() }
    }

    func setActive(_ value: Bool) {
        active = value
        errorMessage = ""
        refresh()
    }

    func selectSpeaker(_ speaker: Bool) {
        select(speaker: speaker, input: nil)
    }

    func selectInput(_ input: AVAudioSessionPortDescription) {
        select(speaker: false, input: input)
    }

    private func select(speaker: Bool, input: AVAudioSessionPortDescription?) {
        guard active else { return }
        let session = AVAudioSession.sharedInstance()
        do {
            let builtIn = AVAudioSession.sharedInstance().availableInputs?.first { $0.portType == .builtInMic }
            try AVAudioSession.sharedInstance().setPreferredInput(speaker ? nil : (input ?? builtIn))
            try session.overrideOutputAudioPort(speaker ? .speaker : .none)
            errorMessage = ""
            refresh()
        } catch {
            errorMessage = error.localizedDescription
            ModemDeckDiagnostics.shared.record(.audio, "route_change_failed", error: error)
        }
    }

    private func refresh() {
        let session = AVAudioSession.sharedInstance()
        output = Self.port(session.currentRoute.outputs.first?.portType)
        externalInputs = (session.availableInputs ?? []).filter { $0.portType != .builtInMic }
        guard active else { return }
        ModemDeckDiagnostics.shared.record(.audio, "route_changed", fields: Self.fields(session))
    }

    static func fields(_ session: AVAudioSession = .sharedInstance()) -> [String: String] {
        ["input_route": port(session.currentRoute.inputs.first?.portType),
         "output_route": port(session.currentRoute.outputs.first?.portType),
         "input_available": String(session.isInputAvailable),
         "input_gain_settable": String(session.isInputGainSettable),
         "input_gain_percent": String(Int(session.inputGain * 100)),
         "output_volume_percent": String(Int(session.outputVolume * 100)),
         "sample_rate": String(Int(session.sampleRate)), "channels": String(session.inputNumberOfChannels)]
    }

    private static func port(_ value: AVAudioSession.Port?) -> String {
        switch value {
        case .builtInMic: return "microphone"
        case .builtInReceiver: return "receiver"
        case .builtInSpeaker: return "speaker"
        case .bluetoothHFP, .bluetoothA2DP, .bluetoothLE: return "bluetooth"
        case .headphones, .headsetMic: return "headphones"
        case .none: return "none"
        default: return "external"
        }
    }
}
