import Foundation
struct ModemDeckCallRecord { let id: String; let duration: Int }
struct ModemDeckRecording { let id: String; let call: ModemDeckCallRecord; let playable: Bool }
final class Store { var calls: [ModemDeckCallRecord] = []; var recordings: [ModemDeckRecording] = [] }
// INSERT_PRODUCT_METHODS
@main struct Tests {
    static func main() {
        let store = Store()
        let old = ModemDeckCallRecord(id: "call", duration: 0)
        let other = ModemDeckCallRecord(id: "other", duration: 99)
        let detail = CallDetail(initialCall: old, store: store)
        precondition(detail.call.duration == 0 && detail.recordings.isEmpty)
        store.calls = [ModemDeckCallRecord(id: "call", duration: 19), other]
        let pending = ModemDeckRecording(id: "segment", call: old, playable: false)
        store.recordings = [pending, ModemDeckRecording(id: "foreign", call: other, playable: true)]
        let recordingDetail = RecordingDetail(initialRecording: pending, store: store)
        precondition(detail.call.duration == 19 && detail.recordings.count == 1)
        precondition(!recordingDetail.recording.playable)
        store.recordings[0] = ModemDeckRecording(id: "segment", call: old, playable: true)
        precondition(recordingDetail.recording.playable, "an open recording detail must enable playback after finalization")
        store.recordings = []
        precondition(detail.recordings.isEmpty, "deleted recordings must not reappear from a captured navigation snapshot")
    }
}
